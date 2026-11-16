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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const (
	cpuQuery              = "max by (namespace, pod_name, name) (rate(container_cpu_usage_seconds_total{job=\"kubernetes-cadvisor\", pod_name=~\".+\", name!=\"POD\", name!=\"\"}[30s]))"
	memoryQuery           = "max by (namespace, pod_name, name) (container_memory_working_set_bytes{job=\"kubernetes-cadvisor\", pod_name=~\".+\", name!=\"POD\", name!=\"\"})"
	labelsQuery           = "up{job=\"kubernetes-pods\"}"
	cpuNamespacedQuery    = "max by (namespace, pod_name, name) (rate(container_cpu_usage_seconds_total{job=\"kubernetes-cadvisor\", pod_name=~\".+\", name!=\"POD\", name!=\"\", namespace=\"kube-system\"}[30s]))"
	memoryNamespacedQuery = "max by (namespace, pod_name, name) (container_memory_working_set_bytes{job=\"kubernetes-cadvisor\", pod_name=~\".+\", name!=\"POD\", name!=\"\", namespace=\"kube-system\"})"
)

func getDefaultPrometheusHistoryProviderConfigForTest() PrometheusHistoryProviderConfig {
	return PrometheusHistoryProviderConfig{
		Address:                "",
		HistoryLength:          "8d",
		HistoryResolution:      "30s",
		PodLabelPrefix:         "pod_label_",
		PodLabelsMetricName:    "up{job=\"kubernetes-pods\"}",
		PodNamespaceLabel:      "kubernetes_namespace",
		PodNameLabel:           "kubernetes_pod_name",
		CtrNamespaceLabel:      "namespace",
		CtrPodNameLabel:        "pod_name",
		CtrNameLabel:           "name",
		CadvisorMetricsJobName: "kubernetes-cadvisor",
	}
}

type mockPrometheusAPI struct {
	mock.Mock
}

func (m *mockPrometheusAPI) AlertManagers(ctx context.Context) (prometheusv1.AlertManagersResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Alerts(ctx context.Context) (prometheusv1.AlertsResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) CleanTombstones(ctx context.Context) error {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Config(ctx context.Context) (prometheusv1.ConfigResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Flags(ctx context.Context) (prometheusv1.FlagsResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) LabelNames(ctx context.Context, matches []string, startTime time.Time, endTime time.Time, opts ...prometheusv1.Option) ([]string, prometheusv1.Warnings, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) LabelValues(ctx context.Context, label string, matches []string, startTime time.Time, endTime time.Time, opts ...prometheusv1.Option) (prommodel.LabelValues, prometheusv1.Warnings, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time, opts ...prometheusv1.Option) ([]prommodel.LabelSet, prometheusv1.Warnings, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Rules(ctx context.Context) (prometheusv1.RulesResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Snapshot(ctx context.Context, skipHead bool) (prometheusv1.SnapshotResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Targets(ctx context.Context) (prometheusv1.TargetsResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) TargetsMetadata(ctx context.Context, _, _, _ string) ([]prometheusv1.MetricMetadata, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Buildinfo(ctx context.Context) (prometheusv1.BuildinfoResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Metadata(ctx context.Context, metric string, limit string) (map[string][]prometheusv1.Metadata, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) QueryExemplars(ctx context.Context, query string, startTime time.Time, endTime time.Time) ([]prometheusv1.ExemplarQueryResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Runtimeinfo(ctx context.Context) (prometheusv1.RuntimeinfoResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) TSDB(ctx context.Context, opts ...prometheusv1.Option) (prometheusv1.TSDBResult, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) WalReplay(ctx context.Context) (prometheusv1.WalReplayStatus, error) {
	panic("not implemented")
}

func (m *mockPrometheusAPI) Query(ctx context.Context, query string, ts time.Time, opts ...prometheusv1.Option) (prommodel.Value, prometheusv1.Warnings, error) {
	args := m.Called(ctx, query, ts)
	var returnArg prommodel.Value
	if args.Get(0) != nil {
		returnArg = args.Get(0).(prommodel.Value)
	}
	return returnArg, nil, args.Error(1)
}

func (m *mockPrometheusAPI) QueryRange(ctx context.Context, query string, r prometheusv1.Range, opts ...prometheusv1.Option) (prommodel.Value, prometheusv1.Warnings, error) {
	args := m.Called(ctx, query, r)
	var returnArg prommodel.Value
	if args.Get(0) != nil {
		returnArg = args.Get(0).(prommodel.Value)
	}
	return returnArg, nil, args.Error(1)
}

func TestGetEmptyClusterHistory(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:           getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient: &mockClient,
	}
	mockClient.On("Query", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Times(1).Return(
		prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("v1.Range")).Return().Times(2).Return(
		prommodel.Matrix{}, nil)
	tss, err := historyProvider.GetClusterHistory()
	assert.Nil(t, err)
	assert.NotNil(t, tss)
	assert.Empty(t, tss)
}

func TestPrometheusError(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:           getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient: &mockClient,
	}
	mockClient.On("QueryRange", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("v1.Range")).Return().Times(2).Return(
		nil, errors.New("bla"))
	_, err := historyProvider.GetClusterHistory()
	assert.NotNil(t, err)
}

func TestGetCPUSamples(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:           getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient: &mockClient,
	}
	mockClient.On("QueryRange", mock.Anything, cpuQuery, mock.AnythingOfType("v1.Range")).Return().Return(
		prommodel.Matrix{
			{
				Metric: map[prommodel.LabelName]prommodel.LabelValue{
					"namespace": "default",
					"pod_name":  "pod",
					"name":      "container",
				},
				Values: []prommodel.SamplePair{
					{
						Timestamp: prommodel.TimeFromUnix(1),
						Value:     5.5,
					},
				},
			},
		},
		nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return().Return(prommodel.Matrix{}, nil)
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Matrix{}, nil)
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	podHistory := &PodHistory{
		LastLabels: map[string]string{},
		Samples: map[string][]model.ContainerUsageSample{"container": {{
			MeasureStart: time.Unix(1, 0),
			Usage:        model.CPUAmountFromCores(5.5),
			Resource:     model.ResourceCPU,
		}}},
	}
	histories, err := historyProvider.GetClusterHistory()
	assert.Nil(t, err)
	assert.Equal(t, histories, map[model.PodID]*PodHistory{podID: podHistory})
}

func TestGetMemorySamples(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:           getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient: &mockClient,
	}
	mockClient.On("QueryRange", mock.Anything, cpuQuery, mock.AnythingOfType("v1.Range")).Return().Return(prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return().Return(
		prommodel.Matrix{
			{
				Metric: map[prommodel.LabelName]prommodel.LabelValue{
					"namespace": "default",
					"pod_name":  "pod",
					"name":      "container",
				},
				Values: []prommodel.SamplePair{
					{
						Timestamp: prommodel.TimeFromUnix(1),
						Value:     12345,
					},
				},
			},
		}, nil)
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Matrix{}, nil)
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	podHistory := &PodHistory{
		LastLabels: map[string]string{},
		Samples: map[string][]model.ContainerUsageSample{"container": {{
			MeasureStart: time.Unix(1, 0),
			Usage:        model.MemoryAmountFromBytes(12345),
			Resource:     model.ResourceMemory,
		}}},
	}
	histories, err := historyProvider.GetClusterHistory()
	assert.Nil(t, err)
	assert.Equal(t, histories, map[model.PodID]*PodHistory{podID: podHistory})
}

func TestGetNamespacedMemorySamples(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	promConfig := getDefaultPrometheusHistoryProviderConfigForTest()
	promConfig.Namespace = "kube-system"
	historyProvider := prometheusHistoryProvider{
		config:           promConfig,
		prometheusClient: &mockClient,
	}

	mockClient.On("QueryRange", mock.Anything, cpuNamespacedQuery, mock.AnythingOfType("v1.Range")).Return().Return(prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryNamespacedQuery, mock.AnythingOfType("v1.Range")).Return().Return(
		prommodel.Matrix{
			{
				Metric: map[prommodel.LabelName]prommodel.LabelValue{
					"namespace": "kube-system",
					"pod_name":  "pod",
					"name":      "container",
				},
				Values: []prommodel.SamplePair{
					{
						Timestamp: prommodel.TimeFromUnix(1),
						Value:     12345,
					},
				},
			},
		}, nil)
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Matrix{}, nil)
	podID := model.PodID{Namespace: "kube-system", PodName: "pod"}
	podHistory := &PodHistory{
		LastLabels: map[string]string{},
		Samples: map[string][]model.ContainerUsageSample{"container": {{
			MeasureStart: time.Unix(1, 0),
			Usage:        model.MemoryAmountFromBytes(12345),
			Resource:     model.ResourceMemory,
		}}},
	}
	histories, err := historyProvider.GetClusterHistory()
	assert.Nil(t, err)
	assert.Equal(t, histories, map[model.PodID]*PodHistory{podID: podHistory})
}

func TestGetLabels(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:           getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient: &mockClient,
	}
	mockClient.On("QueryRange", mock.Anything, cpuQuery, mock.AnythingOfType("v1.Range")).Return().Return(prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return().Return(prommodel.Matrix{}, nil)
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(
		prommodel.Matrix{
			{
				Metric: map[prommodel.LabelName]prommodel.LabelValue{
					"kubernetes_namespace": "default",
					"kubernetes_pod_name":  "pod",
					"pod_label_x":          "y",
				},
				Values: []prommodel.SamplePair{
					{
						Timestamp: prommodel.TimeFromUnix(1),
						Value:     12345,
					},
				},
			},
			{
				Metric: map[prommodel.LabelName]prommodel.LabelValue{
					"kubernetes_namespace": "default",
					"kubernetes_pod_name":  "pod",
					"pod_label_x":          "z",
				},
				Values: []prommodel.SamplePair{
					{
						Timestamp: prommodel.TimeFromUnix(20),
						Value:     12345,
					},
				},
			},
		}, nil)
	podID := model.PodID{Namespace: "default", PodName: "pod"}
	podHistory := &PodHistory{
		LastLabels: map[string]string{"x": "z"},
		LastSeen:   time.Unix(20, 0),
		Samples:    map[string][]model.ContainerUsageSample{},
	}
	histories, err := historyProvider.GetClusterHistory()
	assert.Nil(t, err)
	assert.Equal(t, histories, map[model.PodID]*PodHistory{podID: podHistory})
}

func TestGetClusterHistoryUsesCPURateWindow(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	config := getDefaultPrometheusHistoryProviderConfigForTest()
	config.CPURateWindow = time.Minute
	historyProvider := prometheusHistoryProvider{
		config:            config,
		prometheusClient:  &mockClient,
		historyResolution: prommodel.Duration(30 * time.Second),
	}
	cpuQueryWithRateWindow := "max by (namespace, pod_name, name) (rate(container_cpu_usage_seconds_total{job=\"kubernetes-cadvisor\", pod_name=~\".+\", name!=\"POD\", name!=\"\"}[1m]))"
	// The rate window changes, but the step is still the history resolution.
	resolutionStep := mock.MatchedBy(func(r prometheusv1.Range) bool { return r.Step == 30*time.Second })
	mockClient.On("QueryRange", mock.Anything, cpuQueryWithRateWindow, resolutionStep).Return(prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, resolutionStep).Return(prommodel.Matrix{}, nil)
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Matrix{}, nil)

	_, err := historyProvider.GetClusterHistory()

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestGetClusterHistorySkipsLabelsWithoutLabelsMetric(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	config := getDefaultPrometheusHistoryProviderConfigForTest()
	config.PodLabelsMetricName = ""
	historyProvider := prometheusHistoryProvider{
		config:           config,
		prometheusClient: &mockClient,
	}
	mockClient.On("QueryRange", mock.Anything, cpuQuery, mock.AnythingOfType("v1.Range")).Return(prommodel.Matrix{}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return(prommodel.Matrix{}, nil)

	_, err := historyProvider.GetClusterHistory()

	assert.NoError(t, err)
	mockClient.AssertNotCalled(t, "Query", mock.Anything, mock.Anything, mock.Anything)
}

const (
	podOwnersQuery        = "last_over_time(kube_pod_owner{owner_is_controller=\"true\"}[8d])"
	replicaSetOwnersQuery = "last_over_time(kube_replicaset_owner{owner_is_controller=\"true\"}[8d])"
	jobOwnersQuery        = "last_over_time(kube_job_owner{owner_is_controller=\"true\"}[8d])"
)

func newOwnerSample(nameLabel, name, ownerKind, ownerName string) *prommodel.Sample {
	return &prommodel.Sample{Metric: prommodel.Metric{
		"namespace":                    "default",
		prommodel.LabelName(nameLabel): prommodel.LabelValue(name),
		"owner_kind":                   prommodel.LabelValue(ownerKind),
		"owner_name":                   prommodel.LabelValue(ownerName),
	}}
}

func newOwnersHistoryProvider(mockClient *mockPrometheusAPI) prometheusHistoryProvider {
	config := getDefaultPrometheusHistoryProviderConfigForTest()
	config.PodLabelsMetricName = ""
	config.ReadPodOwners = true
	cpuSample := func(pod string) *prommodel.SampleStream {
		return &prommodel.SampleStream{
			Metric: prommodel.Metric{"namespace": "default", "pod_name": prommodel.LabelValue(pod), "name": "app"},
			Values: []prommodel.SamplePair{{Timestamp: prommodel.TimeFromUnix(1), Value: 0.5}},
		}
	}
	mockClient.On("QueryRange", mock.Anything, cpuQuery, mock.AnythingOfType("v1.Range")).Return(
		prommodel.Matrix{cpuSample("web-7d4b9c-abcde"), cpuSample("report-28401-xyz12"), cpuSample("standalone")}, nil)
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return(prommodel.Matrix{}, nil)
	return prometheusHistoryProvider{config: config, prometheusClient: mockClient}
}

func TestGetClusterHistoryReadsPodOwners(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := newOwnersHistoryProvider(&mockClient)
	mockClient.On("Query", mock.Anything, podOwnersQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Vector{
		newOwnerSample("pod", "web-7d4b9c-abcde", "ReplicaSet", "web-7d4b9c"),
		newOwnerSample("pod", "report-28401-xyz12", "Job", "report-28401"),
		newOwnerSample("pod", "standalone", "<none>", "<none>"),
		// Not in the usage history, so it is left out.
		newOwnerSample("pod", "other", "ReplicaSet", "other-1"),
	}, nil)
	mockClient.On("Query", mock.Anything, replicaSetOwnersQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Vector{
		newOwnerSample("replicaset", "web-7d4b9c", "Deployment", "web"),
	}, nil)
	mockClient.On("Query", mock.Anything, jobOwnersQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Vector{
		newOwnerSample("job_name", "report-28401", "CronJob", "report"),
	}, nil)

	histories, err := historyProvider.GetClusterHistory()

	assert.NoError(t, err)
	assert.Equal(t, []PodOwner{{Kind: "ReplicaSet", Name: "web-7d4b9c"}, {Kind: "Deployment", Name: "web"}},
		histories[model.PodID{Namespace: "default", PodName: "web-7d4b9c-abcde"}].Owners)
	assert.Equal(t, []PodOwner{{Kind: "Job", Name: "report-28401"}, {Kind: "CronJob", Name: "report"}},
		histories[model.PodID{Namespace: "default", PodName: "report-28401-xyz12"}].Owners)
	assert.Empty(t, histories[model.PodID{Namespace: "default", PodName: "standalone"}].Owners)
	assert.NotContains(t, histories, model.PodID{Namespace: "default", PodName: "other"})
}

func TestGetClusterHistoryKeepsTheHistoryWhenOwnersFail(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := newOwnersHistoryProvider(&mockClient)
	mockClient.On("Query", mock.Anything, podOwnersQuery, mock.AnythingOfType("time.Time")).Return(nil, errors.New("unavailable"))

	histories, err := historyProvider.GetClusterHistory()

	assert.NoError(t, err)
	assert.Len(t, histories, 3)
	assert.Empty(t, histories[model.PodID{Namespace: "default", PodName: "web-7d4b9c-abcde"}].Owners)
}

// rangePoints returns the times at which a range query evaluates.
func rangePoints(r prometheusv1.Range) []time.Time {
	if r.Step <= 0 {
		return []time.Time{r.Start}
	}
	var points []time.Time
	for t := r.Start; !t.After(r.End); t = t.Add(r.Step) {
		points = append(points, t)
	}
	return points
}

func TestHistoryRanges(t *testing.T) {
	start := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	testCases := []struct {
		name     string
		length   time.Duration
		step     time.Duration
		expected []prometheusv1.Range
	}{
		{
			name:   "splits the history into chunks",
			length: 3 * time.Hour,
			step:   time.Minute,
			expected: []prometheusv1.Range{
				{Start: start, End: start.Add(59 * time.Minute), Step: time.Minute},
				{Start: start.Add(time.Hour), End: start.Add(time.Hour + 59*time.Minute), Step: time.Minute},
				{Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour), Step: time.Minute},
			},
		},
		{
			name:     "reads a history shorter than a chunk in one query",
			length:   30 * time.Minute,
			step:     time.Minute,
			expected: []prometheusv1.Range{{Start: start, End: start.Add(30 * time.Minute), Step: time.Minute}},
		},
		{
			name:   "rounds the chunk up to whole steps",
			length: 2 * time.Hour,
			step:   7 * time.Minute,
			// 60m becomes 63m, and the second chunk reaches the end.
			expected: []prometheusv1.Range{
				{Start: start, End: start.Add(56 * time.Minute), Step: 7 * time.Minute},
				{Start: start.Add(63 * time.Minute), End: start.Add(2 * time.Hour), Step: 7 * time.Minute},
			},
		},
		{
			name:     "reads in one query without a step",
			length:   3 * time.Hour,
			expected: []prometheusv1.Range{{Start: start, End: start.Add(3 * time.Hour)}},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			historyProvider := prometheusHistoryProvider{historyResolution: prommodel.Duration(tc.step)}
			end := start.Add(tc.length)

			ranges := historyProvider.historyRanges(start, end)

			assert.Equal(t, tc.expected, ranges)
			// The chunks evaluate at the same points as one query over the whole history.
			var points []time.Time
			for _, r := range ranges {
				points = append(points, rangePoints(r)...)
			}
			assert.Equal(t, rangePoints(prometheusv1.Range{Start: start, End: end, Step: tc.step}), points)
		})
	}
}

func TestGetClusterHistoryReadsTheHistoryInChunks(t *testing.T) {
	mockClient := mockPrometheusAPI{}
	historyProvider := prometheusHistoryProvider{
		config:            getDefaultPrometheusHistoryProviderConfigForTest(),
		prometheusClient:  &mockClient,
		historyDuration:   prommodel.Duration(2 * time.Hour),
		historyResolution: prommodel.Duration(time.Hour),
	}
	sampleAt := func(unix int64) prommodel.Matrix {
		return prommodel.Matrix{{
			Metric: prommodel.Metric{"namespace": "default", "pod_name": "pod", "name": "app"},
			Values: []prommodel.SamplePair{{Timestamp: prommodel.TimeFromUnix(unix), Value: 1}},
		}}
	}
	// With a 1h step, the first chunk holds only its start, and the second one runs to the end.
	firstChunk := mock.MatchedBy(func(r prometheusv1.Range) bool { return r.Start.Equal(r.End) })
	secondChunk := mock.MatchedBy(func(r prometheusv1.Range) bool { return r.End.Sub(r.Start) == time.Hour })
	mockClient.On("QueryRange", mock.Anything, cpuQuery, firstChunk).Return(sampleAt(1), nil).Once()
	mockClient.On("QueryRange", mock.Anything, cpuQuery, secondChunk).Return(sampleAt(2), nil).Once()
	mockClient.On("QueryRange", mock.Anything, memoryQuery, mock.AnythingOfType("v1.Range")).Return(prommodel.Matrix{}, nil).Twice()
	mockClient.On("Query", mock.Anything, labelsQuery, mock.AnythingOfType("time.Time")).Return(prommodel.Matrix{}, nil)

	histories, err := historyProvider.GetClusterHistory()

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
	samples := histories[model.PodID{Namespace: "default", PodName: "pod"}].Samples["app"]
	assert.Equal(t, []time.Time{time.Unix(1, 0), time.Unix(2, 0)}, []time.Time{samples[0].MeasureStart, samples[1].MeasureStart})
}

func TestPrometheusAuth(t *testing.T) {
	var capturedRequest *http.Request
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r
		_, _ = w.Write([]byte(`{"status": "success","data": {"resultType": "matrix","result": []}}`))
	}))
	defer ts.Close()

	cfg := PrometheusHistoryProviderConfig{
		Address:           ts.URL,
		Insecure:          true,
		HistoryLength:     "8d",
		HistoryResolution: "30s",
		QueryTimeout:      30 * time.Second,
	}

	t.Run("Basic auth", func(t *testing.T) {
		cfg.Authentication.Username = "user"
		cfg.Authentication.Password = "password"

		prov, _ := NewPrometheusHistoryProvider(cfg)
		_, err := prov.GetClusterHistory()

		assert.Nil(t, err)
		assert.Equal(t, capturedRequest.Header.Get("Authorization"), "Basic dXNlcjpwYXNzd29yZA==") // "user:password"
	})

	t.Run("Bearer token auth", func(t *testing.T) {
		cfg.Authentication.BearerToken = "token"

		prov, _ := NewPrometheusHistoryProvider(cfg)
		_, err := prov.GetClusterHistory()

		assert.Nil(t, err)
		assert.Equal(t, capturedRequest.Header.Get("Authorization"), "Bearer token")
	})

	t.Run("Basic auth and Bearer token auth are set at once", func(t *testing.T) {
		// if both auth methods are set we prefer Bearer token
		cfg.Authentication.BearerToken = "token"
		cfg.Authentication.Username = "user"
		cfg.Authentication.Password = "password"

		prov, _ := NewPrometheusHistoryProvider(cfg)
		_, err := prov.GetClusterHistory()

		assert.Nil(t, err)
		assert.NotContains(t, capturedRequest.Header.Get("Authorization"), "Basic")
		assert.Equal(t, capturedRequest.Header.Get("Authorization"), "Bearer token")
	})

	t.Run("Basic auth with username/password configured as env", func(t *testing.T) {
		// clear auth config so only environment variables are used in this test
		cfg.Authentication = PrometheusCredentials{}
		t.Setenv("PROMETHEUS_USERNAME", "prom_user")
		t.Setenv("PROMETHEUS_PASSWORD", "prom_password")

		prov, _ := NewPrometheusHistoryProvider(cfg)
		_, err := prov.GetClusterHistory()

		assert.Nil(t, err)
		assert.Equal(t, capturedRequest.Header.Get("Authorization"), "Basic cHJvbV91c2VyOnByb21fcGFzc3dvcmQ=") // "prom_user:prom_password"
	})
}
