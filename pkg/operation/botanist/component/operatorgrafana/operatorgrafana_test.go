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

package operatorgrafana_test

import (
	"context"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/gardener/gardener/pkg/operation/botanist/component/operatorgrafana"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	"github.com/gardener/gardener/pkg/operation/botanist/component"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Operator Grafana", func() {
	var (
		ctx       = context.TODO()
		namespace = "some-namespace"
		values    = Values{
			Enabled: true,
		}

		c  client.Client
		sm secretsmanager.Interface
		og component.DeployWaiter
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		sm = fakesecretsmanager.New(c, namespace)
		og = New(c, namespace, sm, values)
	})

	Describe("Deploy", func() {
		It("should successfully deploy all resources", func() {
			Expect(og.Deploy(ctx)).To(Succeed())
			// TODO call resourcemanager reconcile

			secrets := corev1.SecretList{}
			c.List(ctx, &secrets, client.InNamespace(corev1.NamespaceAll))

			GinkgoWriter.Println(serialize(secrets.Items))

			Expect(secrets).To(Equal(corev1.SecretList{}))
		})
	})
})

func serialize(objs []corev1.Secret) string {
	var (
		scheme        = kubernetes.SeedScheme
		groupVersions []schema.GroupVersion
	)

	for k := range scheme.AllKnownTypes() {
		groupVersions = append(groupVersions, k.GroupVersion())
	}

	var (
		ser   = json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, json.SerializerOptions{Yaml: true, Strict: false})
		codec = serializer.NewCodecFactory(scheme).CodecForVersions(ser, ser, schema.GroupVersions(groupVersions), schema.GroupVersions(groupVersions))
	)

	result := ""
	for _, obj := range objs {
		for k := range obj.Data {
			obj.Data[k] = []byte(".")
		}
		serializationYAML, err := runtime.Encode(codec, &obj)
		result += string(serializationYAML)
		result += "---\n"
		Expect(err).NotTo(HaveOccurred())
	}

	return string(result)
}
