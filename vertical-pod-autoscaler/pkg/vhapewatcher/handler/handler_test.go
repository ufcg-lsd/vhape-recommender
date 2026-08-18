package handler

import (
	"errors"
	"reflect"
	"testing"

	"k8s.io/client-go/tools/cache"
)

type fakeEventHandlerReceiver struct {
	handler cache.ResourceEventHandler
	err     error
}

func (r *fakeEventHandlerReceiver) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	if r.err != nil {
		return nil, r.err
	}

	r.handler = handler
	return nil, nil
}

func TestNewRejectsNilSink(t *testing.T) {
	handler, err := New(nil)
	if err == nil {
		t.Fatal("New() expected error, got nil")
	}
	if handler != nil {
		t.Fatalf("New() handler = %#v, want nil", handler)
	}
}

func TestRegisterHandlerFunctionsOnInformersRejectsNilInformers(t *testing.T) {
	handler := &Handler{}

	if err := handler.RegisterHandlerFunctionsOnInformers(nil); err == nil {
		t.Fatal("RegisterHandlerFunctionsOnInformers() expected error, got nil")
	}
}

func TestRegisterHandlers(t *testing.T) {
	resourceNames := []string{
		"deployment",
		"vpa",
		"watched-namespace",
		"watched-namespace-regex",
		"ignored-namespace",
		"ignored-workload",
	}

	var calls []string
	receivers := make([]*fakeEventHandlerReceiver, 0, len(resourceNames))
	registrations := make([]handlerRegistration, 0, len(resourceNames))

	for _, name := range resourceNames {
		receiver := &fakeEventHandlerReceiver{}
		receivers = append(receivers, receiver)
		registrations = append(registrations, handlerRegistration{
			name:     name,
			receiver: receiver,
			handler:  testEventHandlerFuncs(name, &calls),
		})
	}

	if err := registerHandlers(registrations); err != nil {
		t.Fatalf("registerHandlers() returned error: %v", err)
	}

	for _, receiver := range receivers {
		fireAllHandlerFuncs(t, receiver)
	}

	want := make([]string, 0, len(resourceNames)*3)
	for _, name := range resourceNames {
		want = append(want,
			name+"/add",
			name+"/update",
			name+"/delete",
		)
	}

	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRegisterHandlersRejectsNilInformer(t *testing.T) {
	err := registerHandlers([]handlerRegistration{
		{
			name:     "deployment",
			receiver: &fakeEventHandlerReceiver{},
			handler:  testEventHandlerFuncs("deployment", nil),
		},
		{
			name:     "vpa",
			receiver: nil,
			handler:  testEventHandlerFuncs("vpa", nil),
		},
	})
	if err == nil {
		t.Fatal("registerHandlers() expected error, got nil")
	}
}

func TestRegisterHandlersReturnsRegistrationError(t *testing.T) {
	wantErr := errors.New("registration failed")

	err := registerHandlers([]handlerRegistration{
		{
			name:     "deployment",
			receiver: &fakeEventHandlerReceiver{err: wantErr},
			handler:  testEventHandlerFuncs("deployment", nil),
		},
	})
	if err == nil {
		t.Fatal("registerHandlers() expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("registerHandlers() error = %v, want wrapped %v", err, wantErr)
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

	if receiver.handler == nil {
		t.Fatal("handler was not registered")
	}

	receiver.handler.OnAdd(nil, true)
	receiver.handler.OnUpdate(nil, nil)
	receiver.handler.OnDelete(nil)
}
