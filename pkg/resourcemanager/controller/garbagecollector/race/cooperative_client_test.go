// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package race_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/race"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("Cooperative Client", func() {
	const (
		LABEL_KEY = "resources.gardener.cloud/foo"
	)
	var (
		ctx        = context.TODO()
		executor   race.CooperativeExecutor
		c          client.Client
		cA, cB     client.Client
		secretName string
		setup      func()
		assert     func() error

		getSecret = func(secretName string) *corev1.Secret {
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceDefault,
				},
			}
		}
		getValue = func(secret *corev1.Secret) int {
			value, _ := strconv.Atoi(secret.Labels[LABEL_KEY])
			return value
		}
		setValue = func(secret *corev1.Secret, value int) {
			secret.Labels[LABEL_KEY] = strconv.Itoa(value)
		}
	)

	BeforeEach(func() {
		executor = race.NewCooperativeExecutor(race.DEBUG)
		setup = func() {
			c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
			cA = race.NewCooperativeClient("A", c, executor)
			cB = race.NewCooperativeClient("B", c, executor)

			secret := getSecret("secret")
			kubernetesutils.MakeUnique(secret)
			secretName = secret.Name
			c.Create(ctx, secret)
		}
		assert = func() error {
			secret := getSecret(secretName)
			c.Get(ctx, client.ObjectKeyFromObject(secret), secret)
			if getValue(secret) != 2 {
				return fmt.Errorf("Assertion failed: expected 2, got %d", getValue(secret))
			}
			return nil
		}
	})

	It("should detect possible race conditions when actors do not properly use the optimistic locking of the Kubernetes client", func() {
		result := executor.RunAllCombinations(
			setup,
			assert,
			func() {
				secret := getSecret(secretName)
				mutate := func() error {
					setValue(secret, getValue(secret)+1)
					return nil
				}
				controllerutil.CreateOrUpdate(ctx, cA, secret, mutate)
			},
			func() {
				secret := getSecret(secretName)
				mutate := func() error {
					setValue(secret, getValue(secret)+1)
					return nil
				}
				controllerutil.CreateOrUpdate(ctx, cB, secret, mutate)
			},
		)
		Expect(result.NumberOfPathsWithFailedAssertion).To(Equal(4))
		Expect(result.NumberOfPathsChecked).To(Equal(6))
		Expect(strings.TrimSpace(result.PathsChecked)).To(Equal(strings.TrimSpace(`

Path 1:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 2:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
  assertion error: Assertion failed: expected 2, got 1
Path 3:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
  assertion error: Assertion failed: expected 2, got 1
Path 4:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 5:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
  assertion error: Assertion failed: expected 2, got 1
Path 6:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
  assertion error: Assertion failed: expected 2, got 1

		`)), "Here is the actual value of PathsChecked in full length. Copy this and update the string literal in the unit test's source code: \n\n%s\n",
			result.PathsChecked)
	})

	It("should detect no race conditions when actors properly use the optimistic locking of the Kubernetes client", func() {
		result := executor.RunAllCombinations(
			setup,
			assert,
			func() {
				secret := getSecret(secretName)
				mutate := func() error {
					setValue(secret, getValue(secret)+1)
					return nil
				}
				runs := 0
				for {
					if _, err := controllerutil.CreateOrUpdate(ctx, cA, secret, mutate); err == nil {
						runs++
						break
					}
				}
				Expect(runs).To(BeNumerically("<=", 2))
			},
			func() {
				secret := getSecret(secretName)
				mutate := func() error {
					setValue(secret, getValue(secret)+1)
					return nil
				}
				runs := 0
				for {
					if _, err := controllerutil.CreateOrUpdate(ctx, cB, secret, mutate); err == nil {
						runs++
						break
					}
				}
				Expect(runs).To(BeNumerically("<=", 2))
			},
		)
		Expect(result.NumberOfPathsWithFailedAssertion).To(Equal(0))
		Expect(result.NumberOfPathsChecked).To(Equal(6))
		Expect(strings.TrimSpace(result.PathsChecked)).To(Equal(strings.TrimSpace(`

Path 1:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 2:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
- B/2 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/3 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 3:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
- B/2 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/3 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 4:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 5:
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
- A/2 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/3 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>
Path 6:
- A/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/0 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- B/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/1 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 1, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=Operation cannot be fulfilled on secrets "secret-e3b0c442": object was modified
- A/2 Get(default/secret-e3b0c442, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:1 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
- A/3 Update(&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 2, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, []) -> object=&Secret{Namespace: default, Name: secret-e3b0c442, ResourceVersion: 3, Labels: map[resources.gardener.cloud/foo:2 resources.gardener.cloud/garbage-collectable-reference:true resources.gardener.cloud/used:true], Immutable: true, }, error=<nil>
  assertion error: <nil>

		`)), "Here is the actual value of PathsChecked in full length. Copy this and update the string literal in the unit test's source code: \n\n%s\n",
			result.PathsChecked)
	})

	It("should concisely print objects", func() {
		Expect(race.ObjectToString(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: metav1.NamespaceDefault,
		}})).To(Equal(
			"&Secret{Namespace: default, Name: secret, }"))
		Expect(race.ObjectToString(&resourcesv1alpha1.ManagedResource{ObjectMeta: metav1.ObjectMeta{
			Name:      "mr",
			Namespace: metav1.NamespaceDefault,
		}})).To(Equal(
			"&ManagedResource{Namespace: default, Name: mr, }"))
	})
})
