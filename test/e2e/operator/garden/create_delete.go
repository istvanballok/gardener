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

package garden

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	. "github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
)

var _ = Describe("Garden Tests", Label("Garden", "default"), func() {
	var (
		backupSecret = defaultBackupSecret()
		garden       = defaultGarden(backupSecret)
	)

	It("Create, Delete", Label("simple"), func() {
		By("Create Garden")
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Minute)
		defer cancel()

		Expect(runtimeClient.Create(ctx, backupSecret)).To(Succeed())
		Expect(runtimeClient.Create(ctx, garden)).To(Succeed())
		waitForGardenToBeReconciled(ctx, garden)

		DeferCleanup(func() {
			By("Delete Garden")
			ctx, cancel = context.WithTimeout(parentCtx, 5*time.Minute)
			defer cancel()

			Expect(gardenerutils.ConfirmDeletion(ctx, runtimeClient, garden)).To(Succeed())
			Expect(runtimeClient.Delete(ctx, garden)).To(Succeed())
			Expect(runtimeClient.Delete(ctx, backupSecret)).To(Succeed())
			waitForGardenToBeDeleted(ctx, garden)
			cleanupVolumes(ctx)
			Expect(runtimeClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(namespace), client.MatchingLabels{"role": "kube-apiserver-etcd-encryption-configuration"})).To(Succeed())
			Expect(runtimeClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(namespace), client.MatchingLabels{"role": "gardener-apiserver-etcd-encryption-configuration"})).To(Succeed())

			By("Verify deletion")
			secretList := &corev1.SecretList{}
			Expect(runtimeClient.List(ctx, secretList, client.InNamespace(namespace), client.MatchingLabels{
				secretsmanager.LabelKeyManagedBy:       secretsmanager.LabelValueSecretsManager,
				secretsmanager.LabelKeyManagerIdentity: operatorv1alpha1.SecretManagerIdentityOperator,
			})).To(Succeed())
			Expect(secretList.Items).To(BeEmpty())

			crdList := &apiextensionsv1.CustomResourceDefinitionList{}
			Expect(runtimeClient.List(ctx, crdList)).To(Succeed())
			Expect(crdList.Items).To(ContainElement(MatchFields(IgnoreExtras, Fields{"ObjectMeta": MatchFields(IgnoreExtras, Fields{"Name": Equal("gardens.operator.gardener.cloud")})})))

			Expect(runtimeClient.Get(ctx, client.ObjectKey{Name: v1beta1constants.DeploymentNameGardenerResourceManager, Namespace: namespace}, &appsv1.Deployment{})).To(BeNotFoundError())
		})

		By("Verify creation")
		CEventually(ctx, func(g Gomega) {
			managedResourceList := &resourcesv1alpha1.ManagedResourceList{}
			g.Expect(runtimeClient.List(ctx, managedResourceList, client.InNamespace(namespace))).To(Succeed())
			g.Expect(managedResourceList.Items).To(ConsistOf(
				healthyManagedResource("garden-system"),
				healthyManagedResource("hvpa"),
				healthyManagedResource("vpa"),
				healthyManagedResource("etcd-druid"),
				healthyManagedResource("kube-state-metrics"),
				healthyManagedResource("shoot-core-kube-controller-manager"),
				healthyManagedResource("shoot-core-gardener-resource-manager"),
				healthyManagedResource("shoot-core-gardeneraccess"),
				healthyManagedResource("nginx-ingress"),
				healthyManagedResource("fluent-bit"),
				healthyManagedResource("fluent-operator"),
				healthyManagedResource("fluent-operator-custom-resources-garden"),
				healthyManagedResource("vali"),
				healthyManagedResource("plutono"),
			))

			g.Expect(runtimeClient.List(ctx, managedResourceList, client.InNamespace("istio-system"))).To(Succeed())
			g.Expect(managedResourceList.Items).To(ConsistOf(
				healthyManagedResource("istio-system"),
				healthyManagedResource("virtual-garden-istio"),
			))
		}).WithPolling(2 * time.Second).Should(Succeed())

		By("Verify virtual cluster access using token-request kubeconfig")
		Eventually(func(g Gomega) {
			virtualClusterClient, err := kubernetes.NewClientFromSecret(ctx, runtimeClient, namespace, "gardener", kubernetes.WithDisabledCachedClient())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(virtualClusterClient.Client().List(ctx, &corev1.NamespaceList{})).To(Succeed())
		}).Should(Succeed())
	})
})

func healthyManagedResource(name string) gomegatypes.GomegaMatcher {
	return MatchFields(IgnoreExtras, Fields{
		"ObjectMeta": MatchFields(IgnoreExtras, Fields{"Name": Equal(name)}),
		"Status": MatchFields(IgnoreExtras, Fields{"Conditions": And(
			ContainCondition(OfType(resourcesv1alpha1.ResourcesApplied), WithStatus(gardencorev1beta1.ConditionTrue)),
			ContainCondition(OfType(resourcesv1alpha1.ResourcesHealthy), WithStatus(gardencorev1beta1.ConditionTrue)),
			ContainCondition(OfType(resourcesv1alpha1.ResourcesProgressing), WithStatus(gardencorev1beta1.ConditionFalse)),
		)}),
	})
}
