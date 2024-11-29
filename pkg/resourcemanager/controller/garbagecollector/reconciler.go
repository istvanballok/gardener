// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package garbagecollector

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/resourcemanager/apis/config"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/references"
	errorsutils "github.com/gardener/gardener/pkg/utils/errors"
)

// Reconciler performs garbage collection.
type Reconciler struct {
	TargetClient          client.Client
	Config                config.GarbageCollectorControllerConfig
	Clock                 clock.Clock
	MinimumObjectLifetime *time.Duration
}

// Reconcile performs the main reconciliation logic.
func (r *Reconciler) Reconcile(reconcileCtx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(reconcileCtx)

	ctx, cancel := controllerutils.GetMainReconciliationContext(reconcileCtx, r.Config.SyncPeriod.Duration)
	defer cancel()

	log.Info("Starting garbage collection")
	defer log.Info("Garbage collection finished")

	var (
		labels                  = client.MatchingLabels{references.LabelKeyGarbageCollectable: references.LabelValueGarbageCollectable}
		objectsToGarbageCollect = map[objectId]*metav1.PartialObjectMetadata{}
		usedObjects             = map[objectId]*metav1.PartialObjectMetadata{}
	)

	for _, resource := range []struct {
		kind     string
		listKind string
	}{
		{references.KindSecret, "SecretList"},
		{references.KindConfigMap, "ConfigMapList"},
	} {
		objList := &metav1.PartialObjectMetadataList{}
		objList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.listKind))
		if err := r.TargetClient.List(ctx, objList, labels); err != nil {
			return reconcile.Result{}, err
		}

		for _, obj := range objList.Items {
			obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.kind))
			if obj.CreationTimestamp.Add(*r.MinimumObjectLifetime).UTC().After(r.Clock.Now().UTC()) {
				// Do not consider recently created objects for garbage collection.
				continue
			}

			objectsToGarbageCollect[objectId{resource.kind, obj.Namespace, obj.Name}] = &obj
		}
	}

	var (
		items             []metav1.PartialObjectMetadata
		groupVersionKinds = []schema.GroupVersionKind{
			appsv1.SchemeGroupVersion.WithKind("DeploymentList"),
			appsv1.SchemeGroupVersion.WithKind("StatefulSetList"),
			appsv1.SchemeGroupVersion.WithKind("DaemonSetList"),
			batchv1.SchemeGroupVersion.WithKind("JobList"),
			corev1.SchemeGroupVersion.WithKind("PodList"),
			batchv1.SchemeGroupVersion.WithKind("CronJobList"),
			resourcesv1alpha1.SchemeGroupVersion.WithKind("ManagedResourceList"),
		}
	)

	for _, gvk := range groupVersionKinds {
		objList := &metav1.PartialObjectMetadataList{}
		objList.SetGroupVersionKind(gvk)
		if err := r.TargetClient.List(ctx, objList); err != nil {
			if !meta.IsNoMatchError(err) {
				return reconcile.Result{}, err
			}
		}
		items = append(items, objList.Items...)
	}

	for _, objectMeta := range items {
		for key, objectName := range objectMeta.Annotations {
			objectKind := references.KindFromAnnotationKey(key)
			if objectKind == "" || objectName == "" {
				continue
			}

			key := objectId{objectKind, objectMeta.Namespace, objectName}
			usedObjects[key] = objectsToGarbageCollect[key]
			delete(objectsToGarbageCollect, key)
		}
	}

	var (
		results       = make(chan error, 1)
		requeueNeeded = make(chan bool, 1)

		wg        wait.Group
		errorList = &multierror.Error{ErrorFormat: errorsutils.NewErrorFormatFuncWithPrefix("Could not delete all unused resources")}
	)

	for _, o := range usedObjects {
		obj := o

		wg.StartWithContext(ctx, func(ctx context.Context) {
			if _, ok := obj.Labels[references.LabelKeyUnusedAt]; ok {
				patch := client.StrategicMergeFrom(obj.DeepCopy(), client.MergeFromWithOptimisticLock{})
				delete(obj.Labels, references.LabelKeyUnusedAt)
				obj.Labels[references.LabelKeyUsed] = references.LabelValueUsed
				if err := r.TargetClient.Patch(ctx, obj, patch); err != nil {
					results <- err
				}
				return
			}
		})
	}

	for _, o := range objectsToGarbageCollect {
		obj := o

		wg.StartWithContext(ctx, func(ctx context.Context) {
			unusedAtStr, ok := obj.Labels[references.LabelKeyUnusedAt]
			if !ok {
				// If the object does not have the unusedAt label, set the label to the current time.
				patch := client.StrategicMergeFrom(obj.DeepCopy(), client.MergeFromWithOptimisticLock{})
				obj.Labels[references.LabelKeyUnusedAt] = r.Clock.Now().UTC().Format(time.RFC3339)
				obj.Labels[references.LabelKeyUsed] = references.LabelValueUnused
				if err := r.TargetClient.Patch(ctx, obj, patch); err != nil {
					results <- err
				}
				// requeue soon to delete the object
				requeueNeeded <- true
				return
			}

			unusedAt, err := time.Parse(time.RFC3339, unusedAtStr)
			if err != nil {
				errorList = multierror.Append(errorList, fmt.Errorf("failed to parse unusedAt %s: %w", unusedAtStr, err))
				return
			}

			if unusedAt.Add(*r.MinimumObjectLifetime).UTC().After(r.Clock.Now().UTC()) {
				// requeue soon to delete the object
				requeueNeeded <- true
				// Do not consider objects for garbage collection that were marked as unused only recently.
				return
			}

			log.Info("Delete resource",
				"kind", obj.Kind,
				"namespace", obj.Namespace,
				"name", obj.Name,
			)

			deleteOptions := []client.DeleteOption{
				client.Preconditions(metav1.Preconditions{
					ResourceVersion: ptr.To(obj.GetResourceVersion()),
					UID:             ptr.To(obj.GetUID()),
				}),
			}
			if err := r.TargetClient.Delete(ctx, obj, deleteOptions...); client.IgnoreNotFound(err) != nil {
				results <- err
			}
		})
	}

	go func() {
		wg.Wait()
		close(results)
		close(requeueNeeded)
	}()

	requeueAfter := r.Config.SyncPeriod.Duration

	var err error
	resultsOK := true
	requeueNeededOK := true
	for resultsOK || requeueNeededOK {
		select {
		case err, resultsOK = <-results:
			if resultsOK {
				errorList = multierror.Append(errorList, err)
				requeueAfter = *r.MinimumObjectLifetime + 1*time.Minute
			}
		case _, requeueNeededOK = <-requeueNeeded:
			if requeueNeededOK {
				requeueAfter = *r.MinimumObjectLifetime + 1*time.Minute
			}
		}
	}

	return reconcile.Result{Requeue: true, RequeueAfter: requeueAfter}, errorList.ErrorOrNil()
}

type objectId struct {
	kind      string
	namespace string
	name      string
}
