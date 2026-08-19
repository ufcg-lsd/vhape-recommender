/*
This file defines how a heuristic configured in a VhapePolicy is turned into a
ResourceEstimator.

Heuristics are self-registering: each implementation calls RegisterHeuristic
from an init function, providing the name used in the policy and a factory that
decodes its own parameters. Adding a heuristic therefore only requires adding a
file to this package, with no central switch to update.
*/

package estimators

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// HeuristicSpec is the parsed configuration of a heuristic.
//
// Each heuristic is responsible for creating the estimator used for one
// resource, such as CPU or memory.
type HeuristicSpec interface {
	NewEstimator(resourceName model.ResourceName) ResourceEstimator
}

// HeuristicFactory decodes the raw parameters of a heuristic into its spec.
type HeuristicFactory func(config []byte) (HeuristicSpec, error)

// heuristics holds every registered heuristic, keyed by the name used in a
// VhapePolicy spec.
var heuristics = map[string]HeuristicFactory{}

// RegisterHeuristic makes a heuristic available to VhapePolicy specs.
//
// It is meant to be called from an init function by each heuristic
// implementation, and panics on a duplicated name since that can only be a
// programming error.
func RegisterHeuristic(name string, factory HeuristicFactory) {
	if _, exists := heuristics[name]; exists {
		panic(fmt.Sprintf("heuristic %q registered twice", name))
	}

	heuristics[name] = factory
}

// BuildHeuristic resolves the single heuristic configured for one resource,
// returning its spec and the name it was registered under.
//
// The parameters are validated by the VhapePolicy CRD schema, so decoding them
// is all that is left to do here.
func BuildHeuristic(configs map[string]runtime.RawExtension) (HeuristicSpec, string, error) {
	if len(configs) != 1 {
		return nil, "", fmt.Errorf("resource spec must define exactly one heuristic")
	}

	var name string
	var config runtime.RawExtension
	for heuristicName, heuristicConfig := range configs {
		name = heuristicName
		config = heuristicConfig
	}

	factory, ok := heuristics[name]
	if !ok {
		return nil, "", fmt.Errorf("unsupported heuristic %q", name)
	}

	spec, err := factory(config.Raw)
	if err != nil {
		return nil, "", fmt.Errorf("heuristic %q: %w", name, err)
	}

	return spec, name, nil
}
