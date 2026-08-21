---
status: draft
spec: [048-agent-egress-proxy]
created: "2026-08-21T10:30:00Z"
branch: dark-factory/agent-egress-proxy
---

<summary>
- The chart renders a tinyproxy Deployment, a Service, and an allowlist ConfigMap in the per-environment sandbox namespace (`<env>-agents-sandbox`), so the dev and prod installs each own a separate proxy
- The proxy runs two replicas and listens on port 8888, so one pod's loss does not take down agent internet egress
- The allowlist lives in a ConfigMap as plain data (domain list + self-test script), so changing it is a ConfigMap edit, never an image rebuild
- The proxy is configured to deny by default and permit only the domains on the allowlist, filtering on the CONNECT line with no TLS interception
- A two-direction self-test (a non-allowlisted domain must fail, an allowlisted one must succeed) runs as the pod's readiness probe from a dedicated curl sidecar
- A broken-open or over-tight allowlist keeps the proxy pod un-ready instead of serving traffic — the designed failure mode from the spec
- The proxy Service is named `egress-proxy`, reachable by agents at `egress-proxy.<env>-agents-sandbox.svc.cluster.local:8888`
- The dev and prod renders both pass the spec's build-time acceptance criteria for the proxy
</summary>

<objective>
Render a two-replica tinyproxy egress proxy (Deployment + Service + allowlist ConfigMap carrying the domain allowlist, the tinyproxy config, and the two-direction self-test) inside the per-environment sandbox namespace, so that agent pods have a single, domain-allowlisted, fail-closed path to the public internet. After this prompt the chart's dev and prod renders each contain the proxy resources and the spec's proxy-related build-time acceptance criteria pass.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing (they establish the template conventions this change must follow):
- `docs/agent-network-security.md` — the background document the spec cites; its "The self-test is not optional" section gives the exact two-direction probe (`curl --proxy ... https://example.com` must fail, `curl --proxy ... https://api.github.com/zen` must succeed) and its reference policy shows the future end-state proxy rule.
- `helm/templates/_helpers.tpl` — MUST already contain the `agent.sandboxNamespace` define (spec 047). Verify it exists before editing; see the precondition below.
- `helm/templates/sandbox.yaml` — MUST already exist (spec 047). It renders the `dev-agents-sandbox` Namespace and the `agent-sandbox-egress` NetworkPolicy; the proxy lives in that namespace and the NetworkPolicy is extended by a later prompt of this same spec (048), NOT here.
- `helm/templates/executor-deployment.yaml` — shows the chart's container `securityContext` hardening style (allowPrivilegeEscalation false, capabilities drop ALL, seccomp RuntimeDefault) that the proxy container should mirror.
- `helm/templates/agents.yaml` — the Config CR template; NOT touched in this prompt (the sibling prompt of this spec injects proxy env vars there).

Key facts (verified against the repo):
- The chart requires `namespace`, `executor.kafkaBrokers`, `executor.existingSecret` — a bare `helm template helm/` exits 1. Every render/grep in this prompt MUST use the spec's RENDER variable values.
- The spec's build-time ACs all run against this exact render:
  `helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret`
- The sandbox namespace helper is `{{ include "agent.sandboxNamespace" . }}` → `<env>-agents-sandbox` (from spec 047). The proxy's Service FQDN that agents will use is `egress-proxy.{{ include "agent.sandboxNamespace" . }}.svc.cluster.local`.
- The proxy container image is `monokal/tinyproxy:latest`. Verified mechanics of this image (do not rely on anything else from it): config is generated from env vars; `Filter=<path>` points at a mounted filter (allowlist) file; `FilterDefaultDeny=Yes` + `FilterURLs=On` make the filter an allowlist applied to the CONNECT host; the default port is 8888; the FIRST positional arg is the client ACL and must be present (`ANY` = allow all sources — the k8s NetworkPolicy is what actually gates who reaches the proxy). The image's baked `/etc/tinyproxy/tinyproxy.conf` is read by its entrypoint, so DO NOT mount anything over the `/etc/tinyproxy` directory (that would hide the baked config and the container would fail to start) — mount the allowlist file at a different path.
- The self-test sidecar image is `curlimages/curl:8.21.0` (Alpine-based: ships `curl`, `/bin/sh`, and CA certificates; overridden command, so its image entrypoint does not matter).
- The AC `$RENDER | grep -A5 'name: egress-proxy' | grep -c 'replicas: 2'` requires `replicas: 2` within 5 lines of the Deployment's `name:` line. Like spec 047's NetworkPolicy, the Deployment MUST carry NO `metadata.labels` block (a labels block would push `replicas` out of the grep window). The Service and ConfigMap keep the chart-standard `agent.labels`.
- The AC `grep -c 'curl --proxy'` must return ≥2, `grep -c 'exit 1'` ≥1, `grep -c 'example\.com'` ≥1, `grep -c 'api\.github\.com'` ≥1 — these all come from the self-test script carried in the ConfigMap, so the script MUST contain two literal `curl --proxy` invocations, an `exit 1`, `https://example.com`, and `https://api.github.com/zen`.
- The Post-Deploy AC for the broken-open allowlist greps `readinessProbe.exec.command` for `selftest` — the readiness probe's exec command array MUST contain the string `selftest`.
- This prompt changes NO Go code and NO values keys — run the render + greps below, not `make precommit`.

**Precondition (spec 047 must be merged on this branch):** `helm/templates/_helpers.tpl` MUST define `agent.sandboxNamespace`, and `helm/templates/sandbox.yaml` MUST exist. If either is missing, STOP and report a precondition failure — do NOT recreate the helper, the sandbox namespace, or the `agent-sandbox-egress` policy (spec 047 owns those; recreating them here conflicts on merge).
</context>

<requirements>

1. **Create `helm/templates/egress-proxy.yaml`** with exactly three documents — ConfigMap `egress-proxy-allowlist`, Deployment `egress-proxy`, Service `egress-proxy` — all inside a single `{{- if .Values.executor.enabled }}` gate (same gate as `helm/templates/sandbox.yaml`). The full file:

   ```yaml
   {{- if .Values.executor.enabled }}
   # Egress proxy (spec 048): the ONLY path from a sandbox agent pod to the public
   # internet. tinyproxy filters the CONNECT host:port against the allowlist below
   # (FilterDefaultDeny=Yes => everything not listed is denied with a 403). No TLS
   # interception, no certificate injection.
   #
   # The allowlist + config + self-test all live in this ConfigMap so changing the
   # allowlist is an edit + proxy restart, never an image rebuild. NOTE: the
   # Deployment intentionally carries NO metadata.labels — the spec's acceptance
   # criteria grep 'replicas: 2' within 5 lines of the name line (same deliberate
   # divergence as the 047 NetworkPolicy).
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: egress-proxy-allowlist
     namespace: {{ include "agent.sandboxNamespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   data:
     # Domain allowlist (tinyproxy Filter file). One regex per line, matched against
     # the CONNECT host:port; FilterDefaultDeny=Yes denies everything not listed.
     #
     # >>> SEED REQUIRED: replace/extend this list with claude-yolo's
     #     files/tinyproxy-allowlist (extended with api.minimax.io) before deploy. <<<
     # (Reviewer must paste the curated seed; the entries below are the spec-mandated
     # minimum + the domains the platform's own agents actually call.)
     tinyproxy-allowlist: |
       api.anthropic.com
       api.minimax.io
       github.com
       api.github.com
       codeload.github.com
       raw.githubusercontent.com
     # Two-direction self-test. The readiness probe on the selftest sidecar runs
     # this; a broken-open allowlist (negative check) or an over-tight allowlist
     # (positive check) keeps the pod un-ready (spec failure mode).
     selftest.sh: |
       #!/bin/sh
       set -u
       PROXY="http://127.0.0.1:8888"
       # Negative: a non-allowlisted domain MUST fail (tinyproxy 403 => curl --fail
       # exits 22). Without --fail, a 403 is still exit 0 and this check is a no-op.
       if curl --fail --proxy "$PROXY" --connect-timeout 5 https://example.com >/dev/null 2>&1; then
         echo "selftest FAIL: non-allowlisted https://example.com was allowed (allowlist broken open)" >&2
         exit 1
       fi
       # Positive: an allowlisted domain MUST succeed.
       if ! curl --fail --proxy "$PROXY" --connect-timeout 5 -o /dev/null https://api.github.com/zen; then
         echo "selftest FAIL: allowlisted https://api.github.com/zen was blocked (allowlist too tight)" >&2
         exit 1
       fi
       echo "selftest OK"
   ---
   # No metadata.labels on this Deployment (see the note above the ConfigMap).
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: egress-proxy
     namespace: {{ include "agent.sandboxNamespace" . }}
   spec:
     replicas: 2
     selector:
       matchLabels:
         app: egress-proxy
     template:
       metadata:
         labels:
           app: egress-proxy
       spec:
         containers:
           - name: egress-proxy
             image: monokal/tinyproxy:latest
             args: ["ANY"]   # ACL: allow all sources; NetworkPolicy gates who reaches it
             env:
               - name: Filter
                 value: /allowlist/tinyproxy-allowlist
               - name: FilterDefaultDeny
                 value: "Yes"
               - name: FilterURLs
                 value: "On"
             volumeMounts:
               - name: proxy-files
                 mountPath: /allowlist
                 readOnly: true
             securityContext:
               allowPrivilegeEscalation: false
               capabilities:
                 drop: [ALL]
               seccompProfile:
                 type: RuntimeDefault
           - name: selftest
             image: curlimages/curl:8.21.0
             command: ["/bin/sh", "-c", "sleep infinity"]
             volumeMounts:
               - name: proxy-files
                 mountPath: /allowlist
                 readOnly: true
             readinessProbe:
               exec:
                 command: ["/bin/sh", "/allowlist/selftest.sh"]
               initialDelaySeconds: 5
               periodSeconds: 60
               timeoutSeconds: 25
               failureThreshold: 3
               successThreshold: 1
             securityContext:
               allowPrivilegeEscalation: false
               capabilities:
                 drop: [ALL]
               seccompProfile:
                 type: RuntimeDefault
         volumes:
           - name: proxy-files
             configMap:
               name: egress-proxy-allowlist
   ---
   apiVersion: v1
   kind: Service
   metadata:
     name: egress-proxy
     namespace: {{ include "agent.sandboxNamespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   spec:
     selector:
       app: egress-proxy
     ports:
       - name: http
         port: 8888
         targetPort: 8888
   {{- end }}
   ```

   Copy this file verbatim. Do not reformat the flow-mapped port/spec lines. Do not move `replicas: 2` — its position is asserted by the spec AC.

2. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT add TLS interception / `ssl_bump` — the CONNECT-host filter is the whole mechanism.
   - Do NOT add per-agent allowlists — one shared allowlist.
   - Do NOT touch `helm/templates/sandbox.yaml` or `helm/templates/_helpers.tpl` in this prompt — the NetworkPolicy extension and env injection ship in the sibling prompts of this spec; the `agent.sandboxNamespace` helper is a spec-047 precondition.
   - Do NOT mount anything over the image's baked `/etc/tinyproxy` directory (hides the baked config and breaks the monokal entrypoint).
   - Do NOT add `metadata.labels` to the `egress-proxy` Deployment (breaks the `replicas: 2` AC grep window).
   - Do NOT add any new key to `helm/values.yaml` — the proxy URL/namespace derive from the existing required `namespace` value.
   - Do NOT change any Go code, any agent image, or any agent pod security context (`drop: ALL`, no `NET_ADMIN`).
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- No agent image or agent Go code changes — proxy configuration travels through `ConfigSpec.Env` only (handled by a sibling prompt).
- Agent pods keep `drop: ALL`; no `NET_ADMIN` is introduced — the proxy is a separate pod, not a sidecar in the agent pod.
- Changing the allowlist must not require rebuilding any image — the allowlist is ConfigMap data.
- The internal-egress rules from `agent-sandbox-namespace.md` (spec 047) must not be loosened — this prompt adds no NetworkPolicy at all; the sibling prompt does.
- The dev and prod installs must never share proxy resources — everything lands in the parameterized `{{ .Values.namespace }}-agents-sandbox` namespace.
- Existing chart behavior must not regress — the sandbox namespace, policy, and RBAC from spec 047 must still render.
- No TLS interception — filtering on the CONNECT host only.
</constraints>

<verification>
Render the chart and run the spec's build-time ACs for the proxy. The executor container may not ship `helm` and `get.helm.sh` is unreachable from the build network (403) — install helm from the Go proxy (verified working) before rendering:

```bash
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
helm version --short   # must print v3.16

RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered-dev.yaml
echo "AC1 render exit: $? (must be 0)"
grep -c 'api\.minimax\.io' /tmp/rendered-dev.yaml                    # AC2: >=1
grep -c 'example\.com' /tmp/rendered-dev.yaml                        # AC3: >=1
grep -c 'api\.github\.com' /tmp/rendered-dev.yaml                    # AC3: >=1
grep -c 'curl --proxy' /tmp/rendered-dev.yaml                        # AC4: >=2
grep -c 'exit 1' /tmp/rendered-dev.yaml                              # AC4: >=1
grep -A5 'name: egress-proxy' /tmp/rendered-dev.yaml | grep -c 'replicas: 2'   # AC5: >=1
grep -A8 'readinessProbe' /tmp/rendered-dev.yaml | grep -c 'selftest'          # AC9 build-time: >=1
grep -c 'name: egress-proxy-allowlist' /tmp/rendered-dev.yaml        # >=1
grep -c 'port: 8888' /tmp/rendered-dev.yaml                          # >=1
grep -c 'name: dev-agents-sandbox' /tmp/rendered-dev.yaml            # 047 still renders: >=1
```

Then the per-environment isolation:

```bash
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-prod.yaml
grep -c 'name: prod-agents-sandbox' /tmp/rendered-prod.yaml          # >=1
grep -c 'name: dev-agents-sandbox' /tmp/rendered-prod.yaml           # negative: ==0
```

Every grep must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + AC greps above are the verification.

The Post-Deploy ACs (rung-2/rung-3: example.com blocked from a real pod, api.github.com allowed, direct-egress denied, broken-open allowlist keeps the pod unready, one real agent completes end-to-end) are operator-executed against real clusters with `kubectlquant` after this prompt AND its siblings AND the executor's sandbox rollout all land — they are NOT part of this prompt's verification.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **Allowlist seed content (CRITICAL).** The spec says the ConfigMap is "seeded from claude-yolo's `files/tinyproxy-allowlist`, extended with `api.minimax.io`". That file lives in a local workspace that is NOT mounted in this repo or the YOLO container, so the executor cannot read it. The template ships a minimal functional seed (the spec-mandated `api.minimax.io` + the domains the platform's own agents actually call). Before approving/deploying, paste claude-yolo's real `files/tinyproxy-allowlist` contents (plus `api.minimax.io`) into the `tinyproxy-allowlist` key of `helm/templates/egress-proxy.yaml`.
- **Proxy image choice.** `monokal/tinyproxy:latest` was chosen because its config mechanics are verifiable (env-driven `Filter`/`FilterDefaultDeny`/`FilterURLs`, ACL as first arg, default port 8888). Two consequences: (a) the tag is unpinned `latest` — consider pinning an immutable tag; (b) `MaxClients` and `ConnectPort` are NOT env-configurable on this image (baked into its `/etc/tinyproxy/tinyproxy.conf`), so the spec's failure-mode recovery "raise MaxClients in the ConfigMap" is unavailable — the recovery becomes "scale replicas". If ConfigMap-editable `MaxClients` is required, switch to an image that loads a full mounted `tinyproxy.conf` (e.g. the official tinyproxy container) — unverified from here, so the reviewer must confirm its entrypoint.
- **Spec AC#4 deploy_check anomaly.** The "non-allowlisted domain is blocked" AC's `deploy_check` compares `containers[0].image` tag to `$(git rev-parse --short HEAD)`, which is meaningless for a third-party tinyproxy image and would never match. Likely a copy-paste artifact from a bborbe-image AC. Recommend re-pointing that deploy_check (e.g. to the `egress-proxy-allowlist` ConfigMap data or the Service) or accepting it as a deploy-marker no-op.
- **Filter regex anchoring.** The allowlist uses plain hostname lines (tinyproxy regex substring match, consistent with claude-yolo's "domain allowlist regex" file). A substring match means `api.github.com` also matches `api.github.com.evil.com`. Anchoring (`(^|\.)api\.github\.com(:443)?$`) would tighten it but adds port-aware complexity; the enforcement layer (NetworkPolicy: only the proxy pod reaches the internet) bounds the blast radius either way. Reviewer decides against claude-yolo's proven file.
- **Proxy pod runs as root with drop ALL.** `monokal/tinyproxy` runs as root; the container carries the chart-standard `capabilities.drop: [ALL]` + no privilege escalation. tinyproxy is a userspace TCP forwarder on a non-privileged port and should not need capabilities; if it fails to start, relax deliberately (the spec only constrains AGENT pods, not the proxy pod).
- **Self-test sidecar shell assumption.** `curlimages/curl:8.21.0` is Alpine-based (ships `/bin/sh`, `curl`, CA certs). If the audit prefers an image with a guaranteed long-lived command, `sleep infinity` (busybox) is valid on Alpine.
