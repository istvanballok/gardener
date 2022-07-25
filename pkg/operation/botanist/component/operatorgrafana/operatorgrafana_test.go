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
	"fmt"
	"os"
	"strings"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	. "github.com/gardener/gardener/pkg/operation/botanist/component/operatorgrafana"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/managedresource"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		r  reconcile.Reconciler
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(kubernetes.SeedScheme).Build()
		sm = fakesecretsmanager.New(c, namespace)
		og = New(c, namespace, sm, values)
		restMapper := meta.NewDefaultRESTMapper(nil)
		restMapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
		r = managedresource.New(c, c, restMapper, kubernetes.SeedScheme)
	})

	Describe("Deploy", func() {
		It("should successfully deploy all resources", func() {
			Expect(og.Deploy(ctx)).To(Succeed())
			letResourceManagerReconcile(ctx, r)
			assertClientState(ctx, c, "operatorgrafana_test_1.yaml")
		})
	})
})

func letResourceManagerReconcile(ctx context.Context, r reconcile.Reconciler) {
	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "some-namespace",
			Name:      "operatorgrafana"}})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

// Assert client state
func assertClientState(ctx context.Context, c client.Client, filename string) {
	secrets := corev1.SecretList{}
	c.List(ctx, &secrets, client.InNamespace(corev1.NamespaceAll))
	managedResources := resourcesv1alpha1.ManagedResourceList{}
	c.List(ctx, &managedResources, client.InNamespace(corev1.NamespaceAll))
	deployments := appsv1.DeploymentList{}
	c.List(ctx, &deployments, client.InNamespace(corev1.NamespaceAll))
	items := []interface{}{}
	for _, secret := range secrets.Items {
		items = append(items, &secret)
	}
	for _, managedResource := range managedResources.Items {
		items = append(items, &managedResource)
	}
	for _, deployment := range deployments.Items {
		items = append(items, &deployment)
	}
	expected, err := os.ReadFile(filename)
	if err != nil && strings.Contains(err.Error(), "no such file or directory") {
		os.WriteFile(filename, []byte(serialize(items)), 0644)
		Fail(fmt.Sprintf("Regenerating the expected yaml file %s", filename))
	}

	Expect(serialize(items)).To(Equal(string(expected)))
}

func serialize(objs []interface{}) string {
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
		if obj, ok := obj.(*corev1.Secret); ok {
			for k := range obj.Data {
				obj.Data[k] = []byte(".")
			}
		}
		if obj, ok := obj.(*resourcesv1alpha1.ManagedResource); ok {
			obj.Status = resourcesv1alpha1.ManagedResourceStatus{}
		}
		if obj, ok := obj.(runtime.Object); ok {
			serializationYAML, err := runtime.Encode(codec, obj)
			result += string(serializationYAML)
			result += "---\n"
			Expect(err).NotTo(HaveOccurred())
		}
	}

	return string(result)
}
