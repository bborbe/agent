---
status: completed
spec: [062-sandbox-domains]
summary: Added sandbox.domains/proxyNamespace values and the domain, peer-catalog, storage-namespace and fail-fast validator helpers; legacy render unchanged and all render checks pass
execution_id: agent-sandbox-domains-exec-253-spec-062-sandbox-domains-helpers
dark-factory-version: v0.198.7
created: "2026-10-24T20:38:48Z"
queued: "2026-10-24T20:57:58Z"
started: "2026-10-24T20:58:00Z"
completed: "2026-10-24T20:58:58Z"
branch: dark-factory/sandbox-domains
---

# feat(helm): add `sandbox.domains` values and the peer-catalog helpers

<summary>
- A values overlay can now declare an arbitrary set of sandbox namespaces under `sandbox.domains`, each with its own peer list
- A named peer catalog (dns, router, pushgateway, sentry-proxy, three vaults, two frontends, vault-api, kafka, proxy) resolves each peer name to a fixed namespace and selector, so a values file cannot point a peer at an arbitrary namespace
- `dns` and `proxy` are always part of every domain's policy, whether or not the domain lists them
- Omitting a domain's `peers` key renders today's full union, so the first apply of a domains install changes no agent's reach
- Bad values fail the render with a message naming the offending key, instead of rendering a policy that silently denies traffic
- The chart knows which namespace hosts the single egress proxy, and the proxy environment variables point at it
- The chart knows where an agent's storage copy belongs when domains are declared
- An install that declares no domains keeps its exact current render
</summary>

<objective>
Introduce the values surface and the template helpers that the rest of spec `062-sandbox-domains` resolves through: the `sandbox.domains` map, `sandbox.proxyNamespace`, a closed peer catalog with fixed namespace rules, the namespace list, the per-agent storage-namespace resolver, and a single fail-fast validator. No rendered object changes shape in this prompt beyond the proxy environment URL (which keeps resolving to the legacy sandbox when no domains are declared).
</objective>

<context>
Read `CLAUDE.md` for project conventions (single root `Makefile`, single global `CHANGELOG.md`, chart changes go through `hack/test-helm-render.sh`). Read `docs/dod.md` for the definition of done. Read `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` before touching `CHANGELOG.md` in the later prompts — this prompt does not touch it.

This is prompt 1 of 6 for spec `062-sandbox-domains`. It owns only `helm/values.yaml`, `helm/templates/_helpers.tpl` and one line at the top of `helm/templates/sandbox.yaml`. Do not restructure the sandbox templates, the egress-proxy templates, the executor RBAC or `agents.yaml` — later prompts own those.

Files to read before editing (anchor on the quoted names, not on line numbers):

- `helm/templates/_helpers.tpl` — the helper file. The definitions you must extend or mirror are `agent.namespace`, `agent.sandboxNamespace` (fails when `sandbox.namespace` equals the control namespace), `agent.sandboxPeerNamespace` (defaults to the control namespace) and `agent.proxyEnv` (currently interpolates `include "agent.sandboxNamespace" .` into four proxy URLs).
- `helm/templates/sandbox.yaml` — the legacy policy whose peer rules the catalog must reproduce in domains mode. Its first line is `{{- if .Values.executor.enabled }}`; the validator include goes immediately above that line, outside the gate, so a bad values file fails even with the executor disabled.
- `helm/values.yaml` — the `sandbox:` block at the top of the file (`namespace`, `peerNamespace`, `kafka`, `routerInstance`, `vaultApiApp`, `renderAgentStorage`).
- `helm/Chart.yaml` — version 0.22.3 (not bumped in this prompt).

The helper style in this chart is: a `{{- define "agent.<name>" -}}` block with a doc comment, called as `include "agent.<name>" .` and consumed with `| splitList ","` or `| fromYaml`. Values maps are dereferenced defensively as `(.Values.sandbox | default dict)`, because `sandbox` may be absent from an overlay. Follow that style exactly.

The peer catalog and its namespace rules are fixed by spec 062 DB2:

| Peer name | Namespace | Selector | Port |
|---|---|---|---|
| `dns` | `kube-system` | `k8s-app: kube-dns` | 53 UDP + TCP |
| `router` | control namespace (`agent.namespace`) | `app.kubernetes.io/instance:` `sandbox.routerInstance` or `claude-code-router-<namespace>` | 8788 |
| `pushgateway` | `monitoring` | `app: pushgateway` | 9090 |
| `sentry-proxy` | `sandbox.peerNamespace` (default control ns) | `app: sentry-proxy` | 9090 |
| `vault-personal` | `sandbox.peerNamespace` | `app: vault-obsidian-personal` | 9090 |
| `vault-openclaw` | `sandbox.peerNamespace` | `app: vault-obsidian-openclaw` | 9090 |
| `vault-trading` | `sandbox.peerNamespace` | `app: vault-obsidian-trading` | 9090 |
| `frontend-gateway` | `sandbox.peerNamespace` | `app: frontend-gateway` | 9090 |
| `frontend-strategy` | `sandbox.peerNamespace` | `app: frontend-strategy` | 9090 |
| `vault-api` | `sandbox.peerNamespace` | `app:` `sandbox.vaultApiApp` (required) | 9090 |
| `kafka` | `sandbox.kafka.namespace` + `servicePort` + optional `podLabels`, or the three `/32` nodeports on 32159 | namespace + optional pod selector | servicePort / 32159 |
| `proxy` | `sandbox.proxyNamespace` | `namespaceSelector` **and** `podSelector: app: egress-proxy` in the SAME `to:` entry | 8888 |
</context>

<requirements>
1. **`helm/values.yaml` — add `sandbox.domains` and `sandbox.proxyNamespace`.** Insert both keys inside the existing `sandbox:` block, directly after the `namespace: ""` comment block and before `peerNamespace: ""`. Keep `namespace: ""` in place and unchanged. Add comments in the surrounding style (two-space indent, `#` continuation lines aligned as in the existing block):

   ```yaml
     # A map of sandbox namespace name to its egress peer set. When non-empty the
     # chart renders one Namespace and one `<name>-egress` default-deny policy per
     # entry, and the egress proxy renders once in `proxyNamespace`. Empty (the
     # default) renders today's single sandbox exactly, so no existing install
     # changes. Mutually exclusive with `namespace` above — declaring both fails
     # the render, because a fifth namespace or one silently ignored is ambiguous.
     #
     # Each entry takes an optional `peers` list naming destinations from the
     # fixed catalog: dns, router, pushgateway, sentry-proxy, vault-personal,
     # vault-openclaw, vault-trading, frontend-gateway, frontend-strategy,
     # vault-api, kafka, proxy. An unknown name fails the render. `dns` and
     # `proxy` are added to every domain unconditionally — they are how a pod
     # resolves anything and the only route to the public internet. OMITTING
     # `peers` renders today's full union (every catalog peer except proxy/dns
     # duplication), deliberately: the first apply of a domains install must not
     # narrow any agent's reach. An explicit `peers: []` renders only dns+proxy.
     domains: {}
     # Namespace hosting the single egress proxy (Deployment, Service, PDB,
     # allowlist ConfigMap and its two policies). Required when more than one
     # domain is declared, and must be one of the declared domains. With exactly
     # one domain it defaults to that domain. Setting it with no `domains`
     # declared fails the render — it would name a namespace nothing creates.
     proxyNamespace: ""
   ```

   Add no other uncommented key and change no existing default.

2. **`helm/templates/_helpers.tpl` — append the domain helpers.** Add the definitions below at the end of the file, each preceded by a doc comment in the existing style. Do not modify `agent.labels`, `agent.namespace`, `agent.executor.image`, `agent.recurringTaskCreator.image`, `agent.kafkaCertVolumeMounts`, `agent.kafkaCertVolumes` or `agent.sandboxNamespace`.

   a. The sorted domain-name list. Map iteration in Go templates is already sorted by key; `sortAlpha` makes the contract explicit and keeps the helper's output stable for tests:

   ```gotemplate
   {{/* Declared sandbox domain names, comma-separated and alphabetically sorted;
        empty string when `sandbox.domains` is unset or empty. The comma form is
        what `splitList ","` consumes in the templates. */}}
   {{- define "agent.sandboxDomainNames" -}}
   {{- $domains := (.Values.sandbox | default dict).domains | default dict -}}
   {{- keys $domains | sortAlpha | join "," -}}
   {{- end -}}
   ```

   b. The namespace list that hosts agent pods — every domain in domains mode, the single sandbox otherwise. Used by the per-domain RBAC and the proxy's ingress policy:

   ```gotemplate
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
   ```

   c. The proxy namespace:

   ```gotemplate
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
   ```

   d. The resolved peer list for one domain. Input is a dict with `root` (the root context) and `name` (the domain). Output is a comma-separated list, `dns` first and `proxy` last, deduplicated:

   ```gotemplate
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
   {{- $peers = list "router" "pushgateway" "sentry-proxy" "vault-personal" "vault-openclaw" "vault-trading" "frontend-gateway" "frontend-strategy" "kafka" -}}
   {{- if $sandbox.vaultApiApp -}}{{- $peers = append $peers "vault-api" -}}{{- end -}}
   {{- end -}}
   {{- $all := list "dns" -}}
   {{- range $peer := $peers -}}
   {{- if and (ne $peer "dns") (ne $peer "proxy") (not (has $peer $all)) -}}{{- $all = append $all $peer -}}{{- end -}}
   {{- end -}}
   {{- $all = append $all "proxy" -}}
   {{- join "," $all -}}
   {{- end -}}
   ```

   e. The catalog renderer. Input is a dict with `root` and `name` (one catalog peer). It emits ONE egress rule with **zero** base indentation — callers apply `| nindent 4` — so the same block lands correctly in a 4-space-indented `egress:` list. Reproduce the selectors from the table in `<context>` exactly; `namespaceSelector` and `podSelector` stay in the same `to:` entry for every peer, and Kafka is either the in-cluster form or the three `/32` nodeports on 32159 (never a `/24`):

   ```gotemplate
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
       - { protocol: TCP, port: 32159 }
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
   ```

   The `proxy` branch is the load-bearing one: a bare `podSelector` matches only same-namespace pods, so in every domain except the proxy's own it would match nothing and all internet egress would be lost with no render failure.

   f. The per-agent storage namespace:

   ```gotemplate
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
   ```

   g. The fail-fast validator. Every message must name the offending key so the render test can grep it. Keep the Kafka message text **identical** to the one currently in `sandbox.yaml` (`sandbox.kafka.servicePort is required as a positive integer when sandbox.kafka.namespace is set`) — the existing `kafka-missing-port` and `kafka-nonint-port` cases grep it:

   ```gotemplate
   {{/* Fail-fast validation for the sandbox domain values. Called once from the
        top of templates/sandbox.yaml, outside the executor gate. Every message
        names the offending key, because the render tests assert on the text. */}}
   {{- define "agent.sandboxValidate" -}}
   {{- $sandbox := .Values.sandbox | default dict -}}
   {{- $controlNS := include "agent.namespace" . -}}
   {{- $domains := $sandbox.domains | default dict -}}
   {{- $explicitSandboxNS := $sandbox.namespace | default "" -}}
   {{- $proxyNS := $sandbox.proxyNamespace | default "" -}}
   {{- $catalog := list "dns" "router" "pushgateway" "sentry-proxy" "vault-personal" "vault-openclaw" "vault-trading" "frontend-gateway" "frontend-strategy" "vault-api" "kafka" "proxy" -}}
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
   {{- fail (printf "sandbox.domains.%s.peers names unknown peer %q: the catalog is dns, router, pushgateway, sentry-proxy, vault-personal, vault-openclaw, vault-trading, frontend-gateway, frontend-strategy, vault-api, kafka, proxy" $name $peer) -}}
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
          else-if fires and fails a configuration the spec explicitly allows. */ -}}
   {{- if and $proxyNS (not (hasKey $domains $proxyNS)) -}}
   {{- fail (printf "sandbox.proxyNamespace %q is not one of the declared sandbox.domains: a proxy in a namespace nothing creates is a silent outage" $proxyNS) -}}
   {{- end -}}
   {{- if $sandbox.kafka.namespace -}}
   {{- if not (regexMatch "^[1-9][0-9]*$" (toString $sandbox.kafka.servicePort)) -}}
   {{- fail "sandbox.kafka.servicePort is required as a positive integer when sandbox.kafka.namespace is set" -}}
   {{- end -}}
   {{- end -}}
   {{- end -}}
   {{- end -}}
   ```

   Two independent guards, deliberately **not** an if/else chain: more than one domain with no `proxyNamespace` fails, and a `proxyNamespace` that is set but names no declared domain fails. With exactly one domain and no `proxyNamespace` both guards pass and `agent.proxyNamespace` picks that domain. Do not collapse them into one `if not $proxyNS … else if` — that shape makes the single-domain default unreachable, because `hasKey $domains ""` is false.

   The block above has **15 opens and 15 closes** (`define` 1, `if`/`else`/`range` 14). A missing close is a parse error — `unexpected EOF` — and the whole chart fails to render, so count them if you edit this block.

3. **`helm/templates/_helpers.tpl` — point the proxy env at the proxy namespace.** In `agent.proxyEnv`, replace all four occurrences of `include "agent.sandboxNamespace" .` with `include "agent.proxyNamespace" .` (the `HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy` and `https_proxy` lines). Update the helper's doc comment: the URL is derived from the namespace that hosts the proxy, not from "the" sandbox, so an agent in any domain reaches the one proxy. Leave the `NO_PROXY` computation untouched.

4. **`helm/templates/sandbox.yaml` — invoke the validator.** Insert this line as the very first line of the file, above `{{- if .Values.executor.enabled }}`, so a bad values file fails even when the executor is disabled:

   ```gotemplate
   {{- include "agent.sandboxValidate" . -}}
   ```

   Do not change anything else in `sandbox.yaml` in this prompt. In particular do not delete the inline Kafka `servicePort` check yet — prompt 2 removes it when it restructures the file. The validator runs first, so the inline copy is unreachable for the same inputs and the two messages are identical.

5. **Do not add render tests in this prompt.** Prompt 6 owns `hack/test-helm-render.sh`. The existing cases must stay green.
</requirements>

<constraints>
- `sandbox.namespace` keeps working exactly as today; the legacy render (no `domains`) must be unchanged.
- Every new values key is optional. No existing install gains a required value.
- `helm.sh/resource-policy: keep` semantics are untouched by this prompt.
- A values file must not be able to point a catalog peer at an arbitrary namespace or port — the catalog is closed and each name resolves its namespace by a fixed rule.
- No `values.schema.json` exists; a typo in a key is accepted silently, which is why every key added here is asserted by a render test in prompt 6.
- Do NOT commit — dark-factory handles git.
- `docs/dod.md` (the daemon's `validationPrompt`) asks for a `CHANGELOG.md` entry and a `README.md` update. Both are owned by prompt 6 of this spec: report them as deferred, do not edit either file, and do not report them as a blocker for this prompt. Its remaining items do not apply to a chart-only change.
- Do not edit `specs/`, `hack/test-helm-render.sh`, `CHANGELOG.md`, `helm/Chart.yaml`, `helm/README.md` or `docs/` in this prompt.
</constraints>

<verification>
```bash
# 1. Existing render suite must stay green (legacy render unchanged, Kafka
#    messages unchanged). This also installs the pinned helm binary if absent.
bash hack/test-helm-render.sh

# 2. Ad-hoc render checks for the new fail-fast rules and the proxy env.
command -v helm >/dev/null 2>&1 || go install helm.sh/helm/v3/cmd/helm@v3.16.4
HELM_BIN="$(command -v helm || echo "$(go env GOPATH)/bin/helm")"

cat > /tmp/domains-valid.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: 192.168.178.41:32159
  sentry:
    dsn: dummy
egressProxy:
  injectAgentEnv: true
sandbox:
  proxyNamespace: agent-sandbox
  domains:
    agent-sandbox: {}
    trading-sandbox:
      peers: [router, frontend-gateway]
EOF

# AC9: with domains declared, the proxy env names the proxy namespace.
# The default values file has no agents, so add one; each Job Config carries
# HTTP_PROXY/HTTPS_PROXY/http_proxy/https_proxy, i.e. 4 lines.
cat >> /tmp/domains-valid.yaml <<'EOF'
agents:
  - name: probe-agent
    enabled: true
    assignee: probe
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF
"$HELM_BIN" template agent ./helm -f /tmp/domains-valid.yaml \
  --show-only templates/agents.yaml \
  | grep -c 'egress-proxy.agent-sandbox.svc.cluster.local:8888'
# expect 4

# AC8: sandbox.namespace + sandbox.domains together must fail, naming both keys.
if "$HELM_BIN" template agent ./helm -f /tmp/domains-valid.yaml \
    --set sandbox.namespace=dev-sandbox >/dev/null 2>/tmp/e1; then
  echo "FAIL: both keys accepted"; else grep -q 'mutually exclusive' /tmp/e1 && echo OK; fi

# AC7: a domain naming the control namespace must fail, naming it.
# Grep the validator's own wording, not the bare word `dev` — that matches a
# path, a namespace or `dev-agents-sandbox` and cannot tell this failure from
# any other render error.
if "$HELM_BIN" template agent ./helm \
    --set namespace=dev --set 'sandbox.domains.dev.peers={router}' >/dev/null 2>/tmp/e2; then
  echo "FAIL: control-namespace domain accepted"
else grep -q 'must not name the control namespace' /tmp/e2 && echo OK || echo "FAIL: wrong message in /tmp/e2"; fi

# Every render below must carry `namespace` and the executor's required values:
# without them `helm template` dies on `namespace is required` before the
# validator runs, the grep finds nothing, and the check prints no line at all —
# which reads as a pass to anyone scanning for FAIL.
if "$HELM_BIN" template agent ./helm --set namespace=dev \
    --set sandbox.proxyNamespace=agent-sandbox >/dev/null 2>/tmp/e3; then
  echo "FAIL: proxyNamespace without domains accepted"
else grep -q 'sandbox.domains is empty' /tmp/e3 && echo OK || echo "FAIL: wrong message in /tmp/e3"; fi
if "$HELM_BIN" template agent ./helm --set namespace=dev \
    --set 'sandbox.domains.dev-sandbox.peers={not-a-peer}' >/dev/null 2>/tmp/e4; then
  echo "FAIL: unknown peer accepted"
else grep -q 'unknown peer' /tmp/e4 && echo OK || echo "FAIL: wrong message in /tmp/e4"; fi

# AC11(d): a proxyNamespace that names no declared domain must fail — this is the
# rule that keeps the proxy out of a namespace nothing creates.
if "$HELM_BIN" template agent ./helm --set namespace=dev \
    --set sandbox.proxyNamespace=not-a-domain \
    --set 'sandbox.domains.agent-sandbox.peers={router}' >/dev/null 2>/tmp/e5; then
  echo "FAIL: undeclared proxyNamespace accepted"
else grep -q 'is not one of the declared' /tmp/e5 && echo OK || echo "FAIL: wrong message in /tmp/e5"; fi

# DB4 third branch: exactly one domain and no proxyNamespace must RENDER — the
# proxy defaults to that domain. An inverted membership guard breaks this and
# nothing else in this block notices.
if "$HELM_BIN" template agent ./helm --set namespace=dev \
    --set executor.kafkaBrokers=k:9092 --set executor.sentry.dsn=dummy \
    --set 'sandbox.domains.agent-sandbox.peers={router}' >/dev/null 2>/tmp/e6; then
  echo OK
else echo "FAIL: single domain without proxyNamespace did not render"; cat /tmp/e6; fi
```

Every command above must print its `OK`/expected count — **a check that prints nothing is a failure, not a pass**. `bash hack/test-helm-render.sh` must exit 0. Do not run `make precommit` (this is a chart-only change); `make test` runs in prompt 6.
</verification>
