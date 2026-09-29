package hpaservice_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	hpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/hpa_service"
)

func TestEnsureAverageValueForDeploymentConvertsUtilizationMetrics(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		"sidecar": {
			corev1.ResourceCPU: resource.MustParse("300m"),
		},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	hpa.Annotations = map[string]string{hpaservice.OriginalUtilizationAnnotation: `{"resource/cpu":50}`}

	service, client := newService(t, hpa)
	if err := service.EnsureAverageValueForDeployment(context.Background(), dep); err != nil {
		t.Fatalf("EnsureAverageValueForDeployment() error = %v", err)
	}

	updated, err := client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(context.Background(), hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get patched HPA: %v", err)
	}
	target := updated.Spec.Metrics[0].Resource.Target
	if target.Type != autoscalingv2.AverageValueMetricType {
		t.Fatalf("target type = %q, want %q", target.Type, autoscalingv2.AverageValueMetricType)
	}
	if target.AverageUtilization != nil {
		t.Fatalf("AverageUtilization = %v, want nil", *target.AverageUtilization)
	}
	if target.AverageValue == nil || target.AverageValue.MilliValue() != 300 {
		t.Fatalf("AverageValue = %v, want 300m", target.AverageValue)
	}

	var originals map[string]int32
	if err := json.Unmarshal([]byte(updated.Annotations[hpaservice.OriginalUtilizationAnnotation]), &originals); err != nil {
		t.Fatalf("decode original utilization annotation: %v", err)
	}
	if got := originals["resource/cpu"]; got != 75 {
		t.Fatalf("original CPU utilization = %d, want 75", got)
	}
}

func TestEnsureAverageValueForDeploymentConvertsContainerResourceMetric(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {
			corev1.ResourceCPU: resource.MustParse("400m"),
		},
		"sidecar": {
			corev1.ResourceCPU: resource.MustParse("900m"),
		},
	})
	hpa := containerResourceHPA("production", "api-hpa", "api", "api", corev1.ResourceCPU, 50)

	service, client := newService(t, hpa)
	if err := service.EnsureAverageValueForDeployment(context.Background(), dep); err != nil {
		t.Fatalf("EnsureAverageValueForDeployment() error = %v", err)
	}

	updated, err := client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(context.Background(), hpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get patched HPA: %v", err)
	}
	if got := updated.Spec.Metrics[0].ContainerResource.Target.AverageValue.MilliValue(); got != 200 {
		t.Fatalf("AverageValue = %dm, want 200m", got)
	}
}

func TestEnsureAverageValueForDeploymentDoesNothingWithoutMatchingHPA(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})
	hpa := resourceHPA("production", "other-hpa", "other", corev1.ResourceCPU, 75)

	service, client := newService(t, hpa)
	if err := service.EnsureAverageValueForDeployment(context.Background(), dep); err != nil {
		t.Fatalf("EnsureAverageValueForDeployment() error = %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("client actions = %#v, want none", actions)
	}
}

func TestEnsureAverageValueForDeploymentLeavesAverageValueMetricsUnchanged(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	average := resource.MustParse("75m")
	hpa.Spec.Metrics[0].Resource.Target = autoscalingv2.MetricTarget{
		Type:         autoscalingv2.AverageValueMetricType,
		AverageValue: &average,
	}

	service, client := newService(t, hpa)
	if err := service.EnsureAverageValueForDeployment(context.Background(), dep); err != nil {
		t.Fatalf("EnsureAverageValueForDeployment() error = %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("client actions = %#v, want none", actions)
	}
}

func TestEnsureAverageValueLeavesConvertedMetricUnchanged(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("400m")},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	oldAverage := resource.MustParse("75m")
	hpa.Spec.Metrics[0].Resource.Target = autoscalingv2.MetricTarget{
		Type:         autoscalingv2.AverageValueMetricType,
		AverageValue: &oldAverage,
	}
	hpa.Annotations = map[string]string{hpaservice.OriginalUtilizationAnnotation: `{"resource/cpu":75}`}

	service, client := newService(t, hpa)
	if _, err := service.EnsureAverageValue(context.Background(), hpa, dep); err != nil {
		t.Fatalf("EnsureAverageValue() error = %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("client actions = %#v, want none", actions)
	}
}

func TestNewHPAServiceValidatesDependencies(t *testing.T) {
	client := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(client, 0).Autoscaling().V2().HorizontalPodAutoscalers()

	if _, err := hpaservice.NewHPAService(nil, client); err == nil {
		t.Fatal("NewHPAService() error = nil, want error for nil informer")
	}
	if _, err := hpaservice.NewHPAService(informer, nil); err == nil {
		t.Fatal("NewHPAService() error = nil, want error for nil client")
	}
}

func TestEnsureAverageValueValidatesConversionInputs(t *testing.T) {
	validDeployment := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})

	tests := []struct {
		name       string
		hpa        *autoscalingv2.HorizontalPodAutoscaler
		deployment *appsv1.Deployment
	}{
		{
			name:       "rejects nil HPA",
			hpa:        nil,
			deployment: validDeployment,
		},
		{
			name:       "rejects nil deployment",
			hpa:        resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75),
			deployment: nil,
		},
		{
			name:       "rejects different namespaces",
			hpa:        resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75),
			deployment: deployment("staging", "api", map[string]corev1.ResourceList{"api": {corev1.ResourceCPU: resource.MustParse("100m")}}),
		},
		{
			name:       "rejects missing resource request",
			hpa:        resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75),
			deployment: deployment("production", "api", map[string]corev1.ResourceList{"api": {}}),
		},
		{
			name:       "rejects missing container resource request",
			hpa:        containerResourceHPA("production", "api-hpa", "api", "api", corev1.ResourceCPU, 75),
			deployment: deployment("production", "api", map[string]corev1.ResourceList{"api": {}}),
		},
		{
			name:       "rejects zero utilization",
			hpa:        resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 0),
			deployment: validDeployment,
		},
		{
			name: "rejects zero request",
			hpa:  resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75),
			deployment: deployment("production", "api", map[string]corev1.ResourceList{
				"api": {corev1.ResourceCPU: resource.MustParse("0")},
			}),
		},
		{
			name: "rejects request multiplication overflow",
			hpa:  resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 2),
			deployment: deployment("production", "api", map[string]corev1.ResourceList{
				"api": {corev1.ResourceCPU: resource.MustParse("9223372036854775807")},
			}),
		},
		{
			name: "rejects invalid saved utilization annotation",
			hpa: func() *autoscalingv2.HorizontalPodAutoscaler {
				hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
				hpa.Annotations = map[string]string{hpaservice.OriginalUtilizationAnnotation: "not-json"}
				return hpa
			}(),
			deployment: validDeployment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := newService(t, tt.hpa)
			if _, err := service.EnsureAverageValue(context.Background(), tt.hpa, tt.deployment); err == nil {
				t.Fatal("EnsureAverageValue() error = nil, want error")
			}
		})
	}
}

func TestEnsureAverageValueForDeploymentRejectsNilDeployment(t *testing.T) {
	service, _ := newService(t)
	if err := service.EnsureAverageValueForDeployment(context.Background(), nil); err == nil {
		t.Fatal("EnsureAverageValueForDeployment() error = nil, want error")
	}
}

func TestEnsureAverageValueForDeploymentReturnsConversionError(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	service, _ := newService(t, hpa)

	if err := service.EnsureAverageValueForDeployment(context.Background(), dep); err == nil {
		t.Fatal("EnsureAverageValueForDeployment() error = nil, want conversion error")
	}
}

func TestEnsureAverageValueReturnsPatchError(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	service, client := newService(t, hpa)
	client.PrependReactor("patch", "horizontalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch failed")
	})

	if _, err := service.EnsureAverageValue(context.Background(), hpa, dep); err == nil {
		t.Fatal("EnsureAverageValue() error = nil, want patch error")
	}
}

func TestEnsureAverageValueRejectsMissingContainerRequest(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})
	hpa := containerResourceHPA("production", "api-hpa", "api", "sidecar", corev1.ResourceCPU, 75)
	service, _ := newService(t, hpa)

	if _, err := service.EnsureAverageValue(context.Background(), hpa, dep); err == nil {
		t.Fatal("EnsureAverageValue() error = nil, want missing container request error")
	}
}

func TestEnsureAverageValueIgnoresUnsupportedMetric(t *testing.T) {
	dep := deployment("production", "api", map[string]corev1.ResourceList{
		"api": {corev1.ResourceCPU: resource.MustParse("100m")},
	})
	hpa := resourceHPA("production", "api-hpa", "api", corev1.ResourceCPU, 75)
	hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{Type: autoscalingv2.ObjectMetricSourceType}}
	service, client := newService(t, hpa)

	if _, err := service.EnsureAverageValue(context.Background(), hpa, dep); err != nil {
		t.Fatalf("EnsureAverageValue() error = %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("client actions = %#v, want none", actions)
	}
}

func newService(t *testing.T, hpas ...*autoscalingv2.HorizontalPodAutoscaler) (*hpaservice.HPAService, *kubefake.Clientset) {
	t.Helper()

	client := kubefake.NewSimpleClientset()
	factory := kubeinformers.NewSharedInformerFactory(client, 0)
	informer := factory.Autoscaling().V2().HorizontalPodAutoscalers()
	for _, hpa := range hpas {
		if hpa == nil {
			continue
		}
		if err := informer.Informer().GetIndexer().Add(hpa); err != nil {
			t.Fatalf("add HPA to informer: %v", err)
		}
		if err := client.Tracker().Add(hpa.DeepCopy()); err != nil {
			t.Fatalf("add HPA to fake client: %v", err)
		}
	}

	service, err := hpaservice.NewHPAService(informer, client)
	if err != nil {
		t.Fatalf("NewHPAService() error = %v", err)
	}
	return service, client
}

func deployment(namespace, name string, requests map[string]corev1.ResourceList) *appsv1.Deployment {
	containers := make([]corev1.Container, 0, len(requests))
	for name, resources := range requests {
		containers = append(containers, corev1.Container{
			Name:      name,
			Resources: corev1.ResourceRequirements{Requests: resources},
		})
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: containers}}},
	}
}

func resourceHPA(namespace, name, target string, resourceName corev1.ResourceName, utilization int32) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "Deployment", Name: target},
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name:   resourceName,
					Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &utilization},
				},
			}},
		},
	}
}

func containerResourceHPA(namespace, name, target, container string, resourceName corev1.ResourceName, utilization int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := resourceHPA(namespace, name, target, resourceName, utilization)
	hpa.Spec.Metrics[0] = autoscalingv2.MetricSpec{
		Type: autoscalingv2.ContainerResourceMetricSourceType,
		ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
			Name:      resourceName,
			Container: container,
			Target:    autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &utilization},
		},
	}
	return hpa
}
