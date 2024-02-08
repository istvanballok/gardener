// Copyright 2024 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package cache

import (
	_ "embed"
	"encoding/json"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/gardener/gardener/third_party/gopkg.in/yaml.v2"
)

var (
	//go:embed assets/prometheusrules/metering.rules.yaml
	meteringYAML []byte
	metering     *monitoringv1.PrometheusRule

	//go:embed assets/prometheusrules/metering.rules.stateful.yaml
	meteringStatefulYAML []byte
	meteringStateful     *monitoringv1.PrometheusRule

	//go:embed assets/prometheusrules/recording-rules.rules.yaml
	recordingRulesYAML []byte
	recordingRules     *monitoringv1.PrometheusRule
)

func init() {
	metering = &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{Kind: monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "metering"},
		Spec:       *unmarshall(meteringYAML)}

	meteringStateful = &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{Kind: monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "metering-stateful"},
		Spec:       *unmarshall(meteringStatefulYAML)}

	recordingRules = &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{Kind: monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "recording-rules"},
		Spec:       *unmarshall(recordingRulesYAML)}
}

func convertMapI2MapS(i interface{}) interface{} {
	switch x := i.(type) {
	case map[interface{}]interface{}:
		m2 := map[string]interface{}{}
		for k, v := range x {
			m2[k.(string)] = convertMapI2MapS(v)
		}
		return m2
	case []interface{}:
		for i, v := range x {
			x[i] = convertMapI2MapS(v)
		}
	}
	return i
}

func unmarshall(in []byte) *monitoringv1.PrometheusRuleSpec {
	var iMap map[interface{}]interface{}
	utilruntime.Must(yaml.Unmarshal(in, &iMap))
	sMap := convertMapI2MapS(iMap)
	jsonData, err := json.Marshal(sMap)
	utilruntime.Must(err)

	out := &monitoringv1.PrometheusRuleSpec{}
	utilruntime.Must(json.Unmarshal(jsonData, out))
	return out
}

// CentralPrometheusRules returns the central PrometheusRule resources for the cache prometheus.
func CentralPrometheusRules() []*monitoringv1.PrometheusRule {
	return []*monitoringv1.PrometheusRule{metering, meteringStateful, recordingRules}
}
