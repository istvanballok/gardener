package operatorgrafana

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Interface interface {
	component.DeployWaiter
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
	fmt.Println("Calling deploy on the new operator grafana component")
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
