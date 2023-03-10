// Copyright 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package common

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
)

// FilterEntriesByPrefix returns a list of strings which begin with the given prefix.
func FilterEntriesByPrefix(prefix string, entries []string) []string {
	var result []string
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
}

// ComputeOffsetIP parses the provided <subnet> and offsets with the value of <offset>.
// For example, <subnet> = 100.64.0.0/11 and <offset> = 10 the result would be 100.64.0.10
// IPv6 and IPv4 is supported.
func ComputeOffsetIP(subnet *net.IPNet, offset int64) (net.IP, error) {
	if subnet == nil {
		return nil, fmt.Errorf("subnet is nil")
	}

	isIPv6 := false

	bytes := subnet.IP.To4()
	if bytes == nil {
		isIPv6 = true
		bytes = subnet.IP.To16()
	}

	ip := net.IP(big.NewInt(0).Add(big.NewInt(0).SetBytes(bytes), big.NewInt(offset)).Bytes())

	if !subnet.Contains(ip) {
		return nil, fmt.Errorf("cannot compute IP with offset %d - subnet %q too small", offset, subnet)
	}

	// there is no broadcast address on IPv6
	if isIPv6 {
		return ip, nil
	}

	for i := range ip {
		// IP address is not the same, so it's not the broadcast ip.
		if ip[i] != ip[i]|^subnet.Mask[i] {
			return ip.To4(), nil
		}
	}

	return nil, fmt.Errorf("computed IPv4 address %q is broadcast for subnet %q", ip, subnet)
}

// GenerateAddonConfig returns the provided <values> in case <enabled> is true. Otherwise, nil is
// being returned.
func GenerateAddonConfig(values map[string]interface{}, enabled bool) map[string]interface{} {
	v := map[string]interface{}{
		"enabled": enabled,
	}
	if enabled {
		for key, value := range values {
			v[key] = value
		}
	}
	return v
}

// DeleteVali  deletes all resources of the Vali in a given namespace.
func DeleteVali(ctx context.Context, k8sClient client.Client, namespace string) error {
	resources := []client.Object{
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-vali", Namespace: namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-to-vali", Namespace: namespace}},
		&hvpav1alpha1.Hvpa{ObjectMeta: metav1.ObjectMeta{Name: "vali", Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "vali", Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "logging", Namespace: namespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "vali", Namespace: namespace}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "vali", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shoot-access-valitail", Namespace: namespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "vali-vali-0", Namespace: namespace}},
	}

	if err := kubernetesutils.DeleteObjects(ctx, k8sClient, resources...); err != nil {
		return err
	}

	deleteOptions := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			v1beta1constants.GardenRole: "logging",
			v1beta1constants.LabelApp:   "vali",
		},
	}

	return k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, deleteOptions...)
}

// DeleteLoki  deletes all resources of the Loki in a given namespace.
func DeleteLokiRetainPvc(ctx context.Context, k8sClient client.Client, namespace string) error {
	resources := []client.Object{
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-loki", Namespace: namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-to-loki", Namespace: namespace}},
		&hvpav1alpha1.Hvpa{ObjectMeta: metav1.ObjectMeta{Name: "loki", Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "loki", Namespace: namespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "loki", Namespace: namespace}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "loki", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shoot-access-promtail", Namespace: namespace}},
		// We retain the PVC and reuse it with Vali.
		//&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "loki-loki-0", Namespace: namespace}},
	}

	if err := kubernetesutils.DeleteObjects(ctx, k8sClient, resources...); err != nil {
		return err
	}

	deleteOptions := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			v1beta1constants.GardenRole: "logging",
			v1beta1constants.LabelApp:   "loki",
		},
	}

	return k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, deleteOptions...)
}

func RenameLokiPvcToValiPvc(ctx context.Context, k8sClient client.Client, namespace string, log logr.Logger) error {
	log.Info("Renaming Loki PVC to Vali")

	// Get Loki PVC.
	log.Info("Step 1/9: Get Loki PVC")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-loki-0",
			Namespace: namespace,
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Loki PVC not found, skipping rename")
			return nil
		} else {
			return err
		}
	}

	// Get Loki PV.
	log.Info("Step 2/9: Get Loki PV")
	pvId := pvc.Spec.VolumeName
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvId,
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		return err
	}

	// When the Loki PVC is deleted, the PV is retained.
	log.Info("Step 3/9: Patch Loki PV's PersistentVolumeReclaimPolicy")
	patch := client.MergeFrom(pv.DeepCopy())
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if err := k8sClient.Patch(ctx, pv, patch); err != nil {
		return err
	}

	// Allow for other controllers to observe the reclaim policy change in the PV before attempting to delete the PVC.
	// Otherwise, if the PVC is deleted right away, the 2 changes (PV patch, PVC deletion) might be processed by other controllers out of order
	// which would lead to deleting the PV as a side effect of deleting the PVC.
	log.Info("Step 4/9: Sleeping for 10 seconds")
	time.Sleep(10 * time.Second)

	// Delete Loki PVC
	log.Info("Step 5/9: Delete Loki PVC")
	if err := kubernetesutils.DeleteObject(ctx, k8sClient, pvc); err != nil {
		return err
	}

	log.Info("Step 6/9: Sleeping for 10 seconds")
	time.Sleep(10 * time.Second)

	// We assert that the PV is still there
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Loki PV not found, it seems that patching the PV's reclaim policy was processed after deleting the PVC and we lost the PV. We get on with a new PV in the next iteration.")
		}
		return err
	}
	if pv.ObjectMeta.DeletionTimestamp != nil {
		return fmt.Errorf("The Loki PV has been deleted. We get on with a new PV in the next iteration.")
	}

	// Delete the ClaimRef from the PV.
	log.Info("Step 7/9: Remove Loki PV's ClaimRef")
	patch = client.MergeFrom(pv.DeepCopy())
	pv.Spec.ClaimRef = nil
	if err := k8sClient.Patch(ctx, pv, patch); err != nil {
		return err
	}

	// Recreate the PVC with the vali name.
	log.Info("Step 8/9: Create Vali PVC")

	// Copy and adapt labels for the new PVC
	labels := pvc.DeepCopy().Labels
	for k, v := range labels {
		if v == "loki" {
			labels[k] = "vali"
		}
	}

	// Create new PVC for vali.
	valiPvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   pvc.Namespace,
			Name:        "vali-vali-0",
			Annotations: pvc.DeepCopy().Annotations,
			Labels:      labels,
		},
		Spec: *pvc.Spec.DeepCopy(),
	}

	if err := k8sClient.Create(ctx, valiPvc); err != nil {
		log.Error(err, "Step 8/9: Create Vali PVC failed")
		return err
	}

	// Change the PV ReclaimPolicy back to Delete, so that when a vali PVC is deleted, we don't leak the PV.
	log.Info("Step 9/9: Patch Vali PV's PersistentVolumeReclaimPolicy")
	patch = client.MergeFrom(pv.DeepCopy())
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
	if err := k8sClient.Patch(ctx, pv, patch); err != nil {
		return err
	}

	log.Info("Successfully renamed Loki PVC to Vali")

	return nil
}

// DeleteSeedLoggingStack deletes all seed resource of the logging stack in the garden namespace.
func DeleteSeedLoggingStack(ctx context.Context, k8sClient client.Client) error {
	resources := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit-config", Namespace: v1beta1constants.GardenNamespace}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit", Namespace: v1beta1constants.GardenNamespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-fluentbit", Namespace: v1beta1constants.GardenNamespace}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit-read"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit-read"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit", Namespace: v1beta1constants.GardenNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "fluent-bit", Namespace: v1beta1constants.GardenNamespace}},
	}

	if err := kubernetesutils.DeleteObjects(ctx, k8sClient, resources...); err != nil {
		return err
	}

	return DeleteVali(ctx, k8sClient, v1beta1constants.GardenNamespace)
}

// DeleteAlertmanager deletes all resources of the Alertmanager in a given namespace.
func DeleteAlertmanager(ctx context.Context, k8sClient client.Client, namespace string) error {
	objs := []client.Object{
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      v1beta1constants.StatefulSetNameAlertManager,
				Namespace: namespace,
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager",
				Namespace: namespace,
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager-client",
				Namespace: namespace,
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager",
				Namespace: namespace,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager-basic-auth",
				Namespace: namespace,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager-config",
				Namespace: namespace,
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager-db-alertmanager-0",
				Namespace: namespace,
			},
		},
	}

	return kubernetesutils.DeleteObjects(ctx, k8sClient, objs...)
}

// DeletePlutono deletes the monitoring stack for the shoot owner.
func DeletePlutono(ctx context.Context, k8sClient kubernetes.Interface, namespace string) error {
	if k8sClient == nil {
		return fmt.Errorf("require kubernetes client")
	}

	deleteOptions := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			"component": "plutono",
		},
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &appsv1.Deployment{}, append(deleteOptions, client.PropagationPolicy(metav1.DeletePropagationForeground))...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &corev1.ConfigMap{}, deleteOptions...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &networkingv1.Ingress{}, deleteOptions...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &corev1.Secret{}, deleteOptions...); err != nil {
		return err
	}

	return client.IgnoreNotFound(
		k8sClient.Client().Delete(
			ctx,
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plutono",
					Namespace: namespace,
				}},
		),
	)
}

// DeleteGrafana deletes the Grafana resources that are no longer necessary due to the migration to Plutono.
func DeleteGrafana(ctx context.Context, k8sClient kubernetes.Interface, namespace string) error {
	if k8sClient == nil {
		return fmt.Errorf("require kubernetes client")
	}

	deleteOptions := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			"component": "grafana",
		},
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &appsv1.Deployment{}, append(deleteOptions, client.PropagationPolicy(metav1.DeletePropagationForeground))...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &corev1.ConfigMap{}, deleteOptions...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &networkingv1.Ingress{}, deleteOptions...); err != nil {
		return err
	}

	if err := k8sClient.Client().DeleteAllOf(ctx, &corev1.Secret{}, deleteOptions...); err != nil {
		return err
	}

	if err := k8sClient.Client().Delete(
		ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grafana",
				Namespace: namespace,
			}},
	); client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}
