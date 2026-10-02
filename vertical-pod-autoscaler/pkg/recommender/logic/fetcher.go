package logic

import (
	"context"
	"fmt"

	vhape_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.vhape.io/v1alpha1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

const vhapePolicyAnnotation = "vhape/policy"

// containerUsage stores current resource usage samples grouped by container name.
type containerUsage struct {
	CPU    map[string][]model.ResourceAmount
	Memory map[string][]model.ResourceAmount
}

// fetchPolicy loads the VhapePolicy referenced by the VPA annotation.
// The policy comes from the informer cache and must be treated as read-only.
func (r *podResourceRecommender) fetchPolicy(vpa *model.Vpa) (*vhape_types.VhapePolicy, error) {
	policyName, ok := vpa.Annotations[vhapePolicyAnnotation]
	if !ok {
		return nil, fmt.Errorf(
			"VPA %q/%q: missing %s annotation",
			vpa.ID.Namespace,
			vpa.ID.VpaName,
			vhapePolicyAnnotation,
		)
	}

	policy, err := r.policyLister.Get(policyName)
	if err != nil {
		return nil, fmt.Errorf(
			"VPA %q/%q: fetch policy %q: %w",
			vpa.ID.Namespace,
			vpa.ID.VpaName,
			policyName,
			err,
		)
	}

	return policy, nil
}

// collectCurrentUsage collects the latest usage metrics for containers running in the pods matched by the VPA.
func (r *podResourceRecommender) collectCurrentUsage(matchingPods []model.PodID) (containerUsage, error) {
	podSet := make(map[model.PodID]struct{}, len(matchingPods))
	for _, podID := range matchingPods {
		podSet[podID] = struct{}{}
	}

	snapshots, err := r.metricsClient.GetContainersMetrics(context.TODO())
	if err != nil {
		return containerUsage{}, fmt.Errorf("collect container metrics: %w", err)
	}

	usage := containerUsage{
		CPU:    make(map[string][]model.ResourceAmount),
		Memory: make(map[string][]model.ResourceAmount),
	}

	for _, snap := range snapshots {
		if _, ok := podSet[snap.ID.PodID]; !ok {
			continue
		}

		containerName := snap.ID.ContainerName
		if cpu, ok := snap.Usage[model.ResourceCPU]; ok {
			usage.CPU[containerName] = append(usage.CPU[containerName], cpu)
		}
		if memory, ok := snap.Usage[model.ResourceMemory]; ok {
			usage.Memory[containerName] = append(usage.Memory[containerName], memory)
		}
	}

	return usage, nil
}
