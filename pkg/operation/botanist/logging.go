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

package botanist

import (
	"context"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencore "github.com/gardener/gardener/pkg/apis/core"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/features"
	gardenlethelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/helper"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/operation/botanist/component/logging/eventlogger"
	"github.com/gardener/gardener/pkg/operation/common"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/images"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/pkg/utils/secrets"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
)

// DeploySeedLogging will install the Helm release "seed-bootstrap/charts/vali" in the Seed clusters.
func (b *Botanist) DeploySeedLogging(ctx context.Context) error {
	if !b.Shoot.IsShootControlPlaneLoggingEnabled(b.Config) {
		return b.destroyShootLoggingStack(ctx)
	}

	//TODO(rickardsjp, istvanballok): Remove in the next release once the Loki to Vali migration is complete.
	if err := b.destroyLokiBasedShootLoggingStackRetainingPvc(ctx); err != nil {
		return err
	}

	// If a Loki PVC exists, rename it to Vali.
	if err := b.renameLokiPvcToVali(ctx); err != nil {
		return err
	}

	seedImages, err := b.InjectSeedSeedImages(map[string]interface{}{},
		images.ImageNameVali,
		images.ImageNameValiCurator,
		images.ImageNameKubeRbacProxy,
		images.ImageNameTelegraf,
		images.ImageNameTune2fs,
	)
	if err != nil {
		return err
	}

	if b.isShootEventLoggerEnabled() {
		if err := b.Shoot.Components.Logging.ShootEventLogger.Deploy(ctx); err != nil {
			return err
		}
	} else {
		if err := b.Shoot.Components.Logging.ShootEventLogger.Destroy(ctx); err != nil {
			return err
		}
	}

	valiValues := map[string]interface{}{
		"global":      seedImages,
		"replicas":    b.Shoot.GetReplicas(1),
		"clusterType": "shoot",
	}

	// check if vali is enabled in gardenlet config, default is true
	if !gardenlethelper.IsValiEnabled(b.Config) {
		// Because ShootNodeLogging is installed as part of the Vali pod
		// we have to delete it too in case it was previously deployed
		if err := b.destroyShootNodeLogging(ctx); err != nil {
			return err
		}
		return common.DeleteVali(ctx, b.SeedClientSet.Client(), b.Shoot.SeedNamespace)
	}

	hvpaValues := make(map[string]interface{})
	hvpaEnabled := gardenletfeatures.FeatureGate.Enabled(features.HVPA)
	if b.ManagedSeed != nil {
		hvpaEnabled = gardenletfeatures.FeatureGate.Enabled(features.HVPAForShootedSeed)
	}

	if b.isShootNodeLoggingEnabled() {
		if err := b.Shoot.Components.Logging.ShootRBACProxy.Deploy(ctx); err != nil {
			return err
		}

		genericTokenKubeconfigSecret, found := b.SecretsManager.Get(v1beta1constants.SecretNameGenericTokenKubeconfig)
		if !found {
			return fmt.Errorf("secret %q not found", v1beta1constants.SecretNameGenericTokenKubeconfig)
		}

		ingressClass, err := gardenerutils.ComputeNginxIngressClassForSeed(b.Seed.GetInfo(), b.Seed.GetInfo().Status.KubernetesVersion)
		if err != nil {
			return err
		}

		ingressTLSSecret, err := b.SecretsManager.Generate(ctx, &secrets.CertificateSecretConfig{
			Name:                        "vali-tls",
			CommonName:                  b.ComputeValiHost(),
			Organization:                []string{"gardener.cloud:monitoring:ingress"},
			DNSNames:                    b.ComputeValiHosts(),
			CertType:                    secrets.ServerCert,
			Validity:                    &ingressTLSCertificateValidity,
			SkipPublishingCACertificate: true,
		}, secretsmanager.SignedByCA(v1beta1constants.SecretNameCACluster))
		if err != nil {
			return err
		}

		valiValues["rbacSidecarEnabled"] = true
		valiValues["ingress"] = map[string]interface{}{
			"class": ingressClass,
			"hosts": []map[string]interface{}{
				{
					"hostName":    b.ComputeValiHost(),
					"secretName":  ingressTLSSecret.Name,
					"serviceName": "vali",
					"servicePort": 8080,
					"backendPath": "/vali/api/v1/push",
				},
			},
		}
		valiValues["genericTokenKubeconfigSecretName"] = genericTokenKubeconfigSecret.Name
	} else {
		if err := b.destroyShootNodeLogging(ctx); err != nil {
			return err
		}
	}

	hvpaValues["enabled"] = hvpaEnabled
	valiValues["hvpa"] = hvpaValues
	valiValues["priorityClassName"] = v1beta1constants.PriorityClassNameShootControlPlane100

	if hvpaEnabled {
		currentResources, err := kubernetesutils.GetContainerResourcesInStatefulSet(ctx, b.SeedClientSet.Client(), kubernetesutils.Key(b.Shoot.SeedNamespace, "vali"))
		if err != nil {
			return err
		}
		if len(currentResources) != 0 && currentResources["vali"] != nil {
			valiValues["resources"] = map[string]interface{}{
				// Copy requests only, effectively removing limits
				"vali": &corev1.ResourceRequirements{Requests: currentResources["vali"].Requests},
			}
		}
	}

	if err := b.SeedClientSet.ChartApplier().Apply(ctx, filepath.Join(ChartsPath, "seed-bootstrap", "charts", "vali"), b.Shoot.SeedNamespace, fmt.Sprintf("%s-logging", b.Shoot.SeedNamespace), kubernetes.Values(valiValues)); err != nil {
		return err
	}

	// TODO(rfranzke): Remove this in a future version.
	return kubernetesutils.DeleteObjects(ctx, b.SeedClientSet.Client(),
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-from-prometheus-to-vali-telegraf", Namespace: b.Shoot.SeedNamespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-to-vali", Namespace: b.Shoot.SeedNamespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-vali", Namespace: b.Shoot.SeedNamespace}},
	)
}

func (b *Botanist) destroyShootLoggingStack(ctx context.Context) error {
	if err := b.destroyShootNodeLogging(ctx); err != nil {
		return err
	}

	if err := b.Shoot.Components.Logging.ShootEventLogger.Destroy(ctx); err != nil {
		return err
	}

	return common.DeleteVali(ctx, b.SeedClientSet.Client(), b.Shoot.SeedNamespace)
}

func (b *Botanist) renameLokiPvcToVali(ctx context.Context) error {
	b.Logger.Info("Renaming Loki PVC to Vali")

	// Get Loki PVC.
	b.Logger.Info("Step 1/7: Get Loki PVC")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-loki-0",
			Namespace: b.Shoot.SeedNamespace,
		},
	}
	if err := b.SeedClientSet.Client().Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
		if apierrors.IsNotFound(err) {
			b.Logger.Info("Loki PVC not found, skipping rename")
			return nil
		} else {
			return err
		}
	}

	// Get Loki PV.
	b.Logger.Info("Step 2/7: Get Loki PV")
	pvId := pvc.Spec.VolumeName
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvId,
			Namespace: b.Shoot.SeedNamespace,
		},
	}
	if err := b.SeedClientSet.Client().Get(ctx, client.ObjectKeyFromObject(pv), pv); err != nil {
		return err
	}

	// When the Loki PVC is deleted, the PV is retained.
	b.Logger.Info("Step 3/7: Patch Loki PV's PersistentVolumeReclaimPolicy")
	patch := client.MergeFrom(pv.DeepCopy())
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if err := b.SeedClientSet.Client().Patch(ctx, pv, patch); err != nil {
		return err
	}

	// Delete Loki PVC
	b.Logger.Info("Step 4/7: Delete Loki PVC")
	if err := kubernetesutils.DeleteObject(ctx, b.SeedClientSet.Client(), pvc); err != nil {
		return err
	}

	// Delete the ClaimRef from the PV.
	b.Logger.Info("Step 5/7: Remove Loki PV's ClaimRef")
	patch = client.MergeFrom(pv.DeepCopy())
	pv.Spec.ClaimRef = nil
	if err := b.SeedClientSet.Client().Patch(ctx, pv, patch); err != nil {
		return err
	}

	// Recreate the PVC with the vali name.
	b.Logger.Info("Step 6/7: Create Vali PVC")

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

	if err := b.SeedClientSet.Client().Create(ctx, valiPvc); err != nil {
		b.Logger.Error(err, "Step 6/7: Create Vali PVC failed")
		return err
	}

	// Change the PV ReclaimPolicy back to Delete, so that when a vali PVC is deleted, we don't leak the PV.
	b.Logger.Info("Step 7/7: Patch Vali PV's PersistentVolumeReclaimPolicy")
	patch = client.MergeFrom(pv.DeepCopy())
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
	if err := b.SeedClientSet.Client().Patch(ctx, pv, patch); err != nil {
		return err
	}

	b.Logger.Info("Successfully renamed Loki PVC to Vali")

	return nil
}

func (b *Botanist) destroyLokiBasedShootLoggingStackRetainingPvc(ctx context.Context) error {
	if err := b.destroyShootNodeLogging(ctx); err != nil {
		return err
	}

	// The EventLogger is not dependent on Loki/Vali and therefore doesn't need to be deleted.
	// if err := b.Shoot.Components.Logging.ShootEventLogger.Destroy(ctx); err != nil {
	// 	return err
	// }

	return common.DeleteLokiRetainPvc(ctx, b.SeedClientSet.Client(), b.Shoot.SeedNamespace)
}

func (b *Botanist) destroyShootNodeLogging(ctx context.Context) error {
	if err := b.Shoot.Components.Logging.ShootRBACProxy.Destroy(ctx); err != nil {
		return err
	}

	return kubernetesutils.DeleteObjects(ctx, b.SeedClientSet.Client(),
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "vali", Namespace: b.Shoot.SeedNamespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-from-prometheus-to-vali-telegraf", Namespace: b.Shoot.SeedNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "telegraf-config", Namespace: b.Shoot.SeedNamespace}},
	)
}

func (b *Botanist) destroyLokiBasedShootNodeLogging(ctx context.Context) error {
	if err := b.Shoot.Components.Logging.ShootRBACProxy.Destroy(ctx); err != nil {
		return err
	}

	return kubernetesutils.DeleteObjects(ctx, b.SeedClientSet.Client(),
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "loki", Namespace: b.Shoot.SeedNamespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-from-prometheus-to-loki-telegraf", Namespace: b.Shoot.SeedNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "telegraf-config", Namespace: b.Shoot.SeedNamespace}},
	)
}

func (b *Botanist) isShootNodeLoggingEnabled() bool {
	if b.Shoot != nil && b.Shoot.IsShootControlPlaneLoggingEnabled(b.Config) &&
		gardenlethelper.IsValiEnabled(b.Config) && b.Config != nil &&
		b.Config.Logging != nil && b.Config.Logging.ShootNodeLogging != nil {

		for _, purpose := range b.Config.Logging.ShootNodeLogging.ShootPurposes {
			if gardencore.ShootPurpose(b.Shoot.Purpose) == purpose {
				return true
			}
		}
	}
	return false
}

func (b *Botanist) isShootEventLoggerEnabled() bool {
	return b.Shoot != nil && b.Shoot.IsShootControlPlaneLoggingEnabled(b.Config) && gardenlethelper.IsEventLoggingEnabled(b.Config)
}

// DefaultEventLogger returns a deployer for the shoot-event-logger.
func (b *Botanist) DefaultEventLogger() (component.Deployer, error) {
	imageEventLogger, err := b.ImageVector.FindImage(images.ImageNameEventLogger, imagevector.RuntimeVersion(b.SeedVersion()), imagevector.TargetVersion(b.ShootVersion()))
	if err != nil {
		return nil, err
	}

	return eventlogger.New(
		b.SeedClientSet.Client(),
		b.Shoot.SeedNamespace,
		b.SecretsManager,
		eventlogger.Values{
			Image:    imageEventLogger.String(),
			Replicas: b.Shoot.GetReplicas(1),
		})
}
