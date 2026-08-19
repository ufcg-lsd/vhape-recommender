package estimators

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// fakeHeuristicSpec is a heuristic used to exercise the registry without
// depending on a real implementation.
type fakeHeuristicSpec struct {
	Value int `json:"value"`
}

func (s *fakeHeuristicSpec) NewEstimator(_ model.ResourceName) ResourceEstimator {
	return nil
}

// registerTestHeuristic registers a heuristic for the duration of one test,
// keeping the global registry unchanged for the others.
func registerTestHeuristic(t *testing.T, name string, factory HeuristicFactory) {
	t.Helper()

	RegisterHeuristic(name, factory)
	t.Cleanup(func() {
		delete(heuristics, name)
	})
}

func TestBuiltInHeuristicsAreRegistered(t *testing.T) {
	assert.Contains(t, heuristics, PercentileHysteresis)
}

func TestBuildHeuristicResolvesRegisteredHeuristic(t *testing.T) {
	registerTestHeuristic(t, "fake", func(config []byte) (HeuristicSpec, error) {
		spec := &fakeHeuristicSpec{}
		if err := json.Unmarshal(config, spec); err != nil {
			return nil, err
		}
		return spec, nil
	})

	spec, name, err := BuildHeuristic(map[string]runtime.RawExtension{
		"fake": {Raw: []byte(`{"value":7}`)},
	})

	assert.NoError(t, err)
	assert.Equal(t, "fake", name)
	assert.Equal(t, &fakeHeuristicSpec{Value: 7}, spec)
}

func TestBuildHeuristicRejectsUnknownHeuristic(t *testing.T) {
	_, _, err := BuildHeuristic(map[string]runtime.RawExtension{
		"unknown": {Raw: []byte(`{}`)},
	})

	assert.ErrorContains(t, err, `unsupported heuristic "unknown"`)
}

func TestBuildHeuristicRequiresExactlyOneHeuristic(t *testing.T) {
	registerTestHeuristic(t, "fake", func([]byte) (HeuristicSpec, error) {
		return &fakeHeuristicSpec{}, nil
	})

	tests := []struct {
		name    string
		configs map[string]runtime.RawExtension
	}{
		{
			name:    "no resource spec",
			configs: nil,
		},
		{
			name:    "no heuristic",
			configs: map[string]runtime.RawExtension{},
		},
		{
			name: "two heuristics",
			configs: map[string]runtime.RawExtension{
				"fake":               {Raw: []byte(`{}`)},
				PercentileHysteresis: {Raw: []byte(`{}`)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildHeuristic(tt.configs)
			assert.ErrorContains(t, err, "resource spec must define exactly one heuristic")
		})
	}
}

func TestBuildHeuristicPropagatesFactoryError(t *testing.T) {
	factoryError := fmt.Errorf("cannot decode")
	registerTestHeuristic(t, "fake", func([]byte) (HeuristicSpec, error) {
		return nil, factoryError
	})

	_, _, err := BuildHeuristic(map[string]runtime.RawExtension{
		"fake": {Raw: []byte(`{}`)},
	})

	assert.ErrorIs(t, err, factoryError)
	assert.ErrorContains(t, err, `heuristic "fake"`)
}

func TestRegisterHeuristicPanicsOnDuplicatedName(t *testing.T) {
	registerTestHeuristic(t, "fake", func([]byte) (HeuristicSpec, error) {
		return &fakeHeuristicSpec{}, nil
	})

	assert.PanicsWithValue(t, `heuristic "fake" registered twice`, func() {
		RegisterHeuristic("fake", func([]byte) (HeuristicSpec, error) {
			return &fakeHeuristicSpec{}, nil
		})
	})
}
