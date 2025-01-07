// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package garbagecollector_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/logger"
	"github.com/gardener/gardener/pkg/resourcemanager/apis/config"
	. "github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/race"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/references"
)

var _ = Describe("Collector", func() {
	var (
		ctx = context.TODO()
		log logr.Logger

		c  client.Client
		gc *Reconciler

		fakeClock *testclock.FakeClock

		minimumObjectLifetime = time.Minute
		creationTimestamp     = metav1.Date(2000, 5, 5, 5, 30, 0, 0, time.Local)
	)

	BeforeEach(func() {
		log = logger.MustNewZapLogger(logger.DebugLevel, logger.FormatText)
		logf.SetLogger(log.WithName("garbagecollector"))
		log = log.WithName("test            ")
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		fakeClock = testclock.NewFakeClock(creationTimestamp.Add(minimumObjectLifetime / 2))
		gc = &Reconciler{
			TargetClient:          c,
			Config:                config.GarbageCollectorControllerConfig{SyncPeriod: &metav1.Duration{Duration: 12 * time.Hour}},
			Clock:                 fakeClock,
			MinimumObjectLifetime: &minimumObjectLifetime,
		}
	})

	Describe("#raceCondition", func() {
		var (
			executor     race.CooperativeExecutor
			setupClients func()
			clientGC     client.Client
			clientActor  client.Client
		)
		BeforeEach(func() {
			executor = race.NewCooperativeExecutor(race.INFO)
			setupClients = func() {
				clientGC = race.NewCooperativeClient("GC", c, executor)
				gc.TargetClient = clientGC
				clientActor = race.NewCooperativeClient("Actor", c, executor)
			}
		})

		When("there is an unreferenced secret in the system", func() {
			var (
				secret      *corev1.Secret
				setupSecret func()
			)
			BeforeEach(func() {
				setupSecret = func() {
					secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: metav1.NamespaceDefault,
					}}
					kubernetesutils.MakeUnique(secret)
					Expect(client.IgnoreNotFound(c.Delete(ctx, secret))).To(Succeed())
					Expect(c.Create(ctx, secret)).To(Succeed())
				}
			})
			When("an actor reconciles a MR referencing that secret", func() {
				var (
					cleanupMR          func()
					assertSecretExists func() error
				)
				BeforeEach(func() {
					cleanupMR = func() {
						Expect(client.IgnoreNotFound(c.Delete(ctx, &resourcesv1alpha1.ManagedResource{ObjectMeta: objectMetaFor("mr")}))).To(Succeed())
					}
				})

				It("the referenced secret should exist at the end", func() {
					assertSecretExists = func() error {
						secretList := &corev1.SecretList{}
						Expect(c.List(ctx, secretList)).To(Succeed())
						if len(secretList.Items) != 1 {
							return fmt.Errorf("a (concurrently) referenced secret was deleted by the Garbage Collector")
						}
						return nil
					}
					setup := func() {
						setupClients()
						setupSecret()
						cleanupMR()
						fakeClock.SetTime(creationTimestamp.Add(minimumObjectLifetime / 2))
					}
					assert := assertSecretExists
					GC := func() {
						runs := 0
						for {
							result, err := gc.Reconcile(ctx, reconcile.Request{})
							log.Info(fmt.Sprintf("GC Reconcile() result: %+v, err: %v", result, err))
							runs++
							if result.Requeue && result.RequeueAfter < gc.Config.SyncPeriod.Duration {
								fakeClock.Step(result.RequeueAfter)
							} else {
								if err == nil {
									break
								}
							}
						}
						log.Info(fmt.Sprintf("GC finished after %d runs", runs))
					}
					actor := func() {
						for {
							secret1 := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
								Name:      "secret",
								Namespace: metav1.NamespaceDefault,
							}}
							kubernetesutils.MakeUnique(secret1)

							mr := &resourcesv1alpha1.ManagedResource{ObjectMeta: objectMetaFor("mr", secret1)}
							result, err := controllerutil.CreateOrUpdate(ctx, clientActor, mr, func() error { return nil })
							log.Info(fmt.Sprintf("CreateOrUpdate managed resource returned: OperationResult=%+v, error=%v", result, err))
							if err != nil {
								continue
							}

							secret2 := secret1.DeepCopy()
							mutateSecret := func() error {
								secret2.Labels = secret1.Labels
								secret2.Immutable = secret1.Immutable
								return nil
							}

							result, err = controllerutil.CreateOrUpdate(ctx, clientActor, secret2, mutateSecret)
							log.Info(fmt.Sprintf("CreateOrUpdate secret           returned: OperationResult=%+v, error=%v", result, err))
							if err == nil {
								break
							}
						}
					}
					result := executor.RunAllCombinations(setup, assert, GC, actor)
					Expect(result.NumberOfPathsChecked).To(Equal(8))
					Expect(strings.TrimSpace(result.PathsChecked)).To(Equal(strings.TrimSpace(strings.ReplaceAll(`

  Path 1:
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, ]}, error=<nil>
    assertion error: <nil>
  Path 2:
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, ]}, error=<nil>
    assertion error: <nil>
  Path 3:
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, ]}, error=<nil>
    assertion error: <nil>
  Path 4:
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: []}, error=<nil>
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
  - GC/2 Delete(&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, []) -> error=<nil>
    assertion error: a (concurrently) referenced secret was deleted by the Garbage Collector
  Path 5:
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, ]}, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
    assertion error: <nil>
  Path 6:
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, ]}, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
    assertion error: <nil>
  Path 7:
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: []}, error=<nil>
  - GC/2 Delete(&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, []) -> error=<nil>
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=secrets "secret-e3b0c442" not found
  - Actor/2 Create(&Secret{Namespace: default, Name: secret-e3b0c442, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
    assertion error: <nil>
  Path 8:
  - GC/0 List(SecretList, [map[resources.gardener.cloud/garbage-collectable-reference:true]]) -> list=&PartialObjectMetadataList{Items: [&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, ]}, error=<nil>
  - GC/1 List(ManagedResourceList, []) -> list=&PartialObjectMetadataList{Items: []}, error=<nil>
  - Actor/0 Create(&ManagedResource{Namespace: default, Name: mr, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, []) -> object=&ManagedResource{Namespace: default, Name: mr, ResourceVersion: 1, Annotations: map[reference.resources.gardener.cloud/secret-ccbd158d:secret-e3b0c442], }, error=<nil>
  - GC/2 Delete(&PartialObjectMetadata{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], }, []) -> error=<nil>
  - Actor/1 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=secrets "secret-e3b0c442" not found
  - Actor/2 Create(&Secret{Namespace: default, Name: secret-e3b0c442, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true], Immutable: true, }, error=<nil>
    assertion error: <nil>

`, "\n  ", "\n"))), "Here is the actual value of PathsChecked in full length. Copy this and update the string literal in the unit test's source code: \n\n%s\n",
						result.PathsChecked)
					Expect(result.NumberOfPathsWithFailedAssertion).To(Equal(1))
				})
			})
		})
	})

	Describe("#collectGarbage", func() {
		var (
			unlabeledSecret    *corev1.Secret
			unlabeledConfigMap *corev1.ConfigMap

			labeledObjectMeta metav1.ObjectMeta

			labeledSecret1       *corev1.Secret
			labeledSecret1System *corev1.Secret
			labeledSecret2       *corev1.Secret
			labeledSecret3       *corev1.Secret
			labeledSecret4       *corev1.Secret
			labeledSecret5       *corev1.Secret
			labeledSecret6       *corev1.Secret
			labeledSecret7       *corev1.Secret
			labeledSecret8       *corev1.Secret
			labeledSecret9       *corev1.Secret

			labeledConfigMap1       *corev1.ConfigMap
			labeledConfigMap1System *corev1.ConfigMap
			labeledConfigMap2       *corev1.ConfigMap
			labeledConfigMap3       *corev1.ConfigMap
			labeledConfigMap4       *corev1.ConfigMap
			labeledConfigMap5       *corev1.ConfigMap
			labeledConfigMap6       *corev1.ConfigMap
			labeledConfigMap7       *corev1.ConfigMap
			labeledConfigMap8       *corev1.ConfigMap
		)

		BeforeEach(func() {
			unlabeledSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unlabeledsecret1",
					Namespace: metav1.NamespaceDefault,
				},
			}
			unlabeledConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unlabeledcm1",
					Namespace: metav1.NamespaceDefault,
				},
			}

			labeledObjectMeta = metav1.ObjectMeta{
				Name:      "labeledobj",
				Namespace: metav1.NamespaceDefault,
				Labels: map[string]string{
					"resources.gardener.cloud/garbage-collectable-reference": "true",
				},
			}

			labeledSecret1 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret1System = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret2 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret3 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret4 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret5 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret6 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret7 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret8 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret9 = &corev1.Secret{ObjectMeta: labeledObjectMeta}
			labeledSecret1.Name += "1"
			labeledSecret1System.Name += "1"
			labeledSecret1System.Namespace = metav1.NamespaceSystem
			labeledSecret2.Name += "2"
			labeledSecret3.Name += "3"
			labeledSecret4.Name += "4"
			labeledSecret5.Name += "5"
			labeledSecret6.Name += "6"
			labeledSecret7.Name += "7"
			labeledSecret8.Name += "8"
			labeledSecret9.Name += "9"
			labeledSecret9.CreationTimestamp = creationTimestamp

			labeledConfigMap1 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap1System = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap2 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap3 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap4 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap6 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap7 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap8 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap5 = &corev1.ConfigMap{ObjectMeta: labeledObjectMeta}
			labeledConfigMap1.Name += "1"
			labeledConfigMap1System.Name += "1"
			labeledConfigMap1System.Namespace += metav1.NamespaceSystem
			labeledConfigMap2.Name += "2"
			labeledConfigMap3.Name += "3"
			labeledConfigMap4.Name += "4"
			labeledConfigMap5.Name += "5"
			labeledConfigMap5.CreationTimestamp = creationTimestamp
			labeledConfigMap6.Name += "6"
			labeledConfigMap7.Name += "7"
			labeledConfigMap8.Name += "8"
		})

		It("should do nothing because no secrets or configmaps found", func() {
			secretList := &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(BeEmpty())

			configMapList := &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(BeEmpty())

			runs := 0
			for {
				result, err := gc.Reconcile(ctx, reconcile.Request{})
				runs++
				Expect(err).NotTo(HaveOccurred())
				if result.Requeue && result.RequeueAfter < gc.Config.SyncPeriod.Duration {
					fakeClock.Step(result.RequeueAfter)
				} else {
					break
				}
			}
			Expect(runs).To(Equal(2))

			secretList = &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(BeEmpty())

			configMapList = &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(BeEmpty())
		})

		It("should delete nothing because no labeled secrets or configmaps found", func() {
			Expect(c.Create(ctx, unlabeledSecret)).To(Succeed())
			Expect(c.Create(ctx, unlabeledConfigMap)).To(Succeed())

			secretList := &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(ConsistOf(*unlabeledSecret))

			configMapList := &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(ConsistOf(*unlabeledConfigMap))

			runs := 0
			for {
				result, err := gc.Reconcile(ctx, reconcile.Request{})
				runs++
				Expect(err).NotTo(HaveOccurred())
				if result.Requeue && result.RequeueAfter < gc.Config.SyncPeriod.Duration {
					fakeClock.Step(result.RequeueAfter)
				} else {
					break
				}
			}
			Expect(runs).To(Equal(2))

			secretList = &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(ConsistOf(*unlabeledSecret))

			configMapList = &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(ConsistOf(*unlabeledConfigMap))
		})

		It("should delete the unused resources", func() {
			Expect(c.Create(ctx, labeledSecret1)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret1System)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret2)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret3)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret4)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret5)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret6)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret7)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret8)).To(Succeed())
			Expect(c.Create(ctx, labeledSecret9)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap1)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap1System)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap2)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap3)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap4)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap5)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap6)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap7)).To(Succeed())
			Expect(c.Create(ctx, labeledConfigMap8)).To(Succeed())

			secretList := &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(ConsistOf(
				*labeledSecret1, *labeledSecret1System, *labeledSecret2, *labeledSecret3,
				*labeledSecret4, *labeledSecret6, *labeledSecret7, *labeledSecret8,
				*labeledSecret9, *labeledSecret5,
			))

			configMapList := &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(ConsistOf(
				*labeledConfigMap1, *labeledConfigMap1System, *labeledConfigMap2, *labeledConfigMap3,
				*labeledConfigMap4, *labeledConfigMap6, *labeledConfigMap7, *labeledConfigMap8, *labeledConfigMap5,
			))

			Expect(c.Create(ctx, &appsv1.Deployment{ObjectMeta: objectMetaFor("deploy1", labeledSecret1, labeledConfigMap1)})).To(Succeed())
			Expect(c.Create(ctx, &appsv1.StatefulSet{ObjectMeta: objectMetaFor("sts1", labeledSecret2, labeledConfigMap2)})).To(Succeed())
			Expect(c.Create(ctx, &appsv1.DaemonSet{ObjectMeta: objectMetaFor("ds1", labeledSecret3, labeledConfigMap3)})).To(Succeed())
			Expect(c.Create(ctx, &batchv1.Job{ObjectMeta: objectMetaFor("job1", labeledSecret4, labeledConfigMap4)})).To(Succeed())
			Expect(c.Create(ctx, &resourcesv1alpha1.ManagedResource{ObjectMeta: objectMetaFor("mr1", labeledSecret5)})).To(Succeed())
			Expect(c.Create(ctx, &corev1.Pod{ObjectMeta: objectMetaFor("pod1", labeledSecret6, labeledConfigMap6)})).To(Succeed())
			Expect(c.Create(ctx, &batchv1.CronJob{ObjectMeta: objectMetaFor("cronjob2", labeledSecret7, labeledConfigMap7)})).To(Succeed())

			runs := 0
			for {
				result, err := gc.Reconcile(ctx, reconcile.Request{})
				runs++
				Expect(err).NotTo(HaveOccurred())
				if result.Requeue && result.RequeueAfter < gc.Config.SyncPeriod.Duration {
					fakeClock.Step(result.RequeueAfter)
					labeledSecret9.CreationTimestamp.Time = labeledSecret9.CreationTimestamp.Time.Add(result.RequeueAfter)
					c.Update(ctx, labeledSecret9)
					labeledConfigMap5.CreationTimestamp.Time = labeledConfigMap5.CreationTimestamp.Time.Add(result.RequeueAfter)
					c.Update(ctx, labeledConfigMap5)
				} else {
					break
				}
			}
			Expect(runs).To(Equal(2))

			secretList = &corev1.SecretList{}
			Expect(c.List(ctx, secretList)).To(Succeed())
			Expect(secretList.Items).To(ConsistOf(
				*labeledSecret1, *labeledSecret2, *labeledSecret3,
				*labeledSecret4, *labeledSecret5, *labeledSecret6,
				*labeledSecret7, *labeledSecret9,
			))

			configMapList = &corev1.ConfigMapList{}
			Expect(c.List(ctx, configMapList)).To(Succeed())
			Expect(configMapList.Items).To(ConsistOf(
				*labeledConfigMap1, *labeledConfigMap2, *labeledConfigMap3,
				*labeledConfigMap4, *labeledConfigMap5, *labeledConfigMap6,
				*labeledConfigMap7,
			))
		})
	})
})

func objectMetaFor(name string, objs ...runtime.Object) metav1.ObjectMeta {
	annotations := make(map[string]string)

	for _, obj := range objs {
		var kind, name string

		switch t := obj.(type) {
		case *corev1.Secret:
			kind, name = references.KindSecret, t.Name
		case *corev1.ConfigMap:
			kind, name = references.KindConfigMap, t.Name
		}

		annotations[references.AnnotationKey(kind, name)] = name
	}

	return metav1.ObjectMeta{
		Name:        name,
		Namespace:   metav1.NamespaceDefault,
		Annotations: annotations,
	}
}
