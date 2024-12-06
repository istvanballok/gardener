package race_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/race"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestIncrement(t *testing.T) {
	var (
		executor   = race.NewCooperativeExecutor(0)
		ctx        = context.TODO()
		labelKey   = "resources.gardener.cloud/foo"
		c          client.Client
		cA, cB     client.Client
		secretName string
	)

	setup := func() {
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		cA = race.NewCooperativeClient("A", c, executor)
		cB = race.NewCooperativeClient("B", c, executor)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secret",
				Namespace: metav1.NamespaceDefault,
			},
		}
		kubernetesutils.MakeUnique(secret)
		secretName = secret.Name
		c.Create(ctx, secret)
	}

	assert := func() error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: metav1.NamespaceDefault,
			},
		}
		c.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		if secret.Labels[labelKey] != "2" {
			return fmt.Errorf("Assertion failed: expected 2, got %s", secret.Labels[labelKey])
		}
		return nil
	}

	result := executor.RunAllCombinations(
		setup,
		assert,
		func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceDefault,
				},
			}
			mutate := func() error {
				value, _ := strconv.Atoi(secret.Labels[labelKey])
				value = value + 1
				secret.Labels[labelKey] = strconv.Itoa(value)
				return nil
			}
			controllerutil.CreateOrUpdate(ctx, cA, secret, mutate)
		},
		func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceDefault,
				},
			}
			mutate := func() error {
				value, _ := strconv.Atoi(secret.Labels[labelKey])
				value = value + 1
				secret.Labels[labelKey] = strconv.Itoa(value)
				return nil
			}
			controllerutil.CreateOrUpdate(ctx, cB, secret, mutate)
		},
	)
	if result.NumberOfPathsWithFailedAssertion == 0 {
		t.Error("The race condition was not detected")
	}
	if result.NumberOfPathsWithFailedAssertion != 4 {
		t.Errorf("Expected to find 4 paths that lead to a race condition, found: %d", result.NumberOfPathsWithFailedAssertion)
	}
	if result.NumberOfPathsChecked != 6 {
		t.Errorf("Not all paths were checked: expected to check 6 paths in total, got %d", result.NumberOfPathsChecked)
	}
}

func TestIncrementWithOptimisticLocking(t *testing.T) {
	var (
		executor   = race.NewCooperativeExecutor(0)
		ctx        = context.TODO()
		labelKey   = "resources.gardener.cloud/foo"
		c          client.Client
		cA, cB     client.Client
		secretName string
	)

	setup := func() {
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		cA = race.NewCooperativeClient("A", c, executor)
		cB = race.NewCooperativeClient("B", c, executor)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secret",
				Namespace: metav1.NamespaceDefault,
			},
		}
		kubernetesutils.MakeUnique(secret)
		secretName = secret.Name
		c.Create(ctx, secret)
	}

	assert := func() error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: metav1.NamespaceDefault,
			},
		}
		c.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		if secret.Labels[labelKey] != "2" {
			return fmt.Errorf("Assertion failed: expected 2, got %s", secret.Labels[labelKey])
		}
		return nil
	}

	result := executor.RunAllCombinations(
		setup,
		assert,
		func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceDefault,
				},
			}
			mutate := func() error {
				value, _ := strconv.Atoi(secret.Labels[labelKey])
				value = value + 1
				secret.Labels[labelKey] = strconv.Itoa(value)
				return nil
			}
			for {
				if _, err := controllerutil.CreateOrUpdate(ctx, cA, secret, mutate); err == nil {
					break
				}
			}
		},
		func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceDefault,
				},
			}
			mutate := func() error {
				value, _ := strconv.Atoi(secret.Labels[labelKey])
				value = value + 1
				secret.Labels[labelKey] = strconv.Itoa(value)
				return nil
			}
			for {
				if _, err := controllerutil.CreateOrUpdate(ctx, cB, secret, mutate); err == nil {
					break
				}
			}
		},
	)
	if result.NumberOfPathsWithFailedAssertion != 0 {
		t.Error("There is no race condition if the operation is retried when a conflict was detected")
	}
	if result.NumberOfPathsChecked != 6 {
		t.Errorf("Not all paths were checked: expected to check 6 paths in total, got %d", result.NumberOfPathsChecked)
	}
}
