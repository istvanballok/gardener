// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubestatemetrics

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/component"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	"github.com/gardener/gardener/third_party/gopkg.in/yaml.v2"
)

const (
	managedResourceName      = "kube-state-metrics"
	managedResourceNameShoot = "shoot-core-" + managedResourceName

	containerName = "kube-state-metrics"

	labelKeyComponent   = "component"
	labelKeyType        = "type"
	labelValueComponent = "kube-state-metrics"

	port            = 8080
	portNameMetrics = "metrics"
)

// New creates a new instance of DeployWaiter for the kube-state-metrics.
func New(
	client client.Client,
	namespace string,
	secretsManager secretsmanager.Interface,
	values Values,
) component.DeployWaiter {
	return &kubeStateMetrics{
		client:         client,
		secretsManager: secretsManager,
		namespace:      namespace,
		values:         values,
	}
}

type kubeStateMetrics struct {
	client         client.Client
	secretsManager secretsmanager.Interface
	namespace      string
	values         Values
}

// Values is a set of configuration values for the kube-state-metrics.
type Values struct {
	// ClusterType specifies the type of the cluster to which kube-state-metrics is being deployed.
	// For seeds, all resources are being deployed as part of a ManagedResource.
	// For shoots, the kube-state-metrics runs in the shoot namespace in the seed as part of the control plane. Hence,
	// only the runtime resources (like Deployment, Service, etc.) are being deployed directly (with the client). All
	// other application-related resources (like RBAC roles, CRD, etc.) are deployed as part of a ManagedResource.
	ClusterType component.ClusterType
	// KubernetesVersion is the Kubernetes version of the cluster.
	KubernetesVersion *semver.Version
	// Image is the container image.
	Image string
	// PriorityClassName is the name of the priority class.
	PriorityClassName string
	// Replicas is the number of replicas.
	Replicas int32
	// IsWorkerless specifies whether the cluster has worker nodes.
	IsWorkerless bool
}

func (k *kubeStateMetrics) Deploy(ctx context.Context) error {
	customResourceStateConfig, err := yaml.Marshal(NewCustomResourceStateConfig())
	if err != nil {
		return err
	}

	registry2 := managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer)
	resources2, err := registry2.AddAllAndSerialize(
		k.clusterRole(),
	)
	if err != nil {
		return err
	}
	if err := managedresources.CreateForSeedWithLabels(ctx,
		k.client,
		k.namespace,
		"kube-state-metrics2",
		false,
		map[string]string{v1beta1constants.LabelCareConditionType: v1beta1constants.ObservabilityComponentsHealthy},
		resources2); err != nil {
		return err
	}

	var (
		genericTokenKubeconfigSecretName string
		shootAccessSecret                *gardenerutils.AccessSecret
	)

	if k.values.ClusterType == component.ClusterTypeShoot {
		genericTokenKubeconfigSecret, found := k.secretsManager.Get(v1beta1constants.SecretNameGenericTokenKubeconfig)
		if !found {
			return fmt.Errorf("secret %q not found", v1beta1constants.SecretNameGenericTokenKubeconfig)
		}
		genericTokenKubeconfigSecretName = genericTokenKubeconfigSecret.Name

		shootAccessSecret = k.newShootAccessSecret()
		if err := shootAccessSecret.Reconcile(ctx, k.client); err != nil {
			return err
		}
	}

	var registry *managedresources.Registry
	if k.values.ClusterType == component.ClusterTypeSeed {
		registry = managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer)
	} else {
		registry = managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer)
	}

	return component.DeployResourceConfigs(ctx, k.client, k.namespace, k.values.ClusterType, k.managedResourceName(), map[string]string{v1beta1constants.LabelCareConditionType: v1beta1constants.ObservabilityComponentsHealthy}, registry, k.getResourceConfigs(genericTokenKubeconfigSecretName, shootAccessSecret, string(customResourceStateConfig)))
}

func (k *kubeStateMetrics) Destroy(ctx context.Context) error {
	if err := component.DestroyResourceConfigs(ctx, k.client, k.namespace, k.values.ClusterType, k.managedResourceName(), k.getResourceConfigs("", nil, "")); err != nil {
		return err
	}

	if k.values.ClusterType == component.ClusterTypeShoot {
		return client.IgnoreNotFound(k.client.Delete(ctx, k.newShootAccessSecret().Secret))
	}

	return nil
}

// TimeoutWaitForManagedResource is the timeout used while waiting for the ManagedResources to become healthy
// or deleted.
var TimeoutWaitForManagedResource = 2 * time.Minute

func (k *kubeStateMetrics) Wait(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilHealthy(timeoutCtx, k.client, k.namespace, k.managedResourceName())
}

func (k *kubeStateMetrics) WaitCleanup(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilDeleted(timeoutCtx, k.client, k.namespace, k.managedResourceName())
}

func (k *kubeStateMetrics) managedResourceName() string {
	if k.values.ClusterType == component.ClusterTypeSeed {
		return managedResourceName
	}
	return managedResourceNameShoot
}
