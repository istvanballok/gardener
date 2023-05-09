// Copyright 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package seed_test

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/gardener/gardener/pkg/gardenlet/controller/seed/seed"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
)

var _ = Describe("Reconcile", func() {

	Describe("#ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame", func() {
		const (
			valiPVCName         = "vali-vali-0"
			valiStatefulSetName = "vali"
			gardenNamespace     = "garden"
		)

		var (
			ctrl              *gomock.Controller
			runtimeClient     *mockclient.MockClient
			ctx               = context.TODO()
			log               = logr.Discard()
			valiPVCObjectMeta = metav1.ObjectMeta{
				Name:      valiPVCName,
				Namespace: gardenNamespace,
			}
			valiPVC = &corev1.PersistentVolumeClaim{
				ObjectMeta: valiPVCObjectMeta,
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.ResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							"storage": resource.MustParse("100Gi"),
						},
					},
				},
			}
			patch       = client.MergeFrom(valiPVC.DeepCopy())
			statefulset = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      valiStatefulSetName,
					Namespace: gardenNamespace,
				},
			}
			scaledToZeroLokiStatefulset = appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       valiStatefulSetName,
					Namespace:  gardenNamespace,
					Generation: 2,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.Int32(0),
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2,
					Replicas:           0,
					AvailableReplicas:  0,
				},
			}
			zeroReplicaRawPatch     = client.RawPatch(types.MergePatchType, []byte(`{"spec":{"replicas":0}}`))
			errNotFound             = &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonNotFound}}
			errForbidden            = &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonForbidden}}
			new200GiStorageQuantity = resource.MustParse("200Gi")
			new100GiStorageQuantity = resource.MustParse("100Gi")
			new80GiStorageQuantity  = resource.MustParse("80Gi")
			valiPVCKey              = kubernetesutils.Key("garden", "vali-vali-0")
			valiStatefulSetKey      = kubernetesutils.Key("garden", "vali")
			funcGetLokiPVC          = func(_ context.Context, _ types.NamespacedName, pvc *corev1.PersistentVolumeClaim, _ ...client.GetOption) error {
				*pvc = *valiPVC
				return nil
			}
			funcGetScaledToZeroLokiStatefulset = func(_ context.Context, _ types.NamespacedName, sts *appsv1.StatefulSet, _ ...client.GetOption) error {
				*sts = scaledToZeroLokiStatefulset
				return nil
			}
			funcPatchTo200GiStorage = func(_ context.Context, pvc *corev1.PersistentVolumeClaim, _ client.Patch, _ ...interface{}) error {
				if pvc.Spec.Resources.Requests.Storage().Cmp(resource.MustParse("200Gi")) != 0 {
					return fmt.Errorf("expect 200Gi found %v", *pvc.Spec.Resources.Requests.Storage())
				}
				return nil
			}
			objectOfTypePVC = gomock.AssignableToTypeOf(&corev1.PersistentVolumeClaim{})
			objectOfTypeSTS = gomock.AssignableToTypeOf(&appsv1.StatefulSet{})
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			runtimeClient = mockclient.NewMockClient(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("should patch garden/vali's PVC when new size is greater than the current one", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Patch(ctx, objectOfTypePVC, gomock.AssignableToTypeOf(patch)).DoAndReturn(funcPatchTo200GiStorage)
			runtimeClient.EXPECT().Delete(ctx, statefulset)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new200GiStorageQuantity)).To(Succeed())
		})

		It("should delete garden/vali's PVC when new size is less than the current one", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Delete(ctx, valiPVC)
			runtimeClient.EXPECT().Delete(ctx, statefulset)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).To(Succeed())
		})

		It("shouldn't do anything when garden/vali's PVC is missing", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).Return(errNotFound)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).To(Succeed())
		})

		It("shouldn't do anything when garden/vali's PVC storage is the same as the new one", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new100GiStorageQuantity)).To(Succeed())
		})

		It("should proceed with the garden/vali's PVC resizing when Loki StatefulSet is missing", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch).Return(errNotFound)
			runtimeClient.EXPECT().Patch(ctx, objectOfTypePVC, gomock.AssignableToTypeOf(patch)).DoAndReturn(funcPatchTo200GiStorage)
			runtimeClient.EXPECT().Delete(ctx, statefulset).Return(errNotFound)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new200GiStorageQuantity)).To(Succeed())
		})

		It("should succeed with the garden/vali's PVC resizing when Loki StatefulSet was deleted during function execution", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Patch(ctx, objectOfTypePVC, gomock.AssignableToTypeOf(patch)).DoAndReturn(funcPatchTo200GiStorage)
			runtimeClient.EXPECT().Delete(ctx, statefulset).Return(errNotFound)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new200GiStorageQuantity)).To(Succeed())
		})

		It("should not fail with patching garden/vali's PVC when the PVC itself was deleted during function execution", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Patch(ctx, objectOfTypePVC, gomock.AssignableToTypeOf(patch)).Return(errNotFound)
			runtimeClient.EXPECT().Delete(ctx, statefulset)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new200GiStorageQuantity)).To(Succeed())
		})

		It("should not fail with deleting garden/vali's PVC when the PVC itself was deleted during function execution", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Delete(ctx, valiPVC).Return(errNotFound)
			runtimeClient.EXPECT().Delete(ctx, statefulset)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).To(Succeed())
		})

		It("should not neglect errors when getting garden/vali's PVC", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).ToNot(Succeed())
		})

		It("should not neglect errors when patching garden/vali's StatefulSet", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).ToNot(Succeed())
		})

		It("should not neglect errors when getting garden/vali's StatefulSet", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).ToNot(Succeed())
		})

		It("should not neglect errors when patching garden/vali's PVC", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Patch(ctx, objectOfTypePVC, gomock.AssignableToTypeOf(patch)).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new200GiStorageQuantity)).ToNot(Succeed())
		})

		It("should not neglect errors when deleting garden/vali's PVC", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Delete(ctx, valiPVC).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).ToNot(Succeed())
		})

		It("should not neglect errors when deleting garden/vali's StatefulSet", func() {
			runtimeClient.EXPECT().Get(ctx, valiPVCKey, objectOfTypePVC).DoAndReturn(funcGetLokiPVC)
			runtimeClient.EXPECT().Patch(ctx, statefulset, zeroReplicaRawPatch)
			runtimeClient.EXPECT().Get(gomock.Any(), valiStatefulSetKey, objectOfTypeSTS).DoAndReturn(funcGetScaledToZeroLokiStatefulset)
			runtimeClient.EXPECT().Delete(ctx, valiPVC)
			runtimeClient.EXPECT().Delete(ctx, statefulset).Return(errForbidden)
			Expect(ResizeOrDeleteLokiDataVolumeIfStorageNotTheSame(ctx, log, runtimeClient, new80GiStorageQuantity)).ToNot(Succeed())
		})
	})

	Describe("#CleanupOldFluentBit", func() {
		const (
			fluentBitName   = "fluent-bit"
			gardenNamespace = "garden"
		)

		var (
			ctrl          *gomock.Controller
			runtimeClient *mockclient.MockClient
			ctx           = context.TODO()

			fluentBitClusterRole        = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: fluentBitName + "-read"}}
			fluentBitClusterRoleBinding = &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: fluentBitName + "-read"}}
			fluentBitDaemonSet          = &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: fluentBitName, Namespace: gardenNamespace}}
			fluentBitService            = &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: fluentBitName, Namespace: gardenNamespace}}
			fluentBitServiceAccount     = &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: fluentBitName, Namespace: gardenNamespace}}

			fluentOperatorOwnerReferenceKind       = "FluentBit"
			fluentOperatorOwnerReferenceAPIVersion = "fluentbit.fluent.io/v1alpha2"

			managedByOperatorDaemonSet = &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fluentBitName,
					Namespace: gardenNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: fluentOperatorOwnerReferenceKind, APIVersion: fluentOperatorOwnerReferenceAPIVersion},
					},
				},
			}

			managedByOperatorService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fluentBitName,
					Namespace: gardenNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: fluentOperatorOwnerReferenceKind, APIVersion: fluentOperatorOwnerReferenceAPIVersion},
					},
				},
			}

			managedByOperatorServiceAccount = &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fluentBitName,
					Namespace: gardenNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: fluentOperatorOwnerReferenceKind, APIVersion: fluentOperatorOwnerReferenceAPIVersion},
					},
				},
			}

			funcGetManagedByOperatorFluentBitDaemonSet = func(_ context.Context, _ types.NamespacedName, ds *appsv1.DaemonSet, _ ...client.GetOption) error {
				*ds = *managedByOperatorDaemonSet
				return nil
			}

			funcGetNotManagedByOperatorFluentBitDaemonSet = func(_ context.Context, _ types.NamespacedName, ds *appsv1.DaemonSet, _ ...client.GetOption) error {
				*ds = *fluentBitDaemonSet
				return nil
			}

			funcGetManagedByOperatorFluentBitService = func(_ context.Context, _ types.NamespacedName, svc *corev1.Service, _ ...client.GetOption) error {
				*svc = *managedByOperatorService
				return nil
			}

			funcGetNotManagedByOperatorFluentBitService = func(_ context.Context, _ types.NamespacedName, svc *corev1.Service, _ ...client.GetOption) error {
				*svc = *fluentBitService
				return nil
			}

			funcGetManagedByOperatorFluentBitServiceAccount = func(_ context.Context, _ types.NamespacedName, sa *corev1.ServiceAccount, _ ...client.GetOption) error {
				*sa = *managedByOperatorServiceAccount
				return nil
			}

			funcGetNotManagedByOperatorFluentBitServiceAccount = func(_ context.Context, _ types.NamespacedName, sa *corev1.ServiceAccount, _ ...client.GetOption) error {
				*sa = *fluentBitServiceAccount
				return nil
			}
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			runtimeClient = mockclient.NewMockClient(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("should delete all fluent bit resources if they are not managed by the fluent operator", func() {
			gomock.InOrder(
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitDaemonSet.GetNamespace(), fluentBitDaemonSet.GetName()), fluentBitDaemonSet).DoAndReturn(funcGetNotManagedByOperatorFluentBitDaemonSet),
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitService.GetNamespace(), fluentBitService.GetName()), fluentBitService).DoAndReturn(funcGetNotManagedByOperatorFluentBitService),
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitServiceAccount.GetNamespace(), fluentBitServiceAccount.GetName()), fluentBitServiceAccount).DoAndReturn(funcGetNotManagedByOperatorFluentBitServiceAccount),
				runtimeClient.EXPECT().Delete(ctx, fluentBitDaemonSet),
				runtimeClient.EXPECT().Delete(ctx, fluentBitService),
				runtimeClient.EXPECT().Delete(ctx, fluentBitServiceAccount),
				runtimeClient.EXPECT().Delete(ctx, fluentBitClusterRole),
				runtimeClient.EXPECT().Delete(ctx, fluentBitClusterRoleBinding),
			)

			Expect(CleanupOldFluentBit(ctx, runtimeClient)).To(Succeed())
		})

		It("should not delete resources if they are managed by the fluent operator", func() {
			gomock.InOrder(
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitDaemonSet.GetNamespace(), fluentBitDaemonSet.GetName()), fluentBitDaemonSet).DoAndReturn(funcGetManagedByOperatorFluentBitDaemonSet),
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitService.GetNamespace(), fluentBitService.GetName()), fluentBitService).DoAndReturn(funcGetManagedByOperatorFluentBitService),
				runtimeClient.EXPECT().Get(ctx, kubernetesutils.Key(fluentBitServiceAccount.GetNamespace(), fluentBitServiceAccount.GetName()), fluentBitServiceAccount).DoAndReturn(funcGetManagedByOperatorFluentBitServiceAccount),
				runtimeClient.EXPECT().Delete(ctx, fluentBitClusterRole),
				runtimeClient.EXPECT().Delete(ctx, fluentBitClusterRoleBinding),
			)

			Expect(CleanupOldFluentBit(ctx, runtimeClient)).To(Succeed())
		})
	})
})
