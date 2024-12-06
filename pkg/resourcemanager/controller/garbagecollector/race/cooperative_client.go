package race

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
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
	callID     int
	mu         sync.Mutex
	client     client.Client
	executor   CooperativeExecutor
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
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Create(%v, %v)", obj, opts))
	defer cc.executor.Release(cc.clientName, callID)
	return cc.client.Create(ctx, obj, opts...)
}

func (cc *cooperativeClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Delete(%v, %v)", obj, opts))
	defer cc.executor.Release(cc.clientName, callID)
	return cc.client.Delete(ctx, obj, opts...)
}

func (cc *cooperativeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Get(%v, %v)", key, opts))
	defer cc.executor.Release(cc.clientName, callID)
	return cc.client.Get(ctx, key, obj, opts...)
}

func (cc *cooperativeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	// if it is a secret list
	if list.GetObjectKind().GroupVersionKind().Kind == "SecretList" {
		callID := cc.nextCallID()
		cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("List(%v, %v)", list, opts))
		defer cc.executor.Release(cc.clientName, callID)
	}
	return cc.client.List(ctx, list, opts...)
}

func (cc *cooperativeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	callID := cc.nextCallID()
	cc.executor.LockAndYield(cc.clientName, callID, fmt.Sprintf("Update(%v, %v)", obj, opts))
	defer cc.executor.Release(cc.clientName, callID)
	return cc.client.Update(ctx, obj, opts...)
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
func (cc *cooperativeClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return cc.client.Patch(ctx, obj, patch, opts...)
}
func (cc *cooperativeClient) Status() client.SubResourceWriter { return cc.client.Status() }
func (cc *cooperativeClient) SubResource(subResource string) client.SubResourceClient {
	return cc.client.SubResource(subResource)
}
