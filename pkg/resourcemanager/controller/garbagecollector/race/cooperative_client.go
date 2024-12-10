// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package race

import (
	"context"
	"fmt"
	"sync"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewCooperativeClient(clientName string, client client.Client, executor CooperativeExecutor) client.Client {
	return &cooperativeClient{
		clientName: clientName,
		client:     client,
		executor:   executor,
	}
}

type cooperativeClient struct {
	clientName string
	client     client.Client
	executor   CooperativeExecutor
	mu         sync.Mutex
	callID     int
}

func (cc *cooperativeClient) nextCallID() int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	callID := cc.callID
	cc.callID++
	return callID
}

func (cc *cooperativeClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Create(%v, %v)", ObjectToString(obj), opts))
	err := cc.client.Create(ctx, obj, opts...)
	cc.executor.Release(cc.clientName, callID, fmt.Sprintf("object=%s, error=%v", ObjectToString(obj), err))
	return err
}

func (cc *cooperativeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		callID := cc.nextCallID()
		cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Get(%v, %v)", key, opts))
		err := cc.client.Get(ctx, key, obj, opts...)
		cc.executor.Release(cc.clientName, callID, fmt.Sprintf("object=%s, error=%v", ObjectToString(obj), err))
		return err
	}
	return cc.client.Get(ctx, key, obj, opts...)
}

func (cc *cooperativeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if list.GetObjectKind().GroupVersionKind().Kind == "SecretList" ||
		list.GetObjectKind().GroupVersionKind().Kind == "ManagedResourceList" {
		callID := cc.nextCallID()
		cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("List(%s, %v)", list.GetObjectKind().GroupVersionKind().Kind, opts))
		err := cc.client.List(ctx, list, opts...)
		cc.executor.Release(cc.clientName, callID, fmt.Sprintf("list=%v, error=%v", ListToString(list), err))
		return err
	}
	return cc.client.List(ctx, list, opts...)
}

func (cc *cooperativeClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Patch(%v, %v, %v)", ObjectToString(obj), PatchToString(obj, patch), opts))
	err := cc.client.Patch(ctx, obj, patch, opts...)
	cc.executor.Release(cc.clientName, callID, fmt.Sprintf("object=%v, error=%v", ObjectToString(obj), err))
	return err
}

func (cc *cooperativeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Update(%v, %v)", ObjectToString(obj), opts))
	err := cc.client.Update(ctx, obj, opts...)
	cc.executor.Release(cc.clientName, callID, fmt.Sprintf("object=%s, error=%v", ObjectToString(obj), err))
	return err
}

func (cc *cooperativeClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Delete(%v, %s)", ObjectToString(obj), DeleteOptionToString(opts)))
	err := cc.client.Delete(ctx, obj, opts...)
	cc.executor.Release(cc.clientName, callID, fmt.Sprintf("error=%v", err))
	return err
}

func (cc *cooperativeClient) Scheme() *runtime.Scheme     { return cc.client.Scheme() }
func (cc *cooperativeClient) RESTMapper() meta.RESTMapper { return cc.client.RESTMapper() }
func (cc *cooperativeClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return cc.client.GroupVersionKindFor(obj)
}
func (cc *cooperativeClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return cc.client.IsObjectNamespaced(obj)
}
func (cc *cooperativeClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return cc.client.DeleteAllOf(ctx, obj, opts...)
}
func (cc *cooperativeClient) Status() client.SubResourceWriter { return cc.client.Status() }
func (cc *cooperativeClient) SubResource(subResource string) client.SubResourceClient {
	return cc.client.SubResource(subResource)
}

func ObjectToString(obj client.Object) string {
	fromString := func(name, value string) string {
		if value == "" {
			return ""
		}
		return fmt.Sprintf("%s: %s, ", name, value)
	}
	fromTime := func(name string, value metav1.Time) string {
		if value.IsZero() {
			return ""
		}
		return fmt.Sprintf("%s: %s, ", name, value)
	}
	fromMap := func(name string, value map[string]string) string {
		if len(value) == 0 {
			return ""
		}
		return fmt.Sprintf("%s: %v, ", name, value)
	}
	fromBool := func(name string, value *bool) string {
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%s: %t, ", name, *value)
	}
	objectMetaToString := func(obj metav1.Object) string {
		result := fromString("Namespace", obj.GetNamespace())
		result += fromString("Name", obj.GetName())
		result += fromString("ResourceVersion", obj.GetResourceVersion())
		result += fromTime("CreationTimestamp", obj.GetCreationTimestamp())
		result += fromMap("Labels", obj.GetLabels())
		result += fromMap("Annotations", obj.GetAnnotations())
		return result
	}
	switch obj := obj.(type) {
	case *corev1.Secret:
		result := objectMetaToString(obj)
		result += fromBool("Immutable", obj.Immutable)
		return fmt.Sprintf("&Secret{%s}", result)
	case *resourcesv1alpha1.ManagedResource:
		result := objectMetaToString(obj)
		return fmt.Sprintf("&ManagedResource{%s}", result)
	case *metav1.PartialObjectMetadata:
		result := objectMetaToString(obj)
		return fmt.Sprintf("&PartialObjectMetadata{%s}", result)
	}
	return fmt.Sprintf("%v", obj)
}

func ListToString(list client.ObjectList) string {
	if list, ok := list.(*metav1.PartialObjectMetadataList); ok {
		result := ""
		for _, item := range list.Items {
			result += fmt.Sprintf("%s, ", ObjectToString(&item))
		}
		return fmt.Sprintf("&PartialObjectMetadataList{Items: [%s]}", result)
	}
	return fmt.Sprintf("%v", list)
}

func DeleteOptionToString(opts []client.DeleteOption) string {
	if len(opts) != 1 {
		return fmt.Sprintf("%v", opts)
	}
	if p, ok := opts[0].(client.Preconditions); ok {
		if p.UID != nil && p.ResourceVersion != nil {
			return fmt.Sprintf("Preconditions{UID: %v, ResourceVersion: %v}", *p.UID, *p.ResourceVersion)
		}
	}
	return ""
}

func PatchToString(obj client.Object, patch client.Patch) string {
	data, _ := patch.Data(obj)
	return fmt.Sprintf("Patch{%v, %s}", patch.Type(), data)
}
