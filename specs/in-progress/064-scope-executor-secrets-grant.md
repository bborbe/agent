---
status: approved
approved: "2026-11-03T07:03:59Z"
branch: dark-factory/scope-executor-secrets-grant
---

## Summary

- The executor Role grants `secrets: get` with no `resourceNames`, so the `agent-task-executor` ServiceAccount can read **every** Secret in its namespace.
- The executor reads at most two Secrets through the API: the git-rest gateway secret and the vault-api gateway secret, both by *name*, both resolved at startup.
- Both names come from values, and each has a per-executor override — so the correct scope is the **union** of the global values and every enabled executor's overrides, not one name.
- The Role's secrets rule gains `resourceNames` when that union is non-empty, and is dropped entirely when it is empty.
- An install that uses neither gateway secret loses a grant it never used; every other rule in the Role is unchanged.

## Problem

`helm/templates/executor-rbac.yaml` grants the control-namespace Role:

```yaml
  - apiGroups: [""]
    resources: [secrets]
    verbs: [get]
```

with no `resourceNames` scope. The `agent-task-executor` ServiceAccount can therefore read every Secret in its namespace — the git-rest gateway secret, the vault-api gateway secret, every component's `sentry-dsn` Secret, and the Strimzi KafkaUser client-cert Secrets. This is GitHub issue #72 on this repo, raised while retiring the World-A `task/executor/` manifests: that cutover made this chart the sole declarer of the executor, so the grant now reaches production with no second manifest to compare against, and consumers pin the chart by tag, so narrowing it here is the only way to close it.

The grant is genuinely required, but only for named Secrets. `pkg/gitrest_gateway_secret.go` and `pkg/vault_api_gateway_secret.go` in `bborbe/agent-task-executor` both resolve a Secret *name* taken from `GITREST_GATEWAY_SECRET` / `VAULT_API_GATEWAY_SECRET` through `readGatewaySecret` against the cluster API. `get` supports `resourceNames`, so the rule can name exactly those Secrets and nothing else.

⚠️ **The issue undercounts the set, and copying it verbatim would break production.** It says "the single Secret the executor actually reads — the one already supplied via `executor.gitRestGatewaySecret`". That is one of **two** names, and both are overridable per executor:

- `helm/templates/executor-deployment.yaml:110-111` renders `GITREST_GATEWAY_SECRET` from `.Values.executor.gitRestGatewaySecret`;
- `:316-317` renders it from `$e.gitRestGatewaySecret | default $.Values.executor.gitRestGatewaySecret` inside the `executors` range;
- `:330-331` renders `VAULT_API_GATEWAY_SECRET` from `$e.vaultApiGatewaySecret | default $.Values.executor.vaultApiGatewaySecret`.

Every executor Deployment uses the same ServiceAccount — `serviceAccountName: agent-task-executor` at `executor-deployment.yaml:39` and `:249` — so all of them share this one Role. Scoping to the global git-rest name alone silently revokes the vault-api read and every per-executor override, which surfaces at executor startup as a missing-secret error rather than at render time.

## Goal

The control-namespace executor Role's secrets rule names exactly the Secrets the executor is configured to read, and nothing else. The set is the union of the two global gateway-secret values and the two overrides of every enabled entry in `executors`. An install that configures no gateway secret renders no secrets rule at all. Every other rule in the Role, and the Role's identity, is unchanged.

## Non-goals

- Changing the executor binary, its startup behaviour, or the `readGatewaySecret` path.
- Touching the **sandbox** Role's separate `secrets: get` rule (`executor-rbac.yaml:149`) or the `a2a-worker-controller` Role's grants. Those are different ServiceAccounts in different namespaces and are not what issue #72 names; widening this change to them would make one review carry three unrelated risk decisions.
- Adding a values knob, an allowlist file, or a chart flag. The scope is derived from values that already exist.
- Changing the `executors[]` schema or the per-executor rendering condition.
- Publishing the chart, bumping `Chart.yaml`, or cutting a release tag.

## Assumptions

- The executor reads no Secret other than these two by name. Verified in `bborbe/agent-task-executor` at `origin/master`: the only API Secret reads are `pkg/gitrest_gateway_secret.go:63` and the shared `readGatewaySecret` it calls, reached from `pkg/vault_api_gateway_secret.go:34`. Nothing else in the repository constructs a `Secrets(...)` client.
- An empty secret name means "gateway auth disabled" and the reader returns `""` without touching the API, so an install that sets neither name has no use for the grant.
- `executors[]` entries render only when `$e.enabled` is truthy — the same condition the Deployment uses (`executor-deployment.yaml:207-208`) — so an entry that renders no Deployment must contribute no name either.

## Acceptance Criteria

- [ ] **AC1 — a single global git-rest name is the whole scope.** With `executor.gitRestGatewaySecret: gitrest-gw` set and every other name empty, the rendered control-namespace executor Role's secrets rule carries `resourceNames` listing exactly `gitrest-gw`. Evidence: `helm template … --show-only templates/executor-rbac.yaml`, first `Role` document; the secrets rule's `resourceNames` set equals `{gitrest-gw}` exactly.
- [ ] **AC2 — both global names render together.** With `executor.gitRestGatewaySecret: gitrest-gw` and `executor.vaultApiGatewaySecret: vaultapi-gw`, the same rule's `resourceNames` set equals `{gitrest-gw, vaultapi-gw}`. Evidence: as AC1. This is the criterion a git-rest-only implementation fails, and it is the one that protects the vault-api read.
- [ ] **AC3 — per-executor overrides join the union, deduplicated.** With global `gitRestGatewaySecret: gitrest-gw` and `vaultApiGatewaySecret: vaultapi-gw`, and one enabled executor declaring `gitRestGatewaySecret: gitrest-gw-personal` and `vaultApiGatewaySecret: vaultapi-gw-personal`, `resourceNames` contains all four names. With a second enabled executor declaring `gitRestGatewaySecret: gitrest-gw` (the global value again), that name still appears **once**. Evidence: as AC1 — set equality for the four-name case, and an occurrence count of exactly 1 for the repeated name. Both the global and the override must be present, not the override alone: the override wins for that executor, but another executor entry with no override still resolves the global value, and they share the ServiceAccount.
- [ ] **AC4 — a disabled executor contributes no name.** With an executor entry declaring `enabled: false` and a unique secret name, that name does **not** appear in `resourceNames`, while an enabled sibling's does. Evidence: as AC1, asserting the disabled name's absence explicitly rather than only the enabled name's presence.
- [ ] **AC5 — no name configured renders no secrets rule.** With neither global name set and no executor overriding either, the control-namespace executor Role renders **no rule** with `resources: [secrets]`. Evidence: `helm template … --show-only templates/executor-rbac.yaml`, first `Role` document; `grep -c 'resources: \[secrets\]'` returns 0. This is the security outcome the issue asks for: the grant exists for two named reads, and with neither configured it has no user.
- [ ] **AC6 — nothing else in the Role changes.** In a render with names set, `--show-only templates/executor-rbac.yaml` differs from the pinned baseline `a8c19e1f6bc0dbc2496a97aece01264954b872d5` in **exactly one hunk** — the secrets rule gaining `resourceNames` — and the render still contains exactly **one** ServiceAccount object, so no new ServiceAccount was introduced. Evidence: the normalised `diff` below is non-empty and names only the secrets rule, and `grep -c '^kind: ServiceAccount$'` returns 1. **Three anchors are load-bearing, each measured, and each one a raw diff would fail on a correct change:**

- **Comments are stripped** (`grep -v '^[[:space:]]*#'`) — this chart's templates are densely commented and a correct implementation adds a comment explaining the derivation.
- **`helm.sh/chart:` labels are stripped** — `helm/Chart.yaml` states "Chart version — bumped on chart changes", so a correct implementation bumps `version`, which rewrites that label in *every* document. Measured: two renders of the same chart differ only in that label (0.23.14 vs 0.17.0) and produce a 40-line diff with no behavioural change.
- **The ServiceAccount grep is anchored** — an unanchored `grep -c 'kind: ServiceAccount'` returns **4** on a correct render, because three RoleBinding `subjects` entries carry the same string. Measured against `a8c19e1f`: object count 1, unanchored 4.
- [ ] **AC7 — the values documentation matches the new scope.** The `gitRestGatewaySecret` and `vaultApiGatewaySecret` comments in `helm/values.yaml` describe the narrowed grant instead of the current wording. Evidence: `grep -c 'resourceNames' helm/values.yaml` returns ≥1, and both stale phrases are gone — `grep -c 'executor-rbac.yaml grants' helm/values.yaml` returns 0 (currently 1, at line 218) and `grep -c 'adds no RBAC' helm/values.yaml` returns 0 (currently 1, at line 235).
- [ ] **AC8 — the chart's own suite passes and the new cases are discriminating.** `make test` exits 0, including one new `hack/test-helm-render.sh` case per AC1–AC5, each asserting the exact `resourceNames` set or its absence rather than the mere presence of the key. At least one new case must be shown to go **red** against the naive implementation — scope derived from `executor.gitRestGatewaySecret` alone — before the change is claimed complete. Evidence: exit code 0, plus the pasted red run for the AC2 or AC3 case against that implementation.

## Verification

Container-executable — no cluster access, no operator rung. The container mounts the repo as its working directory; no `cd` is needed.

```bash
make test          # AC8, plus every existing chart case

# The chart requires kafkaBrokers and a Sentry DSN to render at all.
cat > /tmp/scope-both.yaml <<'EOF'
namespace: dev
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
  gitRestGatewaySecret: gitrest-gw
  vaultApiGatewaySecret: vaultapi-gw
EOF

# `--show-only` plus the first-Role selector isolates the CONTROL-namespace Role
# (`name: agent-task-executor`). A bare grep over the full render also matches the
# sandbox Role at executor-rbac.yaml:149, which this spec does not touch.
helm template scope-both ./helm -f /tmp/scope-both.yaml \
  --show-only templates/executor-rbac.yaml \
  | awk '/^kind: Role$/{n++} n==1' \
  | grep -n 'resourceNames'          # AC2: one line, both names

# AC5 — with no name configured, the control-namespace Role has no secrets rule.
cat > /tmp/scope-none.yaml <<'EOF'
namespace: dev
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
EOF
helm template scope-none ./helm -f /tmp/scope-none.yaml \
  --show-only templates/executor-rbac.yaml \
  | awk '/^kind: Role$/{n++} n==1' \
  | grep -c 'resources: \[secrets\]'  # AC5: 0

# AC6 — diff against the pinned baseline. The archive is required: `git show`
# yields a raw template, which cannot be rendered. A worktree is unnecessary.
# No `-C`: cwd is already the repo root, here and in the container.
BASE=a8c19e1f6bc0dbc2496a97aece01264954b872d5
TMP="$(mktemp -d)"
git archive "$BASE" helm | tar -x -C "$TMP"
helm template base "$TMP/helm" -f /tmp/scope-both.yaml \
  --show-only templates/executor-rbac.yaml > "$TMP/base.yaml"
helm template head ./helm -f /tmp/scope-both.yaml \
  --show-only templates/executor-rbac.yaml > "$TMP/head.yaml"
# Strip comments (a correct change adds one) and the chart label (a correct
# change bumps Chart.yaml, which rewrites that label in every document).
NORM='^[[:space:]]*#|helm.sh/chart:'
diff <(grep -Ev "$NORM" "$TMP/base.yaml") \
     <(grep -Ev "$NORM" "$TMP/head.yaml")   # AC6: only the secrets rule
grep -c '^kind: ServiceAccount$' "$TMP/head.yaml"   # AC6: 1 (unanchored reads 4)
```

## Desired Behavior

1. The control-namespace executor Role's secrets rule renders `resourceNames` whenever at least one of the two gateway-secret names is configured, anywhere.
2. The rendered set is the union of `executor.gitRestGatewaySecret`, `executor.vaultApiGatewaySecret`, and both overrides of every `executors` entry whose `enabled` is truthy.
3. The set is deduplicated, so a name configured in two places appears once.
4. When the union is empty, the secrets rule is not rendered.
5. No other rule in the Role changes.
6. The Role's own identity — kind, name, namespace, labels — is unchanged.

## Constraints

- `resourceNames` is the only mechanism: no wildcard names, no separate Role per executor, no new ServiceAccount.
- The rule must never render an empty or blank entry in `resourceNames` — an empty string in `resourceNames` is a valid-looking render that grants nothing, which fails closed at runtime instead of at render time.
- The `executors` rendering condition must match the Deployment's (`$e.enabled` truthy), so the Role cannot name a Secret belonging to an executor that was not rendered.
- Existing chart behaviour for an install that sets nothing must not regress beyond the removal of an unused grant.
- `helm/Chart.yaml`'s `version` is bumped, as that file's own comment requires ("bumped on chart changes"). AC6's comparison strips the `helm.sh/chart` label for exactly this reason, so the bump does not read as a behavioural change.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| The scope is derived from `executor.gitRestGatewaySecret` alone | The render succeeds and the executor fails at startup with a missing-Secret error when `VAULT_API_GATEWAY_SECRET` or a per-executor override is set | AC2/AC3 fail first; if one reaches a cluster, set the missing name in values and re-render — the grant is declarative, so no executor restart ordering is needed |
| A per-executor override is omitted from the union | That executor's Deployment reads its own Secret name and is refused | Add the override to the union derivation; the failure is per-executor and visible in its own startup log |
| A disabled executor's name is included | No runtime failure — an over-broad grant for a Secret that no rendered Deployment reads | AC4 catches it before merge |
| The rule is dropped although a name is configured | The executor is refused `secrets "x" is forbidden` at startup | Revert the derivation; AC1–AC3 fail first |
| A consumer deploys the chart with `GITREST_GATEWAY_SECRET` injected outside the chart (a hand-written Deployment reusing the Role) | The Role no longer grants the read, so that deployment breaks where the chart would have worked | Out of chart scope by construction: such a deployment does not use this Role's values. Named here so the removal in AC5 is a recorded decision, not an accident |

## Security / Abuse

This change **is** the security fix: it removes a namespace-wide Secret read from a workload that needs two named Secrets. Two properties matter and both are asserted above:

- **Fail closed, not open.** An unconfigured install loses the grant rather than keeping a broad one (AC5). An empty `resourceNames` entry is forbidden by Constraints because it renders cleanly and grants nothing.
- **No silent widening.** A future executor that reads a third Secret must add it to the derivation; the render will not fall back to an unscoped grant. That failure is a startup error, not a silent permission.

No user input, no HTTP surface, no filesystem path is involved.

## Suggested Decomposition

Prompts should be generated in this order — each row is a single prompt with a clear scope.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | The whole change: the union derivation in `helm/templates/executor-rbac.yaml`, the `helm/values.yaml` comments, and the `hack/test-helm-render.sh` cases | 1–6 | 1–8 | — |

Rationale: **one prompt**, because this is one indivisible change. The test cases cannot be written before the rule renders, and the values comment describes the same derivation — splitting them produces prompts that cannot be completed or verified independently. A three-row split was tried first and is recorded here because it failed for a reason worth keeping: the dark-factory Job has `activeDeadlineSeconds: 5400` (90 min), each row became a sequential prompt inside that one Job, and both attempts were killed at the deadline with zero commits (`DeadlineExceeded`, measured 2026-11-03). The last successful single-prompt run on this platform took 59 minutes. AC8 still guards the test harness — the prompt is one, the assertions are not optional.

## Do-Nothing Option

The grant stays unscoped. The executor continues to be able to read every Secret in its namespace, including the KafkaUser client-cert Secrets and every component's Sentry DSN — a blast-radius question rather than an active incident, since the executor is a first-party workload in a namespace that is not multi-tenant, and spawned task Jobs run as the namespace `default` ServiceAccount and do not inherit the grant. The cost of doing nothing is that the one workload with the most third-party code paths keeps a namespace-wide read that two names would satisfy, and issue #72 stays open as the record of that decision.
