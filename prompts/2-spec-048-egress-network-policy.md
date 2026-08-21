---
status: draft
spec: [048-agent-egress-proxy]
created: "2026-08-21T10:35:00Z"
branch: dark-factory/agent-egress-proxy
---

<summary>
- The sandbox default-deny policy gains one explicit rule: agent pods may reach the proxy pod on port 8888 — the only path to the public internet
- Direct internet egress from agent pods remains denied exactly as before; nothing else is loosened
- The proxy pod gets its own egress policy: it may reach the public internet (0.0.0.0/0) but not private ranges, the cloud metadata endpoint (169.254.0.0/16), or any in-cluster service
- The proxy's policy also keeps cluster DNS (kube-dns, 53) reachable so allowlisted hostnames can resolve — without this the proxy cannot resolve anything and its self-test would fail
- The spec 047 acceptance criteria for the sandbox policy are preserved: it still selects `app: agent`, still declares Egress-only, still renders per-environment
- The dev and prod renders both pass the policy checks
</summary>

<objective>
Complete the network layer of the egress proxy: extend `agent-sandbox-egress` so agent pods can reach the proxy on 8888 (and nothing else changes), and give the proxy pod its own egress policy that permits the public internet minus private ranges plus kube-dns. After this prompt the block/allow pair is enforced by policy, not merely configured.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing:
- `docs/agent-network-security.md` — the background document; its "NetworkPolicy reference" section shows the future end-state `agent-sandbox-egress` including the `app: egress-proxy` :8888 rule and the "proxy pod carries its own policy allowing egress to 0.0.0.0/0 minus private ranges" requirement (DB 5).
- `helm/templates/sandbox.yaml` — the file this prompt extends. It was created by spec 047 and MUST already exist with the `agent-sandbox-egress` NetworkPolicy (kube-dns 53 UDP+TCP rule, then strimzi Kafka 9092 rule). If it is missing, STOP and report a precondition failure — do not recreate it (spec 047 owns it).
- `helm/templates/egress-proxy.yaml` — created by the sibling prompt of this spec (execution order: this prompt runs after it). The proxy Deployment's pod template carries `labels: {app: egress-proxy}` — the selector both new policies depend on.
- `helm/templates/_helpers.tpl` — the `agent.sandboxNamespace` helper (spec 047) that all policies use for the namespace.

Key facts (verified against the repo):
- `agent-sandbox-egress` lives in `helm/templates/sandbox.yaml`, namespace `{{ include "agent.sandboxNamespace" . }}`, selects `app: agent`, `policyTypes: [Egress]`, and currently has exactly two egress rules (kube-dns, strimzi Kafka). The spec 047 acceptance criteria that MUST keep passing are:
  - `grep -A5 'name: agent-sandbox-egress' | grep -c 'app: agent'` ≥ 1 (podSelector stays within 5 lines of the name line — do not add labels or reorder metadata)
  - `grep -A30 'name: agent-sandbox-egress' | grep -c 'policyTypes'` == 1 and `grep -A30 ... | grep -c 'ingress:'` == 0
  - per-environment: `dev-agents-sandbox` only in the dev render, `prod-agents-sandbox` only in the prod render
- The 048 post-deploy AC "direct egress bypassing the proxy is denied" checks the live policy's egress podSelector labels contain `egress-proxy` — the agent→proxy rule must use `podSelector: { matchLabels: { app: egress-proxy } }` and port 8888.
- The existing rule blocks in `sandbox.yaml` use flow-mapped ports (`{ protocol: UDP, port: 53 }` style) and `kubernetes.io/metadata.name` for namespaceSelector — match that style exactly.
- The proxy pod's egress policy MUST include a kube-dns rule (53 UDP+TCP) in addition to the 0.0.0.0/0-minus-private rule: kube-dns resolves at a cluster IP inside 10.0.0.0/8, which the `except` ranges would otherwise deny, and the proxy's own self-test would fail without DNS.
- This prompt changes NO Go code and NO values keys — run the render + greps below, not `make precommit`.
</context>

<requirements>

1. **Extend `helm/templates/sandbox.yaml`: add the agent→proxy rule to `agent-sandbox-egress`**

   Inside the existing `agent-sandbox-egress` policy's `egress:` list, insert a new rule block BETWEEN the kube-dns rule and the strimzi Kafka rule, exactly:

   ```yaml
       # The only path to the public internet: the domain-allowlisting egress proxy
       # (spec 048). Direct internet stays denied by omission.
       - to:
           - podSelector:
               matchLabels:
                 app: egress-proxy
         ports:
           - { protocol: TCP, port: 8888 }
   ```

   Do not reorder the kube-dns or Kafka rules. Do not add labels to the policy, do not change `policyTypes`, do not add ingress. The rule count becomes three (DNS, proxy, Kafka) matching `docs/agent-network-security.md`'s reference policy.

2. **Add a new NetworkPolicy `egress-proxy-egress` to `helm/templates/sandbox.yaml`**

   Append a second policy document inside the same `{{- if .Values.executor.enabled }}` gate, after the `agent-sandbox-egress` document, exactly:

   ```yaml
   ---
   # Egress policy for the proxy pod itself (spec 048 DB 5): it may reach the public
   # internet (0.0.0.0/0) minus private ranges, plus kube-dns so it can resolve
   # allowlisted hostnames. The cloud metadata endpoint (169.254.0.0/16) and every
   # in-cluster service are denied by the except ranges. Intentionally carries NO
   # metadata.labels, consistent with agent-sandbox-egress.
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: egress-proxy-egress
     namespace: {{ include "agent.sandboxNamespace" . }}
   spec:
     podSelector:
       matchLabels:
         app: egress-proxy
     policyTypes:
       - Egress
     egress:
       # kube-dns — required for the proxy to resolve allowlisted hostnames
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
       # Public internet minus private ranges (169.254.0.0/16 = metadata endpoint)
       - to:
           - ipBlock:
               cidr: 0.0.0.0/0
               except:
                 - 10.0.0.0/8
                 - 172.16.0.0/12
                 - 192.168.0.0/16
                 - 169.254.0.0/16
   ```

   Copy verbatim. Do not add `ports` to the internet rule (DB 5 specifies the destination, not ports; the tinyproxy CONNECT-port restriction is the layer-7 control).

3. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT loosen or remove any existing rule in `agent-sandbox-egress` — the internal-egress rules from `agent-sandbox-namespace.md` must not be weakened to accommodate the proxy.
   - Do NOT add ingress rules anywhere.
   - Do NOT add per-agent egress policies or any rule that would let an agent reach anything except kube-dns, the proxy, and Kafka.
   - Do NOT add `metadata.labels` to either policy.
   - Do NOT touch `helm/templates/egress-proxy.yaml`, `helm/templates/_helpers.tpl`, or any Go code in this prompt.
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- No agent image or agent Go code changes.
- Kafka result publishing must not regress — the Kafka 9092 rule is untouched.
- Agent pods keep `drop: ALL`; no `NET_ADMIN` is introduced.
- The internal-egress rules from `agent-sandbox-namespace.md` must not be loosened to accommodate the proxy.
- Direct internet egress from agent pods stays denied — the proxy rule is the ONLY new permitted destination.
- The dev and prod installs must never share a NetworkPolicy — every policy is in the parameterized `{{ .Values.namespace }}-agents-sandbox` namespace.
- No TLS interception — the proxy filters the CONNECT line; the policy layer only controls reachability.
</constraints>

<verification>
Render the chart (helm from the Go proxy if missing, as in the sibling prompt) and check that both the 047 ACs and the new 048 policy rules hold:

```bash
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered-dev.yaml
echo "render exit: $? (must be 0)"

# 047 ACs preserved
grep -A5 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'app: agent'   # >=1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'policyTypes' # ==1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'ingress:'     # ==0

# 048: agent -> proxy rule present
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'egress-proxy' # >=1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'port: 8888'   # >=1

# 048: proxy pod's own policy
grep -A8 'name: egress-proxy-egress' /tmp/rendered-dev.yaml | grep -c 'Egress'         # >=1
grep -A40 'name: egress-proxy-egress' /tmp/rendered-dev.yaml | grep -c 'kube-dns'      # >=1 (DNS exception)
grep -A40 'name: egress-proxy-egress' /tmp/rendered-dev.yaml | grep -c '0.0.0.0/0'     # >=1
grep -A40 'name: egress-proxy-egress' /tmp/rendered-dev.yaml | grep -c '169.254.0.0/16' # >=1 (metadata endpoint denied)

# Per-environment isolation
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-prod.yaml
grep -c 'name: egress-proxy-egress' /tmp/rendered-prod.yaml  # >=1
grep -c 'namespace: dev-agents-sandbox' /tmp/rendered-prod.yaml  # negative: ==0
grep -c 'namespace: prod-agents-sandbox' /tmp/rendered-prod.yaml # >=1
```

Every grep must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + greps above are the verification.

The Post-Deploy ACs (rung-2: non-allowlisted domain blocked, allowlisted domain succeeds, direct egress denied, broken-open allowlist keeps the pod unready; rung-3: same pair in prod) are operator-executed against real clusters with `kubectlquant` after this prompt and its siblings land — they are NOT part of this prompt's verification.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **DNS exception inside the proxy's egress policy.** DB 5 says the proxy policy allows "0.0.0.0/0 minus private CIDRs". kube-dns resolves at a cluster IP inside 10.0.0.0/8, so a literal reading of DB 5 would deny DNS and break the proxy's self-test. This prompt adds the kube-dns 53 rule as a required exception. Confirm this is the intended reading (it matches the `agent-sandbox-egress` pattern).
- **No port restriction on the proxy's internet rule.** The internet egress rule has no `ports`, per DB 5's literal wording. The L7 port restriction is the tinyproxy default CONNECT ports (443/563). If the reviewer wants port-level restriction at the policy layer too, add `ports: [443, 80]` to the internet rule.
- **Ordering / branch dependency on spec 047.** This prompt edits `helm/templates/sandbox.yaml`, which only exists after spec 047 merges. The operator must sequence 047 (and the sibling 048 resource prompt) before this prompt executes; if the file is missing the executor reports a precondition failure by design.
