/*
Copyright 2019 The Kubernetes Authors.

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

package mocktarget

import (
	"context"

	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

// MockVpaTargetSelectorFetcher is a mock of VpaTargetSelectorFetcher interface
type MockVpaTargetSelectorFetcher struct {
	ctrl     *gomock.Controller
	recorder *_MockVpaTargetSelectorFetcherRecorder
}

// Recorder for MockVpaTargetSelectorFetcher (not exported)
type _MockVpaTargetSelectorFetcherRecorder struct {
	mock *MockVpaTargetSelectorFetcher
}

// NewMockVpaTargetSelectorFetcher returns mock instance of a mock of VpaTargetSelectorFetcher
func NewMockVpaTargetSelectorFetcher(ctrl *gomock.Controller) *MockVpaTargetSelectorFetcher {
	mock := &MockVpaTargetSelectorFetcher{ctrl: ctrl}
	mock.recorder = &_MockVpaTargetSelectorFetcherRecorder{mock}
	return mock
}

// EXPECT enables configuring expectations
func (_m *MockVpaTargetSelectorFetcher) EXPECT() *_MockVpaTargetSelectorFetcherRecorder {
	return _m.recorder
}

// Fetch enables configuring expectations on Fetch method
func (_m *MockVpaTargetSelectorFetcher) Fetch(_ context.Context, vpa *vpa_types.VerticalPodAutoscaler) (labels.Selector, error) {
	ret := _m.ctrl.Call(_m, "Fetch", vpa)
	ret0, _ := ret[0].(labels.Selector)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockVpaTargetSelectorFetcherRecorder) Fetch(arg0 any) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Fetch", arg0)
}

// FetchPodTemplate enables configuring expectations on FetchPodTemplate method.
func (_m *MockVpaTargetSelectorFetcher) FetchPodTemplate(_ context.Context, vpa *vpa_types.VerticalPodAutoscaler) (*corev1.PodTemplateSpec, error) {
	ret := _m.ctrl.Call(_m, "FetchPodTemplate", vpa)
	ret0, _ := ret[0].(*corev1.PodTemplateSpec)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockVpaTargetSelectorFetcherRecorder) FetchPodTemplate(arg0 any) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "FetchPodTemplate", arg0)
}
