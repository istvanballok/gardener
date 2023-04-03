{{- define "kubeadmConfigPatches" -}}
- |
  kind: ClusterConfiguration
  apiServer:
{{- if .Values.gardener.apiserverRelay.deployed }}
    certSANs:
      - localhost
      - 127.0.0.1
      - gardener-apiserver.relay.svc.cluster.local
{{- end }}
    extraArgs:
      audit-log-path: "-"
      audit-policy-file: /etc/gardener/controlplane/audit-policy.yaml
{{- if not .Values.gardener.controlPlane.deployed }}
      authorization-mode: RBAC,Node
    - name: audit-policy
      hostPath: /etc/gardener/controlplane/audit-policy.yaml
      mountPath: /etc/gardener/controlplane/audit-policy.yaml
      readOnly: true
      pathType: File
{{- else }}
      authorization-mode: RBAC,Node,Webhook
      authorization-webhook-config-file: /etc/gardener/controlplane/auth-webhook-kubeconfig-{{ .Values.environment }}.yaml
      authorization-webhook-cache-authorized-ttl: "0"
      authorization-webhook-cache-unauthorized-ttl: "0"
    extraVolumes:
    - name: gardener
      hostPath: /etc/gardener/controlplane/auth-webhook-kubeconfig-{{ .Values.environment }}.yaml
      mountPath: /etc/gardener/controlplane/auth-webhook-kubeconfig-{{ .Values.environment }}.yaml
      readOnly: true
      pathType: File
    - name: audit-policy
      hostPath: /etc/gardener/controlplane/audit-policy.yaml
      mountPath: /etc/gardener/controlplane/audit-policy.yaml
      readOnly: true
      pathType: File
{{- end }}
- |
  apiVersion: kubelet.config.k8s.io/v1beta1
  kind: KubeletConfiguration
  maxPods: 500
  serializeImagePulls: false
  registryPullQPS: 10
  registryBurst: 20
{{- end -}}
