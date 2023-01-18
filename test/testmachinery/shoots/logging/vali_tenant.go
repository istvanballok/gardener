// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package logging

import (
	"context"
	"time"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/utils"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/test/framework"
	"github.com/gardener/gardener/test/framework/resources/templates"

	"github.com/onsi/ginkgo/v2"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tenantInitializationTimeout          = 2 * time.Minute
	tenantGetLogsFromLokiTimeout         = 5 * time.Minute
	tenantLoggerDeploymentCleanupTimeout = 5 * time.Minute

	randomLength           = 11
	userLoggerAppLabel     = "kube-logger"
	operatorLoggerAppLabel = "logger"
	tenantDeltaLogsCount   = 0
	tenantLogsCount        = 100
	tenantLogsDuration     = "20s"
)

var (
	userLoggerName     = "kube-apiserver-"
	operatorLoggerName = "logger-"
	valiLabels         = map[string]string{
		"app":  "vali",
		"role": "logging",
	}
)

var _ = ginkgo.Describe("Seed logging testing", func() {

	f := framework.NewShootFramework(nil)

	var (
		plutonoOperatorsIngress client.Object = &networkingv1.Ingress{}
		plutonoUsersIngress     client.Object = &networkingv1.Ingress{}

		shootNamespace           = &corev1.Namespace{}
		shootNamespaceLabelKey   = "gardener.cloud/test"
		shootNamespaceLabelValue = "logging"
	)

	framework.CBeforeEach(func(ctx context.Context) {
		checkRequiredResources(ctx, f.SeedClient)
		// Get shoot namespace name
		shootNamespace.ObjectMeta.Name = f.ShootSeedNamespace()
		// Get the plutono-operators Ingress
		framework.ExpectNoError(f.SeedClient.Client().Get(ctx, types.NamespacedName{Namespace: f.ShootSeedNamespace(), Name: v1beta1constants.DeploymentNameGrafanaOperators}, plutonoOperatorsIngress))
		// Get the plutono-users Ingress
		framework.ExpectNoError(f.SeedClient.Client().Get(ctx, types.NamespacedName{Namespace: f.ShootSeedNamespace(), Name: v1beta1constants.DeploymentNameGrafanaUsers}, plutonoUsersIngress))
		// Set label to the testing namespace
		_, err := controllerutils.GetAndCreateOrMergePatch(ctx, f.SeedClient.Client(), shootNamespace, func() error {
			metav1.SetMetaDataLabel(&shootNamespace.ObjectMeta, shootNamespaceLabelKey, shootNamespaceLabelValue)
			return nil
		})
		framework.ExpectNoError(err)

		// Deploy Loki ValidatingWebhookConfiguration
		validatingWebhookParams := map[string]interface{}{
			"NamespaceLabelKey":   shootNamespaceLabelKey,
			"NamespaceLabelValue": shootNamespaceLabelValue,
			"CABundle": utils.EncodeBase64([]byte(`-----BEGIN CERTIFICATE-----
MIIC+jCCAeKgAwIBAgIUTp3XvhrWOVM8ZGe86YoXMV/UJ7AwDQYJKoZIhvcNAQEL
BQAwFTETMBEGA1UEAxMKa3ViZXJuZXRlczAeFw0xOTAyMjcxNTM0MDBaFw0yNDAy
MjYxNTM0MDBaMBUxEzARBgNVBAMTCmt1YmVybmV0ZXMwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQCyi0QGOcv2bTf3N8OLN97RwsgH6QAr8wSpAOrttBJg
FnfnU2T1RHgxm7qd190WL8DChv0dZf76d6eSQ4ZrjjyArTzufb4DtPwg+VWq7XvF
BNyn+2hf4SySkwd6k7XLhUTRx048IbByC4v+FEvmoLAwrc0d0G14ec6snD+7jO7e
kykQ/NgAOL7P6kDs9z6+bOfgF0nGN+bmeWQqJejR0t+OyQDCx5/FMtUfEVR5QX80
aeefgp3JFZb6fAw9KhLtdRV3FP0tz6hS+e4Sg0mwAAOqijZsV87kP5GYzjtcfA12
lDYl/nb1GtVvvkQD49VnV7mDnl6mG3LCMNCNH6WlZNv3AgMBAAGjQjBAMA4GA1Ud
DwEB/wQEAwIBBjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBSFA3LvJM21d8qs
ZVVCe6RrTT9wiTANBgkqhkiG9w0BAQsFAAOCAQEAns/EJ3yKsjtISoteQ714r2Um
BMPyUYTTdRHD8LZMd3RykvsacF2l2y88Nz6wJcAuoUj1h8aBDP5oXVtNmFT9jybS
TXrRvWi+aeYdb556nEA5/a94e+cb+Ck3qy/1xgQoS457AZQODisDZNBYWkAFs2Lc
ucpcAtXJtIthVm7FjoAHYcsrY04yAiYEJLD02TjUDXg4iGOGMkVHdmhawBDBF3Aj
esfcqFwji6JyAKFRACPowykQONFwUSom89uYESSCJFvNCk9MJmjJ2PzDUt6CypR4
epFdd1fXLwuwn7fvPMmJqD3HtLalX1AZmPk+BI8ezfAiVcVqnTJQMXlYPpYe9A==
-----END CERTIFICATE-----`)),
		}
		err = f.RenderAndDeployTemplate(ctx, f.SeedClient, templates.BlockLokiValidatingWebhookConfiguration, validatingWebhookParams)
		framework.ExpectNoError(err)
	}, tenantInitializationTimeout)

	f.Beta().CIt("should get container logs from vali by tenant", func(ctx context.Context) {
		userLoggerName = userLoggerName + utilrand.String(randomLength)
		userLoggerRegex := userLoggerName + "-.*"

		operatorLoggerName = operatorLoggerName + utilrand.String(randomLength)
		operatorLoggerRegex := operatorLoggerName + "-.*"

		ginkgo.By("Get Loki tenant IDs")
		userID := getXScopeOrgID(plutonoUsersIngress.GetAnnotations())
		operatorID := getXScopeOrgID(plutonoOperatorsIngress.GetAnnotations())

		ginkgo.By("Wait until Loki StatefulSet is ready")
		framework.ExpectNoError(f.WaitUntilStatefulSetIsRunning(ctx, valiName, f.ShootSeedNamespace(), f.SeedClient))

		ginkgo.By("Compute expected logs for the user tenant")
		search, err := f.GetLokiLogs(ctx, valiLabels, userID, f.ShootSeedNamespace(), "pod_name", userLoggerRegex, f.SeedClient)
		framework.ExpectNoError(err)
		initialUsersLogs, err := getLogCountFromResult(search)
		framework.ExpectNoError(err)
		expectedUserLogs := tenantLogsCount + initialUsersLogs

		ginkgo.By("Compute expected logs for the operator tenant")
		search, err = f.GetLokiLogs(ctx, valiLabels, operatorID, f.ShootSeedNamespace(), "pod_name", operatorLoggerRegex, f.SeedClient)
		framework.ExpectNoError(err)
		initialOperatorsLogs, err := getLogCountFromResult(search)
		framework.ExpectNoError(err)
		expectedOperatorLogs := tenantLogsCount + initialOperatorsLogs

		ginkgo.By("Deploy the user logger application")
		loggerParams := map[string]interface{}{
			"LoggerName":          userLoggerName,
			"HelmDeployNamespace": f.ShootSeedNamespace(),
			"AppLabel":            userLoggerAppLabel,
			"LogsCount":           tenantLogsCount,
			"LogsDuration":        tenantLogsDuration,
		}

		ginkgo.By("Check again if Loki StatefulSet is ready")
		framework.ExpectNoError(f.WaitUntilStatefulSetIsRunning(ctx, valiName, f.ShootSeedNamespace(), f.SeedClient))

		err = f.RenderAndDeployTemplate(ctx, f.SeedClient, templates.LoggerAppName, loggerParams)
		framework.ExpectNoError(err)

		ginkgo.By("Deploy the operator logger application")
		loggerParams["LoggerName"] = operatorLoggerName
		loggerParams["AppLabel"] = operatorLoggerAppLabel

		err = f.RenderAndDeployTemplate(ctx, f.SeedClient, templates.LoggerAppName, loggerParams)
		framework.ExpectNoError(err)

		ginkgo.By("Wait until user logger application is ready")
		loggerLabels := labels.SelectorFromSet(map[string]string{
			"app": userLoggerAppLabel,
		})

		err = f.WaitUntilDeploymentsWithLabelsIsReady(ctx, loggerLabels, f.ShootSeedNamespace(), f.SeedClient)
		framework.ExpectNoError(err)

		ginkgo.By("Wait until operator logger application is ready")
		loggerLabels = labels.SelectorFromSet(map[string]string{
			"app": operatorLoggerAppLabel,
		})

		err = f.WaitUntilDeploymentsWithLabelsIsReady(ctx, loggerLabels, f.ShootSeedNamespace(), f.SeedClient)
		framework.ExpectNoError(err)

		ginkgo.By("Verify vali received all user logger application logs")
		err = WaitUntilLokiReceivesLogs(ctx, 30*time.Second, f, valiLabels, userID, f.ShootSeedNamespace(), "pod_name", userLoggerRegex, expectedUserLogs, tenantDeltaLogsCount, f.SeedClient)
		framework.ExpectNoError(err)

		ginkgo.By("Verify vali received all user logger application logs as an operator")
		err = WaitUntilLokiReceivesLogs(ctx, 30*time.Second, f, valiLabels, operatorID, f.ShootSeedNamespace(), "pod_name", userLoggerRegex, expectedUserLogs, tenantDeltaLogsCount, f.SeedClient)
		framework.ExpectNoError(err)

		ginkgo.By("Verify vali received all operator logger application logs")
		err = WaitUntilLokiReceivesLogs(ctx, 30*time.Second, f, valiLabels, operatorID, f.ShootSeedNamespace(), "pod_name", operatorLoggerRegex, expectedOperatorLogs, tenantDeltaLogsCount, f.SeedClient)
		framework.ExpectNoError(err)

		ginkgo.By("Verify that vali will not show the operator logs to the user tenant")
		err = WaitUntilLokiReceivesLogs(ctx, 30*time.Second, f, valiLabels, userID, f.ShootSeedNamespace(), "pod_name", operatorLoggerRegex, 0, tenantDeltaLogsCount, f.SeedClient)
		framework.ExpectNoError(err)

	}, tenantGetLogsFromLokiTimeout, framework.WithCAfterTest(func(ctx context.Context) {
		ginkgo.By("Cleanup user logger app resources")
		loggerDeploymentToDelete := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: f.ShootSeedNamespace(),
				Name:      userLoggerName,
			},
		}
		err := kubernetesutils.DeleteObject(ctx, f.SeedClient.Client(), loggerDeploymentToDelete)
		framework.ExpectNoError(err)

		ginkgo.By("Cleanup operator logger app resources")
		loggerDeploymentToDelete = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: f.ShootSeedNamespace(),
				Name:      operatorLoggerName,
			},
		}
		err = kubernetesutils.DeleteObject(ctx, f.SeedClient.Client(), loggerDeploymentToDelete)
		framework.ExpectNoError(err)

		ginkgo.By("Cleanup vali's MutatingWebhook and the additional label")
		webhookToDelete := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "block-vali-updates",
			},
		}
		err = kubernetesutils.DeleteObject(ctx, f.SeedClient.Client(), webhookToDelete)
		framework.ExpectNoError(err)

		_, err = controllerutils.GetAndCreateOrMergePatch(ctx, f.SeedClient.Client(), shootNamespace, func() error {
			delete(shootNamespace.Labels, shootNamespaceLabelKey)
			return nil
		})
		framework.ExpectNoError(err)

	}, tenantLoggerDeploymentCleanupTimeout))
})
