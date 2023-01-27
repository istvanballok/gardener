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

package care_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
)

var _ = Describe("Shoot Care controller tests", func() {
	var (
		project              *gardencorev1beta1.Project
		seed                 *gardencorev1beta1.Seed
		seedNamespace        *corev1.Namespace
		secret               *corev1.Secret
		internalDomainSecret *corev1.Secret
		secretBinding        *gardencorev1beta1.SecretBinding
		shoot                *gardencorev1beta1.Shoot
		cluster              *extensionsv1alpha1.Cluster
	)

	BeforeEach(func() {
		project = &gardencorev1beta1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:   projectName,
				Labels: map[string]string{testID: testRunID},
			},
			Spec: gardencorev1beta1.ProjectSpec{
				Namespace: &testNamespace.Name,
			},
		}
		seed = &gardencorev1beta1.Seed{
			ObjectMeta: metav1.ObjectMeta{
				Name:   seedName,
				Labels: map[string]string{testID: testRunID},
			},
			Spec: gardencorev1beta1.SeedSpec{
				Provider: gardencorev1beta1.SeedProvider{
					Region: "region",
					Type:   "providerType",
					Zones:  []string{"a", "b", "c"},
				},
				Networks: gardencorev1beta1.SeedNetworks{
					Pods:     "10.0.0.0/16",
					Services: "10.1.0.0/16",
					Nodes:    pointer.String("10.2.0.0/16"),
				},
				DNS: gardencorev1beta1.SeedDNS{
					IngressDomain: pointer.String("someingress.example.com"),
				},
			},
		}

		seedNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   gardenerutils.ComputeGardenNamespace(seed.Name),
				Labels: map[string]string{testID: testRunID},
			},
		}

		internalDomainSecret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "secret-",
			Namespace:    seedNamespace.Name,
			Labels: map[string]string{
				"gardener.cloud/role": "internal-domain",
				testID:                testRunID,
			},
			Annotations: map[string]string{
				"dns.gardener.cloud/provider": "test",
				"dns.gardener.cloud/domain":   "example.com",
			},
		}}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secret-" + testRunID,
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
		}
		secretBinding = &gardencorev1beta1.SecretBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secretbinding-" + testRunID,
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
			SecretRef: corev1.SecretReference{Name: secret.Name},
			Provider:  &gardencorev1beta1.SecretBindingProvider{Type: "foo"},
		}
		shoot = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shootName,
				Namespace: testNamespace.Name,
				Labels:    map[string]string{testID: testRunID},
			},
			Spec: gardencorev1beta1.ShootSpec{
				SecretBindingName: secretBinding.Name,
				CloudProfileName:  "cloudprofile1",
				SeedName:          &seedName,
				Region:            "europe-central-1",
				Provider: gardencorev1beta1.Provider{
					Type: "foo-provider",
					Workers: []gardencorev1beta1.Worker{
						{
							Name:    "cpu-worker",
							Minimum: 3,
							Maximum: 3,
							Machine: gardencorev1beta1.Machine{
								Type: "large",
							},
						},
					},
				},
				Kubernetes: gardencorev1beta1.Kubernetes{
					Version: "1.20.1",
				},
				Networking: gardencorev1beta1.Networking{
					Type:     "foo-networking",
					Services: pointer.String("10.0.0.0/16"),
					Pods:     pointer.String("10.1.0.0/16"),
				},
			},
		}
		cluster = &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{testID: testRunID},
			},
			Spec: extensionsv1alpha1.ClusterSpec{
				Shoot:        runtime.RawExtension{Object: shoot},
				Seed:         runtime.RawExtension{Object: seed},
				CloudProfile: runtime.RawExtension{Object: &gardencorev1alpha1.CloudProfile{}},
			},
		}
	})

	JustBeforeEach(func() {
		// Typically, GCM creates the seed-specific namespace, but it doesn't run in this test, hence we have to do it.
		By("Create seed-specific namespace")
		Expect(testClient.Create(ctx, seedNamespace)).To(Succeed())

		By("Wait until the manager cache observes the namespace")
		Eventually(func() error {
			return mgrClient.Get(ctx, client.ObjectKeyFromObject(seedNamespace), seedNamespace)
		}).Should(Succeed())

		By("Create InternalDomainSecret")
		Expect(testClient.Create(ctx, internalDomainSecret)).To(Succeed())

		By("Wait until the manager cache observes the internal domain secret")
		Eventually(func() error {
			return mgrClient.Get(ctx, client.ObjectKeyFromObject(internalDomainSecret), internalDomainSecret)
		}).Should(Succeed())

		By("Create Shoot")
		Expect(testClient.Create(ctx, shoot)).To(Succeed())
		log.Info("Created Shoot for test", "shoot", shoot.Name)

		By("Patch shoot status")
		patch := client.MergeFrom(shoot.DeepCopy())
		shoot.Status.Gardener.Version = "1.2.3"
		shoot.Status.TechnicalID = testNamespace.Name
		Expect(testClient.Status().Patch(ctx, shoot, patch)).To(Succeed())

		By("Ensure manager has observed status patch")
		Eventually(func(g Gomega) string {
			g.Expect(mgrClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
			return shoot.Status.Gardener.Version
		}).ShouldNot(BeEmpty())

		DeferCleanup(func() {
			By("Delete seed-specific namespace")
			Expect(testClient.Delete(ctx, seedNamespace)).To(Succeed())

			By("Ensure Namespace is gone")
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKeyFromObject(seedNamespace), seedNamespace)
			}).Should(BeNotFoundError())

			By("Delete Secret")
			Expect(testClient.Delete(ctx, internalDomainSecret)).To(Succeed())

			By("Ensure Secret is gone")
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKeyFromObject(internalDomainSecret), internalDomainSecret)
			}).Should(BeNotFoundError())

			By("Delete Shoot")
			Expect(testClient.Delete(ctx, shoot)).To(Succeed())

			By("Ensure Shoot is gone")
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)
			}).Should(BeNotFoundError())
		})
	})

	Context("when operation cannot be initialized", func() {
		It("should set condition to Unknown", func() {
			By("Expect conditions to be Unknown")
			Eventually(func(g Gomega) []gardencorev1beta1.Condition {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				return shoot.Status.Conditions
			}).Should(And(
				ContainCondition(OfType(gardencorev1beta1.ShootAPIServerAvailable), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("operation could not be initialized")),
				ContainCondition(OfType(gardencorev1beta1.ShootControlPlaneHealthy), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("operation could not be initialized")),
				ContainCondition(OfType(gardencorev1beta1.ShootObservabilityComponentsHealthy), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("operation could not be initialized")),
				ContainCondition(OfType(gardencorev1beta1.ShootEveryNodeReady), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("operation could not be initialized")),
				ContainCondition(OfType(gardencorev1beta1.ShootSystemComponentsHealthy), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("operation could not be initialized")),
			))
		})
	})

	Context("when operation can be initialized", func() {
		BeforeEach(func() {
			By("Create Project")
			Expect(testClient.Create(ctx, project)).To(Succeed())
			log.Info("Created Project for test", "project", project.Name)

			By("Create Seed")
			Expect(testClient.Create(ctx, seed)).To(Succeed())
			log.Info("Created Seed for test", "seed", seed.Name)

			By("Create Secret")
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			log.Info("Created Secret for test", "secret", secret.Name)

			By("Create SecretBinding")
			Expect(testClient.Create(ctx, secretBinding)).To(Succeed())
			log.Info("Created SecretBinding for test", "secretBinding", secretBinding.Name)

			DeferCleanup(func() {
				By("Delete SecretBinding")
				Expect(testClient.Delete(ctx, secretBinding)).To(Succeed())

				By("Delete Secret")
				Expect(testClient.Delete(ctx, secret)).To(Succeed())

				By("Delete Seed")
				Expect(testClient.Delete(ctx, seed)).To(Succeed())

				By("Delete Project")
				Expect(testClient.Delete(ctx, project)).To(Succeed())

				By("Ensure SecretBinding is gone")
				Eventually(func() error {
					return mgrClient.Get(ctx, client.ObjectKeyFromObject(secretBinding), secretBinding)
				}).Should(BeNotFoundError())

				By("Ensure Secret is gone")
				Eventually(func() error {
					return mgrClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
				}).Should(BeNotFoundError())

				By("Ensure Seed is gone")
				Eventually(func() error {
					return mgrClient.Get(ctx, client.ObjectKeyFromObject(seed), seed)
				}).Should(BeNotFoundError())

				By("Ensure Project is gone")
				Eventually(func() error {
					return mgrClient.Get(ctx, client.ObjectKeyFromObject(project), project)
				}).Should(BeNotFoundError())
			})
		})

		// Cluster is created in JustBeforeEach because Shoot is also created in JustBeforeEach, so we need to make sure
		// that the Cluster resource contains the most recent version of the Shoot.
		JustBeforeEach(func() {
			By("Create Cluster")
			cluster.Name = shoot.Status.TechnicalID
			Expect(testClient.Create(ctx, cluster)).To(Succeed())
			log.Info("Created Cluster for test", "cluster", cluster.Name)

			DeferCleanup(func() {
				By("Delete Cluster")
				Expect(testClient.Delete(ctx, cluster)).To(Succeed())

				By("Ensure Cluster is gone")
				Eventually(func() error {
					return mgrClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				}).Should(BeNotFoundError())
			})
		})

		Context("when all control plane deployments for the Shoot are missing", func() {
			It("should set conditions", func() {
				By("Expect conditions to be set")
				Eventually(func(g Gomega) []gardencorev1beta1.Condition {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
					return shoot.Status.Conditions
				}).Should(And(
					ContainCondition(OfType(gardencorev1beta1.ShootAPIServerAvailable), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("APIServerDown")),
					ContainCondition(OfType(gardencorev1beta1.ShootControlPlaneHealthy), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("DeploymentMissing"), WithMessageSubstrings("Missing required deployments: [gardener-resource-manager kube-apiserver kube-controller-manager kube-scheduler]")),
					ContainCondition(OfType(gardencorev1beta1.ShootObservabilityComponentsHealthy), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("DeploymentMissing"), WithMessageSubstrings("Missing required deployments: [plutono-operators plutono-users kube-state-metrics]")),
					ContainCondition(OfType(gardencorev1beta1.ShootEveryNodeReady), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("Shoot control plane has not been fully created yet.")),
					ContainCondition(OfType(gardencorev1beta1.ShootSystemComponentsHealthy), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("Shoot control plane has not been fully created yet.")),
				))
			})
		})

		Context("when some control plane deployments for the Shoot are present", func() {
			JustBeforeEach(func() {
				for _, name := range []string{"gardener-resource-manager", "kube-controller-manager", "kube-scheduler", "plutono-operators", "plutono-users"} {
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: testNamespace.Name,
							Labels: map[string]string{
								testID:                      testRunID,
								v1beta1constants.GardenRole: getRole(name),
							},
						},
						Spec: appsv1.DeploymentSpec{
							Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
							Replicas: pointer.Int32(1),
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar"}},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "foo-container",
										Image: "foo",
									}},
								},
							},
						},
					}

					By("Create Deployment " + name)
					Expect(testClient.Create(ctx, deployment)).To(Succeed(), "for deployment "+name)
					log.Info("Created Deployment for test", "deployment", client.ObjectKeyFromObject(deployment))

					By("Ensure manager has observed deployment " + name)
					Eventually(func() error {
						return mgrClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
					}).Should(Succeed())

					DeferCleanup(func() {
						By("Delete Deployment " + name)
						Expect(testClient.Delete(ctx, deployment)).To(Succeed(), "for deployment "+name)

						By("Ensure Deployment " + name + " is gone")
						Eventually(func() error {
							return mgrClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
						}).Should(BeNotFoundError(), "for deployment "+name)

						By("Ensure manager has observed deployment deletion " + name)
						Eventually(func() error {
							return mgrClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
						}).Should(BeNotFoundError())
					})
				}
			})

			It("should set conditions", func() {
				By("Expect conditions to be set")
				Eventually(func(g Gomega) []gardencorev1beta1.Condition {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
					return shoot.Status.Conditions
				}).Should(And(
					ContainCondition(OfType(gardencorev1beta1.ShootAPIServerAvailable), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("APIServerDown")),
					ContainCondition(OfType(gardencorev1beta1.ShootControlPlaneHealthy), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("DeploymentMissing"), WithMessageSubstrings("Missing required deployments: [kube-apiserver]")),
					ContainCondition(OfType(gardencorev1beta1.ShootObservabilityComponentsHealthy), WithStatus(gardencorev1beta1.ConditionProgressing), WithReason("DeploymentMissing"), WithMessageSubstrings("Missing required deployments: [kube-state-metrics]")),
					ContainCondition(OfType(gardencorev1beta1.ShootEveryNodeReady), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("Shoot control plane has not been fully created yet.")),
					ContainCondition(OfType(gardencorev1beta1.ShootSystemComponentsHealthy), WithStatus(gardencorev1beta1.ConditionUnknown), WithReason("ConditionCheckError"), WithMessageSubstrings("Shoot control plane has not been fully created yet.")),
				))
			})
		})
	})
})

func getRole(name string) string {
	switch name {
	case "gardener-resource-manager", "kube-controller-manager", "kube-scheduler":
		return v1beta1constants.GardenRoleControlPlane
	case "plutono-operators", "plutono-users":
		return v1beta1constants.GardenRoleMonitoring
	}
	return ""
}
