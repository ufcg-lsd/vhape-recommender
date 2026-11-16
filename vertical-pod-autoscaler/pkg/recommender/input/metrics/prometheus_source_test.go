package metrics

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/history"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const (
	cpuMetric    = "container_cpu_usage_seconds_total"
	memoryMetric = "container_memory_working_set_bytes"
)

var testQueryTime = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// fakePrometheusAPI answers instant queries with the result of the metric they read.
// Only Query is implemented.
type fakePrometheusAPI struct {
	prometheusv1.API
	results map[string]prommodel.Value
	err     error
	queries []string
	times   []time.Time
}

func (f *fakePrometheusAPI) Query(_ context.Context, query string, ts time.Time, _ ...prometheusv1.Option) (prommodel.Value, prometheusv1.Warnings, error) {
	f.queries = append(f.queries, query)
	f.times = append(f.times, ts)
	if f.err != nil {
		return nil, nil, f.err
	}
	for metric, result := range f.results {
		if strings.Contains(query, metric) {
			return result, nil, nil
		}
	}
	return prommodel.Vector{}, nil, nil
}

func newTestPrometheusClient(api *fakePrometheusAPI, cacheTTL time.Duration) *prometheusMetricsClient {
	c := newPrometheusClient(api, prometheusClientOptions{
		QueryTimeout:           time.Second,
		CPURateWindow:          time.Minute,
		CadvisorMetricsJobName: "kubernetes-nodes-cadvisor",
		CtrNamespaceLabel:      "namespace",
		CtrPodNameLabel:        "pod",
		CtrNameLabel:           "container",
		CacheTTL:               cacheTTL,
	})
	c.now = func() time.Time { return testQueryTime }
	return c
}

func newSample(namespace, pod, container string, value float64) *prommodel.Sample {
	return &prommodel.Sample{
		Metric: prommodel.Metric{
			"namespace": prommodel.LabelValue(namespace),
			"pod":       prommodel.LabelValue(pod),
			"container": prommodel.LabelValue(container),
		},
		Value:     prommodel.SampleValue(value),
		Timestamp: prommodel.TimeFromUnixNano(testQueryTime.UnixNano()),
	}
}

func newContainerID(namespace, pod, container string) model.ContainerID {
	return model.ContainerID{PodID: model.PodID{Namespace: namespace, PodName: pod}, ContainerName: container}
}

// usageByContainer converts the list the same way the metrics client does.
func usageByContainer(list *v1beta1.PodMetricsList) map[model.ContainerID]model.Resources {
	usage := make(map[model.ContainerID]model.Resources)
	for _, podMetrics := range list.Items {
		for _, snapshot := range createContainerMetricsSnapshots(podMetrics) {
			usage[snapshot.ID] = snapshot.Usage
		}
	}
	return usage
}

func TestPrometheusClientList(t *testing.T) {
	testCases := []struct {
		name          string
		cpuSamples    prommodel.Vector
		memorySamples prommodel.Vector
		expected      map[model.ContainerID]model.Resources
	}{
		{
			name: "builds the usage of each container",
			cpuSamples: prommodel.Vector{
				newSample("ns1", "pod-a", "main", 0.4567),
				newSample("ns1", "pod-a", "sidecar", 0.0001),
				newSample("ns2", "pod-b", "main", 2),
			},
			memorySamples: prommodel.Vector{
				newSample("ns1", "pod-a", "main", 512*1024*1024),
				newSample("ns1", "pod-a", "sidecar", 64*1024*1024),
				newSample("ns2", "pod-b", "main", 1024),
			},
			expected: map[model.ContainerID]model.Resources{
				// CPU is rounded up to millicores, as with metrics-server.
				newContainerID("ns1", "pod-a", "main"):    {model.ResourceCPU: 457, model.ResourceMemory: 512 * 1024 * 1024},
				newContainerID("ns1", "pod-a", "sidecar"): {model.ResourceCPU: 1, model.ResourceMemory: 64 * 1024 * 1024},
				newContainerID("ns2", "pod-b", "main"):    {model.ResourceCPU: 2000, model.ResourceMemory: 1024},
			},
		},
		{
			name:          "skips containers without the usage of every resource",
			cpuSamples:    prommodel.Vector{newSample("ns", "pod-a", "main", 0.2)},
			memorySamples: prommodel.Vector{newSample("ns", "pod-a", "main", 1024), newSample("ns", "pod-b", "main", 1024)},
			expected: map[model.ContainerID]model.Resources{
				newContainerID("ns", "pod-a", "main"): {model.ResourceCPU: 200, model.ResourceMemory: 1024},
			},
		},
		{
			name:          "skips samples without container labels",
			cpuSamples:    prommodel.Vector{newSample("ns", "pod-a", "", 0.2), newSample("ns", "", "main", 0.2)},
			memorySamples: prommodel.Vector{newSample("ns", "pod-a", "", 1024), newSample("ns", "", "main", 1024)},
			expected:      map[model.ContainerID]model.Resources{},
		},
		{
			name: "skips invalid values",
			cpuSamples: prommodel.Vector{
				newSample("ns", "pod-a", "main", math.NaN()),
				newSample("ns", "pod-b", "main", math.Inf(1)),
				newSample("ns", "pod-c", "main", -1),
			},
			memorySamples: prommodel.Vector{
				newSample("ns", "pod-a", "main", 1024),
				newSample("ns", "pod-b", "main", 1024),
				newSample("ns", "pod-c", "main", 1024),
			},
			expected: map[model.ContainerID]model.Resources{},
		},
		{
			name:     "returns an empty list without samples",
			expected: map[model.ContainerID]model.Resources{},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, tctx := ktesting.NewTestContext(t)
			api := &fakePrometheusAPI{results: map[string]prommodel.Value{
				cpuMetric:    tc.cpuSamples,
				memoryMetric: tc.memorySamples,
			}}
			client := newTestPrometheusClient(api, 0)

			list, err := client.List(tctx, "", v1.ListOptions{})

			assert.NoError(t, err)
			assert.Equal(t, tc.expected, usageByContainer(list))
			for _, podMetrics := range list.Items {
				assert.Equal(t, testQueryTime, podMetrics.Timestamp.Time)
				assert.Equal(t, time.Minute, podMetrics.Window.Duration)
			}
		})
	}
}

func TestPrometheusClientQueries(t *testing.T) {
	testCases := []struct {
		name            string
		jobName         string
		namespace       string
		rateWindow      time.Duration
		expectedQueries []string
	}{
		{
			name:       "all namespaces",
			jobName:    "kubernetes-nodes-cadvisor",
			rateWindow: time.Minute,
			expectedQueries: []string{
				`max by (namespace, pod, container) (rate(container_cpu_usage_seconds_total{job="kubernetes-nodes-cadvisor", pod=~".+", container!="POD", container!=""}[1m]))`,
				`max by (namespace, pod, container) (container_memory_working_set_bytes{job="kubernetes-nodes-cadvisor", pod=~".+", container!="POD", container!=""})`,
			},
		},
		{
			name:       "one namespace and any job",
			namespace:  "team",
			rateWindow: 90 * time.Second,
			expectedQueries: []string{
				`max by (namespace, pod, container) (rate(container_cpu_usage_seconds_total{pod=~".+", container!="POD", container!="", namespace="team"}[1m30s]))`,
				`max by (namespace, pod, container) (container_memory_working_set_bytes{pod=~".+", container!="POD", container!="", namespace="team"})`,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, tctx := ktesting.NewTestContext(t)
			api := &fakePrometheusAPI{}
			client := newTestPrometheusClient(api, 0)
			client.options.CadvisorMetricsJobName = tc.jobName
			client.options.CPURateWindow = tc.rateWindow

			_, err := client.List(tctx, tc.namespace, v1.ListOptions{})

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedQueries, api.queries)
			assert.Equal(t, []time.Time{testQueryTime, testQueryTime}, api.times)
		})
	}
}

func TestPrometheusClientListErrors(t *testing.T) {
	testCases := []struct {
		name          string
		api           *fakePrometheusAPI
		expectedError string
	}{
		{
			name:          "query fails",
			api:           &fakePrometheusAPI{err: errors.New("connection refused")},
			expectedError: "cannot get cpu usage from Prometheus: connection refused",
		},
		{
			name:          "query returns a matrix",
			api:           &fakePrometheusAPI{results: map[string]prommodel.Value{cpuMetric: prommodel.Matrix{}}},
			expectedError: "expected query to return a vector; got result type model.Matrix",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, tctx := ktesting.NewTestContext(t)
			client := newTestPrometheusClient(tc.api, 0)

			list, err := client.List(tctx, "", v1.ListOptions{})

			assert.EqualError(t, err, tc.expectedError)
			assert.Nil(t, list)
		})
	}
}

func TestPrometheusClientCache(t *testing.T) {
	const cacheTTL = 30 * time.Second
	testCases := []struct {
		name            string
		secondNamespace string
		elapsed         time.Duration
		expectedQueries int
	}{
		{
			name:            "reuses the result within the TTL",
			elapsed:         cacheTTL - time.Nanosecond,
			expectedQueries: 2,
		},
		{
			name:            "queries again once the TTL has passed",
			elapsed:         cacheTTL,
			expectedQueries: 4,
		},
		{
			name:            "queries again for another namespace",
			secondNamespace: "team",
			expectedQueries: 4,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, tctx := ktesting.NewTestContext(t)
			api := &fakePrometheusAPI{}
			client := newTestPrometheusClient(api, cacheTTL)

			first, err := client.List(tctx, "", v1.ListOptions{})
			assert.NoError(t, err)
			client.now = func() time.Time { return testQueryTime.Add(tc.elapsed) }
			second, err := client.List(tctx, tc.secondNamespace, v1.ListOptions{})
			assert.NoError(t, err)

			assert.Len(t, api.queries, tc.expectedQueries)
			if tc.expectedQueries == 2 {
				assert.Same(t, first, second)
			}
		})
	}
}

func TestPrometheusClientDoesNotCacheErrors(t *testing.T) {
	_, tctx := ktesting.NewTestContext(t)
	api := &fakePrometheusAPI{err: errors.New("connection refused")}
	client := newTestPrometheusClient(api, time.Minute)

	_, err := client.List(tctx, "", v1.ListOptions{})
	assert.Error(t, err)

	api.err = nil
	list, err := client.List(tctx, "", v1.ListOptions{})
	assert.NoError(t, err)
	assert.NotNil(t, list)
	assert.Len(t, api.queries, 3)
}

func TestNewPrometheusClientUsesTheSharedConfiguration(t *testing.T) {
	config := history.PrometheusHistoryProviderConfig{
		Address:                "http://prometheus.monitoring.svc:9090",
		QueryTimeout:           30 * time.Second,
		HistoryLength:          "24h",
		HistoryResolution:      "1m",
		CtrNamespaceLabel:      "namespace",
		CtrPodNameLabel:        "pod",
		CtrNameLabel:           "container",
		CadvisorMetricsJobName: "kubernetes-nodes-cadvisor",
		CPURateWindow:          time.Minute,
	}

	client, ok := NewPrometheusClient(config, 30*time.Second).(*prometheusMetricsClient)

	assert.True(t, ok)
	assert.Equal(t, prometheusClientOptions{
		QueryTimeout:           30 * time.Second,
		CPURateWindow:          time.Minute,
		CadvisorMetricsJobName: "kubernetes-nodes-cadvisor",
		CtrNamespaceLabel:      "namespace",
		CtrPodNameLabel:        "pod",
		CtrNameLabel:           "container",
		CacheTTL:               30 * time.Second,
	}, client.options)
}
