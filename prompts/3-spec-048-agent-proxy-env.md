---
status: draft
spec: [048-agent-egress-proxy]
created: "2026-08-21T10:40:00Z"
branch: dark-factory/agent-egress-proxy
---

<summary>
- Every agent Config the chart renders receives standard proxy environment: `HTTP_PROXY`, `HTTPS_PROXY`, and lowercase `http_proxy`/`https_proxy`, all pointing at the per-environment proxy Service URL, plus `NO_PROXY`/`no_proxy` covering in-cluster destinations
- The proxy URL is derived automatically from the environment's sandbox namespace, so dev and prod agents point at their own proxy
- In-cluster traffic (Kafka, kube-dns, sentry-proxy) stays on the direct path via `NO_PROXY` — it is never routed through the proxy
- Chart-provided proxy settings take precedence over any conflicting per-agent `env` values, so an agent cannot accidentally bypass or misroute the proxy
- Agents in installs without the proxy (executor disabled) keep their current behavior — no env is injected
- The change is template-only: no agent image, no agent Go code, and no new chart values
- The chart version is bumped, the CHANGELOG and README are updated, and the full build-time acceptance-criteria set of the spec passes
</summary>

<objective>
Point every agent the chart renders at the per-environment egress proxy through `ConfigSpec.Env` — the one mechanism the spec allows (no agent image or Go changes) — so that sandbox agent pods reach the public internet only through the domain-allowlisting proxy. After this prompt the chart's renders pass the complete spec build-time acceptance-criteria set and the change is released (version bump + changelog + README).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing:
- `helm/templates/_helpers.tpl` — the file to extend with the `agent.proxyEnv` define. MUST already contain the `agent.sandboxNamespace` define (spec 047); if it is missing, STOP and report a precondition failure.
- `helm/templates/agents.yaml` — the Config CR template whose `env:` block this prompt rewrites. Note the current block is `{{- with $agent.env }} env: {{- toYaml . | nindent 4 }} {{- end }}` (lines ~26-29) and that the file iterates `{{- range $agent := .Values.agents }}` with `$` as the root context.
- `helm/templates/egress-proxy.yaml` — created by the sibling prompt of this spec; the proxy Service is named `egress-proxy` in the sandbox namespace on port 8888, so the agent-side URL is `http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888`.
- `helm/Chart.yaml` — current `version: 0.5.2` (may already be `0.6.0` if spec 047 bumped it on this branch — bump relative to whatever it currently is).
- `CHANGELOG.md` — the changelog; add `## Unreleased` if none exists.
- `helm/README.md` — the "Generic cluster (not quant)" section (line ~186) is where the proxy note belongs.

Key facts (verified against the repo):
- `ConfigSpec.env` is a plain string map (`additionalProperties: {type: string}` in `helm/crds/config-crd.yaml`) — no schema change needed.
- The 048 post-deploy ACs assert `spec.env.HTTPS_PROXY` on the live `github-dark-factory-agent` Config equals `http://egress-proxy.dev-agents-sandbox.svc.cluster.local:8888` (dev) / `.prod-agents-sandbox...` (prod). The proxy URL MUST be derived as `http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888`.
- The `env:` map must be a valid string map when rendered. Helm's sprig `mergeOverwrite <dest> <src>` mutates and returns `dest` with `src` values overwriting on key collision. `include "agent.proxyEnv" $ | fromYaml` parses the define's YAML into a map. Sprig's `dict`, `set`, and `toYaml` are already used in this repo's chart.
- The proxy resources (and the sandbox) render only when `{{- if .Values.executor.enabled }}`. The env injection is gated on the same flag so agents only get proxy env in installs where the proxy actually exists.
- `NO_PROXY` must cover the in-cluster destinations the spec names (Kafka, kube-dns, sentry-proxy). All cluster Services resolve under `.svc.cluster.local`, so `127.0.0.1,localhost,.svc.cluster.local` covers them.
- Chart-version convention: every helm change bumps `helm/Chart.yaml` (feature → minor bump). CHANGELOG entries are dash-prefixed bullets with a `feat(helm):` prefix, newest section at top.
- This prompt changes NO Go code — run the render + greps below, not `make precommit`.
</context>

<requirements>

1. **Add an `agent.proxyEnv` define to `helm/templates/_helpers.tpl`**

   Append at the end of the file (after the existing `agent.kafkaCertVolumes` define):

   ```
   {{/* Proxy env injected into every agent Config when the egress proxy renders
        (spec 048). The proxy URL is derived from the sandbox namespace; NO_PROXY
        keeps in-cluster traffic (Kafka, kube-dns, sentry-proxy) on the direct
        path. These values override any conflicting per-agent env so an agent
        cannot bypass or misroute the proxy. */}}
   {{- define "agent.proxyEnv" -}}
   HTTP_PROXY: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
   HTTPS_PROXY: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
   http_proxy: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
   https_proxy: http://egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local:8888
   NO_PROXY: 127.0.0.1,localhost,.svc.cluster.local
   no_proxy: 127.0.0.1,localhost,.svc.cluster.local
   {{- end -}}
   ```

   Copy verbatim.

2. **Rewrite the `env:` block in `helm/templates/agents.yaml` to merge the proxy env in**

   Replace the existing block:

   ```yaml
     {{- with $agent.env }}
     env:
       {{- toYaml . | nindent 4 }}
     {{- end }}
   ```

   with:

   ```yaml
     {{- $env := dict }}
     {{- with $agent.env }}
     {{-   $env = mergeOverwrite $env . }}
     {{- end }}
     {{- if $.Values.executor.enabled }}
     {{-   $env = mergeOverwrite $env (include "agent.proxyEnv" $ | fromYaml) }}
     {{- end }}
     {{- if $env }}
     env:
       {{- toYaml $env | nindent 4 }}
     {{- end }}
   ```

   Semantics (do not change): per-agent env is merged first, then the proxy env, so the chart's proxy settings WIN on any key collision; an agent with no `env` still gets the proxy env; when `executor.enabled` is false no proxy env is injected and agents keep today's behavior. The `env:` key renders whenever the merged map is non-empty.

3. **Bump `helm/Chart.yaml` version by one minor from its CURRENT value** (feature → minor, per repo convention). If it is `0.5.2`, set `version: 0.6.0`; if spec 047 already bumped this branch to `0.6.0`, set `version: 0.7.0`. Nothing else in Chart.yaml changes.

4. **Add a CHANGELOG entry in `CHANGELOG.md`.** If a `## Unreleased` section already exists at the top, append to it; otherwise create it directly under the `# Changelog` heading. One bullet, `feat(helm):` prefix:

   ```
   ## Unreleased

   - feat(helm): add a domain-allowlisting egress proxy (tinyproxy, 2 replicas) in `<namespace>-agents-sandbox` with the allowlist + two-direction self-test carried in a ConfigMap `egress-proxy-allowlist` and wired as the readiness probe; extend `agent-sandbox-egress` to permit agent pods to the proxy on 8888 (the only internet path) and add an `egress-proxy-egress` policy (0.0.0.0/0 minus private ranges + kube-dns); inject `HTTP_PROXY`/`HTTPS_PROXY`/`http_proxy`/`https_proxy`/`NO_PROXY`/`no_proxy` into every agent Config's `spec.env`, derived per environment, with in-cluster traffic (Kafka, kube-dns, sentry-proxy) on the direct path. Chart <old>→<new>.
   ```

   Replace `<old>→<new>` with the actual version transition from step 3.

5. **Add a short note to `helm/README.md`.** In the "Generic cluster (not quant)" section, append one bullet to the existing list:

   ```
   - **Egress proxy** — the chart renders a domain-allowlisting tinyproxy (`egress-proxy`,
     port 8888) in `<namespace>-agents-sandbox` and injects `HTTP_PROXY`/`HTTPS_PROXY`/
     `no_proxy` env into every agent Config. The allowlist lives in the
     `egress-proxy-allowlist` ConfigMap — change it there and restart the proxy, no image
     rebuild. Direct internet egress from agent pods is denied except through the proxy.
   ```

   Do not restructure any other README content.

6. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT change any agent image or agent Go code — the proxy env travels through `ConfigSpec.Env` only.
   - Do NOT touch `helm/templates/egress-proxy.yaml` or `helm/templates/sandbox.yaml` in this prompt.
   - Do NOT add any new key to `helm/values.yaml` — the proxy URL derives from the required `namespace` value.
   - Do NOT add per-agent allowlist knobs, opt-out flags, or a refresh/tunable for the proxy env (spec Non-goals: one shared allowlist; per-class split is future work).
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- No agent image or agent Go code changes — proxy configuration travels through `ConfigSpec.Env` only.
- Kafka, kube-dns, and sentry-proxy stay on the direct in-cluster path via `NO_PROXY`; they must not be routed through the proxy.
- Agent pods keep `drop: ALL`; no `NET_ADMIN` is introduced.
- Changing the allowlist must not require rebuilding any image.
- The dev and prod installs must never share proxy resources — the proxy URL is per-environment.
- Existing chart behavior must not regress — agents in installs without the proxy keep today's behavior (no proxy env injected).
</constraints>

<verification>
Render with a minimal agent values overlay and assert the merged env; then re-run the FULL spec build-time AC set.

```bash
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

# 1. Proxy env injection + collision precedence
cat > /tmp/agent-values.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  existingSecret: agent-secret
agents:
  - name: test-agent
    enabled: true
    assignee: test
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    env:
      HTTPS_PROXY: http://bogus.example:9999   # must be overridden by the chart
      ALLOWED_TOOLS: Read,Grep                 # must be preserved
EOF
helm template helm/ -f /tmp/agent-values.yaml > /tmp/rendered-agent.yaml
echo "render exit: $? (must be 0)"
grep -c 'http://egress-proxy.dev-agents-sandbox.svc.cluster.local:8888' /tmp/rendered-agent.yaml  # AC10 URL: >=1
grep -c 'bogus.example' /tmp/rendered-agent.yaml                          # chart wins on collision: ==0
grep -c 'ALLOWED_TOOLS' /tmp/rendered-agent.yaml                          # per-agent env preserved: >=1
grep -c 'HTTP_PROXY:' /tmp/rendered-agent.yaml                            # >=1
grep -c 'HTTPS_PROXY:' /tmp/rendered-agent.yaml                           # >=1
grep -c 'http_proxy:' /tmp/rendered-agent.yaml                            # lowercase: >=1
grep -c 'https_proxy:' /tmp/rendered-agent.yaml                           # lowercase: >=1
grep -c 'NO_PROXY: 127.0.0.1,localhost,.svc.cluster.local' /tmp/rendered-agent.yaml  # >=1
grep -c 'no_proxy: 127.0.0.1,localhost,.svc.cluster.local' /tmp/rendered-agent.yaml  # >=1

# 2. Gate: executor disabled => NO proxy env injected
helm template helm/ -f /tmp/agent-values.yaml --set executor.enabled=false > /tmp/rendered-gated.yaml
grep -c 'HTTPS_PROXY' /tmp/rendered-gated.yaml                             # negative: ==0

# 3. FULL spec build-time AC set (all three prompts), dev + prod
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered-dev.yaml
echo "AC1 render exit: $? (must be 0)"
grep -c 'api\.minimax\.io' /tmp/rendered-dev.yaml                        # AC2: >=1
grep -c 'example\.com' /tmp/rendered-dev.yaml                            # AC3: >=1
grep -c 'api\.github\.com' /tmp/rendered-dev.yaml                        # AC3: >=1
grep -c 'curl --proxy' /tmp/rendered-dev.yaml                            # AC4: >=2
grep -c 'exit 1' /tmp/rendered-dev.yaml                                  # AC4: >=1
grep -A5 'name: egress-proxy' /tmp/rendered-dev.yaml | grep -c 'replicas: 2'   # AC5: >=1
grep -A5 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'app: agent'  # 047 AC preserved: >=1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'egress-proxy'  # >=1
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-prod.yaml
grep -c 'http://egress-proxy.prod-agents-sandbox.svc.cluster.local:8888' /tmp/rendered-prod.yaml  # AC11 URL: >=1
grep -c 'name: dev-agents-sandbox' /tmp/rendered-prod.yaml               # negative: ==0

# 4. Bookkeeping
grep -n '^version:' helm/Chart.yaml                                      # bumped
head -10 CHANGELOG.md | grep -E 'Unreleased|egress-proxy'                # must show both
grep -n 'egress-proxy' helm/README.md                                    # must show the new bullet
```

Every grep must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + greps above are the verification.

The Post-Deploy ACs (rung-2/rung-3: one real `github-dark-factory-agent` completes end-to-end through the proxy; prod block/allow pair; broken-open allowlist keeps the pod unready) are operator-executed against real clusters with `kubectlquant` after this prompt and its siblings land — they are NOT part of this prompt's verification.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **NO_PROXY form.** `127.0.0.1,localhost,.svc.cluster.local` uses the leading-dot form, which matches subdomains in curl and Go's `httpproxy` (covers Kafka, kube-dns, sentry-proxy — the spec's required direct-path destinations). If any agent tool uses a different `no_proxy` matcher (e.g. literal-only), adjust the value here.
- **Chart version bump collision with spec 047.** Both specs bump `helm/Chart.yaml` on different branches; the prompt bumps relative to the current value to avoid a hardcoded number. The two branches may still collide on the version line at merge — resolve to the higher value when merging.
- **Collision precedence decision.** The chart's proxy env wins over per-agent `env`. This keeps the shared default authoritative (spec: "one shared allowlist first"); the spec's future "per-class split" would relax this at the filter level, not the env level. If per-agent override must be possible now, flip the merge order (per-agent env wins) — flagged because it changes the fail-closed guarantee if an agent sets a bad `NO_PROXY`.
- **Env injection gating.** Injection is gated on `executor.enabled` (the same flag that renders the proxy + sandbox). An install that enables the executor but is deliberately deployed without the proxy would give agents a broken proxy URL — consistent with the sandbox policy already denying direct egress there, but worth confirming no such install exists.
