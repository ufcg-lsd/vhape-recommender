package testutil

import vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

const (
	TestNamespace      = "producao"
	TestDeploymentName = "api"
	TestVPAName        = "vpa-api"

	TestPolicyName = "p93-percentile-hysteresis"

	TestVPAUpdateMode vpav1.UpdateMode = vpav1.UpdateModeInPlaceOrRecreate
)
