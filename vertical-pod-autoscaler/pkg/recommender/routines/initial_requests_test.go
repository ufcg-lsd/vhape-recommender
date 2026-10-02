package routines

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpa_fake "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/fake"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/initialrequests"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

type staticPodTemplateFetcher struct {
	template *corev1.PodTemplateSpec
	calls    int
}

type annotationCapturingPodResourceRecommender struct {
	initialRequests string
}

func (r *annotationCapturingPodResourceRecommender) GetRecommendedPodResources(
	_ model.ContainerNameToAggregateStateMap,
	vpa *model.Vpa,
	_ []model.PodID,
) (logic.RecommendedPodResources, error) {
	r.initialRequests = vpa.Annotations[initialrequests.Annotation]
	return logic.RecommendedPodResources{}, nil
}

func (r *annotationCapturingPodResourceRecommender) Free(model.VpaID) {}

func (f *staticPodTemplateFetcher) Fetch(_ context.Context, _ *vpa_types.VerticalPodAutoscaler) (labels.Selector, error) {
	return labels.Everything(), nil
}

func (f *staticPodTemplateFetcher) FetchPodTemplate(_ context.Context, _ *vpa_types.VerticalPodAutoscaler) (*corev1.PodTemplateSpec, error) {
	f.calls++
	return f.template.DeepCopy(), nil
}

func TestEnsureInitialRequestsAnnotation(t *testing.T) {
	vpa := &vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example"},
	}
	targetFetcher := &staticPodTemplateFetcher{template: &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				}},
			}},
		},
	}}
	client := vpa_fake.NewSimpleClientset(vpa).AutoscalingV1() //nolint:staticcheck // fake client is required for this unit test
	r := &recommender{vpaClient: client, targetFetcher: targetFetcher}

	if err := r.ensureInitialRequestsAnnotation(context.Background(), vpa); err != nil {
		t.Fatalf("ensureInitialRequestsAnnotation() error = %v", err)
	}

	updated, err := client.VerticalPodAutoscalers("default").Get(context.Background(), "example", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated VPA: %v", err)
	}
	var snapshot initialrequests.Snapshot
	if err := json.Unmarshal([]byte(updated.Annotations[initialrequests.Annotation]), &snapshot); err != nil {
		t.Fatalf("unmarshal annotation: %v", err)
	}
	appCPU := snapshot.Containers["app"][corev1.ResourceCPU]
	if got := appCPU.String(); got != "250m" {
		t.Errorf("app CPU request = %q, want 250m", got)
	}
	appMemory := snapshot.Containers["app"][corev1.ResourceMemory]
	if got := appMemory.String(); got != "256Mi" {
		t.Errorf("app memory request = %q, want 256Mi", got)
	}
	// A subsequent run must preserve the first snapshot and avoid fetching again.
	targetFetcher.template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("1")
	if err := r.ensureInitialRequestsAnnotation(context.Background(), updated); err != nil {
		t.Fatalf("second ensureInitialRequestsAnnotation() error = %v", err)
	}
	if targetFetcher.calls != 1 {
		t.Errorf("FetchPodTemplate() calls = %d, want 1", targetFetcher.calls)
	}
}

func TestProcessVPAUpdateLeavesInitialRequestsForTheInformerToLoad(t *testing.T) {
	observedVPA := &vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example"},
	}
	targetFetcher := &staticPodTemplateFetcher{template: &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("250m"),
			}},
		}}},
	}}
	client := vpa_fake.NewSimpleClientset(observedVPA).AutoscalingV1() //nolint:staticcheck // fake client is required for this unit test
	podRecommender := &annotationCapturingPodResourceRecommender{}
	r := &recommender{
		clusterState:           model.NewClusterState(time.Minute),
		vpaClient:              client,
		targetFetcher:          targetFetcher,
		podResourceRecommender: podRecommender,
	}
	modelVPA := model.NewVpa(
		model.VpaID{Namespace: observedVPA.Namespace, VpaName: observedVPA.Name},
		labels.Everything(),
		time.Now(),
	)

	processVPAUpdate(r, modelVPA, observedVPA)

	if podRecommender.initialRequests != "" {
		t.Fatal("recommender received initial requests before the informer cache was refreshed")
	}
	if _, found := modelVPA.Annotations[initialrequests.Annotation]; found {
		t.Error("processVPAUpdate must not mutate the cluster-state VPA annotations")
	}
	updated, err := client.VerticalPodAutoscalers(observedVPA.Namespace).Get(context.Background(), observedVPA.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated VPA: %v", err)
	}
	if _, found := updated.Annotations[initialrequests.Annotation]; !found {
		t.Error("updated VPA is missing the initial requests annotation")
	}
}
