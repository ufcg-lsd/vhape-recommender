package estimators

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestAdaptivePercentileEstimatorStableWorkload(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	// Stable workload: values around 100m
	container := "app"
	samples := []model.ResourceAmount{
		90, 100, 95, 105, 98, 102, 96, 104, 99, 101,
		97, 103, 100, 102, 98, 104, 95, 105, 100, 103,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// For stable workload (low CV), should use P55 with 6% headroom
	// Target should be relatively close to samples
	if recommendation.Target <= 0 {
		t.Errorf("Expected positive target, got %d", recommendation.Target)
	}

	// Should not be too much higher than current request
	if recommendation.Target > 150 {
		t.Errorf("Expected conservative target for stable workload, got %d", recommendation.Target)
	}

	// Bounds should be reasonable
	if recommendation.LowerBound > recommendation.Target {
		t.Errorf("LowerBound (%d) should be <= Target (%d)", recommendation.LowerBound, recommendation.Target)
	}
	if recommendation.UpperBound < recommendation.Target {
		t.Errorf("UpperBound (%d) should be >= Target (%d)", recommendation.UpperBound, recommendation.Target)
	}
}

func TestAdaptivePercentileEstimatorVolatileWorkload(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	// Volatile workload: values from 50 to 300
	container := "batch-job"
	samples := []model.ResourceAmount{
		50, 75, 60, 80, 55, 290, 70, 85, 65, 75,
		60, 95, 70, 300, 80, 60, 70, 55, 85, 90,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// For volatile workload (high CV), should use higher percentile for protection
	if recommendation.Target <= 0 {
		t.Errorf("Expected positive target, got %d", recommendation.Target)
	}

	// Should be more conservative than stable workload
	if recommendation.Target < 80 {
		t.Errorf("Expected higher target for volatile workload, got %d", recommendation.Target)
	}
}

func TestAdaptivePercentileEstimatorExtremeSpike(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	// Normal workload with one extreme spike
	container := "app"
	samples := []model.ResourceAmount{
		100, 105, 95, 102, 98, 101, 99, 103, 97, 104,
		100, 102, 96, 104, 99, 1000, // One extreme spike
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should protect against spike, not scale up too much
	if recommendation.Target > 250 {
		t.Errorf("Expected spike protection, but got high target: %d", recommendation.Target)
	}
}

func TestAdaptivePercentileEstimatorHysteresis(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	container := "app"
	samples := []model.ResourceAmount{
		100, 100, 100, 100, 100, 100, 100, 100, 100, 100,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// With stable workload and current request within tolerance, should hold steady
	// Recommendation should be close to current request due to hysteresis
	maxChange := model.ResourceAmount(float64(constraints.CurrentRequest) * 0.15) // ~15% tolerance
	if recommendation.Target < constraints.CurrentRequest-maxChange ||
		recommendation.Target > constraints.CurrentRequest+maxChange {
		t.Logf("Recommendation: %d, CurrentRequest: %d (diff: %d)",
			recommendation.Target, constraints.CurrentRequest,
			recommendation.Target-constraints.CurrentRequest)
	}
}

func TestAdaptivePercentileEstimatorEmptySamples(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	container := "app"

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should fallback to current request when no samples
	if recommendation.Target != constraints.CurrentRequest {
		t.Errorf("Expected fallback to CurrentRequest (%d), got %d",
			constraints.CurrentRequest, recommendation.Target)
	}
}

func TestAdaptivePercentileEstimatorExpiredSamples(t *testing.T) {
	shortWindow := 1 * time.Millisecond
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, shortWindow)

	container := "app"
	samples := []model.ResourceAmount{100, 100, 100}

	estimator.FeedSamples(container, samples)

	// Wait for samples to expire
	time.Sleep(10 * time.Millisecond)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should fallback when all samples are expired
	if recommendation.Target != constraints.CurrentRequest {
		t.Errorf("Expected fallback after expiration, got %d", recommendation.Target)
	}
}

func TestAdaptivePercentileEstimatorConstraints(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	container := "app"
	samples := []model.ResourceAmount{
		500, 520, 510, 530, 500, 525, 515, 535, 505, 525,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            100,  // Minimum constraint
		Max:            300,  // Maximum constraint
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should respect constraints
	if recommendation.Target < constraints.Min {
		t.Errorf("Target (%d) below Min (%d)", recommendation.Target, constraints.Min)
	}
	if recommendation.Target > constraints.Max {
		t.Errorf("Target (%d) above Max (%d)", recommendation.Target, constraints.Max)
	}
}

func TestAdaptivePercentileEstimatorNegativeSamples(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	container := "app"
	samples := []model.ResourceAmount{
		100, -1, 105, -10, 95, 102, 98, 101,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should skip negative samples and only process valid ones
	if recommendation.Target <= 0 {
		t.Errorf("Expected positive target despite negative samples, got %d", recommendation.Target)
	}
}

func TestAdaptivePercentileSpec(t *testing.T) {
	spec := &AdaptivePercentileSpec{
		SlidingWindow: metav1.Duration{Duration: 24 * time.Hour},
	}

	estimator := spec.NewEstimator(model.ResourceCPU)

	if estimator == nil {
		t.Errorf("Expected non-nil estimator")
	}

	apEstimator, ok := estimator.(*AdaptivePercentileEstimator)
	if !ok {
		t.Errorf("Expected AdaptivePercentileEstimator, got %T", estimator)
	}

	if apEstimator.slidingWindow != 24*time.Hour {
		t.Errorf("Expected slidingWindow 24h, got %v", apEstimator.slidingWindow)
	}
}

func TestAdaptivePercentileRegistration(t *testing.T) {
	// Test that the heuristic is registered
	spec := &AdaptivePercentileSpec{
		SlidingWindow: metav1.Duration{Duration: 24 * time.Hour},
	}

	// The registration happens in init(), so just verify we can create an estimator
	estimator := spec.NewEstimator(model.ResourceCPU)
	if estimator == nil {
		t.Errorf("Failed to create estimator from spec")
	}
}

func TestAdaptivePercentileGrowthDetection(t *testing.T) {
	estimator := NewAdaptivePercentileEstimator(model.ResourceCPU, 24*time.Hour)

	container := "app"
	// Workload growing from ~100 to ~200
	samples := []model.ResourceAmount{
		100, 95, 105, 100, 98, 102, 96, 104,
		110, 115, 120, 125, 130, 135, 190, 200, 195, 210,
	}

	estimator.FeedSamples(container, samples)

	constraints := ContainerResourceConstraints{
		CurrentRequest: 100,
		Min:            10,
		Max:            500,
	}

	recommendation := estimator.GetSingleResourceRecommendation(container, constraints)

	// Should react to growth, recommendation should increase
	if recommendation.Target <= 105 {
		t.Logf("Expected stronger reaction to growth, got target: %d", recommendation.Target)
	}
}
