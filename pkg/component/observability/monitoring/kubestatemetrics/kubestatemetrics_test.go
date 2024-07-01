// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubestatemetrics_test

import (
	"context"

	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/component"
	. "github.com/gardener/gardener/pkg/component/observability/monitoring/kubestatemetrics"
	componenttest "github.com/gardener/gardener/pkg/component/test"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/references"
	"github.com/gardener/gardener/pkg/utils/retry"
	retryfake "github.com/gardener/gardener/pkg/utils/retry/fake"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
)

var _ = Describe("KubeStateMetrics", func() {
	var (
		ctx = context.Background()

		namespace         = "some-namespace"
		image             = "some-image:some-tag"
		priorityClassName = "some-priorityclass"
		values            = Values{}

		c         client.Client
		sm        secretsmanager.Interface
		ksm       component.DeployWaiter
		consistOf func(...client.Object) gomegatypes.GomegaMatcher

		managedResourceName   string
		managedResource       *resourcesv1alpha1.ManagedResource
		managedResourceSecret *corev1.Secret

		vpaUpdateMode       = vpaautoscalingv1.UpdateModeAuto
		vpaControlledValues = vpaautoscalingv1.ContainerControlledValuesRequestsOnly

		serviceAccount               *corev1.ServiceAccount
		secretShootAccess            *corev1.Secret
		vpa                          *vpaautoscalingv1.VerticalPodAutoscaler
		pdbFor                       func(bool) *policyv1.PodDisruptionBudget
		customResourceStateConfigMap *corev1.ConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-resource-state-config",
				Namespace: namespace,
			},
			Data: map[string]string{
				"custom-resource-state.yaml": expectedCustomResourceStateConfig(),
			},
		}

		clusterRoleFor = func(clusterType component.ClusterType) *rbacv1.ClusterRole {
			name := "gardener.cloud:monitoring:kube-state-metrics"
			if clusterType == component.ClusterTypeSeed {
				name += "-seed"
			}

			obj := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						"component": "kube-state-metrics",
						"type":      string(clusterType),
					},
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{
							"nodes",
							"pods",
							"services",
							"resourcequotas",
							"replicationcontrollers",
							"limitranges",
							"persistentvolumeclaims",
							"namespaces",
						},
						Verbs: []string{"list", "watch"},
					},
					{
						APIGroups: []string{"apps", "extensions"},
						Resources: []string{"daemonsets", "deployments", "replicasets", "statefulsets"},
						Verbs:     []string{"list", "watch"},
					},
					{
						APIGroups: []string{"batch"},
						Resources: []string{"cronjobs", "jobs"},
						Verbs:     []string{"list", "watch"},
					},
					{
						APIGroups: []string{"apiextensions.k8s.io"},
						Resources: []string{"customresourcedefinitions"},
						Verbs:     []string{"list", "watch"},
					},
					{
						APIGroups: []string{"autoscaling.k8s.io"},
						Resources: []string{"verticalpodautoscalers"},
						Verbs:     []string{"list", "watch"},
					},
				},
			}

			if clusterType == component.ClusterTypeSeed {
				obj.Rules = append(obj.Rules, rbacv1.PolicyRule{
					APIGroups: []string{"autoscaling"},
					Resources: []string{"horizontalpodautoscalers"},
					Verbs:     []string{"list", "watch"},
				})
			}

			return obj
		}
		clusterRoleBindingFor = func(clusterType component.ClusterType) *rbacv1.ClusterRoleBinding {
			name := "gardener.cloud:monitoring:kube-state-metrics"
			if clusterType == component.ClusterTypeSeed {
				name += "-seed"
			}

			obj := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						"component": "kube-state-metrics",
						"type":      string(clusterType),
					},
					Annotations: map[string]string{
						"resources.gardener.cloud/delete-on-invalid-update": "true",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     name,
				},
				Subjects: []rbacv1.Subject{{
					Kind: rbacv1.ServiceAccountKind,
					Name: "kube-state-metrics",
				}},
			}

			if clusterType == component.ClusterTypeSeed {
				obj.Subjects[0].Namespace = namespace
			} else {
				obj.Subjects[0].Namespace = "kube-system"
			}

			return obj
		}
		serviceFor = func(clusterType component.ClusterType) *corev1.Service {
			obj := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-state-metrics",
					Namespace: namespace,
					Labels: map[string]string{
						"component": "kube-state-metrics",
						"type":      string(clusterType),
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"component": "kube-state-metrics",
						"type":      string(clusterType),
					},
					Ports: []corev1.ServicePort{{
						Name:       "metrics",
						Port:       80,
						TargetPort: intstr.FromInt32(8080),
						Protocol:   corev1.ProtocolTCP,
					}},
				},
			}

			if clusterType == component.ClusterTypeSeed {
				obj.Annotations = map[string]string{
					"networking.resources.gardener.cloud/from-all-garden-scrape-targets-allowed-ports": `[{"protocol":"TCP","port":8080}]`,
					"networking.resources.gardener.cloud/from-all-seed-scrape-targets-allowed-ports":   `[{"protocol":"TCP","port":8080}]`,
				}
			}
			if clusterType == component.ClusterTypeShoot {
				obj.Annotations = map[string]string{"networking.resources.gardener.cloud/from-all-scrape-targets-allowed-ports": `[{"protocol":"TCP","port":8080}]`}
			}

			return obj
		}
		deploymentFor = func(clusterType component.ClusterType) *appsv1.Deployment {
			var (
				maxUnavailable = intstr.FromInt32(1)
				selectorLabels = map[string]string{
					"component": "kube-state-metrics",
					"type":      string(clusterType),
				}

				deploymentLabels             map[string]string
				podLabels                    map[string]string
				args                         []string
				automountServiceAccountToken *bool
				serviceAccountName           string
				volumeMounts                 []corev1.VolumeMount
				volumes                      []corev1.Volume
			)

			if clusterType == component.ClusterTypeSeed {
				deploymentLabels = map[string]string{
					"component": "kube-state-metrics",
					"type":      string(clusterType),
					"role":      "monitoring",
				}
				podLabels = map[string]string{
					"component":                        "kube-state-metrics",
					"type":                             string(clusterType),
					"role":                             "monitoring",
					"networking.gardener.cloud/to-dns": "allowed",
					"networking.gardener.cloud/to-runtime-apiserver": "allowed",
				}
				args = []string{
					"--port=8080",
					"--telemetry-port=8081",
					"--resources=deployments,pods,statefulsets,nodes,horizontalpodautoscalers,persistentvolumeclaims,replicasets,namespaces",
					"--metric-labels-allowlist=nodes=[*]",
					"--metric-annotations-allowlist=namespaces=[shoot.gardener.cloud/uid]",
					"--metric-allowlist=" +
						"kube_daemonset_metadata_generation," +
						"kube_daemonset_status_current_number_scheduled," +
						"kube_daemonset_status_desired_number_scheduled," +
						"kube_daemonset_status_number_available," +
						"kube_daemonset_status_number_unavailable," +
						"kube_daemonset_status_updated_number_scheduled," +
						"kube_deployment_metadata_generation," +
						"kube_deployment_spec_replicas," +
						"kube_deployment_status_observed_generation," +
						"kube_deployment_status_replicas," +
						"kube_deployment_status_replicas_available," +
						"kube_deployment_status_replicas_unavailable," +
						"kube_deployment_status_replicas_updated," +
						"kube_horizontalpodautoscaler_spec_max_replicas," +
						"kube_horizontalpodautoscaler_spec_min_replicas," +
						"kube_horizontalpodautoscaler_status_current_replicas," +
						"kube_horizontalpodautoscaler_status_desired_replicas," +
						"kube_horizontalpodautoscaler_status_condition," +
						"kube_namespace_annotations," +
						"kube_node_info," +
						"kube_node_labels," +
						"kube_node_spec_taint," +
						"kube_node_spec_unschedulable," +
						"kube_node_status_allocatable," +
						"kube_node_status_capacity," +
						"kube_node_status_condition," +
						"kube_persistentvolumeclaim_resource_requests_storage_bytes," +
						"kube_pod_container_info," +
						"kube_pod_container_resource_limits," +
						"kube_pod_container_resource_requests," +
						"kube_pod_container_status_restarts_total," +
						"kube_pod_info," +
						"kube_pod_labels," +
						"kube_pod_owner," +
						"kube_pod_spec_volumes_persistentvolumeclaims_info," +
						"kube_pod_status_phase," +
						"kube_pod_status_ready," +
						"kube_replicaset_owner," +
						"kube_statefulset_metadata_generation," +
						"kube_statefulset_replicas," +
						"kube_statefulset_status_observed_generation," +
						"kube_statefulset_status_replicas," +
						"kube_statefulset_status_replicas_current," +
						"kube_statefulset_status_replicas_ready," +
						"kube_statefulset_status_replicas_updated," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_cpu," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_memory," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_cpu," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_memory," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_cpu," +
						"kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_memory," +
						"kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_cpu," +
						"kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_memory," +
						"kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_cpu," +
						"kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_memory," +
						"kube_customresource_verticalpodautoscaler_spec_updatepolicy_updatemode",
					"--custom-resource-state-config-file=/config/custom-resource-state.yaml",
				}
				serviceAccountName = "kube-state-metrics"
			}

			volumes = []corev1.Volume{{
				Name: "custom-resource-state-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "custom-resource-state-config",
						},
					},
				},
			}}

			volumeMounts = []corev1.VolumeMount{{
				Name:      "custom-resource-state-config",
				MountPath: "/config",
				ReadOnly:  true,
			}}

			if clusterType == component.ClusterTypeShoot {
				deploymentLabels = map[string]string{
					"component":           "kube-state-metrics",
					"type":                string(clusterType),
					"gardener.cloud/role": "monitoring",
				}
				podLabels = map[string]string{
					"component":                        "kube-state-metrics",
					"type":                             string(clusterType),
					"gardener.cloud/role":              "monitoring",
					"networking.gardener.cloud/to-dns": "allowed",
					"networking.resources.gardener.cloud/to-kube-apiserver-tcp-443": "allowed",
				}
				args = []string{
					"--port=8080",
					"--telemetry-port=8081",
					"--resources=daemonsets,deployments,nodes,pods,statefulsets,replicasets",
					"--namespaces=kube-system",
					"--kubeconfig=/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig/kubeconfig",
					"--metric-labels-allowlist=nodes=[*]",
					"--custom-resource-state-config-file=/config/custom-resource-state.yaml",
				}
				automountServiceAccountToken = ptr.To(false)
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      "kubeconfig",
					MountPath: "/var/run/secrets/gardener.cloud/shoot/generic-kubeconfig",
					ReadOnly:  true,
				})
				volumes = append(volumes, corev1.Volume{
					Name: "kubeconfig",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							DefaultMode: ptr.To[int32](420),
							Sources: []corev1.VolumeProjection{
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "generic-token-kubeconfig",
										},
										Items: []corev1.KeyToPath{{
											Key:  "kubeconfig",
											Path: "kubeconfig",
										}},
										Optional: ptr.To(false),
									},
								},
								{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "shoot-access-kube-state-metrics",
										},
										Items: []corev1.KeyToPath{{
											Key:  "token",
											Path: "token",
										}},
										Optional: ptr.To(false),
									},
								},
							},
						},
					},
				})
			}

			return &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-state-metrics",
					Namespace: namespace,
					Labels:    deploymentLabels,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas:             ptr.To[int32](0),
					RevisionHistoryLimit: ptr.To[int32](2),
					Selector:             &metav1.LabelSelector{MatchLabels: selectorLabels},
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.RollingUpdateDeploymentStrategyType,
						RollingUpdate: &appsv1.RollingUpdateDeployment{
							MaxUnavailable: &maxUnavailable,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: podLabels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:            "kube-state-metrics",
								Image:           image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Args:            args,
								Ports: []corev1.ContainerPort{{
									Name:          "metrics",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								}},
								LivenessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/healthz",
											Port: intstr.FromInt32(8080),
										},
									},
									InitialDelaySeconds: 5,
									TimeoutSeconds:      5,
								},
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/healthz",
											Port: intstr.FromInt32(8080),
										},
									},
									InitialDelaySeconds: 5,
									PeriodSeconds:       30,
									SuccessThreshold:    1,
									FailureThreshold:    3,
									TimeoutSeconds:      5,
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("10m"),
										corev1.ResourceMemory: resource.MustParse("32Mi"),
									},
								},
								VolumeMounts: volumeMounts,
							}},
							AutomountServiceAccountToken: automountServiceAccountToken,
							PriorityClassName:            priorityClassName,
							ServiceAccountName:           serviceAccountName,
							Volumes:                      volumes,
						},
					},
				},
			}
		}

		scrapeConfigCache = &monitoringv1alpha1.ScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cache-kube-state-metrics",
				Namespace: namespace,
				Labels:    map[string]string{"prometheus": "cache"},
			},
			Spec: monitoringv1alpha1.ScrapeConfigSpec{
				KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
					Role:       "service",
					Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{namespace}},
				}},
				RelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_service_label_component",
							"__meta_kubernetes_service_port_name",
						},
						Regex:  "kube-state-metrics;metrics",
						Action: "keep",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_service_label_type"},
						Regex:        `(.+)`,
						Replacement:  ptr.To(`${1}`),
						TargetLabel:  "type",
					},
					{
						Action:      "replace",
						Replacement: ptr.To("kube-state-metrics"),
						TargetLabel: "job",
					},
					{
						TargetLabel: "instance",
						Replacement: ptr.To("kube-state-metrics"),
					},
				},
				MetricRelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{"pod"},
						Regex:        `^.+\.tf-pod.+$`,
						Action:       "drop",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"__name__"},
						Action:       "keep",
						Regex:        `^(kube_daemonset_metadata_generation|kube_daemonset_status_current_number_scheduled|kube_daemonset_status_desired_number_scheduled|kube_daemonset_status_number_available|kube_daemonset_status_number_unavailable|kube_daemonset_status_updated_number_scheduled|kube_deployment_metadata_generation|kube_deployment_spec_replicas|kube_deployment_status_observed_generation|kube_deployment_status_replicas|kube_deployment_status_replicas_available|kube_deployment_status_replicas_unavailable|kube_deployment_status_replicas_updated|kube_horizontalpodautoscaler_spec_max_replicas|kube_horizontalpodautoscaler_spec_min_replicas|kube_horizontalpodautoscaler_status_current_replicas|kube_horizontalpodautoscaler_status_desired_replicas|kube_horizontalpodautoscaler_status_condition|kube_namespace_annotations|kube_node_info|kube_node_labels|kube_node_spec_taint|kube_node_spec_unschedulable|kube_node_status_allocatable|kube_node_status_capacity|kube_node_status_condition|kube_persistentvolumeclaim_resource_requests_storage_bytes|kube_pod_container_info|kube_pod_container_resource_limits|kube_pod_container_resource_requests|kube_pod_container_status_restarts_total|kube_pod_info|kube_pod_labels|kube_pod_owner|kube_pod_spec_volumes_persistentvolumeclaims_info|kube_pod_status_phase|kube_pod_status_ready|kube_replicaset_owner|kube_statefulset_metadata_generation|kube_statefulset_replicas|kube_statefulset_status_observed_generation|kube_statefulset_status_replicas|kube_statefulset_status_replicas_current|kube_statefulset_status_replicas_ready|kube_statefulset_status_replicas_updated|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_memory|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_memory|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_memory|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_cpu|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_memory|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_cpu|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_memory|kube_customresource_verticalpodautoscaler_spec_updatepolicy_updatemode)$`,
					},
				},
			},
		}
		scrapeConfigSeed = &monitoringv1alpha1.ScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "seed-kube-state-metrics",
				Namespace: namespace,
				Labels:    map[string]string{"prometheus": "seed"},
			},
			Spec: monitoringv1alpha1.ScrapeConfigSpec{
				KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
					Role:       "service",
					Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{namespace}},
				}},
				RelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_service_label_component",
							"__meta_kubernetes_service_port_name",
						},
						Regex:  "kube-state-metrics;metrics",
						Action: "keep",
					},
					{
						Action:      "replace",
						Replacement: ptr.To("kube-state-metrics"),
						TargetLabel: "job",
					},
					{
						TargetLabel: "instance",
						Replacement: ptr.To("kube-state-metrics"),
					},
				},
				MetricRelabelConfigs: []monitoringv1.RelabelConfig{{
					SourceLabels: []monitoringv1.LabelName{"namespace"},
					Regex:        `shoot-.+`,
					Action:       "drop",
				}},
			},
		}
		scrapeConfigGarden = &monitoringv1alpha1.ScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "garden-kube-state-metrics",
				Namespace: namespace,
				Labels:    map[string]string{"prometheus": "garden"},
			},
			Spec: monitoringv1alpha1.ScrapeConfigSpec{
				KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
					Role:       "service",
					Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{namespace}},
				}},
				RelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_service_label_component",
							"__meta_kubernetes_service_port_name",
						},
						Regex:  "kube-state-metrics;metrics",
						Action: "keep",
					},
					{
						Action:      "replace",
						Replacement: ptr.To("kube-state-metrics"),
						TargetLabel: "job",
					},
					{
						TargetLabel: "instance",
						Replacement: ptr.To("kube-state-metrics"),
					},
				},
				MetricRelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{"pod"},
						Regex:        `^.+\.tf-pod.+$`,
						Action:       "drop",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"namespace"},
						Regex:        "garden",
						Action:       "drop",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"__name__"},
						Action:       "keep",
						Regex:        `^(kube_pod_container_status_restarts_total|kube_pod_status_phase)$`,
					},
				},
			},
		}
		scrapeConfigShoot = &monitoringv1alpha1.ScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shoot-kube-state-metrics",
				Namespace: namespace,
				Labels:    map[string]string{"prometheus": "shoot"},
			},
			Spec: monitoringv1alpha1.ScrapeConfigSpec{
				KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
					Role:       "service",
					Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{namespace}},
				}},
				RelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{
							"__meta_kubernetes_service_label_component",
							"__meta_kubernetes_service_port_name",
						},
						Regex:  "kube-state-metrics;metrics",
						Action: "keep",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_service_label_type"},
						Regex:        `(.+)`,
						Replacement:  ptr.To(`${1}`),
						TargetLabel:  "type",
					},
					{
						Action:      "replace",
						Replacement: ptr.To("kube-state-metrics"),
						TargetLabel: "job",
					},
					{
						TargetLabel: "instance",
						Replacement: ptr.To("kube-state-metrics"),
					},
				},
				MetricRelabelConfigs: []monitoringv1.RelabelConfig{
					{
						SourceLabels: []monitoringv1.LabelName{"pod"},
						Regex:        `^.+\.tf-pod.+$`,
						Action:       "drop",
					},
					{
						SourceLabels: []monitoringv1.LabelName{"__name__"},
						Action:       "keep",
						Regex:        `^(kube_daemonset_metadata_generation|kube_daemonset_status_current_number_scheduled|kube_daemonset_status_desired_number_scheduled|kube_daemonset_status_number_available|kube_daemonset_status_number_unavailable|kube_daemonset_status_updated_number_scheduled|kube_deployment_metadata_generation|kube_deployment_spec_replicas|kube_deployment_status_observed_generation|kube_deployment_status_replicas|kube_deployment_status_replicas_available|kube_deployment_status_replicas_unavailable|kube_deployment_status_replicas_updated|kube_node_info|kube_node_labels|kube_node_spec_taint|kube_node_spec_unschedulable|kube_node_status_allocatable|kube_node_status_capacity|kube_node_status_condition|kube_pod_container_info|kube_pod_container_resource_limits|kube_pod_container_resource_requests|kube_pod_container_status_restarts_total|kube_pod_info|kube_pod_labels|kube_pod_status_phase|kube_pod_status_ready|kube_replicaset_owner|kube_replicaset_metadata_generation|kube_replicaset_spec_replicas|kube_replicaset_status_observed_generation|kube_replicaset_status_replicas|kube_replicaset_status_ready_replicas|kube_statefulset_metadata_generation|kube_statefulset_replicas|kube_statefulset_status_observed_generation|kube_statefulset_status_replicas|kube_statefulset_status_replicas_current|kube_statefulset_status_replicas_ready|kube_statefulset_status_replicas_updated|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_memory|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_memory|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_cpu|kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_memory|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_cpu|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_memory|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_cpu|kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_memory|kube_customresource_verticalpodautoscaler_spec_updatepolicy_updatemode)$`,
					},
				},
			},
		}
		prometheusRuleShoot = func(workerless bool) *monitoringv1.PrometheusRule {
			rules := []monitoringv1.Rule{{
				Alert: "KubeStateMetricsSeedDown",
				Expr:  intstr.FromString(`absent(count({exported_job="kube-state-metrics"}))`),
				For:   ptr.To(monitoringv1.Duration("15m")),
				Labels: map[string]string{
					"service":    "kube-state-metrics-seed",
					"severity":   "critical",
					"type":       "seed",
					"visibility": "operator",
				},
				Annotations: map[string]string{
					"summary":     "There are no kube-state-metrics metrics for the control plane",
					"description": "Kube-state-metrics is scraped by the cache prometheus and federated by the control plane prometheus. Something is broken in that process.",
				},
			}}

			if !workerless {
				rules = append(rules,
					monitoringv1.Rule{
						Alert: "KubeStateMetricsShootDown",
						Expr:  intstr.FromString(`absent(up{job="kube-state-metrics", type="shoot"} == 1)`),
						For:   ptr.To(monitoringv1.Duration("15m")),
						Labels: map[string]string{
							"service":    "kube-state-metrics-shoot",
							"severity":   "info",
							"type":       "seed",
							"visibility": "operator",
						},
						Annotations: map[string]string{
							"summary":     "Kube-state-metrics for shoot cluster metrics is down.",
							"description": "There are no running kube-state-metric pods for the shoot cluster. No kubernetes resource metrics can be scraped.",
						},
					},
					monitoringv1.Rule{
						Alert: "NoWorkerNodes",
						Expr:  intstr.FromString(`sum(kube_node_spec_unschedulable) == count(kube_node_info) or absent(kube_node_info)`),
						For:   ptr.To(monitoringv1.Duration("25m")),
						Labels: map[string]string{
							"service":    "nodes",
							"severity":   "blocker",
							"visibility": "all",
						},
						Annotations: map[string]string{
							"summary":     "No nodes available. Possibly all workloads down.",
							"description": "There are no worker nodes in the cluster or all of the worker nodes in the cluster are not schedulable.",
						},
					},
					monitoringv1.Rule{
						Record: "shoot:kube_node_status_capacity_cpu_cores:sum",
						Expr:   intstr.FromString(`sum(kube_node_status_capacity{resource="cpu",unit="core"})`),
					},
					monitoringv1.Rule{
						Record: "shoot:kube_node_status_capacity_memory_bytes:sum",
						Expr:   intstr.FromString(`sum(kube_node_status_capacity{resource="memory",unit="byte"})`),
					},
					monitoringv1.Rule{
						Record: "shoot:machine_types:sum",
						Expr:   intstr.FromString(`sum(kube_node_labels) by (label_beta_kubernetes_io_instance_type)`),
					},
					monitoringv1.Rule{
						Record: "shoot:node_operating_system:sum",
						Expr:   intstr.FromString(`sum(kube_node_info) by (os_image, kernel_version)`),
					},
					monitoringv1.Rule{
						Record: "kube_pod_container_resource_limits_cpu_cores",
						Expr:   intstr.FromString(`kube_pod_container_resource_limits{resource="cpu", unit="core"}`),
					},
					monitoringv1.Rule{
						Record: "kube_pod_container_resource_requests_cpu_cores",
						Expr:   intstr.FromString(`kube_pod_container_resource_requests{resource="cpu", unit="core"}`),
					},
					monitoringv1.Rule{
						Record: "kube_pod_container_resource_limits_memory_bytes",
						Expr:   intstr.FromString(`kube_pod_container_resource_limits{resource="memory", unit="byte"}`),
					},
					monitoringv1.Rule{
						Record: "kube_pod_container_resource_requests_memory_bytes",
						Expr:   intstr.FromString(`kube_pod_container_resource_requests{resource="memory", unit="byte"}`),
					},
				)
			}

			return &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shoot-kube-state-metrics",
					Namespace: namespace,
					Labels:    map[string]string{"prometheus": "shoot"},
				},
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{{
						Name:  "kube-state-metrics.rules",
						Rules: rules,
					}},
				},
			}
		}
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		sm = fakesecretsmanager.New(c, namespace)
		consistOf = NewManagedResourceConsistOfObjectsMatcher(c)

		ksm = New(c, namespace, sm, values)
		managedResourceName = ""

		selectorLabelsClusterTypeSeed := map[string]string{
			"component": "kube-state-metrics",
			"type":      string(component.ClusterTypeSeed),
		}

		By("Create secrets managed outside of this package for whose secretsmanager.Get() will be called")
		Expect(c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "generic-token-kubeconfig", Namespace: namespace}})).To(Succeed())

		serviceAccount = &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-state-metrics",
				Namespace: namespace,
				Labels:    selectorLabelsClusterTypeSeed,
			},
			AutomountServiceAccountToken: ptr.To(false),
		}
		secretShootAccess = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shoot-access-kube-state-metrics",
				Namespace: namespace,
				Annotations: map[string]string{
					"serviceaccount.resources.gardener.cloud/name":      "kube-state-metrics",
					"serviceaccount.resources.gardener.cloud/namespace": "kube-system",
				},
				Labels: map[string]string{
					"resources.gardener.cloud/purpose": "token-requestor",
					"resources.gardener.cloud/class":   "shoot",
				},
			},
			Type: corev1.SecretTypeOpaque,
		}
		vpa = &vpaautoscalingv1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-state-metrics-vpa",
				Namespace: namespace,
			},
			Spec: vpaautoscalingv1.VerticalPodAutoscalerSpec{
				TargetRef: &autoscalingv1.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "kube-state-metrics",
				},
				UpdatePolicy: &vpaautoscalingv1.PodUpdatePolicy{UpdateMode: &vpaUpdateMode},
				ResourcePolicy: &vpaautoscalingv1.PodResourcePolicy{
					ContainerPolicies: []vpaautoscalingv1.ContainerResourcePolicy{
						{
							ContainerName:    "*",
							ControlledValues: &vpaControlledValues,
							MinAllowed: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
						},
					},
				},
			},
		}
		maxUnavailable := intstr.FromInt32(1)
		pdbFor = func(k8sGreaterEqual126 bool) *policyv1.PodDisruptionBudget {
			var (
				unhealthyPodEvictionPolicyAlwatysAllow = policyv1.AlwaysAllow
				pdb                                    = &policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-state-metrics-pdb",
						Namespace: namespace,
						Labels:    selectorLabelsClusterTypeSeed,
					},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: selectorLabelsClusterTypeSeed,
						},
						MaxUnavailable: &maxUnavailable,
					},
				}
			)

			if k8sGreaterEqual126 {
				pdb.Spec.UnhealthyPodEvictionPolicy = &unhealthyPodEvictionPolicyAlwatysAllow
			}

			return pdb
		}
	})

	JustBeforeEach(func() {
		managedResource = &resourcesv1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedResourceName,
				Namespace: namespace,
			},
		}
		managedResourceSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "managedresource-" + managedResource.Name,
				Namespace: namespace,
			},
		}
	})

	Describe("#Deploy", func() {
		Context("cluster type seed", func() {
			var expectedObjects []client.Object

			BeforeEach(func() {
				ksm = New(c, namespace, nil, Values{
					ClusterType:       component.ClusterTypeSeed,
					KubernetesVersion: semver.MustParse("1.26.3"),
					Image:             image,
					PriorityClassName: priorityClassName,
				})
				managedResourceName = "kube-state-metrics"
			})

			JustBeforeEach(func() {
				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())

				Expect(ksm.Deploy(ctx)).To(Succeed())

				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(Succeed())
				expectedMr := &resourcesv1alpha1.ManagedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      managedResourceName,
						Namespace: namespace,
						Labels: map[string]string{
							"gardener.cloud/role":                "seed-system-component",
							"care.gardener.cloud/condition-type": "ObservabilityComponentsHealthy",
						},
						ResourceVersion: "1",
					},
					Spec: resourcesv1alpha1.ManagedResourceSpec{
						Class: ptr.To("seed"),
						SecretRefs: []corev1.LocalObjectReference{{
							Name: managedResource.Spec.SecretRefs[0].Name,
						}},
						KeepObjects: ptr.To(false),
					},
				}
				utilruntime.Must(references.InjectAnnotations(expectedMr))
				Expect(managedResource).To(DeepEqual(expectedMr))
				expectedObjects = []client.Object{
					serviceAccount,
					clusterRoleFor(component.ClusterTypeSeed),
					clusterRoleBindingFor(component.ClusterTypeSeed),
					serviceFor(component.ClusterTypeSeed),
					deploymentFor(component.ClusterTypeSeed),
					vpa,
					scrapeConfigCache,
					scrapeConfigSeed,
					scrapeConfigGarden,
					customResourceStateConfigMap,
				}

				managedResourceSecret.Name = managedResource.Spec.SecretRefs[0].Name
				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(Succeed())
				Expect(managedResourceSecret.Type).To(Equal(corev1.SecretTypeOpaque))
				Expect(managedResourceSecret.Immutable).To(Equal(ptr.To(true)))
				Expect(managedResourceSecret.Labels["resources.gardener.cloud/garbage-collectable-reference"]).To(Equal("true"))
			})

			Context("Kubernetes versions >= 1.26", func() {
				It("should successfully deploy all resources", func() {
					expectedObjects = append(expectedObjects, pdbFor(true))
					Expect(managedResource).To(consistOf(expectedObjects...))
				})
			})

			Context("Kubernetes versions < 1.26", func() {
				BeforeEach(func() {
					ksm = New(c, namespace, nil, Values{
						ClusterType:       component.ClusterTypeSeed,
						KubernetesVersion: semver.MustParse("1.25.3"),
						Image:             image,
						PriorityClassName: priorityClassName,
						IsWorkerless:      false,
					})
				})

				It("should successfully deploy all resources", func() {
					expectedObjects = append(expectedObjects, pdbFor(false))
					Expect(managedResource).To(consistOf(expectedObjects...))
				})
			})
		})

		Context("cluster type shoot", func() {
			BeforeEach(func() {
				values = Values{
					ClusterType:       component.ClusterTypeShoot,
					Image:             image,
					PriorityClassName: priorityClassName,
				}
				managedResourceName = "shoot-core-kube-state-metrics"
			})

			JustBeforeEach(func() {
				ksm = New(c, namespace, sm, values)
			})

			Context("when shoot is not workerless", func() {
				BeforeEach(func() {
					values.IsWorkerless = false
				})

				It("should successfully deploy all resources", func() {
					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())

					Expect(ksm.Deploy(ctx)).To(Succeed())

					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(Succeed())
					expectedMr := &resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Name:            managedResourceName,
							Namespace:       namespace,
							ResourceVersion: "1",
							Labels:          map[string]string{"origin": "gardener"},
						},
						Spec: resourcesv1alpha1.ManagedResourceSpec{
							InjectLabels: map[string]string{"shoot.gardener.cloud/no-cleanup": "true"},
							SecretRefs: []corev1.LocalObjectReference{{
								Name: managedResource.Spec.SecretRefs[0].Name,
							}},
							KeepObjects: ptr.To(false),
						},
					}
					utilruntime.Must(references.InjectAnnotations(expectedMr))
					Expect(managedResource).To(DeepEqual(expectedMr))
					Expect(managedResource).To(consistOf(
						clusterRoleFor(component.ClusterTypeShoot),
						clusterRoleBindingFor(component.ClusterTypeShoot),
					))

					managedResourceSecret.Name = managedResource.Spec.SecretRefs[0].Name
					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(Succeed())
					Expect(managedResourceSecret.Type).To(Equal(corev1.SecretTypeOpaque))
					Expect(managedResourceSecret.Immutable).To(Equal(ptr.To(true)))
					Expect(managedResourceSecret.Labels["resources.gardener.cloud/garbage-collectable-reference"]).To(Equal("true"))

					actualSecretShootAccess := &corev1.Secret{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(secretShootAccess), actualSecretShootAccess)).To(Succeed())
					expectedSecretShootAccess := secretShootAccess.DeepCopy()
					expectedSecretShootAccess.ResourceVersion = "1"
					Expect(actualSecretShootAccess).To(Equal(expectedSecretShootAccess))

					actualService := &corev1.Service{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(serviceFor(component.ClusterTypeShoot)), actualService)).To(Succeed())
					expectedService := serviceFor(component.ClusterTypeShoot).DeepCopy()
					expectedService.ResourceVersion = "1"
					Expect(actualService).To(Equal(expectedService))

					actualDeployment := &appsv1.Deployment{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(deploymentFor(component.ClusterTypeShoot)), actualDeployment)).To(Succeed())
					expectedDeployment := deploymentFor(component.ClusterTypeShoot).DeepCopy()
					expectedDeployment.ResourceVersion = "1"
					Expect(actualDeployment).To(Equal(expectedDeployment))

					actualVPA := &vpaautoscalingv1.VerticalPodAutoscaler{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(vpa), actualVPA)).To(Succeed())
					expectedVPA := vpa.DeepCopy()
					expectedVPA.ResourceVersion = "1"
					Expect(actualVPA).To(Equal(expectedVPA))

					actualScrapeConfig := &monitoringv1alpha1.ScrapeConfig{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(scrapeConfigShoot), actualScrapeConfig)).To(Succeed())
					expectedScrapeConfig := scrapeConfigShoot.DeepCopy()
					expectedScrapeConfig.ResourceVersion = "1"
					Expect(actualScrapeConfig).To(Equal(expectedScrapeConfig))

					actualPrometheusRule := &monitoringv1.PrometheusRule{}
					prometheusRule := prometheusRuleShoot(values.IsWorkerless)
					Expect(c.Get(ctx, client.ObjectKeyFromObject(prometheusRule), actualPrometheusRule)).To(Succeed())
					expectedPrometheusRule := prometheusRule.DeepCopy()
					expectedPrometheusRule.ResourceVersion = "1"
					Expect(actualPrometheusRule).To(Equal(expectedPrometheusRule))

					componenttest.PrometheusRule(prometheusRule, "testdata/shoot-kube-state-metrics.prometheusrule.test.yaml")
				})
			})

			Context("when shoot is workerless", func() {
				BeforeEach(func() {
					values.IsWorkerless = true
				})

				It("should successfully deploy all resources", func() {
					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())

					Expect(ksm.Deploy(ctx)).To(Succeed())

					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(Succeed())
					expectedMr := &resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Name:            managedResourceName,
							Namespace:       namespace,
							ResourceVersion: "1",
							Labels:          map[string]string{"origin": "gardener"},
						},
						Spec: resourcesv1alpha1.ManagedResourceSpec{
							InjectLabels: map[string]string{"shoot.gardener.cloud/no-cleanup": "true"},
							SecretRefs: []corev1.LocalObjectReference{{
								Name: managedResource.Spec.SecretRefs[0].Name,
							}},
							KeepObjects: ptr.To(false),
						},
					}
					utilruntime.Must(references.InjectAnnotations(expectedMr))
					Expect(managedResource).To(DeepEqual(expectedMr))
					Expect(managedResource).To(consistOf(
						clusterRoleFor(component.ClusterTypeShoot),
						clusterRoleBindingFor(component.ClusterTypeShoot),
					))

					managedResourceSecret.Name = managedResource.Spec.SecretRefs[0].Name
					Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(Succeed())
					Expect(managedResourceSecret.Type).To(Equal(corev1.SecretTypeOpaque))
					Expect(managedResourceSecret.Immutable).To(Equal(ptr.To(true)))
					Expect(managedResourceSecret.Labels["resources.gardener.cloud/garbage-collectable-reference"]).To(Equal("true"))

					actualSecretShootAccess := &corev1.Secret{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(secretShootAccess), actualSecretShootAccess)).To(Succeed())
					expectedSecretShootAccess := secretShootAccess.DeepCopy()
					expectedSecretShootAccess.ResourceVersion = "1"
					Expect(actualSecretShootAccess).To(Equal(expectedSecretShootAccess))

					actualService := &corev1.Service{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(serviceFor(component.ClusterTypeShoot)), actualService)).To(Succeed())
					expectedService := serviceFor(component.ClusterTypeShoot).DeepCopy()
					expectedService.ResourceVersion = "1"
					Expect(actualService).To(Equal(expectedService))

					actualDeployment := &appsv1.Deployment{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(deploymentFor(component.ClusterTypeShoot)), actualDeployment)).To(Succeed())
					expectedDeployment := deploymentFor(component.ClusterTypeShoot).DeepCopy()
					expectedDeployment.ResourceVersion = "1"
					Expect(actualDeployment).To(Equal(expectedDeployment))

					actualVPA := &vpaautoscalingv1.VerticalPodAutoscaler{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(vpa), actualVPA)).To(Succeed())
					expectedVPA := vpa.DeepCopy()
					expectedVPA.ResourceVersion = "1"
					Expect(actualVPA).To(Equal(expectedVPA))

					Expect(c.Get(ctx, client.ObjectKeyFromObject(scrapeConfigShoot), &monitoringv1alpha1.ScrapeConfig{})).To(BeNotFoundError())

					actualPrometheusRule := &monitoringv1.PrometheusRule{}
					prometheusRule := prometheusRuleShoot(values.IsWorkerless)
					Expect(c.Get(ctx, client.ObjectKeyFromObject(prometheusRule), actualPrometheusRule)).To(Succeed())
					expectedPrometheusRule := prometheusRule.DeepCopy()
					expectedPrometheusRule.ResourceVersion = "1"
					Expect(actualPrometheusRule).To(Equal(expectedPrometheusRule))

					componenttest.PrometheusRule(prometheusRule, "testdata/shoot-kube-state-metrics.workerless.prometheusrule.test.yaml")
				})
			})
		})
	})

	Describe("#Destroy", func() {
		Context("cluster type seed", func() {
			BeforeEach(func() {
				ksm = New(c, namespace, nil, Values{ClusterType: component.ClusterTypeSeed})
				managedResourceName = "kube-state-metrics"
			})

			It("should successfully destroy all resources", func() {
				Expect(c.Create(ctx, managedResource)).To(Succeed())
				Expect(c.Create(ctx, managedResourceSecret)).To(Succeed())

				Expect(ksm.Destroy(ctx)).To(Succeed())

				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(BeNotFoundError())
			})
		})

		Context("cluster type shoot", func() {
			BeforeEach(func() {
				ksm = New(c, namespace, sm, Values{ClusterType: component.ClusterTypeShoot})
				managedResourceName = "shoot-core-kube-state-metrics"
			})

			It("should successfully destroy all resources", func() {
				Expect(c.Create(ctx, managedResource)).To(Succeed())
				Expect(c.Create(ctx, managedResourceSecret)).To(Succeed())
				Expect(c.Create(ctx, secretShootAccess)).To(Succeed())
				Expect(c.Create(ctx, serviceFor(component.ClusterTypeShoot))).To(Succeed())
				Expect(c.Create(ctx, deploymentFor(component.ClusterTypeShoot))).To(Succeed())
				Expect(c.Create(ctx, vpa)).To(Succeed())
				Expect(c.Create(ctx, scrapeConfigShoot)).To(Succeed())
				Expect(c.Create(ctx, prometheusRuleShoot(false))).To(Succeed())

				Expect(ksm.Destroy(ctx)).To(Succeed())

				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(managedResourceSecret), managedResourceSecret)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(secretShootAccess), secretShootAccess)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(serviceFor(component.ClusterTypeShoot)), serviceFor(component.ClusterTypeShoot))).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(deploymentFor(component.ClusterTypeShoot)), deploymentFor(component.ClusterTypeShoot))).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(vpa), vpa)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(scrapeConfigSeed), scrapeConfigSeed)).To(BeNotFoundError())
				Expect(c.Get(ctx, client.ObjectKeyFromObject(prometheusRuleShoot(false)), prometheusRuleShoot(false))).To(BeNotFoundError())
			})
		})
	})

	Context("waiting functions", func() {
		var fakeOps *retryfake.Ops

		BeforeEach(func() {
			fakeOps = &retryfake.Ops{MaxAttempts: 1}
			DeferCleanup(test.WithVars(
				&retry.Until, fakeOps.Until,
				&retry.UntilTimeout, fakeOps.UntilTimeout,
			))
		})

		Describe("#Wait", func() {
			tests := func(managedResourceName string) {
				It("should fail because reading the ManagedResource fails", func() {
					Expect(ksm.Wait(ctx)).To(MatchError(ContainSubstring("not found")))
				})

				It("should fail because the ManagedResource doesn't become healthy", func() {
					fakeOps.MaxAttempts = 2

					Expect(c.Create(ctx, &resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Name:       managedResourceName,
							Namespace:  namespace,
							Generation: 1,
						},
						Status: resourcesv1alpha1.ManagedResourceStatus{
							ObservedGeneration: 1,
							Conditions: []gardencorev1beta1.Condition{
								{
									Type:   resourcesv1alpha1.ResourcesApplied,
									Status: gardencorev1beta1.ConditionFalse,
								},
								{
									Type:   resourcesv1alpha1.ResourcesHealthy,
									Status: gardencorev1beta1.ConditionFalse,
								},
							},
						},
					})).To(Succeed())

					Expect(ksm.Wait(ctx)).To(MatchError(ContainSubstring("is not healthy")))
				})

				It("should successfully wait for the managed resource to become healthy", func() {
					fakeOps.MaxAttempts = 2

					Expect(c.Create(ctx, &resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Name:       managedResourceName,
							Namespace:  namespace,
							Generation: 1,
						},
						Status: resourcesv1alpha1.ManagedResourceStatus{
							ObservedGeneration: 1,
							Conditions: []gardencorev1beta1.Condition{
								{
									Type:   resourcesv1alpha1.ResourcesApplied,
									Status: gardencorev1beta1.ConditionTrue,
								},
								{
									Type:   resourcesv1alpha1.ResourcesHealthy,
									Status: gardencorev1beta1.ConditionTrue,
								},
							},
						},
					})).To(Succeed())

					Expect(ksm.Wait(ctx)).To(Succeed())
				})
			}

			Context("cluster type seed", func() {
				BeforeEach(func() {
					ksm = New(c, namespace, nil, Values{ClusterType: component.ClusterTypeSeed})
				})

				tests("kube-state-metrics")
			})

			Context("cluster type shoot", func() {
				BeforeEach(func() {
					ksm = New(c, namespace, sm, Values{ClusterType: component.ClusterTypeShoot})
				})

				tests("shoot-core-kube-state-metrics")
			})
		})

		Describe("#WaitCleanup", func() {
			tests := func(managedResourceName string) {
				It("should fail when the wait for the managed resource deletion times out", func() {
					fakeOps.MaxAttempts = 2

					managedResource := &resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Name:      managedResourceName,
							Namespace: namespace,
						},
					}

					Expect(c.Create(ctx, managedResource)).To(Succeed())

					Expect(ksm.WaitCleanup(ctx)).To(MatchError(ContainSubstring("still exists")))
				})

				It("should not return an error when it's already removed", func() {
					Expect(ksm.WaitCleanup(ctx)).To(Succeed())
				})
			}

			Context("cluster type seed", func() {
				BeforeEach(func() {
					ksm = New(c, namespace, nil, Values{ClusterType: component.ClusterTypeSeed})
				})

				tests("kube-state-metrics")
			})

			Context("cluster type shoot", func() {
				BeforeEach(func() {
					ksm = New(c, namespace, sm, Values{ClusterType: component.ClusterTypeShoot})
				})

				tests("shoot-core-kube-state-metrics")
			})
		})
	})
})
