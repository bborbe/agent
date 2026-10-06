---
status: completed
summary: Rendered per-agent zombieJobTimeoutSeconds in the Helm chart, bumped chart to 0.6.7, and updated CHANGELOG/README/values docs
execution_id: agent-zombie-job-timeout-exec-227-render-per-agent-job-deadline
dark-factory-version: v0.196.0
created: "2026-10-06T00:00:00Z"
queued: "2026-10-06T06:07:18Z"
started: "2026-10-06T06:07:34Z"
completed: "2026-10-06T06:08:51Z"
---

# Render the per-agent Job deadline in the chart

<summary>
- The chart can render a per-agent Job deadline, so an agent whose work outlives the default can keep its Job alive
- The deadline is a per-agent Config field the executor already stamps onto every Job it spawns
- That field already exists in the CRD and in the running executor — only the chart's render is missing, so this is a template-only change
- An agent that does not set the value renders exactly as it does today: the executor's default deadline stays in force
- The chart version is bumped, the CHANGELOG and README are updated
- No agent image, no agent Go code, and no CRD change
</summary>

<objective>
Let a values overlay raise one agent's Job deadline by rendering `spec.zombieJobTimeoutSeconds` from the `agents` list, so a long-running agent — one that waits on sibling tasks — is no longer killed by the executor's 1800s default before its own work completes. After this prompt the chart renders the field when an agent sets it and omits it when unset, and the change is released (version bump + changelog + README).
</objective>

<context>
Read CLAUDE.md for project conventions and `docs/dod.md` for the Definition of Done (a config change updates the README and adds a CHANGELOG entry under `## Unreleased`).

Files:
- `helm/templates/agents.yaml` — renders one `Config` CR per enabled `agents` entry. The `maxConcurrentJobs` block is the exact pattern to mirror.
- `helm/Chart.yaml` — current `version: 0.6.6`. Bump relative to whatever it currently is.
- `CHANGELOG.md` — the changelog; add `## Unreleased` if none exists.
- `helm/README.md` — the `### agents (leaf agents — values-driven)` section is where the new field belongs.
- `helm/values.yaml` — the commented `agents:` example.

Key facts (verified against the repo):
- `spec.zombieJobTimeoutSeconds` is already declared in `helm/crds/config-crd.yaml` as `{type: integer, minimum: 30}` — the CRD carries it, so NO CRD change is needed.
- The field is a sibling of `maxConcurrentJobs` in `ConfigSpec` — both sit directly under `spec`, not nested.
- The executor stamps the value onto `Job.Spec.ActiveDeadlineSeconds`; when unset it uses its compiled default of 1800 seconds.
- `helm/templates/agents.yaml` currently renders `maxConcurrentJobs` guarded by `{{- if $agent.maxConcurrentJobs }}`; mirror that shape exactly.
- The closest precedent is the `maxConcurrentJobs` render (`CHANGELOG.md` § v0.81.0): a **patch** bump (`chart 0.5.0→0.5.1`), a `feat(helm):` bullet, and "emitted only when present, so no existing agent's rendered Config changes".
- This prompt changes NO Go code — run the render + greps below, not `make precommit`.
</context>

<requirements>

1. **Render `zombieJobTimeoutSeconds` in `helm/templates/agents.yaml`**

   Immediately after the existing `maxConcurrentJobs` block:

   ```yaml
     {{- if $agent.maxConcurrentJobs }}
     maxConcurrentJobs: {{ $agent.maxConcurrentJobs }}
     {{- end }}
   ```

   insert:

   ```yaml
     {{- if $agent.zombieJobTimeoutSeconds }}
     zombieJobTimeoutSeconds: {{ $agent.zombieJobTimeoutSeconds }}
     {{- end }}
   ```

   Semantics (do not change): the key renders only when the agent sets it; an agent that does not set it renders no key at all, so the executor's 1800s default stays in force unchanged. Do not default the value in the chart — a chart-side default would silently change every existing agent's deadline.

2. **Bump `helm/Chart.yaml` version by one PATCH from its CURRENT value**, matching the `maxConcurrentJobs` precedent (a newly rendered per-agent field was `chart 0.5.0→0.5.1`). If it is `0.6.6`, set `version: 0.6.7`. Nothing else in Chart.yaml changes.

3. **Add a CHANGELOG entry in `CHANGELOG.md`.** If a `## Unreleased` section already exists, append to it; otherwise insert `## Unreleased` immediately above the newest existing `## vX.Y.Z` section (below the `# Changelog` boilerplate, not above it). One bullet, `feat(helm):` prefix, mirroring the precedent's shape:

   ```
   ## Unreleased

   - feat(helm): render `zombieJobTimeoutSeconds` on the agent Config when `agents[].zombieJobTimeoutSeconds` is set; chart <old>→<new>. Emitted only when present, so no existing agent's rendered Config changes. Consumed by `agent-task-executor` to set that agent's Job `activeDeadlineSeconds`; older executors ignore the field.
   ```

   Replace `<old>→<new>` with the actual version transition from step 2.

4. **Document the field in `helm/README.md`.** In the `### agents (leaf agents — values-driven)` section, add a line to the YAML example directly after its `heartbeat:` line:

   ```
       zombieJobTimeoutSeconds: 5400     # OPTIONAL: Job deadline (seconds) — must exceed the
                                         # agent's own internal wait, or its Job dies first.
                                         # Omit for the executor default (1800s).
   ```

   Then add one sentence to the prose beneath the code block: an agent that waits on sibling tasks must set this above its own wait timeout, because the executor stamps it onto the Job's `activeDeadlineSeconds` and the Job is killed when it elapses. Do not restructure any other README content.

5. **Document the field in the commented example in `helm/values.yaml`.** In the commented `agents:` example, add a commented line mirroring the same key directly after the `heartbeat:` line:

   ```
     #   zombieJobTimeoutSeconds: 5400   # OPTIONAL: Job deadline (seconds); omit for the
     #                                 # executor default (1800). Must exceed the agent's
     #                                 # own internal wait, or its Job dies first.
   ```

   Do not add any uncommented key to `helm/values.yaml` — `agents` stays `[]`.

6. **Do NOT make any of these changes:**
   - Do NOT change any agent image or agent Go code.
   - Do NOT change `helm/crds/config-crd.yaml` — the field is already declared there.
   - Do NOT set a chart-side default for the deadline.

</requirements>

<constraints>
- Template-only change: no agent image, no agent Go code, no CRD edit.
- An agent that does not set `zombieJobTimeoutSeconds` must render exactly as it does today — no key emitted.
- The rendered value must be a bare integer matching the CRD's `{type: integer, minimum: 30}`, not a quoted string.
- Existing chart behavior must not regress: the full render must still succeed for a minimal and for a full values overlay.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Render with a two-agent values overlay — one setting the deadline, one not — and assert the key appears exactly once, only on the agent that set it, as a bare integer inside the Config's spec.

⚠️ **`helm` is NOT installed in this container image**, and `get.helm.sh` is blocked by the egress allowlist — install it from the Go proxy.

⚠️ **Run every command below in ONE shell invocation.** `export PATH` does not survive into a new shell, so if the install and the renders are split across separate calls, `helm` is not found, the renders silently produce nothing, and every grep below then reads a stale or empty file. The daemon does not check `<verification>` exit codes, so this failure ships silently.

```bash
command -v helm >/dev/null 2>&1 || { go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }
helm version --short                                            # must print v3.16

cat > /tmp/zt-values.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  existingSecret: agent-secret
agents:
  - name: zt-agent
    enabled: true
    assignee: zt
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    zombieJobTimeoutSeconds: 5400
  - name: no-zt-agent
    enabled: true
    assignee: nozt
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF

helm lint helm/                                                 # must report 0 chart(s) failed

helm template helm/ -f /tmp/zt-values.yaml > /tmp/rendered-zt.yaml
echo "render exit: $? (must be 0)"

grep -c '^  zombieJobTimeoutSeconds: 5400$' /tmp/rendered-zt.yaml   # must print 1 — rendered, two-space indent, inside spec
grep -c 'zombieJobTimeoutSeconds' /tmp/rendered-zt.yaml             # must print 1 — the unset agent emits no key
! grep -q 'zombieJobTimeoutSeconds: "5400"' /tmp/rendered-zt.yaml   # absence: the value must not be quoted

# no regression: a minimal overlay still renders
helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-min.yaml
echo "minimal render exit: $? (must be 0)"

# bookkeeping — each of these MUST fail if the corresponding edit was skipped
grep -q '^version: 0.6.7$' helm/Chart.yaml && echo 'version bumped OK' || echo 'FAIL: expected 0.6.7'
awk '/^## /{print "first section: "$0; exit}' CHANGELOG.md            # must print "first section: ## Unreleased"
grep -c 'zombieJobTimeoutSeconds' helm/README.md                    # must print 1
grep -c 'zombieJobTimeoutSeconds' helm/values.yaml                  # must print 1
```

Every check must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + greps above are the verification.

Before you finish, re-run the whole block above in a single shell and confirm every assertion holds; then walk each `<constraints>` bullet against the rendered output.
</verification>
