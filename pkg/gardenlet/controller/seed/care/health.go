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

package care

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/clock"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	v1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/component/clusterautoscaler"
	"github.com/gardener/gardener/pkg/component/clusteridentity"
	"github.com/gardener/gardener/pkg/component/dependencywatchdog"
	"github.com/gardener/gardener/pkg/component/etcd"
	"github.com/gardener/gardener/pkg/component/hvpa"
	"github.com/gardener/gardener/pkg/component/istio"
	"github.com/gardener/gardener/pkg/component/kubestatemetrics"
	"github.com/gardener/gardener/pkg/component/logging/fluentoperator"
	"github.com/gardener/gardener/pkg/component/logging/vali"
	"github.com/gardener/gardener/pkg/component/nginxingress"
	"github.com/gardener/gardener/pkg/component/seedsystem"
	"github.com/gardener/gardener/pkg/component/vpa"
	"github.com/gardener/gardener/pkg/features"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	healthchecker "github.com/gardener/gardener/pkg/utils/kubernetes/health/checker"
)

var requiredManagedResourcesSeed = sets.New(
	etcd.Druid,
	clusterautoscaler.ManagedResourceControlName,
	kubestatemetrics.ManagedResourceName,
	nginxingress.ManagedResourceName,
	seedsystem.ManagedResourceName,
	vpa.ManagedResourceControlName,
)

// health contains information needed to execute health checks for a seed.
type health struct {
	seed                *gardencorev1beta1.Seed
	seedClient          client.Client
	clock               clock.Clock
	namespace           *string
	seedIsGarden        bool
	loggingEnabled      bool
	valiEnabled         bool
	conditionThresholds map[gardencorev1beta1.ConditionType]time.Duration
	healthChecker       *healthchecker.HealthChecker
}

// NewHealth creates a new Health instance with the given parameters.
func NewHealth(
	seed *gardencorev1beta1.Seed,
	seedClient client.Client,
	clock clock.Clock,
	namespace *string,
	seedIsGarden bool,
	loggingEnabled bool,
	valiEnabled bool,
	conditionThresholds map[gardencorev1beta1.ConditionType]time.Duration,
) HealthCheck {
	return &health{
		seedClient:          seedClient,
		seed:                seed,
		clock:               clock,
		namespace:           namespace,
		seedIsGarden:        seedIsGarden,
		loggingEnabled:      loggingEnabled,
		valiEnabled:         valiEnabled,
		conditionThresholds: conditionThresholds,
		healthChecker:       healthchecker.NewHealthChecker(seedClient, clock, conditionThresholds, seed.Status.LastOperation),
	}
}

// Check conducts the health checks on all the given conditions.
func (h *health) Check(
	ctx context.Context,
	conditions SeedConditions,
) []gardencorev1beta1.Condition {
	newSystemComponentsCondition, err := h.checkSystemComponents(ctx, conditions.systemComponentsHealthy)
	return []gardencorev1beta1.Condition{v1beta1helper.NewConditionOrError(h.clock, conditions.systemComponentsHealthy, newSystemComponentsCondition, err)}
}

func (h *health) checkSystemComponents(
	ctx context.Context,
	condition gardencorev1beta1.Condition,
) (
	*gardencorev1beta1.Condition,
	error,
) {
	managedResources := sets.List(requiredManagedResourcesSeed)
	managedResources = append(managedResources, istio.ManagedResourceNames(!h.seedIsGarden, "")...)

	seedIsOriginOfClusterIdentity, err := clusteridentity.IsClusterIdentityEmptyOrFromOrigin(ctx, h.seedClient, v1beta1constants.ClusterIdentityOriginSeed)
	if err != nil {
		return nil, err
	}
	if seedIsOriginOfClusterIdentity {
		managedResources = append(managedResources, clusteridentity.ManagedResourceControlName)
	}

	if features.DefaultFeatureGate.Enabled(features.HVPA) {
		managedResources = append(managedResources, hvpa.ManagedResourceName)
	}
	if v1beta1helper.SeedSettingDependencyWatchdogWeederEnabled(h.seed.Spec.Settings) {
		managedResources = append(managedResources, dependencywatchdog.ManagedResourceDependencyWatchdogWeeder)
	}
	if v1beta1helper.SeedSettingDependencyWatchdogProberEnabled(h.seed.Spec.Settings) {
		managedResources = append(managedResources, dependencywatchdog.ManagedResourceDependencyWatchdogProber)
	}
	if h.loggingEnabled {
		managedResources = append(managedResources, fluentoperator.OperatorManagedResourceName)
		managedResources = append(managedResources, fluentoperator.CustomResourcesManagedResourceName)
		managedResources = append(managedResources, fluentoperator.FluentBitManagedResourceName)
	}
	if h.valiEnabled {
		managedResources = append(managedResources, vali.ManagedResourceNameRuntime)
	}

	for _, name := range managedResources {
		namespace := v1beta1constants.GardenNamespace
		if sets.New(istio.ManagedResourceNames(true, "")...).Has(name) {
			namespace = v1beta1constants.IstioSystemNamespace
		}
		namespace = pointer.StringDeref(h.namespace, namespace)

		mr := &resourcesv1alpha1.ManagedResource{}
		if err := h.seedClient.Get(ctx, kubernetesutils.Key(namespace, name), mr); err != nil {
			if apierrors.IsNotFound(err) {
				exitCondition := v1beta1helper.FailedCondition(h.clock, h.seed.Status.LastOperation, h.conditionThresholds, condition, "ResourceNotFound", err.Error())
				return &exitCondition, nil
			}
			return nil, err
		}

		if exitCondition := h.healthChecker.CheckManagedResource(condition, mr, nil); exitCondition != nil {
			return exitCondition, nil
		}
	}

	sts := &appsv1.StatefulSet{}
	if err := h.seedClient.Get(ctx, kubernetesutils.Key("garden", "prometheus"), sts); err != nil {
		if apierrors.IsNotFound(err) {
			exitCondition := v1beta1helper.FailedCondition(h.clock, h.seed.Status.LastOperation, h.conditionThresholds, condition, "ResourceNotFound", err.Error())
			return &exitCondition, nil
		}
		return nil, err
	}
	statefulsets := []appsv1.StatefulSet{*sts}
	if exitCondition := h.healthChecker.CheckStatefulSets(condition, statefulsets); exitCondition != nil {
		return exitCondition, nil
	}

	c := v1beta1helper.UpdatedConditionWithClock(h.clock, condition, gardencorev1beta1.ConditionTrue, "SystemComponentsRunning", "All system components are healthy.")
	return &c, nil
}

// SeedConditions contains all seed related conditions of the seed status subresource.
type SeedConditions struct {
	systemComponentsHealthy gardencorev1beta1.Condition
}

// ConvertToSlice returns the seed conditions as a slice.
func (s SeedConditions) ConvertToSlice() []gardencorev1beta1.Condition {
	return []gardencorev1beta1.Condition{
		s.systemComponentsHealthy,
	}
}

// ConditionTypes returns all seed condition types.
func (s SeedConditions) ConditionTypes() []gardencorev1beta1.ConditionType {
	return []gardencorev1beta1.ConditionType{
		s.systemComponentsHealthy.Type,
	}
}

// NewSeedConditions returns a new instance of SeedConditions.
// All conditions are retrieved from the given 'status' or newly initialized.
func NewSeedConditions(clock clock.Clock, status gardencorev1beta1.SeedStatus) SeedConditions {
	return SeedConditions{
		systemComponentsHealthy: v1beta1helper.GetOrInitConditionWithClock(clock, status.Conditions, gardencorev1beta1.SeedSystemComponentsHealthy),
	}
}
