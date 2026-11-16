package metrics

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	k8sapiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/input/history"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// prometheusMetricsClient is the Prometheus source of metrics.
type prometheusMetricsClient struct {
	prometheusClient prometheusv1.API
	options          prometheusClientOptions
	now              func() time.Time

	mu        sync.Mutex
	cached    *v1beta1.PodMetricsList
	cachedNs  string
	fetchedAt time.Time
}

// prometheusClientOptions specifies parameters for reading the current usage from Prometheus.
type prometheusClientOptions struct {
	// Timeout of each query.
	QueryTimeout time.Duration
	// Range of the CPU usage rate.
	CPURateWindow time.Duration
	// Job that scrapes the cAdvisor metrics. Empty matches any job.
	CadvisorMetricsJobName string
	// Labels to use for the container namespace, pod name and container name.
	CtrNamespaceLabel, CtrPodNameLabel, CtrNameLabel string
	// How long a result is reused. The VHAPE recommender lists the metrics once
	// per VPA in each cycle, and without it every VPA would query Prometheus.
	CacheTTL time.Duration
}

// prometheusUsageQuery is the query that reads the usage of one resource.
type prometheusUsageQuery struct {
	resourceName k8sapiv1.ResourceName
	query        string
}

// NewPrometheusClient returns a Source for a Prometheus server. It reads the
// same Prometheus configuration as the history, whose CPURateWindow is the
// range of the CPU usage rate. Results are reused for cacheTTL.
func NewPrometheusClient(config history.PrometheusHistoryProviderConfig, cacheTTL time.Duration) PodMetricsLister {
	if config.CPURateWindow <= 0 {
		klog.ErrorS(nil, "Prometheus metrics source needs a CPU usage rate window", "cpuRateWindow", config.CPURateWindow)
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	prometheusClient, err := history.NewPrometheusAPI(history.PrometheusClientConfig{
		Address:        config.Address,
		Insecure:       config.Insecure,
		Authentication: config.Authentication,
	})
	if err != nil {
		klog.ErrorS(err, "Failed initializing Prometheus client")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	return newPrometheusClient(prometheusClient, prometheusClientOptions{
		QueryTimeout:           config.QueryTimeout,
		CPURateWindow:          config.CPURateWindow,
		CadvisorMetricsJobName: config.CadvisorMetricsJobName,
		CtrNamespaceLabel:      config.CtrNamespaceLabel,
		CtrPodNameLabel:        config.CtrPodNameLabel,
		CtrNameLabel:           config.CtrNameLabel,
		CacheTTL:               cacheTTL,
	})
}

func newPrometheusClient(prometheusClient prometheusv1.API, options prometheusClientOptions) *prometheusMetricsClient {
	return &prometheusMetricsClient{
		prometheusClient: prometheusClient,
		options:          options,
		now:              time.Now,
	}
}

// List returns the usage of every container in the namespace, or in all
// namespaces if it is empty. The returned list can be shared with other
// callers, so it must be treated as read-only.
func (c *prometheusMetricsClient) List(ctx context.Context, namespace string, _ v1.ListOptions) (*v1beta1.PodMetricsList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached != nil && c.cachedNs == namespace && c.now().Sub(c.fetchedAt) < c.options.CacheTTL {
		return c.cached, nil
	}

	queryTime := c.now()
	usage := make(map[model.ContainerID]k8sapiv1.ResourceList)
	queries := c.usageQueries(namespace)
	for _, q := range queries {
		klog.V(4).InfoS("Prometheus usage query", "resource", q.resourceName, "query", q.query)
		if err := c.readUsage(ctx, q, queryTime, usage); err != nil {
			return nil, err
		}
	}

	pods := make(map[model.PodID]*v1beta1.PodMetrics)
	skipped := 0
	for containerID, resources := range usage {
		// A container that misses a resource, such as a CPU rate that still
		// needs a second scrape, would be read as zero usage of it.
		if len(resources) != len(queries) {
			skipped++
			continue
		}
		pod, found := pods[containerID.PodID]
		if !found {
			pod = &v1beta1.PodMetrics{
				ObjectMeta: v1.ObjectMeta{Namespace: containerID.Namespace, Name: containerID.PodName},
				Timestamp:  v1.Time{Time: queryTime},
				Window:     v1.Duration{Duration: c.options.CPURateWindow},
			}
			pods[containerID.PodID] = pod
		}
		pod.Containers = append(pod.Containers, v1beta1.ContainerMetrics{Name: containerID.ContainerName, Usage: resources})
	}
	if skipped > 0 {
		klog.V(3).InfoS("Skipped containers without usage of every resource", "containers", skipped)
	}

	result := &v1beta1.PodMetricsList{Items: make([]v1beta1.PodMetrics, 0, len(pods))}
	for _, pod := range pods {
		result.Items = append(result.Items, *pod)
	}

	c.cached, c.cachedNs, c.fetchedAt = result, namespace, queryTime
	return result, nil
}

// usageQueries returns the queries that read the usage of every resource.
func (c *prometheusMetricsClient) usageQueries(namespace string) []prometheusUsageQuery {
	var selector string
	if c.options.CadvisorMetricsJobName != "" {
		selector = fmt.Sprintf("job=\"%s\", ", c.options.CadvisorMetricsJobName)
	}
	selector = selector + fmt.Sprintf("%s=~\".+\", %s!=\"POD\", %s!=\"\"",
		c.options.CtrPodNameLabel, c.options.CtrNameLabel, c.options.CtrNameLabel)
	if namespace != "" {
		selector = fmt.Sprintf("%s, %s=\"%s\"", selector, c.options.CtrNamespaceLabel, namespace)
	}

	// max rather than sum, so that a container scraped by two jobs is not counted twice.
	by := fmt.Sprintf("%s, %s, %s", c.options.CtrNamespaceLabel, c.options.CtrPodNameLabel, c.options.CtrNameLabel)
	return []prometheusUsageQuery{
		{
			resourceName: k8sapiv1.ResourceCPU,
			query: fmt.Sprintf("max by (%s) (rate(container_cpu_usage_seconds_total{%s}[%s]))",
				by, selector, prommodel.Duration(c.options.CPURateWindow)),
		},
		{
			resourceName: k8sapiv1.ResourceMemory,
			query:        fmt.Sprintf("max by (%s) (container_memory_working_set_bytes{%s})", by, selector),
		},
	}
}

// readUsage runs the query at the given time and adds the usage of each container to usage.
func (c *prometheusMetricsClient) readUsage(ctx context.Context, q prometheusUsageQuery, queryTime time.Time, usage map[model.ContainerID]k8sapiv1.ResourceList) error {
	ctx, cancel := context.WithTimeout(ctx, c.options.QueryTimeout)
	defer cancel()

	result, warnings, err := c.prometheusClient.Query(ctx, q.query, queryTime)
	if err != nil {
		return fmt.Errorf("cannot get %v usage from Prometheus: %w", q.resourceName, err)
	}
	if len(warnings) > 0 {
		klog.V(2).InfoS("Prometheus usage query returned warnings", "resource", q.resourceName, "warnings", warnings)
	}
	vector, ok := result.(prommodel.Vector)
	if !ok {
		return fmt.Errorf("expected query to return a vector; got result type %T", result)
	}

	for _, sample := range vector {
		containerID, ok := c.getContainerIDFromLabels(sample.Metric)
		if !ok {
			klog.V(4).InfoS("Skipping Prometheus sample without container labels", "metric", sample.Metric)
			continue
		}
		value := float64(sample.Value)
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			klog.V(4).InfoS("Skipping invalid Prometheus sample", "container", containerID, "resource", q.resourceName, "value", value)
			continue
		}
		if usage[containerID] == nil {
			usage[containerID] = make(k8sapiv1.ResourceList)
		}
		usage[containerID][q.resourceName] = quantityFromValue(value, q.resourceName)
	}
	return nil
}

func (c *prometheusMetricsClient) getContainerIDFromLabels(metric prommodel.Metric) (model.ContainerID, bool) {
	namespace := metric[prommodel.LabelName(c.options.CtrNamespaceLabel)]
	podName := metric[prommodel.LabelName(c.options.CtrPodNameLabel)]
	containerName := metric[prommodel.LabelName(c.options.CtrNameLabel)]
	if namespace == "" || podName == "" || containerName == "" {
		return model.ContainerID{}, false
	}
	return model.ContainerID{
		PodID:         model.PodID{Namespace: string(namespace), PodName: string(podName)},
		ContainerName: string(containerName),
	}, true
}

// quantityFromValue converts a value in cores for CPU and in bytes for memory,
// which is the unit of the metrics queried from Prometheus. CPU keeps nanocores,
// as metrics-server does, so both sources are rounded the same way to millicores.
func quantityFromValue(value float64, resourceName k8sapiv1.ResourceName) resource.Quantity {
	switch resourceName {
	case k8sapiv1.ResourceCPU:
		return *resource.NewScaledQuantity(int64(math.Round(value*1e9)), resource.Nano)
	case k8sapiv1.ResourceMemory:
		return *resource.NewQuantity(int64(math.Round(value)), resource.BinarySI)
	default:
		return resource.Quantity{}
	}
}
