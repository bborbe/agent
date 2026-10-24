---
status: completed
spec: [062-sandbox-domains]
summary: 'Moved all egress proxy objects (Deployment, Service, ingress NetworkPolicy, PDB, allowlist ConfigMap) to agent.proxyNamespace and generalized egress-proxy-ingress to admit app: agent pods from every rendered sandbox namespace'
execution_id: agent-sandbox-domains-exec-256-spec-062-sandbox-domains-egress-proxy
dark-factory-version: v0.198.7
created: "2026-10-24T20:38:48Z"
queued: "2026-10-24T20:57:59Z"
started: "2026-10-24T21:24:03Z"
completed: "2026-10-24T21:37:08Z"
branch: dark-factory/sandbox-domains
---

# feat(helm): render the egress proxy once, in `proxyNamespace`, and admit every domain

<summary>
- The egress proxy's Deployment, Service, PodDisruptionBudget and allowlist ConfigMap render exactly once, in the namespace the values name as the proxy host
- The proxy's ingress policy admits agent pods from every rendered sandbox namespace, not only the one the proxy happens to live in
- A newly declared domain therefore reaches the proxy without any extra hand-written policy, which is the difference between working egress and a silent outage
- An install with no declared domains renders every proxy object in the single sandbox namespace exactly as today
- The proxy still runs with the same image, resources, probes, security context and allowlist contents
</summary>

<objective>
Move every object in `helm/templates/egress-proxy.yaml` and `helm/templates/egress-proxy-allowlist.yaml` from `agent.sandboxNamespace` to `agent.proxyNamespace`, and extend `egress-proxy-ingress` so its `from:` list carries one `app: agent` peer per namespace returned by `agent.sandboxNamespaces`. The proxy is the only route to the public internet; a domain missing from the ingress peer list keeps its Service reachable but is refused by the proxy with no render error.
</objective>

<context>
This is prompt 4 of 6 for spec `062-sandbox-domains`; it depends on prompts 1 (helpers) and 2 (`egress-proxy-egress` placement, in `sandbox.yaml`). Do not run it concurrently with prompt 2. Read both files fully before editing:

- `helm/templates/egress-proxy.yaml` — one Deployment, one Service, one `egress-proxy-ingress` NetworkPolicy and one PodDisruptionBudget, all inside `{{- if .Values.executor.enabled }}` and all carrying `namespace: {{ include "agent.sandboxNamespace" . }}`. The Deployment's `checksum/allowlist` annotation hashes `egress-proxy-allowlist.yaml` through `include (print $.Template.BasePath "/egress-proxy-allowlist.yaml") . | sha256sum` — leave that mechanism alone. The ingress policy currently has exactly one `from:` entry (namespace = sandbox namespace, podSelector `app: agent`) plus an optional `ipBlock` entry gated on `.Values.egressProxy.nodeCidr`.
- `helm/templates/egress-proxy-allowlist.yaml` — one ConfigMap, also in `agent.sandboxNamespace`. Its data (the tinyproxy filter and the self-test script) must not change.
- `helm/templates/_helpers.tpl` — `agent.proxyNamespace` resolves to `sandbox.proxyNamespace` (or the only declared domain) in domains mode and to the single sandbox namespace otherwise; `agent.sandboxNamespaces` is a comma-separated list of every namespace hosting agent pods.

Current ingress peer (the one to generalize):

```gotemplate
  ingress:
    # Agent pods in the sandbox namespace — the only intended clients.
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ include "agent.sandboxNamespace" . }}
          podSelector:
            matchLabels:
              app: agent
      ports:
        - { protocol: TCP, port: 8888 }
```

Verified current behaviour the render tests pin: `proxy-disabled` asserts no `name: egress-proxy`, `name: egress-proxy-allowlist`, `name: egress-proxy-ingress` or `kind: PodDisruptionBudget` renders with `executor.enabled=false`; `proxy-env url` expects the proxy URL in the default render to be `egress-proxy.test-agents-sandbox.svc.cluster.local:8888`.
</context>

<requirements>
1. **`helm/templates/egress-proxy.yaml` — Deployment namespace.** Replace `namespace: {{ include "agent.sandboxNamespace" . }}` with `namespace: {{ include "agent.proxyNamespace" . }}` in the Deployment metadata. Update the file's header comment: the proxy renders once for every sandbox domain, in `sandbox.proxyNamespace`, and its own egress policy lives in `sandbox.yaml`.

2. **`helm/templates/egress-proxy.yaml` — Service namespace.** Same replacement in the Service metadata (keep the `agent.labels` block).

3. **`helm/templates/egress-proxy.yaml` — ingress policy.** Same namespace replacement in `egress-proxy-ingress` metadata, and replace the single fixed `from:` peer with a loop over every rendered sandbox namespace. The `podSelector` and the `namespaceSelector` must stay in the SAME `from:` entry (as two entries they OR and admit every pod in either namespace):

   ```gotemplate
     ingress:
       # Agent pods in every rendered sandbox namespace — the only intended clients.
       # One entry per namespace so a newly declared domain is admitted without a
       # second edit; a domain missing here reaches the Service and is refused by the
       # proxy, which is a silent outage rather than a render failure.
       {{- range $sandboxNS := (include "agent.sandboxNamespaces" . | splitList ",") }}
       - from:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ $sandboxNS }}
             podSelector:
               matchLabels:
                 app: agent
         ports:
           - { protocol: TCP, port: 8888 }
       {{- end }}
       # Optional extra peer, only for httpGet/TCP probes or another in-cluster
       # client. The chart's own probes are exec probes that curl 127.0.0.1 inside
       # the pod, and pod loopback never crosses the interface NetworkPolicy filters,
       # so this stays empty by default.
       {{- with .Values.egressProxy.nodeCidr }}
       - from:
           - ipBlock:
               cidr: {{ . | quote }}
         ports:
           - { protocol: TCP, port: 8888 }
       {{- end }}
   ```

   Never add a namespace-wide peer (a `from:` entry with a `namespaceSelector` and no `podSelector`): the proxy runs with the `ANY` client ACL, so a namespace-wide peer would let every pod in that namespace use it to reach every allowlisted domain.

4. **`helm/templates/egress-proxy.yaml` — PodDisruptionBudget namespace.** Same replacement in the PDB metadata.

5. **`helm/templates/egress-proxy-allowlist.yaml` — ConfigMap namespace.** Same replacement in the ConfigMap metadata. Do not change any key or value in `data:` — in particular the 32 allowlist entries and both probe scripts stay byte-identical.

6. **Do not touch** `helm/templates/sandbox.yaml`, `executor-rbac.yaml`, `agents.yaml`, `helm/values.yaml`, `helm/README.md`, `docs/`, `CHANGELOG.md`, `helm/Chart.yaml` or `hack/test-helm-render.sh`. Prompt 6 owns the render tests and docs.
</requirements>

<constraints>
- Exactly one Deployment, one Service, one PDB, one allowlist ConfigMap, one `egress-proxy-ingress` and one `egress-proxy-egress` render per install — never one per domain.
- `proxyNamespace` must be a declared domain in domains mode; prompt 1's validator enforces that, so this template can trust the helper.
- `egress-proxy-ingress` never gains a namespace-wide peer: its clients are `app: agent` pods in the rendered sandbox namespaces, and nothing else.
- The allowlist contents and the two self-test scripts are unchanged.
- `executor.enabled: false` still suppresses every proxy object.
- Do NOT commit — dark-factory handles git.
- `docs/dod.md` (the daemon's `validationPrompt`) asks for a `CHANGELOG.md` entry and a `README.md` update. Both are owned by prompt 6 of this spec: report them as deferred, do not edit either file, and do not report them as a blocker for this prompt. Its remaining items do not apply to a chart-only change.
</constraints>

<verification>
```bash
bash hack/test-helm-render.sh   # proxy-disabled and proxy-env url cases must stay green
command -v helm >/dev/null 2>&1 || go install helm.sh/helm/v3/cmd/helm@v3.16.4
HELM_BIN="$(command -v helm || echo "$(go env GOPATH)/bin/helm")"

cat > /tmp/domains.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: 192.168.178.41:32159
  sentry:
    dsn: dummy
sandbox:
  proxyNamespace: agent-sandbox
  domains:
    agent-sandbox: {}
    development-sandbox:
      peers: [router, pushgateway, sentry-proxy]
    trading-sandbox:
      peers: [router, frontend-gateway]
EOF

"$HELM_BIN" template agent ./helm -f /tmp/domains.yaml > /tmp/proxy.out

# AC4: each proxy object renders exactly once, identified by kind + name + namespace.
for spec in "Deployment egress-proxy" "Service egress-proxy" "PodDisruptionBudget egress-proxy" "ConfigMap egress-proxy-allowlist" "NetworkPolicy egress-proxy-ingress"; do
  kind="${spec%% *}"; name="${spec#* }"
  count="$(awk -v K="$kind" -v N="$name" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: " N "\n") && index($0,"  namespace: agent-sandbox\n"){c++} END{print c+0}' /tmp/proxy.out)"
  [ "$count" -eq 1 ] && echo "OK: $kind/$name once in agent-sandbox" || echo "FAIL: $kind/$name count=$count"
done

# AC4: the ingress policy admits one app: agent peer per declared domain.
ingress="$(awk 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: egress-proxy-ingress\n"){printf "%s",$0}' /tmp/proxy.out)"
[ "$(grep -c 'app: agent' <<<"$ingress")" -eq 3 ] && echo "OK: three agent peers"
for d in agent-sandbox development-sandbox trading-sandbox; do
  grep -q "kubernetes.io/metadata.name: $d" <<<"$ingress" && echo "OK: ingress admits $d" || echo "FAIL: ingress does not admit $d"
done
# No namespace-wide peer: every from: entry that has a namespaceSelector must also
# carry a podSelector.
# The record separator is the RENDERED 4-space "    - from:" marker. A 6-space
# separator never matches, the document stays one record, podSelector: is found
# somewhere in it, and the guard then passes vacuously on any render — including
# one that turned the proxy into an open proxy for the namespace.
bad=0
awk 'BEGIN{RS="    - from:"} /namespaceSelector:/{if (index($0,"podSelector:")==0) {print "FAIL: namespace-wide peer"; bad=1}} END{exit(bad?1:0)}' <<<"$ingress" && echo "OK: no namespace-wide peer" || bad=1

# Legacy: one ingress peer, proxy in the single sandbox namespace.
# Every render requires executor.kafkaBrokers and executor.sentry.dsn (or
# executor.existingSecret), so a bare `--set namespace=dev` exits 1 and writes
# nothing — the assertions below would then print no OK line at all.
cat > /tmp/proxy-legacy.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
EOF
"$HELM_BIN" template agent ./helm -f /tmp/proxy-legacy.yaml > /tmp/proxy-legacy.out
[ "$(awk 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: egress-proxy-ingress\n") && index($0,"  namespace: dev-agents-sandbox\n"){c++} END{print c+0}' /tmp/proxy-legacy.out)" -eq 1 ] && echo "OK: legacy ingress placement" || { echo "FAIL: legacy ingress placement"; bad=1; }
[ "$(grep -c 'kubernetes.io/metadata.name: dev-agents-sandbox' /tmp/proxy-legacy.out)" -ge 1 ] && echo "OK: legacy ingress admits the sandbox" || { echo "FAIL: legacy ingress does not admit the sandbox"; bad=1; }

[ "$bad" -eq 0 ] && echo "OK: all proxy checks passed" || { echo "FAIL: proxy checks"; exit 1; }
```

Every `OK` line must print, no `FAIL` line may appear, and the block must exit 0 — the trailing `[ "$bad" -eq 0 ]` is what makes that true; without it the block's exit status is whatever the last `echo` returned. `bash hack/test-helm-render.sh` must exit 0.
</verification>
