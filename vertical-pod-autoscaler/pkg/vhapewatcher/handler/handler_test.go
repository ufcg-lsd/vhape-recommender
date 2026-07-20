package handler

import (
	"reflect"
	"testing"

	"k8s.io/client-go/tools/cache"
)

// defines a fake type to allow for testing
type fakeEventHandlerReceiver struct {
	handler cache.ResourceEventHandler
	err     error
}

// implements required interface
func (r *fakeEventHandlerReceiver) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	if r.err != nil {
		return nil, r.err
	}

	r.handler = handler
	return nil, nil
}

func TestRegisterHandlersOnReceivers(t *testing.T) {
	deploymentInformer := &fakeEventHandlerReceiver{}
	vpaInformer := &fakeEventHandlerReceiver{}
	watchedNamespaceInformer := &fakeEventHandlerReceiver{}
	ignoredWorkloadInformer := &fakeEventHandlerReceiver{}

	var calls []string

	err := registerHandlersOnReceivers(
		deploymentInformer,
		vpaInformer,
		watchedNamespaceInformer,
		ignoredWorkloadInformer,
		testInformerHandlerFuncs(&calls),
	)
	if err != nil {
		t.Fatalf("registerHandlersOnReceivers() returned error: %v", err)
	}

	fireAllHandlerFuncs(t, deploymentInformer)
	fireAllHandlerFuncs(t, vpaInformer)
	fireAllHandlerFuncs(t, watchedNamespaceInformer)
	fireAllHandlerFuncs(t, ignoredWorkloadInformer)

	want := []string{
		"deployment/add",
		"deployment/update",
		"deployment/delete",
		"vpa/add",
		"vpa/update",
		"vpa/delete",
		"watched-namespace/add",
		"watched-namespace/update",
		"watched-namespace/delete",
		"ignored-workload/add",
		"ignored-workload/update",
		"ignored-workload/delete",
	}

	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRegisterHandlersOnReceiversRejectsNilInformer(t *testing.T) {
	validInformer := &fakeEventHandlerReceiver{}
	handlers := testInformerHandlerFuncs(nil)

	tests := []struct {
		name                     string
		deploymentInformer       eventHandlerReceiver
		vpaInformer              eventHandlerReceiver
		watchedNamespaceInformer eventHandlerReceiver
		ignoredWorkloadInformer  eventHandlerReceiver
	}{
		{
			name:                     "deployment informer nil",
			deploymentInformer:       nil,
			vpaInformer:              validInformer,
			watchedNamespaceInformer: validInformer,
			ignoredWorkloadInformer:  validInformer,
		},
		{
			name:                     "vpa informer nil",
			deploymentInformer:       validInformer,
			vpaInformer:              nil,
			watchedNamespaceInformer: validInformer,
			ignoredWorkloadInformer:  validInformer,
		},
		{
			name:                     "watched namespace informer nil",
			deploymentInformer:       validInformer,
			vpaInformer:              validInformer,
			watchedNamespaceInformer: nil,
			ignoredWorkloadInformer:  validInformer,
		},
		{
			name:                     "ignored workload informer nil",
			deploymentInformer:       validInformer,
			vpaInformer:              validInformer,
			watchedNamespaceInformer: validInformer,
			ignoredWorkloadInformer:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registerHandlersOnReceivers(
				tt.deploymentInformer,
				tt.vpaInformer,
				tt.watchedNamespaceInformer,
				tt.ignoredWorkloadInformer,
				handlers,
			)
			if err == nil {
				t.Fatal("registerHandlersOnReceivers() expected error, got nil")
			}
		})
	}
}

func testInformerHandlerFuncs(calls *[]string) informerHandlerFuncs {
	return informerHandlerFuncs{
		deployment:       testEventHandlerFuncs("deployment", calls),
		vpa:              testEventHandlerFuncs("vpa", calls),
		watchedNamespace: testEventHandlerFuncs("watched-namespace", calls),
		ignoredWorkload:  testEventHandlerFuncs("ignored-workload", calls),
	}
}

func testEventHandlerFuncs(name string, calls *[]string) cache.ResourceEventHandlerFuncs {
	record := func(event string) {
		if calls == nil {
			return
		}
		*calls = append(*calls, name+"/"+event)
	}

	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			record("add")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			record("update")
		},
		DeleteFunc: func(obj interface{}) {
			record("delete")
		},
	}
}

func fireAllHandlerFuncs(t *testing.T, receiver *fakeEventHandlerReceiver) {
	t.Helper()

	receiver.handler.OnAdd(nil, true)
	receiver.handler.OnUpdate(nil, nil)
	receiver.handler.OnDelete(nil)
}
