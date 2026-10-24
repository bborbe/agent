---
status: completed
spec: [062-sandbox-domains]
summary: Restructured helm/templates/sandbox.yaml into a domains branch rendering one Namespace and one <domain>-egress policy per declared domain via the peer catalog, kept the legacy single-sandbox branch verbatim, moved egress-proxy-egress to agent.proxyNamespace, and made the Kafka servicePort validation apply in both branches.
execution_id: agent-sandbox-domains-exec-254-spec-062-sandbox-domains-policies
dark-factory-version: v0.198.7
created: "2026-10-24T20:38:48Z"
queued: "2026-10-24T20:57:59Z"
started: "2026-10-24T20:59:00Z"
completed: "2026-10-24T21:12:33Z"
branch: dark-factory/sandbox-domains
---

# feat(helm): render one Namespace and one `<domain>-egress` policy per sandbox domain

<summary>
- A values file that declares sandbox domains now gets one Namespace per domain, each protected from `helm uninstall` the same way the single sandbox namespace is today
- Each domain gets its own default-deny egress policy, named `<domain>-egress`, built from that domain's peer list
- A narrowed domain cannot reach another domain's peers: the rendered selector set is exactly what the values named, plus DNS and the proxy
- Every domain can reach the single egress proxy across the namespace boundary, including domains other than the one hosting it
- The proxy's own outbound policy renders once, in the proxy's namespace
- An install that declares no domains renders exactly the sandbox namespace and policy it renders today, under the same names
</summary>

<objective>
Turn `helm/templates/sandbox.yaml` into a two-branch template: a domains branch that renders N Namespaces and N `<domain>-egress` policies through the peer catalog, and the existing single-sandbox branch kept verbatim as the default. The proxy's own `egress-proxy-egress` policy moves to `agent.proxyNamespace` in both branches.
</objective>

<context>
Read `CLAUDE.md` for project conventions. This is prompt 2 of 6 for spec `062-sandbox-domains`; it depends on prompt 1, which added `agent.sandboxDomainNames`, `agent.sandboxNamespaces`, `agent.proxyNamespace`, `agent.sandboxDomainPeers`, `agent.sandboxPeer`, `agent.agentStorageNamespace`, `agent.sandboxValidate`, the `sandbox.domains` / `sandbox.proxyNamespace` values, and the `{{- include "agent.sandboxValidate" . -}}` line at the top of `sandbox.yaml`. Read those definitions in `helm/templates/_helpers.tpl` before editing; if any is missing, stop and report that prompt 1 has not landed.

Files to read fully before editing:

- `helm/templates/sandbox.yaml` — the file you are restructuring. Today it holds: a `Namespace` for `include "agent.sandboxNamespace" .` with `helm.sh/resource-policy: keep` and an `agent.labels` block; the `agent-sandbox-egress` NetworkPolicy with eleven inline egress rules (dns, sentry-proxy, router, pushgateway, three vaults, two frontends, optional vault-api, Kafka with two branches, proxy); and the `egress-proxy-egress` NetworkPolicy in the sandbox namespace. Everything is inside `{{- if .Values.executor.enabled }}`.
- `helm/templates/_helpers.tpl` — `agent.sandboxPeer` renders one rule at zero base indentation; `agent.sandboxDomainPeers` returns the resolved comma-separated peer list for a domain.
- `hack/test-helm-render.sh` — the existing cases that pin the legacy render. `policy_doc <file> <name>` matches by `metadata.name` only, so the legacy policy MUST keep the name `agent-sandbox-egress`: ten cases (`sandbox-dev`, `sandbox-prod`, `sandbox-explicit`, `kafka-incluster`, `kafka-incluster-nolabels`, `kafka-default`, `router-override`, `vault-api`, `peer-namespace`, `peer-namespace-default`) extract that document by name.

Verified facts about the current render:

- The legacy policy's egress rules are indented four spaces under `egress:` (`    - to:`), and its `metadata.labels` block is deliberately absent — the `sandbox-dev`/`sandbox-prod` cases grep the first lines after the name.
- `run_sandbox_case` counts eight in-cluster peer selector lines (`app: sentry-proxy`, `app.kubernetes.io/instance: claude-code-router-<env>`, `app: pushgateway`, three `app: vault-obsidian-*`, `app: frontend-gateway`, `app: frontend-strategy`) across the whole render.
- `helm.sh/resource-policy: keep` on the Namespace is asserted with a bare `grep -q` over the whole render today; the domains branch needs it per document.
- The Kafka `servicePort` check currently lives inline in the legacy policy. `agent.sandboxValidate` now performs the same check with the same message; delete the inline copy in this prompt.
</context>

<requirements>
1. **Restructure `helm/templates/sandbox.yaml` into a domains branch and a legacy branch.** Keep the first line `{{- include "agent.sandboxValidate" . -}}` (added by prompt 1) exactly once. Immediately after `{{- if .Values.executor.enabled }}`, declare:

   ```gotemplate
   {{- $domains := (.Values.sandbox | default dict).domains | default dict }}
   ```

   Then open `{{- if $domains }}` … `{{- else }}` … `{{- end }}` around the Namespace-and-policy section. The `egress-proxy-egress` document stays after that `{{- end }}`, inside the executor gate, in both branches.

2. **Domains branch — Namespaces.** One document per declared domain, each beginning with a bare `---` inside the range (the `agents.yaml` pattern), each carrying `helm.sh/resource-policy: keep` and the chart labels:

   ```gotemplate
   {{- if $domains }}
   {{- range $name, $cfg := $domains }}
   ---
   # Sandbox namespace for one domain. Agent Job pods run here; the per-domain
   # policy below bounds their egress. resource-policy: keep so `helm uninstall`
   # does not delete the namespace and take operator-placed PVCs and Secrets with
   # it — removing a sandbox is a deliberate `kubectl delete ns`.
   apiVersion: v1
   kind: Namespace
   metadata:
     name: {{ $name }}
     labels:
       {{- include "agent.labels" $ | nindent 4 }}
     annotations:
       helm.sh/resource-policy: keep
   {{- end }}
   ```

   `$cfg` is unused in this loop; that is expected.

3. **Domains branch — policies.** A second range over the same map, one NetworkPolicy per domain, named `<domain>-egress` in `<domain>`, egress-only, `podSelector.matchLabels.app: agent`, and its rules generated from the peer list:

   ```gotemplate
   {{- range $name, $cfg := $domains }}
   ---
   # Default-deny egress for agent pods in this domain. Permitted destinations are
   # exactly the domain's declared peers, plus kube-dns and the egress proxy, which
   # are unconditional. Keep this comment free of peer names: the render tests
   # assert on selector lines, and a catalog word in a comment would make a
   # negative assertion pass vacuously.
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: {{ $name }}-egress
     namespace: {{ $name }}
   spec:
     podSelector:
       matchLabels:
         app: agent
     policyTypes:
       - Egress
     egress:
       {{- range $peer := (include "agent.sandboxDomainPeers" (dict "root" $ "name" $name) | splitList ",") }}
       {{- include "agent.sandboxPeer" (dict "root" $ "name" $peer) | nindent 4 }}
       {{- end }}
   {{- end }}
   ```

   The include argument is `(dict "root" $ "name" $peer)` — the root context is `$`, and the rendered block starts at four spaces, i.e. `    - to:`.

   The policy must carry NO `metadata.labels` (same reason as the legacy policy: the tests grep the first lines after the name) and must NOT declare `ingress:`.

4. **Legacy branch — unchanged.** Wrap the existing Namespace document and the existing `agent-sandbox-egress` document (their comment blocks included) in the `{{- else }}` branch verbatim. The only edit inside the legacy policy is deleting the now-redundant inline Kafka port guard:

   ```gotemplate
   {{- if not (regexMatch "^[1-9][0-9]*$" (toString .Values.sandbox.kafka.servicePort)) }}
   {{- fail "sandbox.kafka.servicePort is required as a positive integer when sandbox.kafka.namespace is set" }}
   {{- end }}
   ```

   `agent.sandboxValidate` performs the same check with the same message. Do not rename `agent-sandbox-egress`, do not move the legacy Namespace, and do not reflow the legacy rule order — ten render cases depend on the current content.

5. **`egress-proxy-egress` — render in the proxy namespace.** In the document that follows the branch close, change

   ```gotemplate
     namespace: {{ include "agent.sandboxNamespace" . }}
   ```

   to

   ```gotemplate
     namespace: {{ include "agent.proxyNamespace" . }}
   ```

   and extend its comment: the proxy renders once for every sandbox domain, in `sandbox.proxyNamespace`; without domains the helper resolves to the single sandbox namespace, so the legacy render is unchanged. Leave the rules (kube-dns, `0.0.0.0/0` minus private ranges and `169.254.0.0/16`) untouched.

6. **Do not touch** `helm/templates/egress-proxy.yaml`, `egress-proxy-allowlist.yaml`, `executor-rbac.yaml`, `agents.yaml`, `helm/values.yaml`, `helm/README.md`, `docs/`, `CHANGELOG.md`, `helm/Chart.yaml` or `hack/test-helm-render.sh`. Prompts 3–6 own those.
</requirements>

<constraints>
- `sandbox.namespace` remains supported; an install with no `domains` renders exactly the same objects under the same names.
- `helm.sh/resource-policy: keep` stays on every rendered Namespace.
- `namespaceSelector` and `podSelector` stay in the same `to:` peer for every rule — as two peers they OR and open every service on the port in that namespace. This applies to the new `proxy` peer as much as to the existing ones.
- Kafka stays three `/32` addresses or an in-cluster Service with a namespace selector — never a `/24`.
- A narrowed domain cannot reach another domain's peers; the peer set comes only from the closed catalog.
- No `metadata.labels` on the domain policies, and no peer names in their header comments.
- Do NOT commit — dark-factory handles git.
- `docs/dod.md` (the daemon's `validationPrompt`) asks for a `CHANGELOG.md` entry and a `README.md` update. Both are owned by prompt 6 of this spec: report them as deferred, do not edit either file, and do not report them as a blocker for this prompt. Its remaining items do not apply to a chart-only change.
</constraints>

<verification>
```bash
set -euo pipefail
bash hack/test-helm-render.sh   # existing cases must stay green; installs helm if absent
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
  kafka:
    namespace: kafka
    servicePort: 9092
    podLabels:
      strimzi.io/cluster: sun
  domains:
    agent-sandbox: {}
    development-sandbox:
      peers: [router, pushgateway, sentry-proxy, kafka]
    sentry-sandbox:
      peers: [router, pushgateway, kafka, vault-personal]
    trading-sandbox:
      peers: [router, pushgateway, frontend-gateway, frontend-strategy]
    research-sandbox:
      peers: [router, vault-trading]
EOF

"$HELM_BIN" template agent ./helm -f /tmp/domains.yaml \
  --show-only templates/sandbox.yaml > /tmp/sandbox-domains.out

# AC1: exactly the five declared Namespaces, SET equality — a count cannot see a
# renamed or duplicated Namespace — and `keep` checked per document, not by a
# whole-render grep (every PVC carries the same annotation).
ns_names="$(awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  name: /){sub(/^  name: /,"",a[i]); print a[i]; break}}}' /tmp/sandbox-domains.out | sort | paste -sd, -)"
[ "$ns_names" = "agent-sandbox,development-sandbox,research-sandbox,sentry-sandbox,trading-sandbox" ]
awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){c++; if (index($0,"helm.sh/resource-policy: keep")==0) bad=1} END{exit(bad||c!=5?1:0)}' /tmp/sandbox-domains.out && echo "OK namespaces"

# AC2: five policies named <domain>-egress in their own namespace.
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
  awk -v N="$d-egress" -v NS="$d" 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: " N "\n") && index($0,"  namespace: " NS "\n"){f=1} END{exit(f?0:1)}' /tmp/sandbox-domains.out || echo "FAIL: missing $d-egress in $d"
done

# AC2: trading has no sentry and no vault selector; development has both.
trading="$(awk 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: trading-sandbox-egress\n"){printf "%s",$0}' /tmp/sandbox-domains.out)"
[ "$(grep -c 'app: sentry-proxy' <<<"$trading")" -eq 0 ] && echo "OK no sentry"
[ "$(grep -c 'app: vault-obsidian-' <<<"$trading")" -eq 0 ] && echo "OK no vault"
grep -q 'app: agent' <<<"$trading" && echo "OK pod selector"
dev="$(awk 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: development-sandbox-egress\n"){printf "%s",$0}' /tmp/sandbox-domains.out)"
grep -q 'app: sentry-proxy' <<<"$dev" && grep -q 'metadata.name: kafka' <<<"$dev" && echo "OK development peers"

# AC4: proxy policy peer carries both selectors in one to: entry, in every domain.
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
  doc="$(awk -v N="$d-egress" 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: " N "\n"){printf "%s",$0}' /tmp/sandbox-domains.out)"
  peer="$(awk 'BEGIN{RS="    - to:"} /app: egress-proxy/{print; exit}' <<<"$doc")"
  grep -q 'namespaceSelector:' <<<"$peer" && grep -q 'metadata.name: agent-sandbox' <<<"$peer" && grep -q 'port: 8888' <<<"$peer" || echo "FAIL: proxy peer in $d"
done
grep -q 'name: egress-proxy-egress' /tmp/sandbox-domains.out && awk 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy\n") && index($0,"  name: egress-proxy-egress\n") && index($0,"  namespace: agent-sandbox\n"){f=1} END{exit(f?0:1)}' /tmp/sandbox-domains.out && echo "OK proxy egress placement"

# AC10: a domain the spec never names reads its peers from values; catalog name
# never appears in the output, its rendered selector does; omitted peers = union.
awk -v N="research-sandbox-egress" 'BEGIN{RS="---"} index($0,"  name: " N "\n"){printf "%s",$0}' /tmp/sandbox-domains.out > /tmp/research.out
grep -q 'app: vault-obsidian-trading' /tmp/research.out && echo "OK research vault"
[ "$(grep -c 'vault-trading' /tmp/research.out)" -eq 0 ] && echo "OK no catalog word"
grep -q 'k8s-app: kube-dns' /tmp/research.out && echo "OK research dns"
[ "$(grep -c 'app: pushgateway' /tmp/research.out)" -eq 0 ] && echo "OK research no pushgateway"
[ "$(grep -c 'metadata.name: kafka' /tmp/research.out)" -eq 0 ] && echo "OK research no kafka"

# AC10 (second clause): `agent-sandbox: {}` omits `peers`, so it must render the
# full union — the behaviour that keeps the first apply of a domains install from
# narrowing anyone's reach. Without this, a template that renders only dns+proxy
# for an omitted key passes every assertion above.
union="$(awk 'BEGIN{RS="---"} index($0,"  name: agent-sandbox-egress\n"){printf "%s",$0}' /tmp/sandbox-domains.out)"
[ -n "$union" ]
for s in 'app: sentry-proxy' 'app: pushgateway' 'app: vault-obsidian-personal' 'app: vault-obsidian-openclaw' 'app: vault-obsidian-trading' 'app: frontend-gateway' 'app: frontend-strategy' 'metadata.name: kafka'; do
  grep -q "$s" <<<"$union"
done
grep -q 'app.kubernetes.io/instance: claude-code-router-dev' <<<"$union"
echo "OK union domain"

# Legacy branch unchanged: one Namespace, policy still named agent-sandbox-egress.
# The render requires executor.kafkaBrokers and one of executor.sentry.dsn /
# executor.existingSecret; omitting kafkaBrokers exits 1 and writes nothing, which
# would silently skip every legacy assertion below.
"$HELM_BIN" template agent ./helm --set namespace=dev --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret \
  --show-only templates/sandbox.yaml > /tmp/sandbox-legacy.out
awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  name: /){sub(/^  name: /,"",a[i]); print a[i]; break}}}' /tmp/sandbox-legacy.out
# expect exactly: dev-agents-sandbox
grep -q 'name: agent-sandbox-egress' /tmp/sandbox-legacy.out && echo "OK legacy policy name"
[ "$(grep -c 'app: sentry-proxy' /tmp/sandbox-legacy.out)" -ge 1 ] && echo "OK legacy sentry peer"
```

The Namespace name list must equal exactly the five declared names, the per-document `keep` check must pass, every `OK` line must print, and `bash hack/test-helm-render.sh` must exit 0. Do not run `make precommit`.
</verification>
