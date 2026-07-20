package testutil

import vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

const (
	TestDeploymentAPIVersion = "apps/v1"
	TestDeploymentKind       = "Deployment"

	TestVPAAPIVersion = "autoscaling.k8s.io/v1"
	TestVPAKind       = "VerticalPodAutoscaler"

	TestManagedByLabel = "app.kubernetes.io/managed-by"
	TestManagedByValue = "vhape-watcher"

	TestVhapePolicyAnnotation = "vhape/policy"
	TestVhapeRecommenderName  = "vhape-recommender"

	TestNamespace      = "producao"
	TestDeploymentName = "api"
	TestVPAName        = "vpa-api"

	TestPolicyNamespace = "kube-system"
	TestPolicyName      = "vhape-policy-p93-default"

	TestVPAUpdateMode vpav1.UpdateMode = vpav1.UpdateModeInPlaceOrRecreate
)
