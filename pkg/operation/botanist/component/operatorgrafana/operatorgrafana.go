package operatorgrafana

import (
	"context"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Interface interface {
	component.DeployWaiter
	GrafanaResourceConfigs() component.ResourceConfigs
	// component.MonitoringComponent
}

func New(
	client client.Client,
	namespace string,
	secretsManager secretsmanager.Interface,
	values Values,
) Interface {
	og := &operatorgrafana{
		client:         client,
		namespace:      namespace,
		secretsManager: secretsManager,
		values:         values,
	}
	og.registry = managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer)
	return og
}

type operatorgrafana struct {
	client         client.Client
	namespace      string
	secretsManager secretsmanager.Interface
	values         Values

	registry    *managedresources.Registry
	crdDeployer component.Deployer

	caSecretName                     string
	caBundle                         []byte
	serverSecretName                 string
	genericTokenKubeconfigSecretName *string
}

type Values struct {
	Enabled bool
}

func (og *operatorgrafana) Deploy(ctx context.Context) error {
	var allResources component.ResourceConfigs
	allResources = component.MergeResourceConfigs(allResources, og.GrafanaResourceConfigs())
	if err := component.DeployResourceConfigs(
		ctx,
		og.client,
		og.namespace,
		component.ClusterTypeShoot,
		"operatorgrafana",
		og.registry,
		allResources); err != nil {
		return err
	}
	// TODO add grafana deployment, ingress, network policy, service
	return nil
}
func (og *operatorgrafana) Destroy(ctx context.Context) error {
	return nil
}

func (og *operatorgrafana) Wait(ctx context.Context) error {
	return nil
}

func (og *operatorgrafana) WaitCleanup(ctx context.Context) error {
	return nil
}

func (og *operatorgrafana) emptyDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: og.namespace}}
}

func getAppLabel(appValue string) map[string]string {
	return map[string]string{v1beta1constants.LabelApp: appValue}
}

func getRoleLabel() map[string]string {
	return map[string]string{v1beta1constants.GardenRole: "operatorgrafana"}
}

func getAllLabels(appValue string) map[string]string {
	return utils.MergeStringMaps(getAppLabel(appValue), getRoleLabel())
}
