---
status: completed
spec: [062-sandbox-domains]
summary: Replaced the two hardcoded sandboxNamespace storage copies in helm/templates/agents.yaml with the per-agent agent.agentStorageNamespace resolver, computed once per agent under the existing renderAgentStorage/executor.enabled gate; domains installs now copy into the agent's declared jobNamespace or skip the copy entirely, and no-domains installs keep today's single-sandbox behaviour.
execution_id: agent-sandbox-domains-exec-257-spec-062-sandbox-domains-agent-storage
dark-factory-version: v0.198.7
created: "2026-10-24T20:38:48Z"
queued: "2026-10-24T20:57:59Z"
started: "2026-10-24T21:37:09Z"
completed: "2026-10-24T21:42:10Z"
branch: dark-factory/sandbox-domains
---

# feat(helm): place each agent's storage copy in the sandbox its `jobNamespace` names

<summary>
- With domains declared, an agent's chart-rendered Secret and PVC copy lands in the sandbox namespace that agent's `jobNamespace` names
- An agent with no `jobNamespace`, or one naming a namespace the chart does not create as a domain, gets no sandbox copy at all — no copy is better than a copy in a namespace the Job will never read
- The control-namespace Secret and PVC the executor reads stay exactly where they are
- An install with no declared domains keeps today's behaviour: the copy targets the single sandbox namespace and ignores `jobNamespace`
- The copy stays gated on `sandbox.renderAgentStorage` and `executor.enabled`, and keeps `helm.sh/resource-policy: keep`
</summary>

<objective>
Replace the two hardcoded `agent.sandboxNamespace` copies in `helm/templates/agents.yaml` with the per-agent resolver `agent.agentStorageNamespace`, so a domains install copies storage into the right namespace and skips it when there is none.
</objective>

<context>
This is prompt 5 of 6 for spec `062-sandbox-domains`; it depends on prompt 1 only (`agent.agentStorageNamespace`). It is independent of prompts 2, 3 and 4. Do not edit `helm/templates/sandbox.yaml`, `egress-proxy*.yaml`, `executor-rbac.yaml`, `helm/values.yaml`, the docs, the changelog or the render test script.

Read fully before editing:

- `helm/templates/agents.yaml` — one `Config` per enabled agent, then an optional Secret, an optional sandbox Secret copy, an optional PVC, an optional sandbox PVC copy, a PriorityClass and a ResourceQuota. The two copy blocks are the ones to change; they currently read:

  ```gotemplate
  {{- if and ($.Values.sandbox | default dict).renderAgentStorage $.Values.executor.enabled }}
  ...
    namespace: {{ include "agent.sandboxNamespace" $ }}
  ```

  The Secret copy sits inside the `{{- if $agent.secretEnv }}` block; the PVC copy sits inside the `{{- if and $agent.volumeMountPath (ne ($agent.type | default "job") "service") }}` block.
- `helm/templates/_helpers.tpl` — `agent.agentStorageNamespace` takes a dict `{"root": $, "agent": $agent}` and returns the namespace for the copy, or the empty string when no copy belongs anywhere. In domains mode it follows `jobNamespace` and requires it to be a declared domain; without domains it returns the single sandbox namespace and ignores `jobNamespace`.

Verified current behaviour the render tests pin: `storage-enabled` expects 2 PVCs and 2 Secrets, one each in `test` and `test-agents-sandbox`, for an agent with `volumeMountPath` and `secretEnv` and **no** `jobNamespace`. `storage-disabled` expects the copy suppressed by `executor.enabled: false`. `run_pvc_case` asserts `helm.sh/resource-policy: keep` at four-space indent for any PVC it expects.
</context>

<requirements>
1. **Compute the storage namespace once per agent.** Immediately after `{{- if $agent.enabled }}` at the top of the agent loop (before the first `---`/Config document), add:

   ```gotemplate
   {{- $storageNamespace := "" }}
   {{- if and ($.Values.sandbox | default dict).renderAgentStorage $.Values.executor.enabled }}
   {{- $storageNamespace = include "agent.agentStorageNamespace" (dict "root" $ "agent" $agent) }}
   {{- end }}
   ```

   `$` inside the range is the root context, which is what the helper expects. Compute it only under the existing `renderAgentStorage && executor.enabled` gate: outside that gate no copy renders, and evaluating `agent.sandboxNamespace` eagerly would make an unrelated values collision fail a render that succeeds today.

2. **Sandbox Secret copy.** Change its guard and its namespace:

   ```gotemplate
   {{- if $storageNamespace }}
   ```

   and

   ```gotemplate
   namespace: {{ $storageNamespace }}
   ```

   Update the block's comment: in domains mode the copy follows the agent's `jobNamespace` and is rendered only when that names a declared domain; with no domains it targets the single sandbox namespace regardless of `jobNamespace`. The control-namespace Secret above stays put — the executor reads it from there.

3. **Sandbox PVC copy.** The same two edits, in the PVC copy block: guard on `{{- if $storageNamespace }}` and `namespace: {{ $storageNamespace }}`. Update its comment the same way. `helm.sh/resource-policy: keep` stays on the copy.

4. **Do not change** the control-namespace Secret or PVC, the `Config` document, the PriorityClass, the ResourceQuota, the proxy-env block, or any other guard in the file.

5. **Do not add render tests.** Prompt 6 owns `hack/test-helm-render.sh`; the existing `pvc-*` and `storage-*` cases must stay green.
</requirements>

<constraints>
- In domains mode, an agent whose `jobNamespace` is absent or not a declared domain renders NO sandbox copy. That is deliberate: the Job fails `CreateContainerConfigError` in its namespace instead of reading a Secret that is not there, and the failure is visible in the namespace the Job runs in.
- With no `domains` declared, the copy keeps targeting the single sandbox namespace regardless of `jobNamespace` — the current behaviour, pinned by `storage-enabled`.
- The copy stays gated on `sandbox.renderAgentStorage` and `executor.enabled`.
- `helm.sh/resource-policy: keep` stays on the copied PVC.
- Do NOT commit — dark-factory handles git.
- `docs/dod.md` (the daemon's `validationPrompt`) asks for a `CHANGELOG.md` entry and a `README.md` update. Both are owned by prompt 6 of this spec: report them as deferred, do not edit either file, and do not report them as a blocker for this prompt.
- Do not edit `specs/`, `helm/templates/sandbox.yaml`, `helm/templates/egress-proxy.yaml`, `helm/templates/egress-proxy-allowlist.yaml`, `helm/templates/executor-rbac.yaml`, `helm/values.yaml`, `hack/test-helm-render.sh`, `CHANGELOG.md`, `helm/Chart.yaml`, `helm/README.md` or `docs/`.
</constraints>

<verification>
```bash
fail=0
command -v helm >/dev/null 2>&1 || go install helm.sh/helm/v3/cmd/helm@v3.16.4
HELM_BIN="$(command -v helm || echo "$(go env GOPATH)/bin/helm")"

# pvc-* and storage-* cases must stay green; the exit status is checked, not discarded.
make test >/dev/null || { echo "FAIL: make test exited non-zero"; fail=1; }

cat > /tmp/domains-storage.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
sandbox:
  proxyNamespace: agent-sandbox
  renderAgentStorage: true
  domains:
    agent-sandbox: {}
    trading-sandbox:
      peers: [router]
agents:
  - name: mapped-agent
    enabled: true
    assignee: mapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: trading-sandbox
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
  - name: undeclared-agent
    enabled: true
    assignee: undeclared
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: not-a-domain
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
  - name: unmapped-agent
    enabled: true
    assignee: unmapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
EOF

"$HELM_BIN" template agent ./helm -f /tmp/domains-storage.yaml \
  --show-only templates/agents.yaml > /tmp/storage.out

# AC5 positive: mapped-agent's Secret and PVC in trading-sandbox.
for kind in Secret PersistentVolumeClaim; do
  awk -v K="$kind" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: mapped-agent\n") && index($0,"  namespace: trading-sandbox\n"){f=1} END{exit(f?0:1)}' /tmp/storage.out \
    && echo "OK: $kind mapped-agent in trading-sandbox" || { echo "FAIL: $kind mapped-agent missing in trading-sandbox"; fail=1; }
done

# AC5 negative: no copy in any other sandbox, and none at all for an agent whose
# jobNamespace is absent (unmapped-agent) or names a namespace that is not a
# declared domain (undeclared-agent).
[ "$(grep -c 'namespace: agent-sandbox' /tmp/storage.out)" -eq 0 ] \
  || { echo "FAIL: a storage copy rendered in agent-sandbox"; fail=1; }

# mapped-agent: the control-namespace object plus exactly one copy, so 2 of each
# kind. unmapped-agent / undeclared-agent: the control-namespace object only, so
# exactly 1 — a second one is a copy in some namespace. Counting every object of a
# kind in the render instead would pass on a copy in the wrong place and fail on a
# correct one, since the copy adds to the total.
for kind in Secret PersistentVolumeClaim; do
  n="$(awk -v K="$kind" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: mapped-agent\n"){c++} END{print c+0}' /tmp/storage.out)"
  if [ "$n" -eq 2 ]; then echo "OK: mapped-agent has control + one $kind copy"; else echo "FAIL: mapped-agent has $n $kind (want 2: control + one copy)"; fail=1; fi
  for a in unmapped-agent undeclared-agent; do
    n="$(awk -v K="$kind" -v N="$a" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: " N "\n"){c++} END{print c+0}' /tmp/storage.out)"
    if [ "$n" -eq 1 ]; then echo "OK: $a has no sandbox $kind copy"; else echo "FAIL: $a has $n $kind (want 1: control namespace only)"; fail=1; fi
  done
done

# Legacy mode: the copy still ignores jobNamespace and targets the single sandbox.
cat > /tmp/legacy-storage.yaml <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
sandbox:
  renderAgentStorage: true
agents:
  - name: mapped-agent
    enabled: true
    assignee: mapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: somewhere-else
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
EOF
"$HELM_BIN" template agent ./helm -f /tmp/legacy-storage.yaml \
  --show-only templates/agents.yaml > /tmp/storage-legacy.out \
  || { echo "FAIL: legacy render exited non-zero"; fail=1; }
for kind in Secret PersistentVolumeClaim; do
  awk -v K="$kind" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  namespace: test-agents-sandbox\n"){f=1} END{exit(f?0:1)}' /tmp/storage-legacy.out \
    && echo "OK: legacy $kind copy targets the single sandbox" || { echo "FAIL: legacy $kind copy missing"; fail=1; }
done

[ "$fail" -eq 0 ] && echo "OK: all storage checks passed" || { echo "FAIL: storage checks"; exit 1; }
```

Every `OK` line must print and no `FAIL` line may appear. The block accumulates failures in `$fail` and ends with a non-zero exit when any assertion failed — a check that prints nothing is a failure, not a pass.
</verification>
