package scalingrules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestRequestFloor(t *testing.T) {
	rule, err := newRequestFloor([]byte(`{"minimum":"150%"}`))
	assert.NoError(t, err)

	got := rule.Apply(recommendation.SingleResourceRecommendation{
		Target: 120, LowerBound: 50, UpperBound: 200, UncappedTarget: 120,
	}, model.ResourceAmount(100))

	assert.Equal(t, recommendation.SingleResourceRecommendation{
		Target: 150, LowerBound: 150, UpperBound: 200, UncappedTarget: 120,
	}, got)
}

func TestRequestFloorLeavesValuesAboveLimit(t *testing.T) {
	rule, err := newRequestFloor([]byte(`{"minimum":"150%"}`))
	assert.NoError(t, err)

	want := recommendation.SingleResourceRecommendation{
		Target: 150, LowerBound: 150, UpperBound: 200, UncappedTarget: 120,
	}
	assert.Equal(t, want, rule.Apply(want, model.ResourceAmount(100)))
}

func TestRequestFloorRejectsInvalidParameters(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "malformed JSON", config: `{`, wantErr: "decode parameters"},
		{name: "missing minimum", config: `{}`, wantErr: `missing "minimum"`},
		{name: "additional field", config: `{"minimum":"150%","extra":"value"}`, wantErr: `unknown field "extra"`},
		{name: "not a percentage", config: `{"minimum":"2"}`, wantErr: "must be a percentage"},
		{name: "negative percentage", config: `{"minimum":"-1%"}`, wantErr: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRequestFloor([]byte(tt.config))
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
