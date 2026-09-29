package hpaservice

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	hpainformers "k8s.io/client-go/informers/autoscaling/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	vpaservice "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/vhapewatcher/vpa_service"
)

const OriginalUtilizationAnnotation = "autoscaling.vhape.io/original-hpa-utilization"

// HPAService converts utilization-based HPA targets to request-based average values.
type HPAService struct {
	hpaInformer hpainformers.HorizontalPodAutoscalerInformer
	client      kubernetes.Interface
}

// metricTargetInfo is the common representation of a resource-based HPA metric.
// containerName is empty for Resource metrics, which are measured across the Pod.
type metricTargetInfo struct {
	target        *autoscalingv2.MetricTarget
	resourceName  corev1.ResourceName
	containerName string
	annotationKey string
}

// NewHPAService creates a service backed by an HPA informer cache.
func NewHPAService(
	hpaInformer hpainformers.HorizontalPodAutoscalerInformer,
	client kubernetes.Interface,
) (*HPAService, error) {
	if hpaInformer == nil {
		return nil, fmt.Errorf("hpa informer is nil")
	}
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is nil")
	}

	return &HPAService{hpaInformer: hpaInformer, client: client}, nil
}

// EnsureAverageValueForDeployment converts utilization metrics of HPAs
// targeting dep. If there is no matching HPA, it makes no API calls.
func (s *HPAService) EnsureAverageValueForDeployment(ctx context.Context, dep *appsv1.Deployment) error {
	if dep == nil {
		return fmt.Errorf("deployment is nil")
	}

	hpas, err := s.hpaInformer.Lister().HorizontalPodAutoscalers(dep.Namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list HPAs for Deployment %q/%q: %w", dep.Namespace, dep.Name, err)
	}

	targetHPAs := make([]*autoscalingv2.HorizontalPodAutoscaler, 0, len(hpas))
	for _, hpa := range hpas {
		if hpa == nil {
			continue
		}

		ref := hpa.Spec.ScaleTargetRef
		if ref.Name != dep.Name || !vpaservice.IsDeploymentTarget(ref.APIVersion, ref.Kind, ref.Name) {
			continue
		}
		targetHPAs = append(targetHPAs, hpa)
	}

	if len(targetHPAs) == 0 {
		return nil
	}

	for _, hpa := range targetHPAs {
		if _, err := s.EnsureAverageValue(ctx, hpa, dep); err != nil {
			return err
		}
	}

	return nil
}

// EnsureAverageValue converts utilization targets in hpa using the requests in dep.
func (s *HPAService) EnsureAverageValue(
	ctx context.Context,
	hpa *autoscalingv2.HorizontalPodAutoscaler,
	dep *appsv1.Deployment,
) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	if hpa == nil {
		return nil, fmt.Errorf("hpa is nil")
	}
	if dep == nil {
		return nil, fmt.Errorf("deployment is nil")
	}
	if hpa.Namespace != dep.Namespace {
		return nil, fmt.Errorf("HPA %q/%q and Deployment %q/%q are in different namespaces", hpa.Namespace, hpa.Name, dep.Namespace, dep.Name)
	}

	modified := hpa.DeepCopy()
	changed := false
	var savedUtilizations map[string]int32
	for index := range modified.Spec.Metrics {
		metric := &modified.Spec.Metrics[index]
		info, ok := metricTarget(metric)
		if !ok {
			continue
		}
		if info.target.Type != autoscalingv2.UtilizationMetricType || info.target.AverageUtilization == nil {
			continue
		}
		if savedUtilizations == nil {
			var err error
			savedUtilizations, err = originalUtilizations(hpa.Annotations)
			if err != nil {
				return nil, fmt.Errorf("decode original utilization annotation on HPA %q/%q: %w", hpa.Namespace, hpa.Name, err)
			}
		}

		utilization := *info.target.AverageUtilization

		averageValue, err := averageValueForRequest(dep, info.resourceName, info.containerName, utilization)
		if err != nil {
			return nil, fmt.Errorf("convert HPA %q/%q metric %q: %w", hpa.Namespace, hpa.Name, info.annotationKey, err)
		}

		savedUtilizations[info.annotationKey] = utilization

		info.target.Type = autoscalingv2.AverageValueMetricType
		info.target.AverageValue = &averageValue
		info.target.AverageUtilization = nil
		changed = true
	}

	if !changed {
		return hpa, nil
	}

	payload, err := json.Marshal(savedUtilizations)
	if err != nil {
		return nil, fmt.Errorf("encode original utilization annotation: %w", err)
	}
	if modified.Annotations == nil {
		modified.Annotations = make(map[string]string)
	}
	modified.Annotations[OriginalUtilizationAnnotation] = string(payload)

	currentJSON, err := json.Marshal(hpa)
	if err != nil {
		return nil, fmt.Errorf("marshal current HPA: %w", err)
	}
	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		return nil, fmt.Errorf("marshal modified HPA: %w", err)
	}
	patch, err := jsonpatch.CreateMergePatch(currentJSON, modifiedJSON)
	if err != nil {
		return nil, fmt.Errorf("create HPA merge patch: %w", err)
	}

	patched, err := s.client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Patch(
		ctx,
		hpa.Name,
		types.MergePatchType,
		patch,
		metav1.PatchOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("patch HPA %q/%q: %w", hpa.Namespace, hpa.Name, err)
	}

	klog.InfoS("Converted HPA utilization targets to average values", "hpa", klog.KObj(patched), "deployment", klog.KObj(dep))
	return patched, nil
}

// metricTarget extracts the target and request data from resource-based
// HPA metrics. It supports Resource and ContainerResource metrics only.
func metricTarget(metric *autoscalingv2.MetricSpec) (metricTargetInfo, bool) {
	if metric == nil {
		return metricTargetInfo{}, false
	}

	switch {
	case metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil:
		resourceName := metric.Resource.Name
		return metricTargetInfo{
			target:        &metric.Resource.Target,
			resourceName:  resourceName,
			annotationKey: "resource/" + string(resourceName),
		}, true

	case metric.Type == autoscalingv2.ContainerResourceMetricSourceType && metric.ContainerResource != nil:
		resourceName := metric.ContainerResource.Name
		containerName := metric.ContainerResource.Container
		return metricTargetInfo{
			target:        &metric.ContainerResource.Target,
			resourceName:  resourceName,
			containerName: containerName,
			annotationKey: "container/" + containerName + "/" + string(resourceName),
		}, true
	}

	return metricTargetInfo{}, false
}

// averageValueForRequest calculates the AverageValue target for a percentage
// target. The request is read from the Deployment template, then multiplied by
// utilization / 100. Quantities use milli-units so CPU values such as 100m are
// preserved without rounding them up to a whole CPU.
func averageValueForRequest(
	dep *appsv1.Deployment,
	resourceName corev1.ResourceName,
	containerName string,
	utilization int32,
) (resource.Quantity, error) {
	if utilization <= 0 {
		return resource.Quantity{}, fmt.Errorf("utilization must be greater than zero")
	}

	request, err := deploymentRequest(dep, resourceName, containerName)
	if err != nil {
		return resource.Quantity{}, err
	}

	milliValue := request.MilliValue()
	if milliValue <= 0 {
		return resource.Quantity{}, fmt.Errorf("request for resource %q must be greater than zero", resourceName)
	}
	if milliValue > math.MaxInt64/int64(utilization) {
		return resource.Quantity{}, fmt.Errorf("request is too large to apply utilization %d", utilization)
	}

	return *resource.NewMilliQuantity(milliValue*int64(utilization)/100, request.Format), nil
}

// deploymentRequest returns the current request represented by an HPA metric.
// For a Resource metric (containerName empty), it sums the resource request of
// every regular container in the Deployment template. For a ContainerResource
// metric, it returns the request of that named container only.
func deploymentRequest(dep *appsv1.Deployment, resourceName corev1.ResourceName, containerName string) (resource.Quantity, error) {
	if dep == nil {
		return resource.Quantity{}, fmt.Errorf("deployment is nil")
	}

	if containerName != "" {
		for _, container := range dep.Spec.Template.Spec.Containers {
			if container.Name != containerName {
				continue
			}

			request, exists := container.Resources.Requests[resourceName]
			if !exists {
				return resource.Quantity{}, fmt.Errorf("container %q does not request resource %q", containerName, resourceName)
			}
			return request, nil
		}

		return resource.Quantity{}, fmt.Errorf("container %q was not found", containerName)
	}

	var total resource.Quantity
	found := false
	for _, container := range dep.Spec.Template.Spec.Containers {
		request, exists := container.Resources.Requests[resourceName]
		if !exists {
			continue
		}

		total.Add(request)
		found = true
	}

	if !found {
		return resource.Quantity{}, fmt.Errorf("Deployment has no request for resource %q", resourceName)
	}

	return total, nil
}

// originalUtilizations decodes the utilizations previously written by this
// service. The map key identifies the metric (for example, resource/cpu or
// container/api/cpu); its value is the percentage used for the conversion. A
// missing annotation is equivalent to an empty map.
func originalUtilizations(annotations map[string]string) (map[string]int32, error) {
	utilizations := make(map[string]int32)
	if annotations == nil || annotations[OriginalUtilizationAnnotation] == "" {
		return utilizations, nil
	}

	if err := json.Unmarshal([]byte(annotations[OriginalUtilizationAnnotation]), &utilizations); err != nil {
		return nil, err
	}
	return utilizations, nil
}
