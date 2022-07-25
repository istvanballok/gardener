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
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func (og *operatorgrafana) GrafanaResourceConfigs() component.ResourceConfigs {
	var (
		deployment = og.emptyDeployment("operatorgrafana")
	)

	return component.ResourceConfigs{
		{Obj: deployment, Class: component.Application, MutateFn: func() { og.reconcileOperatorGrafanaDeployment(deployment) }},
	}
}

func (og *operatorgrafana) reconcileOperatorGrafanaDeployment(deployment *appsv1.Deployment) {
	deployment.Labels = getAllLabels("operatorgrafana")
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas:             pointer.Int32(1),
		RevisionHistoryLimit: pointer.Int32(2),
		Selector: &metav1.LabelSelector{
			MatchLabels: getAppLabel("operatorgrafana"),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: getAllLabels("grafana"),
			},
			Spec: corev1.PodSpec{
				//ServiceAccountName: serviceAccount.Name,
				Containers: []corev1.Container{{
					Name:            "grafana",
					Image:           "grafana/grafana", //v.values.Exporter.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{{
						Name:          "web",
						ContainerPort: 3000,
						Protocol:      corev1.ProtocolTCP,
					}},
					Env: getEnv(),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("400Mi"),
						},
					},
				}},
			},
		},
	}
}
