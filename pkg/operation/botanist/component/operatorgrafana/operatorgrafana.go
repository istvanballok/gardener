// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package operatorgrafana

import (
	"context"
	"time"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Interface interface {
	component.DeployWaiter
	GrafanaResourceConfigs() component.ResourceConfigs
	// component.MonitoringComponent
}

func New(
	client client.Client,
	namespace string,
	secretsManager secretsmanager.Interface,
	values Values,
) Interface {
	og := &operatorgrafana{
		client:         client,
		namespace:      namespace,
		secretsManager: secretsManager,
		values:         values,
	}
	og.registry = managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer)
	return og
}

type operatorgrafana struct {
	client         client.Client
	namespace      string
	secretsManager secretsmanager.Interface
	values         Values

	registry    *managedresources.Registry
	crdDeployer component.Deployer

	caSecretName                     string
	caBundle                         []byte
	serverSecretName                 string
	genericTokenKubeconfigSecretName *string
}

type Values struct {
	Enabled bool
}

func (og *operatorgrafana) Deploy(ctx context.Context) error {
	var allResources component.ResourceConfigs
	allResources = component.MergeResourceConfigs(allResources, og.GrafanaResourceConfigs())
	if err := component.DeployResourceConfigs(
		ctx,
		og.client,
		og.namespace,
		component.ClusterTypeShoot,
		"operatorgrafana",
		og.registry,
		allResources); err != nil {
		return err
	}
	// TODO add grafana deployment, ingress, network policy, service
	return nil
}
func (og *operatorgrafana) Destroy(ctx context.Context) error {
	return nil
}

// TimeoutWaitForManagedResource is the timeout used while waiting for the ManagedResources to become healthy
// or deleted.
var TimeoutWaitForManagedResource = 2 * time.Minute

func (og *operatorgrafana) Wait(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilHealthy(timeoutCtx, og.client, og.namespace, "operatorgrafana")
}

func (og *operatorgrafana) WaitCleanup(ctx context.Context) error {
	return nil
}

func (og *operatorgrafana) emptyDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: og.namespace}}
}

func getAppLabel(appValue string) map[string]string {
	return map[string]string{v1beta1constants.LabelApp: appValue}
}

func getRoleLabel() map[string]string {
	return map[string]string{v1beta1constants.GardenRole: "operatorgrafana"}
}

func getAllLabels(appValue string) map[string]string {
	return utils.MergeStringMaps(getAppLabel(appValue), getRoleLabel())
}
