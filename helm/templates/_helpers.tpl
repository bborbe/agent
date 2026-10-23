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
    # 0440 (octal): owner+group read, no world read. Files are owned root:fsGroup,
    # so a non-root pod (runAsUser+fsGroup) reads via group; 0400 would deny it.
    defaultMode: 288
    secretName: {{ .clientSecret }}
    items:
      - key: user.crt
        path: file
- name: client-key
  secret:
    defaultMode: 288
    secretName: {{ .clientSecret }}
    items:
      - key: user.key
        path: file
- name: server-cert
  secret:
    defaultMode: 288
    secretName: {{ .caCertSecret }}
    items:
      - key: ca.crt
        path: file
{{- end -}}

{{/* Sandbox namespace hosting agent Job pods. `.Values.sandbox.namespace` wins
     when non-empty; otherwise fall back to the per-environment
     "<namespace>-agents-sandbox" so every existing install renders unchanged
     (dev + prod own distinct objects instead of colliding on a shared name —
     quant: dev/prod are namespaces on ONE cluster). The fallback is
     load-bearing: changing it would rename a deployed namespace and orphan the
     `resource-policy: keep` object and its PVCs. */}}
{{- define "agent.sandboxNamespace" -}}
{{- if (.Values.sandbox | default dict).namespace -}}
{{- (.Values.sandbox | default dict).namespace -}}
{{- else -}}
{{- printf "%s-agents-sandbox" (include "agent.namespace" .) -}}
{{- end -}}
{{- end -}}

{{/* Proxy env for Job agent Configs (spec 048). Applied only when
     `egressProxy.injectAgentEnv` is true — see agents.yaml. The proxy URL is
     derived from the sandbox namespace; NO_PROXY
     keeps in-cluster traffic (kube-dns, sentry-proxy) and the Kafka brokers on
     the direct path — the Kafka hosts are read from the per-install
     executor.kafkaBrokers value (nuke's Kafka is an external nodeport, not an
     in-cluster Service, so .svc.cluster.local does not cover it). These values
     override any conflicting per-agent env so an agent cannot bypass or
     misroute the proxy. */}}
{{- define "agent.proxyEnv" -}}
{{- $noProxy := list "127.0.0.1" "localhost" ".svc.cluster.local" }}
{{- /* Both broker sources: the singular executor, plus every executors[] entry
       (list mode renders those instead of the singular Deployment, and their
       kafkaBrokers falls back to the global). Missing the list source would leave
       NO_PROXY without those brokers, routing Kafka through the proxy. */}}
{{- $brokerSets := list (toString .Values.executor.kafkaBrokers) }}
{{- range $e := .Values.executors }}
{{-   $brokerSets = append $brokerSets (toString ($e.kafkaBrokers | default $.Values.executor.kafkaBrokers)) }}
{{- end }}
{{- range $brokers := $brokerSets }}
{{- range $broker := splitList "," $brokers }}
{{/* Strip an optional scheme (values.yaml documents the tls:// form) before
     splitting on ':' - otherwise the host parses as the literal "tls". */}}
{{-   $b := regexReplaceAll "^[A-Za-z][A-Za-z0-9+.-]*://" (trim $broker) "" }}
{{-   $host := "" }}
{{-   if hasPrefix "[" $b }}
{{/* IPv6 literal: [::1]:9092 -> ::1 */}}
{{-     $host = trimPrefix "[" (first (splitList "]" $b)) }}
{{-   else }}
{{-     $host = first (splitList ":" $b) }}
{{-   end }}
{{-   if $host }}
{{-     $noProxy = append $noProxy $host }}
{{-   end }}
{{- end }}
{{- end }}
{{- $noProxyValue := join "," $noProxy }}
HTTP_PROXY: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
HTTPS_PROXY: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
http_proxy: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
https_proxy: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
NO_PROXY: {{ $noProxyValue }}
no_proxy: {{ $noProxyValue }}
{{- end -}}
