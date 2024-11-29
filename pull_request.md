**How to categorize this PR?**

/area quality
/kind bug

**What this PR does / why we need it**:

Actors can reconcile (create/update) immutable secrets and configmaps that can be referenced by managed resources, statefulsets, etc. If these secrets or configmaps are labelled with `resources.gardener.cloud/garbage-collectable-reference=true`, the Garbage Collector controller of the Gardener Resource Manager will eventually delete them if they are no longer referenced by other resources.

See [resource-manager.md#garbage-collector](https://github.com/gardener/gardener/blob/master/docs/concepts/resource-manager.md#garbage-collector-for-immutable-configmapssecrets) for more details.

Today, when

- the Garbage Collector decides to delete a resource (a secret or a configmap) because it is not referenced anywhere, and
- an actor concurrently references and reconciles (creates or updates) that resource,

the system **might** silently (without errors) end up in an inconsistent final state where the referenced resource is missing because it was deleted by the Garbage Collector.

**Why "might"?** This is a race condition that happens only if the Kubernetes API calls to create, update, list and delete the resources are executed in a specific interleaved order by the actor and the Garbage Collector. Although this seems to have a low probability, it actually happened a few times in production environments at scale.

The problem is that neither the Garbage Collector nor the actor can detect this situation as an error, so the system will not self heal right away. The system will stay in an inconsistent state indefinitely, until the actor is executed again and the actor recreates the missing referenced resource. If the actor is the shoot reconciler, the next scheduled run could be a day later, so the system stays in this inconsistent state for a day.

This PR proposes changes in the Garbage Collector and in the utils.kubernetes/MakeUnique function to prevent this race condition.

To prevent that the Garbage Collector silently deletes a resource that is referenced somewhere else in the meantime, we can rely on the conflict detection mechanism of the Kubernetes API server for concurrent modifications on a single object.

**Conflict detection** requires that both parties

- (A) mutate the given object and
- (B) execute the Kubernetes API calls in such a way that the Kubernetes API server can assert the resource version to check for conflicts.

**(A)** Clients today might use e.g. the `CreateOrUpdate` function [src:controller-runtime#CreateOrUpdate](https://github.com/kubernetes-sigs/controller-runtime/blob/v0.19.2/pkg/controller/controllerutil/controllerutil.go#L282-L320) to reconcile (create or update) an immutable resource, a secret or a configmap. The name is based on the hash of the *content* of the resource (see `MakeUnique` in [utils/kubernetes/object.go](https://github.com/gardener/gardener/blob/b6b857890afabfa10c75a2016f3f02ddde5762a6/pkg/utils/kubernetes/object.go#L113-L117)), so if a resource with the same content exists, it will get the same name, and the clients will automatically "reuse" the existing resource.

The update operation does not lead to an API call (see [src:controller-runtime#CreateOrUpdate](https://github.com/kubernetes-sigs/controller-runtime/blob/v0.19.2/pkg/controller/controllerutil/controllerutil.go#L312)) if the resource is not changed: this is typical for immutable resources with content based names. Hence when a client reuses an existing resource, it will not mutate the resource: the resource version is not going to be incremented, and there will be no conflict with the Garbage Collector.

To make sure that the resource version of the immutable resource changes with the `CreateOrUpdate` call if a *to-be-deleted* resource is reused, we need to *mutate* the metadata of the immutable object. We need not mutate the object on each `CreateOrUpdate` call: it could lead to a significant overhead. Rather, the Garbage Collector sets a new label, `gc.gardener.cloud/used` to `false` when it is about to delete a resource. Clients should always reconcile to `gc.gardener.cloud/used=true`, so that when clients reuse a resource after the Garbage Collector marked that for deletion, they will always mutate the resource by setting `gc.gardener.cloud/used` label back to `true`, and hence a conflict with the Garbage Collector can be detected.

This also means that the Garbage Collector will have to be split into 2 phases: first, mark the unreferenced resources for deletion, and then after some delay, delete them.

**(B)** The Garbage Collector currently uses a *plain delete* client API call that can not detect a conflict because the UID and the resourceVersion is not sent in the underlying low level Kubernetes API HTTP request. The Kubernetes API allows to pass a `DeleteOptions` object in the request body with the `preconditions.{resourceVersion,uid}` fields to assert that the resource to be deleted has not been changed in the meantime. See [kubernetes-api#delete-secret](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#delete-secret-v1-core).

This PR adjusts the Garbage Collector implementation to use the `DeleteOptions` feature to detect a conflict when a resource was mutated between the list and delete calls. If such a conflict happens, the Garbage Collector can back off and restart later. Previously the Garbage Collector silently (without conflicts) deleted the resource that was actually referenced.

The PR contains 3 main changes:

1. The Garbage Collector uses the `DeleteOptions` feature of the Kubernetes API to detect conflicts.
2. The Garbage Collector is split into 2 phases: marking and deleting.
3. The `MakeUnique` function in `pkg/utils/kubernetes` is adjusted to always mutate resources that are marked for deletion and are reused.

Backwards compatibility: Gardener Extensions are not released synchronously with the Gardener Core, so it is important that older versions of `MakeUnique` that extensions might still use for some time is compatible with the new Garbage Collector implementation. If the `gc.gardener.cloud/used` label is not set by the clients, the Garbage Collector will work as it did before, with the possibility of an unlikely race condition that might infrequently lead to silently deleting a resource that is actually used.

**Which issue(s) this PR fixes**:

Part of #10081

**Special notes for your reviewer**:

This topic of having a race condition in the Garbage Collector is hard to grasp or reproduce. To help with this, I tried to capture this behavior in a unit test. It is challenging to write unit tests for race conditions, which motivated the implementation of the "Cooperative Executor" in the first part of this PR. The Cooperative Executor allows to interleave the Kubernetes API calls of the Garbage Collector and another actor that references and reconciles a secret. The source code of the Garbage Collector need not be changed. The Cooperative Executor is a scheduler that executes the two actors in all possible combinations and hence it can detect the ordering of the API calls that yields unexpected results, the race condition. I added a test that shows that the current version has indeed a race condition. At the end of the PR the test shows that the race condition is fixed.

@rfranzke @vicwicker

**Release note**:

```bugfix developer
The Gardener Resource Manager's Garbage Collector has been improved to prevent a race condition where the Garbage Collector silently deleted resources that were referenced concurrently.
Clients that reconcile immutable resources that are expected to be garbage collected, should mutate the resource when they reuse an existing resource, by reconciling the `gc.gardener.cloud/used` label to `true`. This allows the Garbage Collector to detect concurrent modifications and prevent the unintended deletion of a resource during a race condition. This mutation is already implemented in the `MakeUnique` function of `pkg/utils/kubernetes` such that there is no change required for clients that use that function.
```
