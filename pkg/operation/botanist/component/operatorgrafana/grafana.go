package operatorgrafana

import (
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func (og *operatorgrafana) grafanaResourceConfigs() component.ResourceConfigs {
	var (
		deployment = og.emptyDeployment("operatorgrafana")
	)

	return component.ResourceConfigs{
		{Obj: deployment, Class: component.Runtime, MutateFn: func() { og.reconcileOperatorGrafanaDeployment(deployment) }},
	}
}

func (og *operatorgrafana) reconcileOperatorGrafanaDeployment(deployment *appsv1.Deployment) {
	deployment.Labels = getAllLabels("operatorgrafana")
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas:             pointer.Int32(1),
		RevisionHistoryLimit: pointer.Int32(2),
		Selector:             &metav1.LabelSelector{MatchLabels: getAppLabel("operatorgrafana")},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: getAllLabels("operatorgrafana"),
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
