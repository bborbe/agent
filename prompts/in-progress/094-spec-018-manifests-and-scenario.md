---
status: approved
spec: [018-use-git-rest-for-vault-writes]
created: "2026-05-02T19:50:00Z"
queued: "2026-05-02T19:43:38Z"
branch: dark-factory/use-git-rest-for-vault-writes
---

<summary>
- Controller no longer needs an SSH key on disk; the StatefulSet's SSH-key volume and `/ssh` mount are removed
- Controller talks to git-rest over HTTP via two new env vars (`USE_GIT_REST`, `GIT_REST_URL`); the `datadir` PVC stays for BoltDB
- The `git-ssh-key` value is dropped from the controller Secret; `sentry-dsn` and `gemini-api-key` stay
- A new NetworkPolicy restricts git-rest's port-9090 ingress to controller pods only
- A new scenario walks the full end-to-end sequence (create → update → write result → force-push reset → write result again) and asserts `## Review` content is not silently destroyed by the force-push reset
- No Go code changes in this prompt — purely YAML and Markdown
</summary>

<objective>
Update the Kubernetes manifests for `task/controller` to remove the SSH key dependency and enable the `USE_GIT_REST=true` mode. Add a NetworkPolicy to restrict git-rest ingress to the controller only. Create the acceptance-criteria scenario file for spec-018. These changes can be verified by inspecting the YAML files; no `make precommit` is required (no Go code changed).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**Key files to read in full before editing:**

- `task/controller/k8s/agent-task-controller-sts.yaml` — current StatefulSet. Note the `ssh-key` volume (mounts the `agent-task-controller` Secret key `git-ssh-key`) and the `/ssh` volumeMount. These are removed. The `datadir` volumeClaimTemplate and `/data` mount stay.

- `task/controller/k8s/agent-task-controller-secret.yaml` — current Secret. Remove the `git-ssh-key` data key. The `sentry-dsn` and `gemini-api-key` keys remain (referenced by `SENTRY_DSN` and `GEMINI_API_KEY` env vars in the StatefulSet that still exist for backward compatibility until prompt 5).

- `task/controller/k8s/agent-task-controller-svc.yaml` — read for reference (no changes).

- `scenarios/001-result-writeback-happy-path.md` — read for the scenario format and style. The new scenario MUST follow the same structure: `## Setup`, `## Action`, `## Expected`, `## Cleanup` — top-level sections only. Do NOT introduce per-step `Expected after Step N` sub-headings; consolidate all observable outcomes under `## Expected`.

- `docs/task-flow-and-failure-semantics.md` — read the `## Status Taxonomy` section for valid status/phase values used in the scenario.

- `docs/controller-design.md` — read the `## Atomic Frontmatter Commands` section to understand `UpdateFrontmatterCommand` and `IncrementFrontmatterCommand` payloads for the scenario.

Run before editing:
```bash
cat task/controller/k8s/agent-task-controller-sts.yaml
cat task/controller/k8s/agent-task-controller-secret.yaml
ls scenarios/
```
</context>

<requirements>

1. **Update `task/controller/k8s/agent-task-controller-sts.yaml`**

   Changes to make:

   a. **Add `GIT_REST_URL` and `USE_GIT_REST` env vars** to the container's `env:` list:
   ```yaml
   - name: GIT_REST_URL
     value: 'http://vault-obsidian-openclaw:9090'
   - name: USE_GIT_REST
     value: "true"
   ```
   Add these after the existing `TASK_DIR` env var (keeping alphabetical proximity is fine).

   b. **Remove `GIT_SSH_COMMAND` env var** entirely from the `env:` list.

   c. **Remove the `ssh-key` volume** from the `spec.template.spec.volumes:` list:
   ```yaml
   # REMOVE this entire block:
   - name: ssh-key
     secret:
       secretName: agent-task-controller
       defaultMode: 0600
       items:
         - key: git-ssh-key
           path: id_ed25519
   ```

   d. **Remove the `/ssh` volumeMount** from `spec.template.spec.containers[0].volumeMounts:`:
   ```yaml
   # REMOVE this entire block:
   - mountPath: /ssh
     name: ssh-key
     readOnly: true
   ```

   e. **Keep everything else unchanged:** `datadir` volumeClaimTemplate, `datadir` volumeMount at `/data`, `DATA_DIR=/data/bolt`, `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` (from secret), `BRANCH`, `KAFKA_BROKERS`, `SENTRY_DSN`, `LISTEN`, `POLL_INTERVAL`, `TASK_DIR`, liveness/readiness probes, resources, image, Keel annotations.

2. **Update `task/controller/k8s/agent-task-controller-secret.yaml`**

   Remove the `git-ssh-key` line from `data:`:
   ```yaml
   # REMOVE:
   git-ssh-key: '{{ "GIT_SSH_KEY" | env | teamvaultFile | base64 }}'
   ```

   Keep `sentry-dsn` and `gemini-api-key` as-is.

3. **Create `task/controller/k8s/agent-task-controller-netpol.yaml`**

   A `NetworkPolicy` restricting which pods can reach `vault-obsidian-openclaw` on port 9090.

   Note: NetworkPolicy controls INGRESS to the vault-obsidian-openclaw StatefulSet, not egress from the controller. Create a policy that restricts ingress to vault-obsidian-openclaw's pod port 9090 to only come from `agent-task-controller` pods:

   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: vault-obsidian-openclaw-ingress
     namespace: '{{ "NAMESPACE" | env }}'
   spec:
     podSelector:
       matchLabels:
         app: vault-obsidian-openclaw
     policyTypes:
       - Ingress
     ingress:
       - from:
           - podSelector:
               matchLabels:
                 app: agent-task-controller
         ports:
           - protocol: TCP
             port: 9090
   ```

   This matches the security requirement: "NetworkPolicy must restrict ingress to `task/controller` pods only."

4. **Create `scenarios/use-git-rest-for-vault-writes.md`**

   Use the structure of sibling scenario `001-result-writeback-happy-path.md` exactly: top-level `## Setup`, `## Action`, `## Expected`, `## Cleanup`. Action steps are a numbered checklist; all observable outcomes are consolidated under `## Expected`. Use the `TEST_ID=018test01-1111-2222-3333-444444444444` variable so the scenario is greppable.

   ```markdown
   ---
   status: draft
   ---

   # Scenario: use-git-rest-for-vault-writes

   Validates that the controller reads and writes vault task files via the git-rest HTTP API end-to-end: task creation, frontmatter update, result writeback, force-push reset (real `git push --force`), and a second writeback. Covers the full Acceptance Criteria sequence from spec 018.

   ## Setup

   - [ ] `vault-obsidian-openclaw` StatefulSet running in the target namespace: `kubectlquant -n dev get sts vault-obsidian-openclaw`
   - [ ] `agent-task-controller` running with `USE_GIT_REST=true`: `kubectlquant -n dev get sts agent-task-controller -o jsonpath='{.spec.template.spec.containers[0].env}' | jq '.[] | select(.name=="USE_GIT_REST")'`
   - [ ] Controller log shows git-rest in use: `kubectlquant -n dev logs agent-task-controller-0 --tail=50 | grep "git-rest"`
   - [ ] Local clone of `bborbe/obsidian-openclaw` available at `~/Documents/Obsidian/OpenClaw` for the force-push step
   - [ ] `TEST_ID=018test01-1111-2222-3333-444444444444` (greppable test identifier)
   - [ ] No existing `tasks/$TEST_ID.md` in the vault repo

   ## Action

   1. [ ] **CreateTask** — publish `CreateTaskCommand`:
      ```bash
      ~/Documents/Obsidian/OpenClaw/.claude/scripts/trading-api-write.sh dev \
        "/api/1.0/command/agent-task-v1/create-task" \
        '{"taskIdentifier":"'"$TEST_ID"'","frontmatter":{"assignee":"backtest-agent","status":"todo","phase":"todo"},"body":"Test task for spec-018 git-rest scenario.\n"}'
      ```
      Wait 10 s.

   2. [ ] **UpdateFrontmatter** — transition to `in_progress`:
      ```bash
      ~/Documents/Obsidian/OpenClaw/.claude/scripts/trading-api-write.sh dev \
        "/api/1.0/command/agent-task-v1/update-frontmatter" \
        '{"taskIdentifier":"'"$TEST_ID"'","updates":{"status":"in_progress","phase":"in_progress"}}'
      ```
      Wait 10 s.

   3. [ ] **WriteResult #1** — agent posts initial review:
      ```bash
      ~/Documents/Obsidian/OpenClaw/.claude/scripts/trading-api-write.sh dev \
        "/api/1.0/command/agent-task-v1/update" \
        '{"taskIdentifier":"'"$TEST_ID"'","frontmatter":{"status":"completed","phase":"ai_review"},"content":"Test task for spec-018 git-rest scenario.\n\n## Result\n\nTask completed by backtest-agent.\n\n## Review\n\nInitial review content from spec-018 scenario.\n"}'
      ```
      Wait 10 s.

   4. [ ] **Real force-push** — rewrite history and force-push (NOT a fast-forward push). The pre-write SHA is the parent we reset to:
      ```bash
      cd ~/Documents/Obsidian/OpenClaw && git fetch && git reset --hard origin/master
      PRE_WRITE_SHA=$(git rev-list -n 1 HEAD~1 -- "tasks/$TEST_ID.md" 2>/dev/null || git rev-parse HEAD~1)
      git reset --hard "$PRE_WRITE_SHA"
      git push --force origin HEAD:master
      RESET_SHA=$(git rev-parse HEAD)
      echo "RESET_SHA=$RESET_SHA"
      ```
      Wait 60 s for the controller's poll cycle to observe the force-push.

   5. [ ] **WriteResult #2** — agent posts second review after the reset:
      ```bash
      ~/Documents/Obsidian/OpenClaw/.claude/scripts/trading-api-write.sh dev \
        "/api/1.0/command/agent-task-v1/update" \
        '{"taskIdentifier":"'"$TEST_ID"'","frontmatter":{"status":"completed","phase":"done"},"content":"Test task for spec-018 git-rest scenario.\n\n## Result\n\nSecond agent result after force-push reset.\n\n## Review\n\nSecond review content.\n"}'
      ```
      Wait 10 s.

   6. [ ] **Restart idempotency** — kill the controller pod and replay the last write:
      ```bash
      kubectlquant -n dev delete pod agent-task-controller-0
      kubectlquant -n dev wait --for=condition=Ready pod/agent-task-controller-0 --timeout=120s
      ```
      Re-send the WriteResult #2 command verbatim.

   ## Expected

   - [ ] After step 1: file `tasks/$TEST_ID.md` exists in the vault — fetch via `curl -s http://$(kubectlquant -n dev get svc vault-obsidian-openclaw -o jsonpath='{.spec.clusterIP}'):9090/api/v1/files/tasks/$TEST_ID.md`. Frontmatter has `status: todo`, `assignee: backtest-agent`, `task_identifier: $TEST_ID`.
   - [ ] After step 2: frontmatter shows `status: in_progress`, `phase: in_progress`. `assignee` and `task_identifier` preserved.
   - [ ] After step 3: file contains a `## Review` section with text "Initial review content from spec-018 scenario.". Frontmatter has `status: completed`, `phase: ai_review`.
   - [ ] After step 4 (force-push): controller log shows the reset was observed. The file contents include the prior `## Review` text — either in place under `## Review`, OR moved under a `## Outdated by force-push <sha>` marker. The text "Initial review content from spec-018 scenario." MUST still appear somewhere in the file. (Spec marks the rename-on-force-push as a follow-up controller-logic fix; the assertion here is the weaker, non-negotiable contract: prior review content is never silently destroyed.)
   - [ ] After step 5: frontmatter shows `status: completed`, `phase: done`. The new "Second review content." is present. Prior review text from step 3 is still present (somewhere — see step 4 expected).
   - [ ] After step 6 (restart + replay): file bytes are identical to those produced by step 5 (idempotent replay; no extra `## Review` duplication, no missing content).
   - [ ] One git commit per Kafka command on the vault repo: `cd ~/Documents/Obsidian/OpenClaw && git log --oneline -- "tasks/$TEST_ID.md"` shows a sequence with one commit per Action step (the force-push commit + the controller writes).
   - [ ] Metrics: `controller_gitrest_calls_total{op="post"}` non-zero. `controller_kafka_consume_paused_total` is 0 (no git-rest unavailability during the test).

   ## Cleanup

   - [ ] Remove the test task file:
     ```bash
     cd ~/Documents/Obsidian/OpenClaw && git pull && git rm "tasks/$TEST_ID.md" && git commit -m "spec-018 scenario: cleanup" && git push
     ```
   ```

5. **Update `CHANGELOG.md` at repo root**

   Append bullets to `## Unreleased` (insert the heading under `# Changelog` if absent):

   ```markdown
   - feat(task/controller): remove SSH key volume from StatefulSet manifest; add `GIT_REST_URL` and `USE_GIT_REST=true` env vars
   - feat(task/controller): add `NetworkPolicy` restricting git-rest ingress to agent-task-controller pods only
   - docs: add `scenarios/use-git-rest-for-vault-writes.md` — full E2E acceptance criteria for spec-018
   ```

</requirements>

<constraints>
- ONLY edit YAML files and create one Markdown scenario — NO Go code changes in this prompt
- The `datadir` volumeClaimTemplate and the `/data` volumeMount MUST be preserved — BoltDB uses `/data/bolt`
- `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` env vars MUST remain in the StatefulSet — they are still referenced as flags in the Go binary until prompt 5 removes them
- `USE_GIT_REST=true` in the manifest enables the gitrest path; default in code is `false` so old deployments without this env var are unaffected
- The NetworkPolicy targets `vault-obsidian-openclaw` pods (by label) and restricts ingress from any pod NOT labeled `app: agent-task-controller`
- Before applying the NetworkPolicy, confirm the target pods carry `app: vault-obsidian-openclaw`: `kubectlquant -n dev get sts vault-obsidian-openclaw -o jsonpath='{.spec.template.metadata.labels}'` — if the label differs, adjust the `podSelector` accordingly
- `USE_GIT_REST=true` is set as a literal env value in this prompt's StatefulSet edit (per-environment templating is out of scope; future per-env overrides can convert the literal to `'{{ "USE_GIT_REST" | env }}'`)
- Scenario task_identifier `018test01-1111-2222-3333-444444444444` is specific to this scenario — chosen to be easily grepped in logs
- Do NOT run `make precommit` (no Go code changed) — verify YAML manually
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# Verify SSH key removed from StatefulSet
grep -n "ssh-key\|GIT_SSH_COMMAND" task/controller/k8s/agent-task-controller-sts.yaml
# Must return no matches

# Verify new env vars added
grep -n "USE_GIT_REST\|GIT_REST_URL" task/controller/k8s/agent-task-controller-sts.yaml
# Must show both with correct values

# Verify datadir is still present
grep -n "datadir\|/data" task/controller/k8s/agent-task-controller-sts.yaml
# Must show datadir volumeMount at /data and volumeClaimTemplate

# Verify git-ssh-key removed from secret
grep -n "git-ssh-key" task/controller/k8s/agent-task-controller-secret.yaml
# Must return no matches

# Verify NetworkPolicy created
cat task/controller/k8s/agent-task-controller-netpol.yaml
# Must show podSelector targeting vault-obsidian-openclaw and ingress from agent-task-controller

# Verify scenario created
ls scenarios/use-git-rest-for-vault-writes.md
# Must exist

# Verify scenario contains key sections
grep -n "CreateTaskCommand\|UpdateFrontmatterCommand\|WriteResultCommand\|force-push\|Outdated by force-push\|## Review" scenarios/use-git-rest-for-vault-writes.md
# Must show all key steps

# Verify CHANGELOG
grep -n "SSH key\|NetworkPolicy\|scenario.*spec-018\|use-git-rest" CHANGELOG.md
# Must show Unreleased entries
```
</verification>
