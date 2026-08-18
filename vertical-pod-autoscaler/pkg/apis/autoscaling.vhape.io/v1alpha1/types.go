/*
Copyright 2026 The VHAPE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package v1alpha1 contains definitions of VHAPE related objects.
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeWatchedNamespace marks a Kubernetes namespace as eligible for VHAPE Watcher management.
type VhapeWatchedNamespace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VhapeWatchedNamespaceSpec `json:"spec,omitempty"`
}

type VhapeWatchedNamespaceSpec struct {
	VhapePolicyName string           `json:"vhapePolicyName"`
	VPAUpdateMode   vpav1.UpdateMode `json:"vpaUpdateMode"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeWatchedNamespaceRegex marks namespaces matching Regex as eligible for VHAPE Watcher management.
type VhapeWatchedNamespaceRegex struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VhapeWatchedNamespaceRegexSpec `json:"spec,omitempty"`
}

// VhapeWatchedNamespaceRegexSpec describes a namespace regex and the VHAPE configuration applied to matching namespaces.
type VhapeWatchedNamespaceRegexSpec struct {
	VhapeWatchedNamespaceSpec `json:",inline"`
	Regex                     string `json:"regex"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeWatchedNamespaceRegexList is a list of VhapeWatchedNamespaceRegex objects.
type VhapeWatchedNamespaceRegexList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VhapeWatchedNamespaceRegex `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeIgnoredNamespace marks a namespace that must be excluded from regex-based VHAPE Watcher management.
type VhapeIgnoredNamespace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeIgnoredNamespaceList is a list of VhapeIgnoredNamespace objects.
type VhapeIgnoredNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VhapeIgnoredNamespace `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeWatchedNamespaceList is a list of VhapeWatchedNamespace objects.
type VhapeWatchedNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VhapeWatchedNamespace `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeIgnoredWorkload marks a workload that must be ignored by VHAPE Watcher.
type VhapeIgnoredWorkload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VhapeIgnoredWorkloadSpec `json:"spec,omitempty"`
}

// VhapeIgnoredWorkloadSpec describes the workload ignored by VHAPE Watcher.
type VhapeIgnoredWorkloadSpec struct {
	// TargetRef identifies the workload that should not be managed by VHAPE Watcher.
	TargetRef corev1.ObjectReference `json:"targetRef"`

	// Reason optionally explains why the workload is ignored.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VhapeIgnoredWorkloadList is a list of VhapeIgnoredWorkload objects.
type VhapeIgnoredWorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VhapeIgnoredWorkload `json:"items"`
}
