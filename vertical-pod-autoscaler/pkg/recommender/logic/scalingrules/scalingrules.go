// Package scalingrules turns scalingRules from a VhapePolicy into recommendation
// transformations. Rules register themselves, so adding one does not require a
// central dispatcher to be changed.
package scalingrules

import (
	"fmt"

	vhape_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/recommendation"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// ScalingRule transforms a recommendation using the request captured when the
// VPA was first observed. It must not modify UncappedTarget.
type ScalingRule interface {
	Apply(recommendation.SingleResourceRecommendation, model.ResourceAmount) recommendation.SingleResourceRecommendation
}

// Factory decodes the parameters of one registered scaling rule.
type Factory func([]byte) (ScalingRule, error)

var registeredRules = map[string]Factory{}

// Register makes a scaling rule available to VhapePolicy specs. It is intended
// for use from the rule implementation's init function.
func Register(name string, factory Factory) {
	if _, exists := registeredRules[name]; exists {
		panic(fmt.Sprintf("scaling rule %q registered twice", name))
	}
	registeredRules[name] = factory
}

// Build resolves the scaling rules for one resource in policy order.
func Build(configs []vhape_types.ScalingRule) ([]ScalingRule, error) {
	rules := make([]ScalingRule, 0, len(configs))
	for _, configuredRule := range configs {
		if len(configuredRule) != 1 {
			return nil, fmt.Errorf("each scalingRules item must contain exactly one rule")
		}
		for name, config := range configuredRule {
			factory, found := registeredRules[name]
			if !found {
				return nil, fmt.Errorf("unsupported scaling rule %q", name)
			}
			rule, err := factory(config.Raw)
			if err != nil {
				return nil, fmt.Errorf("scaling rule %q: %w", name, err)
			}
			rules = append(rules, rule)
		}
	}
	return rules, nil
}
