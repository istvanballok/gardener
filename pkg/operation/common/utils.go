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

func LokiPvcExists(ctx context.Context, k8sClient client.Client, namespace string, log logr.Logger) (bool, error) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-loki-0",
			Namespace: namespace,
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("loki2vali: %v: Loki PVC not found", namespace))
			return false, nil
		} else {
			return false, err
		}
	}
	log.Info(fmt.Sprintf("loki2vali: %v: Loki PVC found", namespace))
	return true, nil
}

// DeleteLokiRetainPvc deletes all Loki resources in a given namespace.
func DeleteLokiRetainPvc(ctx context.Context, k8sClient client.Client, namespace string, log logr.Logger) error {
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

	// Loki currently needs 30s to terminate because after 1s of graceful shutdown preparation it waits for 30s until it is eventually
	// forcefully killed by the kubelet. We reduce the graceful termination timeout from 30s to 5s here so that the migration from loki to vali
	// can succeed in the 30s deadline of the shoot reconciliation.
	log.Info(fmt.Sprintf("loki2vali: %v: Deleting the pod loki-0 with a grace period of 5 seconds.", namespace))
	k8sClient.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "loki-0", Namespace: namespace}}, client.GracePeriodSeconds(5))

	log.Info(fmt.Sprintf("loki2vali: %v: Deleting the other artifacts like the loki Statefulset.", namespace))
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

// RenameLokiPvcToValiPvc "renames" the PVC used by loki "loki-loki-0" to "vali-vali-0" so that it can be used by vali.
// It is not possible in kubernetes to rename a PVC, so we have to create a new PVC and delete the old one.
// The PV is not deleted, but reused by the new PVC. To achieve this, the PV's ReclaimPolicy is temporarily set to "Retain".
// For this to succeed it is important that the old PVC is deleted before the new one is created.
func RenameLokiPvcToValiPvc(ctx context.Context, k8sClient client.Client, namespace string, log logr.Logger) error {
	log.Info(fmt.Sprintf("loki2vali: %v: Entering RenameLokiPvcToValiPvc.", namespace))

	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		log.Info(fmt.Sprintf("loki2vali: %v: Context deadline is in %v", namespace, time.Until(deadline)))
	} else {
		log.Info(fmt.Sprintf("loki2vali: %v: No context deadline", namespace))
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Get Loki PVC", namespace))
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-loki-0",
			Namespace: namespace,
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("loki2vali: %v: Loki PVC not found, nothing to do", namespace))
			return nil
		} else {
			return err
		}
	}
	log.Info(fmt.Sprintf("loki2vali: %v: Loki PVC found, attempting to rename it: loki-loki-0 --> vali-vali-0, so that it can be reused by the vali-0 pod", namespace))

	log.Info(fmt.Sprintf("loki2vali: %v: Verify that the pod loki-0 is not running, otherwise we cannot delete the PVC", namespace))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-0",
			Namespace: namespace,
		},
	}
	for {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info(fmt.Sprintf("loki2vali: %v: pod loki-0 not found, continuing with the rename", namespace))
				break
			} else {
				return err
			}
		}
		if hasDeadline {
			log.Info(fmt.Sprintf("loki2vali: %v: Waiting for pod loki-0 to terminate, time until deadline: %v", namespace, time.Until(deadline)))
			if time.Until(deadline) < 0 {
				return fmt.Errorf("loki2vali: %v: Timeout while waiting for the loki-0 pod to terminate", namespace)
			}
		} else {
			log.Info(fmt.Sprintf("loki2vali: %v: Waiting for pod loki-0 to terminate", namespace))
		}
		time.Sleep(1 * time.Second)
	}

	if hasDeadline {
		if time.Until(deadline) < 15*time.Second {
			return fmt.Errorf("loki2vali: %v: Bailing out to avoid hitting context deadline in this reconciliation loop, time remaining is %v", namespace, time.Until(deadline))
		} else {
			log.Info(fmt.Sprintf("loki2vali: %v: Context deadline is %v in the future, continuing with rename", namespace, time.Until(deadline)))
		}
	} else {
		log.Info(fmt.Sprintf("loki2vali: %v: Context has no deadline, continuing with rename", namespace))
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Get Loki PV", namespace))
	pvId := pvc.Spec.VolumeName
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvId,
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Change the Loki PV's PersistentVolumeReclaimPolicy to be Retain temporarily. This way the PV is not deleted when the PVC is deleted during the migration of Loki to Vali.", namespace))
	patch := client.MergeFrom(pv.DeepCopy())
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if err := k8sClient.Patch(ctx, pv, patch); err != nil {
		return err
	}

	defer func() error {
		log.Info(fmt.Sprintf("loki2vali: %v: Starting to wait, deadline in %v.", namespace, time.Until(deadline)))
		time.Sleep(20 * time.Second)
		log.Info(fmt.Sprintf("loki2vali: %v: Finished waiting, deadline in %v.", namespace, time.Until(deadline)))
		log.Info(fmt.Sprintf("loki2vali: %v: Change the Vali PV's PersistentVolumeReclaimPolicy back to be Delete. The reclaim policy should be Delete so that the PV is deleted when the PVC is deleted regularly.", namespace))
		patch = client.MergeFrom(pv.DeepCopy())
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete

		// make sure the the patch gets a context that has at least 5 seconds left till its deadline
		if hasDeadline && time.Until(deadline) < 5 {
			newContext, cancel := context.WithDeadline(context.TODO(), time.Now().Add(5*time.Second))
			defer cancel()
			ctx = newContext
		}

		if err := k8sClient.Patch(ctx, pv, patch); err != nil {
			return err
		}
		log.Info(fmt.Sprintf("loki2vali: %v: Successfully changed the Vali PV's PersistentVolumeReclaimPolicy back to be Delete.", namespace))
		return nil
	}()

	// Wait a bit so that the two changes (PV's reclaim policy and PVC's deletion) can be processed in this order by the kube-controller-manager.
	time.Sleep(1 * time.Second)

	log.Info(fmt.Sprintf("loki2vali: %v: Delete Loki PVC.", namespace))
	if err := kubernetesutils.DeleteObject(ctx, k8sClient, pvc); err != nil {
		return err
	}

	lokiPvc := &corev1.PersistentVolumeClaim{}
	for {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), lokiPvc); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info(fmt.Sprintf("loki2vali: %v: Loki PVC not found, deletion completed.", namespace))
				break
			} else {
				return err
			}
		}
		if hasDeadline {
			log.Info(fmt.Sprintf("loki2vali: %v: Wait for Loki PVC deletion to complete, time until deadline: %v", namespace, time.Until(deadline)))
			if time.Until(deadline) < 0 {
				return fmt.Errorf("loki2vali: %v: Timeout while waiting for the loki-loki-0 PVC to terminate", namespace)
			}
		} else {
			log.Info(fmt.Sprintf("loki2vali: %v: Wait for Loki PVC deletion to complete.", namespace))
		}
		time.Sleep(1 * time.Second)
	}

	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("loki2vali: %v: Loki PV is not found. It seems that it was deleted as a consequence of deleting the PVC, although the PV's ReclaimPolicy was set to Retain. The system can recover from this error: a new PV will be created in the next iteration, although we lost the logs on the old loki disk.", namespace))
		}
		return err
	}
	if pv.ObjectMeta.DeletionTimestamp != nil {
		log.Info(fmt.Sprintf("loki2vali: %v: Loki PV is not found. It seems that it was deleted as a consequence of deleting the PVC, although the PV's ReclaimPolicy was set to Retain. The system can recover from this error: a new PV will be created in the next iteration, although we lost the logs on the old loki disk.", namespace))
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Remove Loki PV's ClaimRef. This is needed so that it can be bound to the soon to be created Vali PVC.", namespace))
	patch = client.MergeFrom(pv.DeepCopy())
	pv.Spec.ClaimRef = nil
	if err := k8sClient.Patch(ctx, pv, patch); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Create Vali PVC. It is basically a copy of the Loki PVC, with the name changed from loki-loki-0 to vali-vali-0.", namespace))
	labels := pvc.DeepCopy().Labels
	for k, v := range labels {
		if v == "loki" {
			labels[k] = "vali"
		}
	}

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
		return fmt.Errorf("loki2vali: %v: Create Vali PVC failed. %w", namespace, err)
	}

	for {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(valiPvc), valiPvc); err != nil {
			return err
		}
		if valiPvc.Status.Phase == corev1.ClaimBound {
			log.Info(fmt.Sprintf("loki2vali: %v: PVC is bound.", namespace))
			break
		} else if valiPvc.Status.Phase == corev1.ClaimLost {
			if err := kubernetesutils.DeleteObject(ctx, k8sClient, valiPvc); err != nil {
				return err
			}
			valiPvc := &corev1.PersistentVolumeClaim{}
			for {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), valiPvc); err != nil {
					if apierrors.IsNotFound(err) {
						log.Info(fmt.Sprintf("loki2vali: %v: Vali PVC not found, deletion completed.", namespace))
						break
					} else {
						return fmt.Errorf("loki2vali: %v: Vali PVC is in Lost status, deleted it to avoid deadlock. The deletion might have failed: %w", namespace, err)
					}
				}
				if hasDeadline {
					log.Info(fmt.Sprintf("loki2vali: %v: Wait for Loki PVC deletion to complete, time until deadline: %v", namespace, time.Until(deadline)))
					if time.Until(deadline) < 0 {
						return fmt.Errorf("loki2vali: %v: Timeout while waiting for the loki-loki-0 PVC to be deleted", namespace)
					}
				} else {
					log.Info(fmt.Sprintf("loki2vali: %v: Wait for Loki PVC deletion to complete.", namespace))
				}
				time.Sleep(1 * time.Second)
			}
			return fmt.Errorf("loki2vali: %v: Vali PVC is in Lost status, deleted it to avoid deadlock. A new PVC will be created in the next iteration", namespace)
		}
		if hasDeadline {
			log.Info(fmt.Sprintf("loki2vali: %v: Waiting for Vali PVC to be bound, time until deadline: %v", namespace, time.Until(deadline)))
			if time.Until(deadline) < 0 {
				return fmt.Errorf("loki2vali: %v: Timeout while waiting for the loki-loki-0 PVC to be bound", namespace)
			}
		} else {
			log.Info(fmt.Sprintf("loki2vali: %v: Waiting for Vali PVC to be bound.", namespace))
		}
		time.Sleep(1 * time.Second)
	}

	log.Info(fmt.Sprintf("loki2vali: %v: Successfully replaced PVC loki-loki-0 with vali-vali-0.", namespace))
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
