package botanist

import (
	"context"

	"github.com/gardener/gardener/pkg/operation/botanist/component/operatorgrafana"
)

func (b *Botanist) DefaultOperatorGrafana() (operatorgrafana.Interface, error) {
	return operatorgrafana.New(
		b.K8sSeedClient.Client(),
		b.Shoot.SeedNamespace,
		b.SecretsManager,
		operatorgrafana.Values{
			Enabled: true,
		},
	), nil
}

func (b *Botanist) DeployOperatorGrafana(ctx context.Context) error {
	return b.Shoot.Components.ControlPlane.OperatorGrafana.Deploy(ctx)
}
