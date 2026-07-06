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

{{/* Recurring-task-creator image ref: registry/repository:tag, tag defaulting to appVersion. */}}
{{- define "agent.recurringTaskCreator.image" -}}
{{- $tag := .Values.recurringTaskCreator.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.recurringTaskCreator.image.repository $tag -}}
{{- end -}}

{{/* Kafka mTLS client-cert volumeMounts. Emitted for a component whose
     kafkaUser.enabled is true. The fixed paths /client-cert/file,
     /client-key/file, /server-cert/file are what github.com/bborbe/kafka reads
     when KAFKA_BROKERS uses the tls:// scheme — do not change them. */}}
{{- define "agent.kafkaCertVolumeMounts" -}}
- name: client-cert
  mountPath: /client-cert
- name: client-key
  mountPath: /client-key
- name: server-cert
  mountPath: /server-cert
{{- end -}}

{{/* Kafka mTLS client-cert volumes. Arg: a dict with `clientSecret` (holds the
     Strimzi-issued user.crt/user.key) and `caCertSecret` (holds the cluster
     ca.crt). Each key is projected to path `file` (matching the fixed mount
     paths above). The chart only references these Secrets by name — Strimzi
     issues them and an external syncer places them in the app namespace. */}}
{{- define "agent.kafkaCertVolumes" -}}
- name: client-cert
  secret:
    defaultMode: 420
    secretName: {{ .clientSecret }}
    items:
      - key: user.crt
        path: file
- name: client-key
  secret:
    defaultMode: 420
    secretName: {{ .clientSecret }}
    items:
      - key: user.key
        path: file
- name: server-cert
  secret:
    defaultMode: 420
    secretName: {{ .caCertSecret }}
    items:
      - key: ca.crt
        path: file
{{- end -}}
