// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubestatemetrics

import (
	"fmt"
	"strings"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
	kubeapiserverconstants "github.com/gardener/gardener/pkg/component/kubernetes/apiserver/constants"
	"github.com/gardener/gardener/pkg/component/observability/monitoring/prometheus/cache"
	"github.com/gardener/gardener/pkg/component/observability/monitoring/prometheus/garden"
	"github.com/gardener/gardener/pkg/component/observability/monitoring/prometheus/seed"
	"github.com/gardener/gardener/pkg/component/observability/monitoring/prometheus/shoot"
	monitoringutils "github.com/gardener/gardener/pkg/component/observability/monitoring/utils"
	"github.com/gardener/gardener/pkg/utils"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
)

func (k *kubeStateMetrics) getResourceConfigs(genericTokenKubeconfigSecretName string, shootAccessSecret *gardenerutils.AccessSecret, customResourceStateConfig string) component.ResourceConfigs {
	var (
		clusterRole                  = k.emptyClusterRole()
		clusterRoleBinding           = k.emptyClusterRoleBinding()
		service                      = k.emptyService()
		deployment                   = k.emptyDeployment()
		vpa                          = k.emptyVerticalPodAutoscaler()
		pdb                          = k.emptyPodDisruptionBudget()
		scrapeConfigCache            = k.emptyScrapeConfigCache()
		scrapeConfigSeed             = k.emptyScrapeConfigSeed()
		scrapeConfigGarden           = k.emptyScrapeConfigGarden()
		scrapeConfigShoot            = k.emptyScrapeConfigShoot()
		prometheusRuleShoot          = k.emptyPrometheusRuleShoot()
		customResourceStateConfigMap = k.emptyCustomResourceStateConfigMap()

		configs = component.ResourceConfigs{
			{Obj: clusterRole, Class: component.Application, MutateFn: func() { k.reconcileClusterRole(clusterRole) }},
			{Obj: service, Class: component.Runtime, MutateFn: func() { k.reconcileService(service) }},
			{Obj: vpa, Class: component.Runtime, MutateFn: func() { k.reconcileVerticalPodAutoscaler(vpa, deployment) }},
			{Obj: customResourceStateConfigMap, Class: component.Runtime, MutateFn: func() {
				k.reconcileCustomResourceStateConfigMap(customResourceStateConfigMap, customResourceStateConfig)
			}},
		}
	)

	if k.values.ClusterType == component.ClusterTypeSeed {
		serviceAccount := k.emptyServiceAccount()

		configs = append(configs,
			component.ResourceConfig{Obj: serviceAccount, Class: component.Runtime, MutateFn: func() { k.reconcileServiceAccount(serviceAccount) }},
			component.ResourceConfig{Obj: clusterRoleBinding, Class: component.Application, MutateFn: func() { k.reconcileClusterRoleBinding(clusterRoleBinding, clusterRole, serviceAccount) }},
			component.ResourceConfig{Obj: deployment, Class: component.Runtime, MutateFn: func() { k.reconcileDeployment(deployment, serviceAccount, "", nil) }},
			component.ResourceConfig{Obj: pdb, Class: component.Runtime, MutateFn: func() { k.reconcilePodDisruptionBudget(pdb, deployment) }},
			component.ResourceConfig{Obj: scrapeConfigCache, Class: component.Runtime, MutateFn: func() { k.reconcileScrapeConfigCache(scrapeConfigCache) }},
			component.ResourceConfig{Obj: scrapeConfigSeed, Class: component.Runtime, MutateFn: func() { k.reconcileScrapeConfigSeed(scrapeConfigSeed) }},
			component.ResourceConfig{Obj: scrapeConfigGarden, Class: component.Runtime, MutateFn: func() { k.reconcileScrapeConfigGarden(scrapeConfigGarden) }},
		)
	}

	if k.values.ClusterType == component.ClusterTypeShoot {
		configs = append(configs,
			component.ResourceConfig{Obj: clusterRoleBinding, Class: component.Application, MutateFn: func() {
				k.reconcileClusterRoleBinding(clusterRoleBinding, clusterRole, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: shootAccessSecret.ServiceAccountName, Namespace: metav1.NamespaceSystem}})
			}},
			component.ResourceConfig{Obj: deployment, Class: component.Runtime, MutateFn: func() { k.reconcileDeployment(deployment, nil, genericTokenKubeconfigSecretName, shootAccessSecret) }},
			component.ResourceConfig{Obj: prometheusRuleShoot, Class: component.Runtime, MutateFn: func() { k.reconcilePrometheusRuleShoot(prometheusRuleShoot) }},
		)

		if !k.values.IsWorkerless {
			configs = append(configs, component.ResourceConfig{Obj: scrapeConfigShoot, Class: component.Runtime, MutateFn: func() { k.reconcileScrapeConfigShoot(scrapeConfigShoot) }})
		}
	}

	return configs
}

func (k *kubeStateMetrics) emptyServiceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics", Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcileServiceAccount(serviceAccount *corev1.ServiceAccount) {
	serviceAccount.Labels = k.getLabels()
	serviceAccount.AutomountServiceAccountToken = ptr.To(false)
}

func (k *kubeStateMetrics) newShootAccessSecret() *gardenerutils.AccessSecret {
	return gardenerutils.NewShootAccessSecret(v1beta1constants.DeploymentNameKubeStateMetrics, k.namespace)
}

func (k *kubeStateMetrics) clusterRole() *rbacv1.ClusterRole {
	clusterRole := rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "gardener.cloud:monitoring:" + k.nameSuffix()}}
	clusterRole.Labels = k.getLabels()
	clusterRole.Rules = []rbacv1.PolicyRule{
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
	}

	if k.values.ClusterType == component.ClusterTypeSeed {
		clusterRole.Rules = append(clusterRole.Rules, rbacv1.PolicyRule{
			APIGroups: []string{"autoscaling"},
			Resources: []string{"horizontalpodautoscalers"},
			Verbs:     []string{"list", "watch"},
		})
	}
	return &clusterRole
}

func (k *kubeStateMetrics) emptyClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "gardener.cloud:monitoring:" + k.nameSuffix()}}
}

func (k *kubeStateMetrics) reconcileClusterRole(clusterRole *rbacv1.ClusterRole) {
	clusterRole.Labels = k.getLabels()
	clusterRole.Rules = []rbacv1.PolicyRule{
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
	}

	if k.values.ClusterType == component.ClusterTypeSeed {
		clusterRole.Rules = append(clusterRole.Rules, rbacv1.PolicyRule{
			APIGroups: []string{"autoscaling"},
			Resources: []string{"horizontalpodautoscalers"},
			Verbs:     []string{"list", "watch"},
		})
	}
}

func (k *kubeStateMetrics) emptyClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "gardener.cloud:monitoring:" + k.nameSuffix()}}
}

func (k *kubeStateMetrics) reconcileClusterRoleBinding(clusterRoleBinding *rbacv1.ClusterRoleBinding, clusterRole *rbacv1.ClusterRole, serviceAccount *corev1.ServiceAccount) {
	clusterRoleBinding.Labels = k.getLabels()
	clusterRoleBinding.Annotations = map[string]string{resourcesv1alpha1.DeleteOnInvalidUpdate: "true"}
	clusterRoleBinding.RoleRef = rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "ClusterRole",
		Name:     clusterRole.Name,
	}
	clusterRoleBinding.Subjects = []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      serviceAccount.Name,
		Namespace: serviceAccount.Namespace,
	}}
}

func (k *kubeStateMetrics) emptyService() *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics", Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcileService(service *corev1.Service) {
	service.Labels = k.getLabels()

	metricsPort := networkingv1.NetworkPolicyPort{
		Port:     ptr.To(intstr.FromInt32(port)),
		Protocol: ptr.To(corev1.ProtocolTCP),
	}

	switch k.values.ClusterType {
	case component.ClusterTypeSeed:
		utilruntime.Must(gardenerutils.InjectNetworkPolicyAnnotationsForGardenScrapeTargets(service, metricsPort))
		utilruntime.Must(gardenerutils.InjectNetworkPolicyAnnotationsForSeedScrapeTargets(service, metricsPort))
	case component.ClusterTypeShoot:
		utilruntime.Must(gardenerutils.InjectNetworkPolicyAnnotationsForScrapeTargets(service, metricsPort))
	}

	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = k.getLabels()
	service.Spec.Ports = kubernetesutils.ReconcileServicePorts(service.Spec.Ports, []corev1.ServicePort{
		{
			Name:       portNameMetrics,
			Port:       80,
			TargetPort: intstr.FromInt32(port),
			Protocol:   corev1.ProtocolTCP,
		},
	}, corev1.ServiceTypeClusterIP)
}

func (k *kubeStateMetrics) emptyDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics", Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcileDeployment(
	deployment *appsv1.Deployment,
	serviceAccount *corev1.ServiceAccount,
	genericTokenKubeconfigSecretName string,
	shootAccessSecret *gardenerutils.AccessSecret,
) {
	var (
		maxUnavailable = intstr.FromInt32(1)

		deploymentLabels = k.getLabels()
		podLabels        = map[string]string{
			v1beta1constants.LabelNetworkPolicyToDNS: v1beta1constants.LabelNetworkPolicyAllowed,
		}
		args = []string{
			fmt.Sprintf("--port=%d", port),
			"--telemetry-port=8081",
		}
	)

	customResourceStateConfigFile := customResourceStateConfigMountDir + "/" + customResourceStateConfigMountFile

	if k.values.ClusterType == component.ClusterTypeSeed {
		deploymentLabels[v1beta1constants.LabelRole] = v1beta1constants.LabelMonitoring
		podLabels = utils.MergeStringMaps(podLabels, deploymentLabels, map[string]string{
			v1beta1constants.LabelNetworkPolicyToRuntimeAPIServer: v1beta1constants.LabelNetworkPolicyAllowed,
		})

		args = append(args,
			"--resources=deployments,pods,statefulsets,nodes,horizontalpodautoscalers,persistentvolumeclaims,replicasets,namespaces",
			"--metric-labels-allowlist=nodes=[*],pods=[origin]",
			"--metric-annotations-allowlist=namespaces=[shoot.gardener.cloud/uid]",
			"--metric-allowlist="+strings.Join(cacheAllowedMetrics, ","),
			"--custom-resource-state-config-file="+customResourceStateConfigFile,
		)
	}

	if k.values.ClusterType == component.ClusterTypeShoot {
		deploymentLabels[v1beta1constants.GardenRole] = v1beta1constants.LabelMonitoring
		podLabels = utils.MergeStringMaps(podLabels, deploymentLabels, map[string]string{
			gardenerutils.NetworkPolicyLabel(v1beta1constants.DeploymentNameKubeAPIServer, kubeapiserverconstants.Port): v1beta1constants.LabelNetworkPolicyAllowed,
		})
		args = append(args,
			"--resources=daemonsets,deployments,nodes,pods,statefulsets,replicasets",
			"--namespaces="+metav1.NamespaceSystem,
			"--kubeconfig="+gardenerutils.PathGenericKubeconfig,
			"--metric-labels-allowlist=nodes=[*],pods=[origin]",
			"--metric-allowlist="+strings.Join(shootAllowedMetrics, ","),
			"--custom-resource-state-config-file="+customResourceStateConfigFile,
		)
	}

	deployment.Labels = deploymentLabels
	deployment.Spec.Replicas = &k.values.Replicas
	deployment.Spec.RevisionHistoryLimit = ptr.To[int32](2)
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: k.getLabels()}
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &maxUnavailable,
		},
	}
	deployment.Spec.Template = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: podLabels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:            containerName,
				Image:           k.values.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            args,
				Ports: []corev1.ContainerPort{{
					Name:          "metrics",
					ContainerPort: port,
					Protocol:      corev1.ProtocolTCP,
				}},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt32(port),
						},
					},
					InitialDelaySeconds: 5,
					TimeoutSeconds:      5,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt32(port),
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
				VolumeMounts: []corev1.VolumeMount{{
					Name:      customResourceStateConfigMapName,
					MountPath: customResourceStateConfigMountDir,
					ReadOnly:  true,
				}},
			}},
			PriorityClassName: k.values.PriorityClassName,
			Volumes: []corev1.Volume{{
				Name: customResourceStateConfigMapName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: customResourceStateConfigMapName,
						},
					},
				},
			}},
		},
	}

	if k.values.ClusterType == component.ClusterTypeSeed {
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccount.Name
	}
	if k.values.ClusterType == component.ClusterTypeShoot {
		deployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
		utilruntime.Must(gardenerutils.InjectGenericKubeconfig(deployment, genericTokenKubeconfigSecretName, shootAccessSecret.Secret.Name))
	}
}

func (k *kubeStateMetrics) emptyVerticalPodAutoscaler() *vpaautoscalingv1.VerticalPodAutoscaler {
	return &vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics-vpa", Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcileVerticalPodAutoscaler(vpa *vpaautoscalingv1.VerticalPodAutoscaler, deployment *appsv1.Deployment) {
	var (
		updateMode       = vpaautoscalingv1.UpdateModeAuto
		controlledValues = vpaautoscalingv1.ContainerControlledValuesRequestsOnly
	)

	vpa.Spec = vpaautoscalingv1.VerticalPodAutoscalerSpec{
		TargetRef: &autoscalingv1.CrossVersionObjectReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       deployment.Name,
		},
		UpdatePolicy: &vpaautoscalingv1.PodUpdatePolicy{UpdateMode: &updateMode},
		ResourcePolicy: &vpaautoscalingv1.PodResourcePolicy{
			ContainerPolicies: []vpaautoscalingv1.ContainerResourcePolicy{
				{
					ContainerName:    "*",
					ControlledValues: &controlledValues,
					MinAllowed: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
			},
		},
	}
}

func (k *kubeStateMetrics) emptyPodDisruptionBudget() *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics-pdb", Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcilePodDisruptionBudget(podDisruptionBudget *policyv1.PodDisruptionBudget, deployment *appsv1.Deployment) {
	podDisruptionBudget.Labels = k.getLabels()
	podDisruptionBudget.Spec = policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: ptr.To(intstr.FromInt32(1)),
		Selector:       deployment.Spec.Selector,
	}

	kubernetesutils.SetAlwaysAllowEviction(podDisruptionBudget, k.values.KubernetesVersion)
}

func (k *kubeStateMetrics) standardScrapeConfigSpec() monitoringv1alpha1.ScrapeConfigSpec {
	return monitoringv1alpha1.ScrapeConfigSpec{
		KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
			// Service is used, because we only care about metric from one kube-state-metrics instance and not multiple
			// in HA setup.
			Role:       "service",
			Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{k.namespace}},
		}},
		RelabelConfigs: []monitoringv1.RelabelConfig{
			{
				SourceLabels: []monitoringv1.LabelName{
					"__meta_kubernetes_service_label_" + labelKeyComponent,
					"__meta_kubernetes_service_port_name",
				},
				Regex:  labelValueComponent + ";" + portNameMetrics,
				Action: "keep",
			},
			{
				SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_service_label_" + labelKeyType},
				Regex:        `(.+)`,
				Replacement:  ptr.To(`${1}`),
				TargetLabel:  labelKeyType,
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
			SourceLabels: []monitoringv1.LabelName{"pod"},
			Regex:        `^.+\.tf-pod.+$`,
			Action:       "drop",
		}},
	}
}

var shootAllowedMetrics = []string{
	"^kube_daemonset_metadata_generation$",
	"^kube_daemonset_status_current_number_scheduled$",
	"^kube_daemonset_status_desired_number_scheduled$",
	"^kube_daemonset_status_number_available$",
	"^kube_daemonset_status_number_unavailable$",
	"^kube_daemonset_status_updated_number_scheduled$",
	"^kube_deployment_metadata_generation$",
	"^kube_deployment_spec_replicas$",
	"^kube_deployment_status_observed_generation$",
	"^kube_deployment_status_replicas$",
	"^kube_deployment_status_replicas_available$",
	"^kube_deployment_status_replicas_unavailable$",
	"^kube_deployment_status_replicas_updated$",
	"^kube_node_info$",
	"^kube_node_labels$",
	"^kube_node_spec_taint$",
	"^kube_node_spec_unschedulable$",
	"^kube_node_status_allocatable$",
	"^kube_node_status_capacity$",
	"^kube_node_status_condition$",
	"^kube_pod_container_info$",
	"^kube_pod_container_resource_limits$",
	"^kube_pod_container_resource_requests$",
	"^kube_pod_container_status_restarts_total$",
	"^kube_pod_info$",
	"^kube_pod_labels$",
	"^kube_pod_status_phase$",
	"^kube_pod_status_ready$",
	"^kube_replicaset_owner$",
	"^kube_replicaset_metadata_generation$",
	"^kube_replicaset_spec_replicas$",
	"^kube_replicaset_status_observed_generation$",
	"^kube_replicaset_status_replicas$",
	"^kube_replicaset_status_ready_replicas$",
	"^kube_statefulset_metadata_generation$",
	"^kube_statefulset_replicas$",
	"^kube_statefulset_status_observed_generation$",
	"^kube_statefulset_status_replicas$",
	"^kube_statefulset_status_replicas_current$",
	"^kube_statefulset_status_replicas_ready$",
	"^kube_statefulset_status_replicas_updated$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_uncappedtarget_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_uncappedtarget_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_cpu$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_cpu$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_updatepolicy_updatemode$",
}

var cacheAllowedMetrics = []string{
	"^kube_daemonset_metadata_generation$",
	"^kube_daemonset_status_current_number_scheduled$",
	"^kube_daemonset_status_desired_number_scheduled$",
	"^kube_daemonset_status_number_available$",
	"^kube_daemonset_status_number_unavailable$",
	"^kube_daemonset_status_updated_number_scheduled$",
	"^kube_deployment_metadata_generation$",
	"^kube_deployment_spec_replicas$",
	"^kube_deployment_status_observed_generation$",
	"^kube_deployment_status_replicas$",
	"^kube_deployment_status_replicas_available$",
	"^kube_deployment_status_replicas_unavailable$",
	"^kube_deployment_status_replicas_updated$",
	"^kube_horizontalpodautoscaler_spec_max_replicas$",
	"^kube_horizontalpodautoscaler_spec_min_replicas$",
	"^kube_horizontalpodautoscaler_status_current_replicas$",
	"^kube_horizontalpodautoscaler_status_desired_replicas$",
	"^kube_horizontalpodautoscaler_status_condition$",
	"^kube_namespace_annotations$",
	"^kube_node_info$",
	"^kube_node_labels$",
	"^kube_node_spec_taint$",
	"^kube_node_spec_unschedulable$",
	"^kube_node_status_allocatable$",
	"^kube_node_status_capacity$",
	"^kube_node_status_condition$",
	"^kube_persistentvolumeclaim_resource_requests_storage_bytes$",
	"^kube_pod_container_info$",
	"^kube_pod_container_resource_limits$",
	"^kube_pod_container_resource_requests$",
	"^kube_pod_container_status_restarts_total$",
	"^kube_pod_info$",
	"^kube_pod_labels$",
	"^kube_pod_owner$",
	"^kube_pod_spec_volumes_persistentvolumeclaims_info$",
	"^kube_pod_status_phase$",
	"^kube_pod_status_ready$",
	"^kube_replicaset_owner$",
	"^kube_statefulset_metadata_generation$",
	"^kube_statefulset_replicas$",
	"^kube_statefulset_status_observed_generation$",
	"^kube_statefulset_status_replicas$",
	"^kube_statefulset_status_replicas_current$",
	"^kube_statefulset_status_replicas_ready$",
	"^kube_statefulset_status_replicas_updated$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_target_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_uncappedtarget_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_uncappedtarget_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound_memory$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_cpu$",
	"^kube_customresource_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_cpu$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_minallowed_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_cpu$",
	"^kube_customresource_verticalpodautoscaler_spec_resourcepolicy_containerpolicies_maxallowed_memory$",
	"^kube_customresource_verticalpodautoscaler_spec_updatepolicy_updatemode$",
}

func (k *kubeStateMetrics) emptyScrapeConfigCache() *monitoringv1alpha1.ScrapeConfig {
	return &monitoringv1alpha1.ScrapeConfig{ObjectMeta: monitoringutils.ConfigObjectMeta("kube-state-metrics", k.namespace, cache.Label)}
}

func (k *kubeStateMetrics) reconcileScrapeConfigCache(scrapeConfig *monitoringv1alpha1.ScrapeConfig) {
	scrapeConfig.Labels = monitoringutils.Labels(cache.Label)
	scrapeConfig.Spec = k.standardScrapeConfigSpec()
}

func (k *kubeStateMetrics) emptyScrapeConfigSeed() *monitoringv1alpha1.ScrapeConfig {
	return &monitoringv1alpha1.ScrapeConfig{ObjectMeta: monitoringutils.ConfigObjectMeta("kube-state-metrics", k.namespace, seed.Label)}
}

func (k *kubeStateMetrics) reconcileScrapeConfigSeed(scrapeConfig *monitoringv1alpha1.ScrapeConfig) {
	scrapeConfig.Labels = monitoringutils.Labels(seed.Label)
	scrapeConfig.Spec = monitoringv1alpha1.ScrapeConfigSpec{
		KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
			Role:       "service",
			Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{k.namespace}},
		}},
		RelabelConfigs: []monitoringv1.RelabelConfig{
			{
				SourceLabels: []monitoringv1.LabelName{
					"__meta_kubernetes_service_label_component",
					"__meta_kubernetes_service_port_name",
				},
				Regex:  "kube-state-metrics;" + portNameMetrics,
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
	}
}

func (k *kubeStateMetrics) emptyScrapeConfigGarden() *monitoringv1alpha1.ScrapeConfig {
	return &monitoringv1alpha1.ScrapeConfig{ObjectMeta: monitoringutils.ConfigObjectMeta("kube-state-metrics", k.namespace, garden.Label)}
}

func (k *kubeStateMetrics) reconcileScrapeConfigGarden(scrapeConfig *monitoringv1alpha1.ScrapeConfig) {
	scrapeConfig.Labels = monitoringutils.Labels(garden.Label)
	scrapeConfig.Spec = monitoringv1alpha1.ScrapeConfigSpec{
		KubernetesSDConfigs: []monitoringv1alpha1.KubernetesSDConfig{{
			Role:       "service",
			Namespaces: &monitoringv1alpha1.NamespaceDiscovery{Names: []string{k.namespace}},
		}},
		RelabelConfigs: []monitoringv1.RelabelConfig{
			{
				SourceLabels: []monitoringv1.LabelName{
					"__meta_kubernetes_service_label_component",
					"__meta_kubernetes_service_port_name",
				},
				Regex:  "kube-state-metrics;" + portNameMetrics,
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
		MetricRelabelConfigs: append([]monitoringv1.RelabelConfig{
			{
				SourceLabels: []monitoringv1.LabelName{"pod"},
				Regex:        `^.+\.tf-pod.+$`,
				Action:       "drop",
			},
			{
				SourceLabels: []monitoringv1.LabelName{"namespace"},
				Regex:        v1beta1constants.GardenNamespace,
				Action:       "drop",
			},
		}, monitoringutils.StandardMetricRelabelConfig(
			"kube_pod_container_status_restarts_total",
			"kube_pod_status_phase",
		)...),
	}
}

func (k *kubeStateMetrics) emptyScrapeConfigShoot() *monitoringv1alpha1.ScrapeConfig {
	return &monitoringv1alpha1.ScrapeConfig{ObjectMeta: monitoringutils.ConfigObjectMeta("kube-state-metrics", k.namespace, shoot.Label)}
}

func (k *kubeStateMetrics) reconcileScrapeConfigShoot(scrapeConfig *monitoringv1alpha1.ScrapeConfig) {
	scrapeConfig.Labels = monitoringutils.Labels(shoot.Label)
	scrapeConfig.Spec = k.standardScrapeConfigSpec()
}

func (k *kubeStateMetrics) emptyPrometheusRuleShoot() *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{ObjectMeta: monitoringutils.ConfigObjectMeta("kube-state-metrics", k.namespace, shoot.Label)}
}

func (k *kubeStateMetrics) reconcilePrometheusRuleShoot(prometheusRule *monitoringv1.PrometheusRule) {
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

	if !k.values.IsWorkerless {
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
				For:   ptr.To(monitoringv1.Duration("25m")), // MCM timeout + grace period to allow self-healing before firing alert
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
			// Mitigation for extension dashboards.
			// TODO(istvanballok): Remove in a future version. For more details, see https://github.com/gardener/gardener/pull/6224.
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

	prometheusRule.Labels = monitoringutils.Labels(shoot.Label)
	prometheusRule.Spec = monitoringv1.PrometheusRuleSpec{
		Groups: []monitoringv1.RuleGroup{{
			Name:  "kube-state-metrics.rules",
			Rules: rules,
		}},
	}
}

func (k *kubeStateMetrics) getLabels() map[string]string {
	t := "seed"
	if k.values.ClusterType == component.ClusterTypeShoot {
		t = "shoot"
	}

	return map[string]string{
		labelKeyComponent: labelValueComponent,
		labelKeyType:      t,
	}
}

func (k *kubeStateMetrics) nameSuffix() string {
	suffix := "kube-state-metrics"
	if k.values.ClusterType == component.ClusterTypeShoot {
		return suffix
	}
	return suffix + "-seed"
}

func (k *kubeStateMetrics) customResourceStateConfigMap(customResourceStateConfig string) *corev1.ConfigMap {
	cm := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: customResourceStateConfigMapName, Namespace: k.namespace}}
	cm.Data = map[string]string{
		customResourceStateConfigMountFile: customResourceStateConfig,
	}
	kubernetesutils.MakeUnique(&cm)
	return &cm
}

func (k *kubeStateMetrics) emptyCustomResourceStateConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: customResourceStateConfigMapName, Namespace: k.namespace}}
}

func (k *kubeStateMetrics) reconcileCustomResourceStateConfigMap(cm *corev1.ConfigMap, customResourceStateConfig string) {
	cm.Data = map[string]string{
		customResourceStateConfigMountFile: customResourceStateConfig,
	}
}
