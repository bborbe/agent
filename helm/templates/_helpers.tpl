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
{{- if eq (toString .Values.sandbox.namespace) (toString .Values.namespace) -}}
{{- fail "sandbox.namespace must differ from namespace: it names a separate namespace, and equal values would render a second Namespace object for the control namespace" -}}
{{- end -}}
{{- (.Values.sandbox | default dict).namespace -}}
{{- else -}}
{{- printf "%s-agents-sandbox" (include "agent.namespace" .) -}}
{{- end -}}
{{- end -}}

{{/* Namespace holding the sandbox policy's NON-agent peers: sentry-proxy, the
     three vault-obsidian-* services, the two trading frontends and the optional
     vault-API app. `.Values.sandbox.peerNamespace` wins when non-empty; the
     fallback is the control namespace, which is where every current install
     places them, so the render is unchanged unless the key is set.

     Why the key exists: those services are not part of the agent control plane.
     An install that relocates the control plane — e.g. to a dedicated `agent`
     namespace — while leaving the vaults and sentry-proxy in their stage
     namespace would otherwise render peers naming the NEW control namespace and
     matching nothing, silently denying every sandboxed Job its vault, its Sentry
     proxy and the trading frontends. The router is deliberately NOT covered: it
     is a control-plane component and moves with the control namespace. */}}
{{- define "agent.sandboxPeerNamespace" -}}
{{- (.Values.sandbox | default dict).peerNamespace | default (include "agent.namespace" .) -}}
{{- end -}}

{{/* Proxy env for Job agent Configs (spec 048). Applied only when
     `egressProxy.injectAgentEnv` is true — see agents.yaml. The proxy URL is
     derived from the namespace that hosts the proxy, not from "the" sandbox, so
     an agent in any declared domain reaches the one proxy; NO_PROXY
     keeps in-cluster traffic (kube-dns, sentry-proxy) and the Kafka brokers on
     the direct path — the Kafka hosts are read from the per-install
     executor.kafkaBrokers value (nuke's Kafka is an external nodeport, not an
     in-cluster Service, so .svc.cluster.local does not cover it). These values
     override any conflicting per-agent env so an agent cannot bypass or
     misroute the proxy.

     `.svc.cluster.local` covers only the FQDN form. Agents also call peers by
     the namespace-qualified short form (`pushgateway.monitoring`,
     `sentry-proxy.<peerNamespace>`), which that suffix does not match, so the
     proxy receives them and refuses them with `403 Filtered`. Every Service
     peer in the sandbox catalog therefore adds its `<service>.<namespace>`
     host, derived from `agent.sandboxPeerHost` so a new catalog peer cannot
     be forgotten here. Never a bare namespace suffix (`.prod`): a NO_PROXY
     suffix matches any host ending in it and would let public hosts bypass
     the proxy. */}}
{{- define "agent.proxyEnv" -}}
{{- $noProxy := list "127.0.0.1" "localhost" ".svc.cluster.local" }}
{{- range $peer := append (include "agent.sandboxServicePeers" . | splitList ",") "vault-api" }}
{{-   $host := include "agent.sandboxPeerHost" (dict "root" $ "name" $peer) }}
{{-   if and $host (not (has $host $noProxy)) }}
{{-     $noProxy = append $noProxy $host }}
{{-   end }}
{{- end }}
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
{{-   if and $host (not (has $host $noProxy)) }}
{{-     $noProxy = append $noProxy $host }}
{{-   end }}
{{- end }}
{{- end }}
{{- $noProxyValue := join "," $noProxy }}
HTTP_PROXY: http://egress-proxy.{{ include "agent.proxyNamespace" . }}.svc.cluster.local:8888
HTTPS_PROXY: http://egress-proxy.{{ include "agent.proxyNamespace" . }}.svc.cluster.local:8888
http_proxy: http://egress-proxy.{{ include "agent.proxyNamespace" . }}.svc.cluster.local:8888
https_proxy: http://egress-proxy.{{ include "agent.proxyNamespace" . }}.svc.cluster.local:8888
NO_PROXY: {{ $noProxyValue }}
no_proxy: {{ $noProxyValue }}
{{- end -}}

{{/* Declared sandbox domain names, comma-separated and alphabetically sorted;
     empty string when `sandbox.domains` is unset or empty. The comma form is
     what `splitList ","` consumes in the templates. */}}
{{- define "agent.sandboxDomainNames" -}}
{{- $domains := (.Values.sandbox | default dict).domains | default dict -}}
{{- keys $domains | sortAlpha | join "," -}}
{{- end -}}

{{/* Every namespace that hosts agent Job pods: the declared domains when
     `sandbox.domains` is non-empty, otherwise the single sandbox namespace.
     Comma-separated, sorted in domains mode. */}}
{{- define "agent.sandboxNamespaces" -}}
{{- $domains := include "agent.sandboxDomainNames" . -}}
{{- if $domains -}}
{{- $domains -}}
{{- else -}}
{{- include "agent.sandboxNamespace" . -}}
{{- end -}}
{{- end -}}

{{/* Namespace hosting the single egress proxy. With domains declared this is
     `sandbox.proxyNamespace`, or the only declared domain when the key is
     absent (the validator rejects the multi-domain case). Without domains it
     is the single sandbox namespace, so the legacy render is unchanged. */}}
{{- define "agent.proxyNamespace" -}}
{{- $sandbox := .Values.sandbox | default dict -}}
{{- $domains := $sandbox.domains | default dict -}}
{{- if $domains -}}
{{- $proxyNS := $sandbox.proxyNamespace | default "" -}}
{{- if $proxyNS -}}
{{- $proxyNS -}}
{{- else -}}
{{- first (keys $domains | sortAlpha) -}}
{{- end -}}
{{- else -}}
{{- include "agent.sandboxNamespace" . -}}
{{- end -}}
{{- end -}}

{{/* Egress peers for one declared domain, comma-separated: `dns` first,
     `proxy` last, both unconditional. When the domain omits the `peers` key
     the full union renders (today's policy); an explicit empty list renders
     dns+proxy only. Input: dict "root" $ "name" <domain>. */}}
{{- define "agent.sandboxDomainPeers" -}}
{{- $root := .root -}}
{{- $sandbox := $root.Values.sandbox | default dict -}}
{{- $cfg := index ($sandbox.domains | default dict) .name | default dict -}}
{{- $peers := list -}}
{{- $peerCfg := $cfg | default dict -}}
{{- if hasKey $peerCfg "peers" -}}
{{- range $peer := ($peerCfg.peers | default list) -}}
{{- if not (has $peer $peers) -}}{{- $peers = append $peers $peer -}}{{- end -}}
{{- end -}}
{{- else -}}
{{- $peers = append (include "agent.sandboxServicePeers" $root | splitList ",") "kafka" -}}
{{- if $sandbox.vaultApiApp -}}{{- $peers = append $peers "vault-api" -}}{{- end -}}
{{- end -}}
{{- $all := list "dns" -}}
{{- range $peer := $peers -}}
{{- if and (ne $peer "dns") (ne $peer "proxy") (not (has $peer $all)) -}}{{- $all = append $all $peer -}}{{- end -}}
{{- end -}}
{{- $all = append $all "proxy" -}}
{{- join "," $all -}}
{{- end -}}

{{/* Kafka nodeports a sandboxed client may be sent to, comma-separated, with
     the built-in nodeport as the fallback. A NetworkPolicy port matches the
     destination pod; a Kafka client bootstraps on one nodeport and then follows
     the broker's advertised metadata to another, so admitting only the
     bootstrap port rejects the connection the client actually uses. Empty or
     unset falls back to [32159] — never render an empty `ports:` list, which
     would open every port on the peer's CIDRs. Input: the root context. */}}
{{- define "agent.sandboxKafkaNodePorts" -}}
{{- $sandbox := .Values.sandbox | default dict -}}
{{- $kafka := $sandbox.kafka | default dict -}}
{{- join "," ($kafka.nodePorts | default (list 32159)) -}}
{{- end -}}

{{/* The catalog peers that are in-cluster Services reached by host name, in
     the order the default domain union renders them. Excluded: `dns` and
     `proxy` (unconditional, never in NO_PROXY), `kafka` (its brokers come
     from `executor.kafkaBrokers`, and the default peer is an ipBlock), and
     `vault-api` (optional — callers append it; `agent.sandboxPeerHost`
     returns "" while `sandbox.vaultApiApp` is unset). */}}
{{- define "agent.sandboxServicePeers" -}}
router,pushgateway,sentry-proxy,vault-personal,vault-openclaw,vault-trading,frontend-gateway,frontend-strategy,frontend-report,frontend-backtest
{{- end -}}

{{/* `<service>.<namespace>` for one catalog peer, or "" when the peer has no
     Service host. Must agree with `agent.sandboxPeer` below: the namespace is
     the one that peer's policy rule selects, and the Service name is the one
     the agents' own URLs use (for the router, the release instance; for the
     rest, the `app` label the rule selects). Input: dict "root" $ "name"
     <catalog name>. */}}
{{- define "agent.sandboxPeerHost" -}}
{{- $root := .root -}}
{{- $sandbox := $root.Values.sandbox | default dict -}}
{{- $peerNS := include "agent.sandboxPeerNamespace" $root -}}
{{- if eq .name "router" -}}
{{- printf "%s.%s" ($sandbox.routerInstance | default (printf "claude-code-router-%s" $root.Values.namespace)) (include "agent.namespace" $root) -}}
{{- else if eq .name "pushgateway" -}}
pushgateway.monitoring
{{- else if eq .name "sentry-proxy" -}}
{{- printf "sentry-proxy.%s" $peerNS -}}
{{- else if eq .name "vault-personal" -}}
{{- printf "vault-obsidian-personal.%s" $peerNS -}}
{{- else if eq .name "vault-openclaw" -}}
{{- printf "vault-obsidian-openclaw.%s" $peerNS -}}
{{- else if eq .name "vault-trading" -}}
{{- printf "vault-obsidian-trading.%s" $peerNS -}}
{{- else if eq .name "frontend-gateway" -}}
{{- printf "frontend-gateway.%s" $peerNS -}}
{{- else if eq .name "frontend-strategy" -}}
{{- printf "frontend-strategy.%s" $peerNS -}}
{{- else if eq .name "frontend-report" -}}
{{- printf "frontend-report.%s" $peerNS -}}
{{- else if eq .name "frontend-backtest" -}}
{{- printf "frontend-backtest.%s" $peerNS -}}
{{- else if and (eq .name "vault-api") $sandbox.vaultApiApp -}}
{{- printf "%s.%s" $sandbox.vaultApiApp $peerNS -}}
{{- end -}}
{{- end -}}

{{/* One egress rule for a catalog peer, at zero base indentation (callers use
     `| nindent 4`). Input: dict "root" $ "name" <catalog name>. Every peer
     keeps namespaceSelector and podSelector in the SAME `to:` entry — as two
     peers they OR and open every service on the port in that namespace. */}}
{{- define "agent.sandboxPeer" -}}
{{- $root := .root -}}
{{- $sandbox := $root.Values.sandbox | default dict -}}
{{- if eq .name "dns" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kube-system
      podSelector:
        matchLabels:
          k8s-app: kube-dns
  ports:
    - { protocol: UDP, port: 53 }
    - { protocol: TCP, port: 53 }
{{- else if eq .name "sentry-proxy" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: sentry-proxy
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "router" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.namespace" $root }}
      podSelector:
        matchLabels:
          app.kubernetes.io/instance: {{ $sandbox.routerInstance | default (printf "claude-code-router-%s" $root.Values.namespace) }}
  ports:
    - { protocol: TCP, port: 8788 }
{{- else if eq .name "pushgateway" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: monitoring
      podSelector:
        matchLabels:
          app: pushgateway
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "vault-personal" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: vault-obsidian-personal
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "vault-openclaw" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: vault-obsidian-openclaw
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "vault-trading" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: vault-obsidian-trading
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "frontend-gateway" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: frontend-gateway
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "frontend-strategy" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: frontend-strategy
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "frontend-report" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: frontend-report
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "frontend-backtest" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: frontend-backtest
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "vault-api" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.sandboxPeerNamespace" $root }}
      podSelector:
        matchLabels:
          app: {{ $sandbox.vaultApiApp }}
  ports:
    - { protocol: TCP, port: 9090 }
{{- else if eq .name "kafka" -}}
{{- if $sandbox.kafka.namespace -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ $sandbox.kafka.namespace }}
      {{- with $sandbox.kafka.podLabels }}
      podSelector:
        matchLabels:
          {{- toYaml . | nindent 10 }}
      {{- end }}
  ports:
    - { protocol: TCP, port: {{ $sandbox.kafka.servicePort }} }
{{- else -}}
- to:
    - ipBlock:
        cidr: 192.168.178.41/32
    - ipBlock:
        cidr: 192.168.178.42/32
    - ipBlock:
        cidr: 192.168.178.43/32
  ports:
    {{- range $port := include "agent.sandboxKafkaNodePorts" $root | splitList "," }}
    - { protocol: TCP, port: {{ $port }} }
    {{- end }}
{{- end -}}
{{- else if eq .name "proxy" -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ include "agent.proxyNamespace" $root }}
      podSelector:
        matchLabels:
          app: egress-proxy
  ports:
    - { protocol: TCP, port: 8888 }
{{- end -}}
{{- end -}}

{{/* Namespace for an agent's chart-rendered Secret/PVC copy, or the empty
     string when no copy belongs anywhere. In domains mode the copy follows
     the agent's `jobNamespace` and exists only when that names a declared
     domain. Without domains it targets the single sandbox namespace and
     ignores `jobNamespace` — today's behaviour. Input: dict "root" $
     "agent" $agent. */}}
{{- define "agent.agentStorageNamespace" -}}
{{- $root := .root -}}
{{- $sandbox := $root.Values.sandbox | default dict -}}
{{- $domains := $sandbox.domains | default dict -}}
{{- if $domains -}}
{{- $jobNS := .agent.jobNamespace | default "" -}}
{{- if and $jobNS (hasKey $domains $jobNS) -}}{{- $jobNS -}}{{- end -}}
{{- else -}}
{{- include "agent.sandboxNamespace" $root -}}
{{- end -}}
{{- end -}}

{{/* Fail-fast validation for the sandbox domain values. Called once from the
     top of templates/sandbox.yaml, outside the executor gate. Every message
     names the offending key, because the render tests assert on the text. */}}
{{- define "agent.sandboxValidate" -}}
{{- $sandbox := .Values.sandbox | default dict -}}
{{- $controlNS := include "agent.namespace" . -}}
{{- $domains := $sandbox.domains | default dict -}}
{{- $explicitSandboxNS := $sandbox.namespace | default "" -}}
{{- $proxyNS := $sandbox.proxyNamespace | default "" -}}
{{- $catalog := list "dns" "router" "pushgateway" "sentry-proxy" "vault-personal" "vault-openclaw" "vault-trading" "frontend-gateway" "frontend-strategy" "frontend-report" "frontend-backtest" "vault-api" "kafka" "proxy" -}}
{{- if and $domains $explicitSandboxNS -}}
{{- fail "sandbox.namespace and sandbox.domains are mutually exclusive: declare either a single sandbox.namespace or a sandbox.domains map, not both" -}}
{{- end -}}
{{- if not $domains -}}
{{- if $proxyNS -}}
{{- fail "sandbox.proxyNamespace is set but sandbox.domains is empty: proxyNamespace only selects which declared domain hosts the egress proxy" -}}
{{- end -}}
{{- else -}}
{{- range $name, $cfg := $domains -}}
{{- if eq (toString $name) $controlNS -}}
{{- fail (printf "sandbox.domains must not name the control namespace %q: the chart would render a second Namespace object for it" $name) -}}
{{- end -}}
{{- $peerCfg := $cfg | default dict -}}
{{- if hasKey $peerCfg "peers" -}}
{{- range $peer := ($peerCfg.peers | default list) -}}
{{- if not (has $peer $catalog) -}}
{{- fail (printf "sandbox.domains.%s.peers names unknown peer %q: the catalog is dns, router, pushgateway, sentry-proxy, vault-personal, vault-openclaw, vault-trading, frontend-gateway, frontend-strategy, frontend-report, frontend-backtest, vault-api, kafka, proxy" $name $peer) -}}
{{- end -}}
{{- if and (eq $peer "vault-api") (not $sandbox.vaultApiApp) -}}
{{- fail (printf "sandbox.domains.%s.peers lists vault-api but sandbox.vaultApiApp is empty: set the vault API app label or drop the peer" $name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if gt (len $domains) 1 -}}
{{- if not $proxyNS -}}
{{- fail "sandbox.proxyNamespace is required when more than one sandbox domain is declared: set it to one of the declared domains" -}}
{{- end -}}
{{- end -}}
{{- /* Only test membership when the key is SET. Guarding on `not $proxyNS`
       and then testing `hasKey $domains $proxyNS` in the same `if` makes the
       single-domain default unreachable: with one domain and no
       proxyNamespace, $proxyNS is "" and hasKey $domains "" is false, so the
       else-if fires and fails a render that is valid. */ -}}
{{- if and $proxyNS (not (hasKey $domains $proxyNS)) -}}
{{- fail (printf "sandbox.proxyNamespace %q is not one of the declared sandbox.domains: a proxy in a namespace nothing creates is a silent outage" $proxyNS) -}}
{{- end -}}
{{- end -}}
{{- /* Applies in both branches: the legacy policy renders the same peer, so a
       namespace without a servicePort must fail there too. */ -}}
{{- if $sandbox.kafka.namespace -}}
{{- if not (regexMatch "^[1-9][0-9]*$" (toString $sandbox.kafka.servicePort)) -}}
{{- fail "sandbox.kafka.servicePort is required as a positive integer when sandbox.kafka.namespace is set" -}}
{{- end -}}
{{- end -}}
{{- /* Applies in both Kafka branches: a non-positive nodeport renders a policy
       that matches nothing, the same silent no-op as `port: 0`. */ -}}
{{- range $port := include "agent.sandboxKafkaNodePorts" . | splitList "," -}}
{{- if not (regexMatch "^[1-9][0-9]*$" (toString $port)) -}}
{{- fail "sandbox.kafka.nodePorts must contain only positive integers" -}}
{{- end -}}
{{- end -}}
{{- end -}}
