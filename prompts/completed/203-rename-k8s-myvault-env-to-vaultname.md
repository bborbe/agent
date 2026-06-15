---
status: completed
summary: Renamed MY_VAULT to VAULT_NAME in task/controller/k8s/agent-task-controller-sts.yaml (one-line yaml edit, value unchanged)
container: agent-task-controller-personal-exec-203-rename-k8s-myvault-env-to-vaultname
dark-factory-version: v0.177.1
created: "2026-06-15T17:40:00Z"
queued: "2026-06-15T15:56:18Z"
started: "2026-06-15T16:10:20Z"
completed: "2026-06-15T16:10:54Z"
---

<summary>
- Rename the `MY_VAULT` env var in the task-controller's StatefulSet to `VAULT_NAME` so it matches the renamed Go field, env tag, and CLI flag landed on master (PR #21 / agent v0.68.0)
- Single-yaml-file mechanical edit — pure rename of one env entry name; value, templating, and tags unchanged
- The env value (the literal `{{ "VAULT" | env }}` substitution that the Makefile fills in per VAULTS loop iteration) is NOT changed
- Migration-note text and Makefile VAULTS loop are unchanged — they reference `VAULT` (the iteration variable), not the controller env name
- After this lands, `kubectlquant -n dev get sts agent-task-controller-openclaw -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="VAULT_NAME")].value}'` returns `openclaw`
- No precommit changes needed — yaml-only edit, not Go code
</summary>

<objective>
Match the open-PR-#19 k8s yaml to the new env-var name `VAULT_NAME` shipped on master. End state: `grep -rn 'MY_VAULT' task/controller/k8s/` returns zero matches; `grep -rn 'VAULT_NAME' task/controller/k8s/agent-task-controller-sts.yaml` returns one match (the env block).
</objective>

<context>
- `/workspace/task/controller/k8s/agent-task-controller-sts.yaml` — the templated StatefulSet manifest produced by the Makefile VAULTS loop. Currently sets `name: MY_VAULT`; needs to set `name: VAULT_NAME`.
- `/workspace/task/controller/main.go` — already reads `VAULT_NAME` (renamed on master in PR #21). This prompt only catches up the k8s side.
- `/workspace/task/controller/k8s/Makefile` — VAULTS loop exports `VAULT` and `GATEWAY_SECRET_TVID` env vars at apply-time. Unrelated to the controller's runtime env-var name; do not touch.
</context>

<requirements>

1. **Rename the env entry in `/workspace/task/controller/k8s/agent-task-controller-sts.yaml`**

   Find the env block entry currently shaped:
   ```yaml
   - name: MY_VAULT
     value: '{{ "VAULT" | env }}'
   ```

   Replace with:
   ```yaml
   - name: VAULT_NAME
     value: '{{ "VAULT" | env }}'
   ```

   Only the `name:` line changes. The `value:` line — including the `{{ "VAULT" | env }}` substitution — stays identical. No other yaml edits.

2. **Verify no other yaml file references `MY_VAULT`**

   ```bash
   cd /workspace && grep -rn 'MY_VAULT' task/controller/k8s/
   ```

   Must return zero matches after the edit.

3. **Verify the substitution still works**

   ```bash
   cd /workspace && grep -A 1 'name: VAULT_NAME' task/controller/k8s/agent-task-controller-sts.yaml
   ```

   Must show `value: '{{ "VAULT" | env }}'` immediately after `name: VAULT_NAME`.

</requirements>

<constraints>
- Pure k8s-yaml rename — no Go code changes, no Makefile changes, no docs/ changes, no CHANGELOG
- Do NOT modify the `value:` line — only the `name:` line. The Makefile loop variable `VAULT` is the source-of-truth for the substitution value; that contract is independent of the controller-side env-var name
- Do NOT touch `task/controller/k8s/Makefile` — its `MIGRATION NOTE` block mentions the STS rename, not the env-var rename; that comment is still factually correct
- Do NOT touch `docs/controller-design.md` or `CHANGELOG.md` — those were renamed on master in PR #21 / agent v0.68.0; this branch will pick up the renamed versions on rebase, not by editing here
- Do NOT add `--my-vault` → `--vault-name` backward-compatibility aliases. The controller binary on master only accepts `VAULT_NAME`; the k8s env must match exactly
- No `go mod vendor`
- No `kubectl`-side operations (no `apply`, no resource mutation)
- **Pre-rebase guard:** This prompt assumes PR #21 has merged to master AND this branch has been rebased onto post-PR-#21 master. If `grep -n 'MY_VAULT' task/controller/main.go` matches any line (i.e. the Go rename hasn't reached this branch yet), abort immediately with a failure status — do NOT edit the yaml ahead of the Go rename. Operator must rebase first.
</constraints>

<verification>
```bash
# 1. Old name gone from k8s
cd /workspace && ! grep -rn 'MY_VAULT' task/controller/k8s/

# 2. New name present, exactly once
cd /workspace && [ "$(grep -c 'VAULT_NAME' task/controller/k8s/agent-task-controller-sts.yaml)" = "1" ]

# 3. Substitution value unchanged
cd /workspace && grep -A 1 'name: VAULT_NAME' task/controller/k8s/agent-task-controller-sts.yaml | grep -q '{{ "VAULT" | env }}'

# 4. No other files touched
cd /workspace && git diff --name-only | tr '\n' ' '
# Must equal: task/controller/k8s/agent-task-controller-sts.yaml
```
</verification>
