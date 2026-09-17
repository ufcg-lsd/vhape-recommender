package scalingrules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestRequestCeiling(t *testing.T) {
	rule, err := newRequestCeiling([]byte(`{"maximum":"200%"}`))
	assert.NoError(t, err)

	got := rule.Apply(recommendation.SingleResourceRecommendation{
		Target: 250, LowerBound: 50, UpperBound: 300, UncappedTarget: 250,
	}, model.ResourceAmount(100))

	assert.Equal(t, recommendation.SingleResourceRecommendation{
		Target: 200, LowerBound: 50, UpperBound: 200, UncappedTarget: 250,
	}, got)
}

func TestRequestCeilingLeavesValuesWithinLimit(t *testing.T) {
	rule, err := newRequestCeiling([]byte(`{"maximum":"200%"}`))
	assert.NoError(t, err)

	want := recommendation.SingleResourceRecommendation{
		Target: 150, LowerBound: 50, UpperBound: 200, UncappedTarget: 250,
	}
	assert.Equal(t, want, rule.Apply(want, model.ResourceAmount(100)))
}

func TestRequestCeilingRejectsInvalidParameters(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "malformed JSON", config: `{`, wantErr: "decode parameters"},
		{name: "missing maximum", config: `{}`, wantErr: `missing "maximum"`},
		{name: "additional field", config: `{"maximum":"200%","extra":"value"}`, wantErr: `unknown field "extra"`},
		{name: "not a percentage", config: `{"maximum":"2"}`, wantErr: "must be a percentage"},
		{name: "negative percentage", config: `{"maximum":"-1%"}`, wantErr: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRequestCeiling([]byte(tt.config))
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
