package scalingrules

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	vhape_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
)

func registerTestRule(t *testing.T, name string, factory Factory) {
	t.Helper()
	Register(name, factory)
	t.Cleanup(func() { delete(registeredRules, name) })
}

func TestBuiltInScalingRulesAreRegistered(t *testing.T) {
	assert.Contains(t, registeredRules, RequestCeilingRule)
	assert.Contains(t, registeredRules, RequestFloorRule)
}

func TestBuildResolvesRegisteredRule(t *testing.T) {
	registerTestRule(t, "fake", func([]byte) (ScalingRule, error) {
		return requestFloor{minimum: 0.5}, nil
	})

	rules, err := Build([]vhape_types.ScalingRule{{"fake": runtime.RawExtension{Raw: []byte(`{}`)}}})
	assert.NoError(t, err)
	assert.Equal(t, []ScalingRule{requestFloor{minimum: 0.5}}, rules)
}

func TestBuildRejectsUnknownRule(t *testing.T) {
	_, err := Build([]vhape_types.ScalingRule{{"unknown": runtime.RawExtension{Raw: []byte(`{}`)}}})
	assert.ErrorContains(t, err, `unsupported scaling rule "unknown"`)
}

func TestRegisterPanicsOnDuplicatedName(t *testing.T) {
	registerTestRule(t, "fake", func([]byte) (ScalingRule, error) { return requestFloor{}, nil })
	assert.PanicsWithValue(t, `scaling rule "fake" registered twice`, func() {
		Register("fake", func([]byte) (ScalingRule, error) { return nil, fmt.Errorf("unreachable") })
	})
}
