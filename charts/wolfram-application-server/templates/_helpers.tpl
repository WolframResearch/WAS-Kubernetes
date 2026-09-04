{{/*
Expand the name of the chart.
*/}}
{{- define "was.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "was.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label value.
*/}}
{{- define "was.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "was.labels" -}}
helm.sh/chart: {{ include "was.chart" . }}
{{ include "was.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — used in matchLabels and Service selectors.
*/}}
{{- define "was.selectorLabels" -}}
app.kubernetes.io/name: {{ include "was.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Kafka bootstrap address.
builtin:  constructed from clusterName + namespace.
external: taken directly from .Values.kafka.bootstrapServers.
*/}}
{{- define "was.kafkaBootstrap" -}}
{{- if eq .Values.kafka.mode "external" }}
{{- required "kafka.bootstrapServers is required when kafka.mode is \"external\"" .Values.kafka.bootstrapServers }}
{{- else }}
{{- printf "%s-kafka-bootstrap.%s.svc.cluster.local:9092" .Values.kafka.clusterName .Values.kafka.namespace }}
{{- end }}
{{- end }}

{{/*
Kafka Bridge service address used by init containers to poll topic availability.
Only meaningful in builtin mode.
*/}}
{{- define "was.kafkaBridgeService" -}}
{{- printf "kafka-bridge-service.%s.svc.cluster.local:%d" .Values.kafka.namespace (.Values.kafkaBridge.http.port | int) }}
{{- end }}

{{/*
Object storage endpoint — use explicit value if set, otherwise derive from cloud.
AWS:   https://s3.<region>.amazonaws.com
Azure: https://<accountName>.blob.core.windows.net/
*/}}
{{- define "was.storageEndpoint" -}}
{{- if .Values.objectStorage.endpoint }}
{{- .Values.objectStorage.endpoint }}
{{- else if eq .Values.cloud "aws" }}
{{- printf "https://s3.%s.amazonaws.com" .Values.objectStorage.region }}
{{- else if eq .Values.cloud "azure" }}
{{- printf "https://%s.blob.core.windows.net/" .Values.objectStorage.azure.accountName }}
{{- end }}
{{- end }}

{{/*
ServiceAccount name for resource-manager.
*/}}
{{- define "was.resourceManagerSAName" -}}
{{- if .Values.resourceManager.serviceAccount.name }}
{{- .Values.resourceManager.serviceAccount.name }}
{{- else }}
{{- printf "%s-resource-manager" (include "was.fullname" .) }}
{{- end }}
{{- end }}

{{/*
ServiceAccount annotations for resource-manager.
Merges cloud-identity shorthands with any extra annotations from values.
  AWS IRSA:              eks.amazonaws.com/role-arn
  Azure WI:              azure.workload.identity/client-id
*/}}
{{- define "was.resourceManagerSAAnnotations" -}}
{{- $ann := dict }}
{{- if and (eq .Values.cloud "aws") (eq .Values.objectStorage.auth.mode "irsa") .Values.resourceManager.serviceAccount.roleArn }}
{{- $_ := set $ann "eks.amazonaws.com/role-arn" .Values.resourceManager.serviceAccount.roleArn }}
{{- end }}
{{- if and (eq .Values.cloud "azure") (eq .Values.objectStorage.auth.mode "workloadIdentity") .Values.resourceManager.serviceAccount.azureClientId }}
{{- $_ := set $ann "azure.workload.identity/client-id" .Values.resourceManager.serviceAccount.azureClientId }}
{{- end }}
{{- $merged := merge $ann (.Values.resourceManager.serviceAccount.annotations | default dict) }}
{{- if $merged }}
{{- toYaml $merged }}
{{- end }}
{{- end }}

{{/*
Name of the object-storage credentials Secret (static mode only).
*/}}
{{- define "was.objectStorageSecretName" -}}
{{- printf "%s-object-storage" (include "was.fullname" .) }}
{{- end }}

{{/*
True when Ingress TLS should be rendered: explicit ingress.tls.enabled, or the
cert-manager chart subchart is enabled (chart-only installs).
*/}}
{{- define "was.ingressTLSEnabled" -}}
{{- $cm := index .Values "cert-manager" | default dict -}}
{{- if or .Values.ingress.tls.enabled (and $cm $cm.enabled) -}}
true
{{- end -}}
{{- end }}

{{/*
cert-manager ingress-shim annotation.

Left empty on purpose: the chart renders an explicit Certificate
(templates/certificate.yaml) that owns was-tls-secret. Annotating every
Ingress would make ingress-shim create one Certificate per Ingress and race
on the same secret.
*/}}
{{- define "was.ingress.tlsAnnotations" -}}
{{- end }}

{{/*
spec.tls block for WAS Ingress resources (was-tls-secret by default).
*/}}
{{- define "was.ingress.tlsSpec" -}}
{{- if eq (include "was.ingressTLSEnabled" .) "true" }}
tls:
  - hosts:
      - {{ .Values.ingress.host | quote }}
    secretName: {{ .Values.ingress.tls.secretName | default "was-tls-secret" | quote }}
{{- end }}
{{- end }}

{{/*
Public URL scheme for AWES env vars and NOTES (https when TLS is enabled).
*/}}
{{- define "was.publicScheme" -}}
{{- if eq (include "was.ingressTLSEnabled" .) "true" -}}
https
{{- else -}}
http
{{- end -}}
{{- end }}
