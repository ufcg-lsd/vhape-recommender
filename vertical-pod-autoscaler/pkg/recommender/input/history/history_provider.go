/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package history

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	"k8s.io/klog/v2"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	metrics_recommender "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/metrics/recommender"
)

// PrometheusBasicAuthTransport injects basic auth headers into HTTP requests.
type PrometheusBasicAuthTransport struct {
	Username string
	Password string
	Base     http.RoundTripper
}

// RoundTrip function injects the username and password in the request's basic auth header
func (t *PrometheusBasicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Use default transport if none specified
	rt := t.Base
	if rt == nil {
		rt = http.DefaultTransport
	}

	// Clone the request before modification to avoid data races and side effects.
	// Original http.Request contains shared fields (Header, URL, Body) that are unsafe to modify directly.
	// Also, RoundTripper interface recommends not to modify the request:
	//   https://cs.opensource.google/go/go/+/refs/tags/go1.24.4:src/net/http/client.go;l=128-132
	// Extra materials: https://pkg.go.dev/net/http#Request.Clone (deep copy requirement)
	//   and https://github.com/golang/go/issues/36095 (concurrency safety discussion)
	cloned := req.Clone(req.Context())
	cloned.SetBasicAuth(t.Username, t.Password)
	return rt.RoundTrip(cloned)
}

// PrometheusBearerTokenAuthTransport injects bearer token into HTTP requests.
type PrometheusBearerTokenAuthTransport struct {
	Token string
	Base  http.RoundTripper
}

// RoundTrip function injects the bearer token in the request's Authorization header
func (bt *PrometheusBearerTokenAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt := bt.Base
	if rt == nil {
		rt = http.DefaultTransport
	}

	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bt.Token))
	return rt.RoundTrip(cloned)
}

// PrometheusHistoryProviderConfig allow to select which metrics
// should be queried to get real resource utilization.
type PrometheusHistoryProviderConfig struct {
	Address                                          string
	Insecure                                         bool
	QueryTimeout                                     time.Duration
	HistoryLength, HistoryResolution                 string
	PodLabelPrefix, PodLabelsMetricName              string
	PodNamespaceLabel, PodNameLabel                  string
	CtrNamespaceLabel, CtrPodNameLabel, CtrNameLabel string
	CadvisorMetricsJobName                           string
	Namespace                                        string
	// CPURateWindow is the range of the CPU usage rate. When zero, HistoryResolution is used.
	CPURateWindow time.Duration
	// ReadPodOwners also reads the controllers that own each pod from the
	// kube-state-metrics owner metrics, so that pods that no longer run can be
	// matched to their VPA without their labels.
	ReadPodOwners bool

	Authentication PrometheusCredentials
}

// PrometheusCredentials keeps credentials for Prometheus API. The Username + Password pair is mutually exclusive with
// the BearerToken field. It's handled in the CLI flags. But if BearerToken is set, it will have priority over the basic auth.
// If both are empty, no authentication is used.
type PrometheusCredentials struct {
	Username    string
	Password    string
	BearerToken string
}

// PodHistory represents history of usage and labels for a given pod.
type PodHistory struct {
	// Current samples if pod is still alive, last known samples otherwise.
	LastLabels map[string]string
	LastSeen   time.Time
	// A map for container name to a list of its usage samples, in chronological
	// order.
	Samples map[string][]model.ContainerUsageSample
	// Controllers that own the pod, from its direct owner up. Read only with ReadPodOwners.
	Owners []PodOwner
}

// PodOwner is a controller in the ownership chain of a pod.
type PodOwner struct {
	Kind string
	Name string
}

func newEmptyHistory() *PodHistory {
	return &PodHistory{LastLabels: map[string]string{}, Samples: map[string][]model.ContainerUsageSample{}}
}

// HistoryProvider gives history of all pods in a cluster.
// TODO(schylek): this interface imposes how history is represented which doesn't work well with checkpoints.
// Consider refactoring to passing clusterState and create history provider working with checkpoints.
type HistoryProvider interface {
	GetClusterHistory() (map[model.PodID]*PodHistory, error)
}

// FetchedHistory is a HistoryProvider for a cluster history that was already
// read, so that it can be shared without querying Prometheus again.
type FetchedHistory map[model.PodID]*PodHistory

// GetClusterHistory returns the history that was already read.
func (h FetchedHistory) GetClusterHistory() (map[model.PodID]*PodHistory, error) {
	return h, nil
}

type prometheusHistoryProvider struct {
	prometheusClient  prometheusv1.API
	config            PrometheusHistoryProviderConfig
	queryTimeout      time.Duration
	historyDuration   prommodel.Duration
	historyResolution prommodel.Duration
}

// PrometheusClientConfig holds what is needed to reach the Prometheus API.
type PrometheusClientConfig struct {
	Address        string
	Insecure       bool
	Authentication PrometheusCredentials
}

// NewPrometheusAPI builds a Prometheus API client with the configured TLS and authentication.
func NewPrometheusAPI(config PrometheusClientConfig) (prometheusv1.API, error) {
	prometheusTransport := promapi.DefaultRoundTripper

	if config.Insecure {
		prometheusTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if config.Authentication.BearerToken != "" {
		prometheusTransport = &PrometheusBearerTokenAuthTransport{
			Token: config.Authentication.BearerToken,
			Base:  prometheusTransport,
		}
	} else if config.Authentication.Username != "" && config.Authentication.Password != "" {
		prometheusTransport = &PrometheusBasicAuthTransport{
			Username: config.Authentication.Username,
			Password: config.Authentication.Password,
			Base:     prometheusTransport,
		}
	} else {
		// check if env vars for credentials are set
		prometheusUsername := os.Getenv("PROMETHEUS_USERNAME")
		prometheusPassword := os.Getenv("PROMETHEUS_PASSWORD")
		if prometheusUsername != "" && prometheusPassword != "" {
			prometheusTransport = &PrometheusBasicAuthTransport{
				Username: prometheusUsername,
				Password: prometheusPassword,
				Base:     prometheusTransport,
			}
		}
	}

	roundTripper := metrics_recommender.NewPrometheusRoundTripperCounter(
		metrics_recommender.NewPrometheusRoundTripperDuration(prometheusTransport),
	)

	promConfig := promapi.Config{
		Address:      config.Address,
		RoundTripper: roundTripper,
	}

	promClient, err := promapi.NewClient(promConfig)
	if err != nil {
		return nil, err
	}
	return prometheusv1.NewAPI(promClient), nil
}

// NewPrometheusHistoryProvider constructs a history provider that gets data from Prometheus.
func NewPrometheusHistoryProvider(config PrometheusHistoryProviderConfig) (HistoryProvider, error) {
	prometheusClient, err := NewPrometheusAPI(PrometheusClientConfig{
		Address:        config.Address,
		Insecure:       config.Insecure,
		Authentication: config.Authentication,
	})
	if err != nil {
		return &prometheusHistoryProvider{}, err
	}

	// Use Prometheus's model.Duration; this can additionally parse durations in days, weeks and years (as well as seconds, minutes, hours etc)
	historyDuration, err := prommodel.ParseDuration(config.HistoryLength)
	if err != nil {
		return &prometheusHistoryProvider{}, fmt.Errorf("history length %s is not a valid Prometheus duration: %v", config.HistoryLength, err)
	}

	historyResolution, err := prommodel.ParseDuration(config.HistoryResolution)
	if err != nil {
		return &prometheusHistoryProvider{}, fmt.Errorf("history resolution %s is not a valid Prometheus duration: %v", config.HistoryResolution, err)
	}

	if config.CPURateWindow > 0 && time.Duration(historyResolution) > config.CPURateWindow {
		klog.InfoS("History resolution is longer than the CPU usage rate window, so the CPU usage between history samples is not read",
			"historyResolution", config.HistoryResolution, "cpuRateWindow", config.CPURateWindow)
	}

	return &prometheusHistoryProvider{
		prometheusClient:  prometheusClient,
		config:            config,
		queryTimeout:      config.QueryTimeout,
		historyDuration:   historyDuration,
		historyResolution: historyResolution,
	}, nil
}

func (p *prometheusHistoryProvider) getContainerIDFromLabels(metric prommodel.Metric) (*model.ContainerID, error) {
	labels := promMetricToLabelMap(metric)
	namespace, ok := labels[p.config.CtrNamespaceLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label", p.config.CtrNamespaceLabel)
	}
	podName, ok := labels[p.config.CtrPodNameLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label", p.config.CtrPodNameLabel)
	}
	containerName, ok := labels[p.config.CtrNameLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label on container data", p.config.CtrNameLabel)
	}
	return &model.ContainerID{
		PodID: model.PodID{
			Namespace: namespace,
			PodName:   podName,
		},
		ContainerName: containerName,
	}, nil
}

func (p *prometheusHistoryProvider) getPodIDFromLabels(metric prommodel.Metric) (*model.PodID, error) {
	labels := promMetricToLabelMap(metric)
	namespace, ok := labels[p.config.PodNamespaceLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label", p.config.PodNamespaceLabel)
	}
	podName, ok := labels[p.config.PodNameLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label", p.config.PodNameLabel)
	}
	return &model.PodID{Namespace: namespace, PodName: podName}, nil
}

func (p *prometheusHistoryProvider) getPodLabelsMap(metric prommodel.Metric) map[string]string {
	podLabels := make(map[string]string)
	for key, value := range metric {
		podLabelKey := strings.TrimPrefix(string(key), p.config.PodLabelPrefix)
		if podLabelKey != string(key) {
			podLabels[podLabelKey] = string(value)
		}
	}
	return podLabels
}

func promMetricToLabelMap(metric prommodel.Metric) map[string]string {
	labels := map[string]string{}
	for k, v := range metric {
		labels[string(k)] = string(v)
	}
	return labels
}

func resourceAmountFromValue(value float64, resource model.ResourceName) model.ResourceAmount {
	// This assumes CPU value is in cores and memory in bytes, which is true
	// for the metrics this class queries from Prometheus.
	switch resource {
	case model.ResourceCPU:
		return model.CPUAmountFromCores(value)
	case model.ResourceMemory:
		return model.MemoryAmountFromBytes(value)
	}
	return model.ResourceAmount(0)
}

func getContainerUsageSamplesFromSamples(samples []prommodel.SamplePair, resource model.ResourceName) []model.ContainerUsageSample {
	res := make([]model.ContainerUsageSample, 0)
	for _, sample := range samples {
		res = append(res, model.ContainerUsageSample{
			MeasureStart: sample.Timestamp.Time(),
			Usage:        resourceAmountFromValue(float64(sample.Value), resource),
			Resource:     resource,
		})
	}
	return res
}

func (p *prometheusHistoryProvider) readResourceHistory(res map[model.PodID]*PodHistory, query string, resource model.ResourceName) error {
	end := time.Now()
	start := end.Add(-time.Duration(p.historyDuration))

	for _, r := range p.historyRanges(start, end) {
		klog.V(4).InfoS("Reading usage history", "resource", resource, "start", r.Start, "end", r.End)
		if err := p.readResourceHistoryRange(res, query, resource, r); err != nil {
			return err
		}
	}
	return nil
}

// historyChunkLength is the length of the history read by each query. Reading
// the history in chunks keeps the memory used by each response low.
const historyChunkLength = time.Hour

// historyRanges splits [start, end] into consecutive ranges of historyChunkLength.
// Each range ends one step before the next one starts, since a range query
// returns the points at both of its ends and they would be read twice.
func (p *prometheusHistoryProvider) historyRanges(start, end time.Time) []prometheusv1.Range {
	step := time.Duration(p.historyResolution)
	if step <= 0 {
		return []prometheusv1.Range{{Start: start, End: end, Step: step}}
	}
	// Whole steps, so that the points of each range follow those of the previous one.
	chunk := (historyChunkLength + step - 1) / step * step

	var ranges []prometheusv1.Range
	for rangeStart := start; ; rangeStart = rangeStart.Add(chunk) {
		if !rangeStart.Add(chunk).Before(end) {
			return append(ranges, prometheusv1.Range{Start: rangeStart, End: end, Step: step})
		}
		ranges = append(ranges, prometheusv1.Range{Start: rangeStart, End: rangeStart.Add(chunk - step), Step: step})
	}
}

func (p *prometheusHistoryProvider) readResourceHistoryRange(res map[model.PodID]*PodHistory, query string, resource model.ResourceName, r prometheusv1.Range) error {
	ctx, cancel := context.WithTimeout(context.Background(), p.queryTimeout)
	defer cancel()

	result, _, err := p.prometheusClient.QueryRange(ctx, query, r)
	if err != nil {
		return fmt.Errorf("cannot get timeseries for %v: %v", resource, err)
	}

	matrix, ok := result.(prommodel.Matrix)
	if !ok {
		return fmt.Errorf("expected query to return a matrix; got result type %T", result)
	}

	for _, ts := range matrix {
		containerID, err := p.getContainerIDFromLabels(ts.Metric)
		if err != nil {
			return fmt.Errorf("cannot get container ID from labels: %v", ts.Metric)
		}

		newSamples := getContainerUsageSamplesFromSamples(ts.Values, resource)
		podHistory, ok := res[containerID.PodID]
		if !ok {
			podHistory = newEmptyHistory()
			res[containerID.PodID] = podHistory
		}
		podHistory.Samples[containerID.ContainerName] = append(
			podHistory.Samples[containerID.ContainerName],
			newSamples...)
	}
	return nil
}

func (p *prometheusHistoryProvider) readLastLabels(res map[model.PodID]*PodHistory, query string) error {
	ctx, cancel := context.WithTimeout(context.Background(), p.queryTimeout)
	defer cancel()

	result, _, err := p.prometheusClient.Query(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("cannot get timeseries for labels: %v", err)
	}

	matrix, ok := result.(prommodel.Matrix)
	if !ok {
		return fmt.Errorf("expected query to return a matrix; got result type %T", result)
	}

	for _, ts := range matrix {
		podID, err := p.getPodIDFromLabels(ts.Metric)
		if err != nil {
			return fmt.Errorf("cannot get container ID from labels %v: %v", ts.Metric, err)
		}
		podHistory, ok := res[*podID]
		if !ok {
			podHistory = newEmptyHistory()
			res[*podID] = podHistory
		}
		podLabels := p.getPodLabelsMap(ts.Metric)

		// time series results will always be sorted chronologically from oldest to
		// newest, so the last element is the latest sample
		lastSample := ts.Values[len(ts.Values)-1]
		if lastSample.Timestamp.Time().After(podHistory.LastSeen) {
			podHistory.LastSeen = lastSample.Timestamp.Time()
			podHistory.LastLabels = podLabels
		}
	}
	return nil
}

func (p *prometheusHistoryProvider) GetClusterHistory() (map[model.PodID]*PodHistory, error) {
	res := make(map[model.PodID]*PodHistory)
	var podSelector string
	if p.config.CadvisorMetricsJobName != "" {
		podSelector = fmt.Sprintf("job=\"%s\", ", p.config.CadvisorMetricsJobName)
	}
	podSelector = podSelector + fmt.Sprintf("%s=~\".+\", %s!=\"POD\", %s!=\"\"",
		p.config.CtrPodNameLabel, p.config.CtrNameLabel, p.config.CtrNameLabel)

	if p.config.Namespace != "" {
		podSelector = fmt.Sprintf("%s, %s=\"%s\"", podSelector, p.config.CtrNamespaceLabel, p.config.Namespace)
	}
	cpuRateWindow := p.config.HistoryResolution
	if p.config.CPURateWindow > 0 {
		cpuRateWindow = prommodel.Duration(p.config.CPURateWindow).String()
	}
	// max rather than the raw series, so that a container scraped by two jobs is not counted twice.
	by := fmt.Sprintf("%s, %s, %s", p.config.CtrNamespaceLabel, p.config.CtrPodNameLabel, p.config.CtrNameLabel)
	historicalCpuQuery := fmt.Sprintf("max by (%s) (rate(container_cpu_usage_seconds_total{%s}[%s]))", by, podSelector, cpuRateWindow)
	klog.V(4).InfoS("Historical CPU usage query", "query", historicalCpuQuery)
	err := p.readResourceHistory(res, historicalCpuQuery, model.ResourceCPU)
	if err != nil {
		return nil, fmt.Errorf("cannot get usage history: %v", err)
	}

	historicalMemoryQuery := fmt.Sprintf("max by (%s) (container_memory_working_set_bytes{%s})", by, podSelector)
	klog.V(4).InfoS("Historical memory usage query", "query", historicalMemoryQuery)
	err = p.readResourceHistory(res, historicalMemoryQuery, model.ResourceMemory)
	if err != nil {
		return nil, fmt.Errorf("cannot get usage history: %v", err)
	}
	for _, podHistory := range res {
		for _, samples := range podHistory.Samples {
			sort.Slice(samples, func(i, j int) bool { return samples[i].MeasureStart.Before(samples[j].MeasureStart) })
		}
	}
	if p.config.PodLabelsMetricName != "" {
		err = p.readLastLabels(res, p.config.PodLabelsMetricName)
		if err != nil {
			return nil, fmt.Errorf("cannot read last labels: %v", err)
		}
	}
	if p.config.ReadPodOwners {
		// Owners only help to match pods that no longer run, so the history is
		// still useful without them.
		if err := p.readPodOwners(res); err != nil {
			klog.ErrorS(err, "Cannot read pod owners, pods that no longer run are matched only by their labels")
		}
	}

	return res, nil
}

// ownerKey identifies a controller in a namespace.
type ownerKey struct {
	namespace string
	owner     PodOwner
}

// Labels of the kube-state-metrics owner metrics.
const (
	ownerNamespaceLabel = "namespace"
	ownerPodNameLabel   = "pod"
)

// maxOwnerChainLength bounds the ownership chain, such as Pod, ReplicaSet and
// Deployment, in case the metrics describe a cycle.
const maxOwnerChainLength = 5

// readPodOwners sets the ownership chain of the pods in res from the
// kube-state-metrics owner metrics of the whole history length.
func (p *prometheusHistoryProvider) readPodOwners(res map[model.PodID]*PodHistory) error {
	podOwners := make(map[model.PodID]PodOwner)
	err := p.readOwners("kube_pod_owner", ownerPodNameLabel, func(namespace, name string, owner PodOwner) {
		podID := model.PodID{Namespace: namespace, PodName: name}
		if _, found := res[podID]; found {
			podOwners[podID] = owner
		}
	})
	if err != nil {
		return err
	}

	// Pods are owned by ReplicaSets and Jobs, which are owned by Deployments and CronJobs.
	controllerOwners := make(map[ownerKey]PodOwner)
	for _, controller := range []struct{ kind, metric, nameLabel string }{
		{"ReplicaSet", "kube_replicaset_owner", "replicaset"},
		{"Job", "kube_job_owner", "job_name"},
	} {
		err := p.readOwners(controller.metric, controller.nameLabel, func(namespace, name string, owner PodOwner) {
			controllerOwners[ownerKey{namespace, PodOwner{Kind: controller.kind, Name: name}}] = owner
		})
		if err != nil {
			return err
		}
	}

	for podID, owner := range podOwners {
		chain := []PodOwner{owner}
		for len(chain) < maxOwnerChainLength {
			next, found := controllerOwners[ownerKey{podID.Namespace, chain[len(chain)-1]}]
			if !found {
				break
			}
			chain = append(chain, next)
		}
		res[podID].Owners = chain
	}
	return nil
}

// readOwners calls add with the controller that owns each object of a
// kube-state-metrics owner metric, such as kube_pod_owner, seen in the history length.
func (p *prometheusHistoryProvider) readOwners(metric, nameLabel string, add func(namespace, name string, owner PodOwner)) error {
	selector := "owner_is_controller=\"true\""
	if p.config.Namespace != "" {
		selector = fmt.Sprintf("%s, %s=\"%s\"", selector, ownerNamespaceLabel, p.config.Namespace)
	}
	query := fmt.Sprintf("last_over_time(%s{%s}[%s])", metric, selector, p.config.HistoryLength)
	klog.V(4).InfoS("Pod owners query", "query", query)

	ctx, cancel := context.WithTimeout(context.Background(), p.queryTimeout)
	defer cancel()

	result, _, err := p.prometheusClient.Query(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("cannot get %s: %v", metric, err)
	}
	vector, ok := result.(prommodel.Vector)
	if !ok {
		return fmt.Errorf("expected query to return a vector; got result type %T", result)
	}

	for _, sample := range vector {
		namespace := string(sample.Metric[ownerNamespaceLabel])
		name := string(sample.Metric[prommodel.LabelName(nameLabel)])
		kind := string(sample.Metric["owner_kind"])
		ownerName := string(sample.Metric["owner_name"])
		// kube-state-metrics reports objects without an owner as "<none>".
		if namespace == "" || name == "" || kind == "" || kind == "<none>" || ownerName == "" {
			continue
		}
		add(namespace, name, PodOwner{Kind: kind, Name: ownerName})
	}
	return nil
}
