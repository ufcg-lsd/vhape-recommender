package recommender

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/client_golang/prometheus/testutil/promlint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var testVPA = VPARef{
	Namespace:        "shop",
	Name:             "api-vpa",
	TargetAPIVersion: "apps/v1",
	TargetKind:       "Deployment",
	TargetName:       "api",
}

func testResources(cpu, memory string) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(memory),
	}
}

func testRecommendation() *vpa_types.RecommendedPodResources {
	return &vpa_types.RecommendedPodResources{
		ContainerRecommendations: []vpa_types.RecommendedContainerResources{{
			ContainerName: "app",
			Target:        testResources("500m", "256Mi"),
			LowerBound:    testResources("450m", "200Mi"),
			UpperBound:    testResources("550m", "300Mi"),
		}},
	}
}

func newRecommendationRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(recommendationTarget, recommendationLowerBound, recommendationUpperBound)
	return reg
}

func TestRecordRecommendationExportsValuesInBaseUnits(t *testing.T) {
	ResetRecommendations()
	RecordRecommendation(testVPA, testRecommendation())

	cpu := testVPA.recommendationLabels("app", "cpu", "core")
	memory := testVPA.recommendationLabels("app", "memory", "byte")

	assert.Equal(t, 0.5, testutil.ToFloat64(recommendationTarget.WithLabelValues(cpu...)))
	assert.Equal(t, 0.45, testutil.ToFloat64(recommendationLowerBound.WithLabelValues(cpu...)))
	assert.Equal(t, 0.55, testutil.ToFloat64(recommendationUpperBound.WithLabelValues(cpu...)))
	assert.Equal(t, float64(256*1024*1024), testutil.ToFloat64(recommendationTarget.WithLabelValues(memory...)))
	assert.Equal(t, float64(200*1024*1024), testutil.ToFloat64(recommendationLowerBound.WithLabelValues(memory...)))
	assert.Equal(t, float64(300*1024*1024), testutil.ToFloat64(recommendationUpperBound.WithLabelValues(memory...)))
}

func TestRecordRecommendationIgnoresNil(t *testing.T) {
	ResetRecommendations()
	RecordRecommendation(testVPA, nil)

	assert.Equal(t, 0, testutil.CollectAndCount(recommendationTarget))
}

func TestResetRecommendationsRemovesSeries(t *testing.T) {
	RecordRecommendation(testVPA, testRecommendation())
	ResetRecommendations()

	assert.Equal(t, 0, testutil.CollectAndCount(recommendationTarget))
	assert.Equal(t, 0, testutil.CollectAndCount(recommendationLowerBound))
	assert.Equal(t, 0, testutil.CollectAndCount(recommendationUpperBound))
}

func TestRecommendationsAreServedOnMetricsEndpoint(t *testing.T) {
	ResetRecommendations()
	reg := newRecommendationRegistry()
	RecordRecommendation(testVPA, testRecommendation())

	server := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/metrics")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	for _, name := range []string{
		"vhape_recommender_recommendation_target",
		"vhape_recommender_recommendation_lowerbound",
		"vhape_recommender_recommendation_upperbound",
	} {
		assert.Contains(t, string(body), "\n"+name+"{", "metric %s not served", name)
	}
}

func TestRecommendationMetricNamesFollowPrometheusConventions(t *testing.T) {
	ResetRecommendations()
	reg := newRecommendationRegistry()
	RecordRecommendation(testVPA, testRecommendation())

	families, err := reg.Gather()
	require.NoError(t, err)

	problems, err := promlint.NewWithMetricFamilies(families).Lint()
	require.NoError(t, err)

	messages := []string{}
	for _, problem := range problems {
		messages = append(messages, problem.Metric+": "+problem.Text)
	}
	assert.Empty(t, messages, strings.Join(messages, "\n"))
}
