package scalingrules

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestSelectScalingRule(t *testing.T) {
	assert.IsType(t, &BlockScaleUp{}, SelectScalingRule(BlockScaleUpRule))
	assert.IsType(t, &BlockScaleDown{}, SelectScalingRule(BlockScaleDownRule))
	assert.Nil(t, SelectScalingRule("unknown"))
}

func TestBlockScaleUp(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         120,
		LowerBound:     90,
		UpperBound:     130,
		UncappedTarget: 140,
	}

	got := (&BlockScaleUp{}).Apply(rec, "app", model.ResourceCPU, 100)

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, model.ResourceAmount(90), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(100), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(140), got.UncappedTarget)
}

func TestBlockScaleUpLeavesRecommendationWithoutCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         120,
		LowerBound:     90,
		UpperBound:     130,
		UncappedTarget: 140,
	}

	got := (&BlockScaleUp{}).Apply(rec, "app", model.ResourceCPU, 0)

	assert.Equal(t, rec, got)
}

func TestBlockScaleDown(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         80,
		LowerBound:     70,
		UpperBound:     130,
		UncappedTarget: 60,
	}

	got := (&BlockScaleDown{}).Apply(rec, "app", model.ResourceCPU, 100)

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, model.ResourceAmount(100), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(130), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(60), got.UncappedTarget)
}

func TestBlockScaleDownLeavesRecommendationWithoutCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         80,
		LowerBound:     70,
		UpperBound:     130,
		UncappedTarget: 60,
	}

	got := (&BlockScaleDown{}).Apply(rec, "app", model.ResourceCPU, 0)

	assert.Equal(t, rec, got)
}

func TestBlockScaleUpAllValuesAboveCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         150,
		LowerBound:     120,
		UpperBound:     200,
		UncappedTarget: 150,
	}

	got := (&BlockScaleUp{}).Apply(rec, "app", model.ResourceCPU, 100)

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, model.ResourceAmount(100), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(100), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(150), got.UncappedTarget)
}

func TestBlockScaleDownAllValuesBelowCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         30,
		LowerBound:     20,
		UpperBound:     50,
		UncappedTarget: 30,
	}

	got := (&BlockScaleDown{}).Apply(rec, "app", model.ResourceCPU, 100)

	assert.Equal(t, model.ResourceAmount(100), got.Target)
	assert.Equal(t, model.ResourceAmount(100), got.LowerBound)
	assert.Equal(t, model.ResourceAmount(100), got.UpperBound)
	assert.Equal(t, model.ResourceAmount(30), got.UncappedTarget)
}

func TestBlockScaleUpNegativeCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         150,
		LowerBound:     120,
		UpperBound:     200,
		UncappedTarget: 150,
	}

	got := (&BlockScaleUp{}).Apply(rec, "app", model.ResourceCPU, -5)

	assert.Equal(t, rec, got)
}

func TestSelectScalingRuleEmptyString(t *testing.T) {
	assert.Nil(t, SelectScalingRule(""))
}

func TestBlockScaleUpNoChangeWhenAlreadyAtCurrentRequest(t *testing.T) {
	rec := recommendation.SingleResourceRecommendation{
		Target:         100,
		LowerBound:     80,
		UpperBound:     100,
		UncappedTarget: 120,
	}

	got := (&BlockScaleUp{}).Apply(rec, "app", model.ResourceCPU, 100)

	assert.Equal(t, rec, got)
}
