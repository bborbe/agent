{{/* Common labels applied to every object. */}}
{{- define "agent.labels" -}}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: agent
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{/* Namespace — required; fail loudly if unset so a misconfigured install can't
     silently land in the release namespace. */}}
{{- define "agent.namespace" -}}
{{- required "namespace is required (set .Values.namespace)" .Values.namespace -}}
{{- end -}}

{{/* Executor image ref: registry/repository:tag, tag defaulting to appVersion. */}}
{{- define "agent.executor.image" -}}
{{- $tag := .Values.executor.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.executor.image.repository $tag -}}
{{- end -}}

{{/* Controller image ref: registry/repository:tag, tag defaulting to appVersion. */}}
{{- define "agent.controller.image" -}}
{{- $tag := .Values.controller.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.controller.image.repository $tag -}}
{{- end -}}

{{/* Recurring-task-creator image ref: registry/repository:tag, tag defaulting to appVersion. */}}
{{- define "agent.recurringTaskCreator.image" -}}
{{- $tag := .Values.recurringTaskCreator.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.recurringTaskCreator.image.repository $tag -}}
{{- end -}}

{{/* Controller GIT_REST_URL: explicit value wins; otherwise derive from vaultName
     as the in-cluster obsidian git-rest service URL. */}}
{{- define "agent.controller.gitRestUrl" -}}
{{- if .Values.controller.gitRestUrl -}}
{{- .Values.controller.gitRestUrl -}}
{{- else -}}
{{- printf "http://vault-obsidian-%s:9090" (required "controller.vaultName is required" .Values.controller.vaultName) -}}
{{- end -}}
{{- end -}}
