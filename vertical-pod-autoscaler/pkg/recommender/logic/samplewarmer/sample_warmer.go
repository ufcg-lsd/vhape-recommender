package samplewarmer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/klog/v2"
)

const samplesWarmUpPath = "/var/lib/vhape-recommender/samples-warm-up.csv"

var samplesWarmUpHeader = []string{
	"timestamp",
	"container_name",
	"deployment_namespace",
	"deployment_key",
	"resource_type",
	"value",
}

type WorkloadKey struct {
	Namespace string
	Name      string
}

type SampleWarmer interface {
	WarmUpEstimators(estimators *estimators.ResourceEstimators, vpa *model.Vpa)
	InitWarmer()
}

type sampleWarmer struct {
	// creation timestamp
	t0 time.Time

	// Usage samples indexed by workload, container name, and resource type,
	// preserving their original timestamps.
	samples map[WorkloadKey]map[string]map[model.ResourceName][]estimators.TimedSample
}

func CreateSampleWarmer(t0 time.Time) SampleWarmer {
	return &sampleWarmer{
		t0:      t0,
		samples: make(map[WorkloadKey]map[string]map[model.ResourceName][]estimators.TimedSample),
	}
}

// InitWarmer loads the samples used to warm up newly-created estimators.
// Invalid files are ignored as a whole so estimators are never initialized
// with a partially parsed data set.
func (wu *sampleWarmer) InitWarmer() {
	samples, err := readSamples(samplesWarmUpPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Warm-up samples file not found", "path", samplesWarmUpPath)
		} else {
			klog.ErrorS(err, "Failed to load warm-up samples", "path", samplesWarmUpPath)
		}
		return
	}

	wu.samples = samples
	klog.InfoS("Warm-up samples loaded", "path", samplesWarmUpPath, "workloads", len(samples))
}

func readSamples(path string) (map[WorkloadKey]map[string]map[model.ResourceName][]estimators.TimedSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = len(samplesWarmUpHeader)

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	if err := validateHeader(header); err != nil {
		return nil, err
	}

	samples := make(map[WorkloadKey]map[string]map[model.ResourceName][]estimators.TimedSample)
	for line := 2; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV line %d: %w", line, err)
		}

		timestamp, err := parseTimestamp(strings.TrimSpace(record[0]))
		if err != nil {
			return nil, fmt.Errorf("CSV line %d: invalid timestamp %q: %w", line, record[0], err)
		}

		containerName := strings.TrimSpace(record[1])
		namespace := strings.TrimSpace(record[2])
		deploymentKey := strings.TrimSpace(record[3])
		if containerName == "" || namespace == "" || deploymentKey == "" {
			return nil, fmt.Errorf("CSV line %d: container_name, deployment_namespace, and deployment_key must not be empty", line)
		}

		resourceName := model.ResourceName(strings.ToLower(strings.TrimSpace(record[4])))
		if resourceName != model.ResourceCPU && resourceName != model.ResourceMemory {
			return nil, fmt.Errorf("CSV line %d: unsupported resource_type %q", line, record[4])
		}

		value, err := strconv.ParseInt(strings.TrimSpace(record[5]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CSV line %d: invalid value %q: %w", line, record[5], err)
		}

		workload := WorkloadKey{Namespace: namespace, Name: deploymentKey}
		if samples[workload] == nil {
			samples[workload] = make(map[string]map[model.ResourceName][]estimators.TimedSample)
		}
		if samples[workload][containerName] == nil {
			samples[workload][containerName] = make(map[model.ResourceName][]estimators.TimedSample)
		}
		samples[workload][containerName][resourceName] = append(
			samples[workload][containerName][resourceName],
			estimators.TimedSample{Timestamp: timestamp, Value: model.ResourceAmount(value)},
		)
	}

	return samples, nil
}

func validateHeader(header []string) error {
	for i, expected := range samplesWarmUpHeader {
		actual := strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
		if actual != expected {
			return fmt.Errorf("invalid CSV header at column %d: got %q, want %q", i+1, actual, expected)
		}
	}
	return nil
}

func parseTimestamp(value string) (time.Time, error) {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected Unix timestamp in seconds: %w", err)
	}

	return time.Unix(seconds, 0).UTC(), nil
}

func (wu *sampleWarmer) WarmUpEstimators(resourceEstimators *estimators.ResourceEstimators, vpa *model.Vpa) {
	if resourceEstimators == nil || vpa == nil || vpa.TargetRef == nil {
		return
	}

	workloadSamples := wu.samples[WorkloadKey{Namespace: vpa.ID.Namespace, Name: vpa.TargetRef.Name}]
	maxTimestamp, found := latestTimestamp(workloadSamples)
	if !found {
		return
	}

	for containerName, samplesByResource := range workloadSamples {
		if cpuSamples := samplesByResource[model.ResourceCPU]; resourceEstimators.CPU != nil && len(cpuSamples) > 0 {
			resourceEstimators.CPU.WarmUpSamples(containerName, transposeSamples(cpuSamples, maxTimestamp, wu.t0))
		}
		if memorySamples := samplesByResource[model.ResourceMemory]; resourceEstimators.Memory != nil && len(memorySamples) > 0 {
			resourceEstimators.Memory.WarmUpSamples(containerName, transposeSamples(memorySamples, maxTimestamp, wu.t0))
		}
	}
}

func latestTimestamp(samples map[string]map[model.ResourceName][]estimators.TimedSample) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, samplesByResource := range samples {
		for _, resourceSamples := range samplesByResource {
			for _, sample := range resourceSamples {
				if !found || sample.Timestamp.After(latest) {
					latest = sample.Timestamp
					found = true
				}
			}
		}
	}
	return latest, found
}

func transposeSamples(samples []estimators.TimedSample, maxTimestamp, t0 time.Time) []estimators.TimedSample {
	transposed := make([]estimators.TimedSample, len(samples))
	for i, sample := range samples {
		transposed[i] = sample
		transposed[i].Timestamp = t0.Add(sample.Timestamp.Sub(maxTimestamp))
	}
	return transposed
}
