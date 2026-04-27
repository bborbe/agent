---
status: idea
---

## Summary

- Every controller command executor authorizes the command's `Initiator` against an IAM permission check before applying any state change
- Mirrors the established pattern already shipping in `bborbe/trading/core/backtest/controller/pkg/command/` — same `cqrsiam.PermissionChecker` + `iam.PermissionCheck` shape
- A new `lib/iam` package defines the agent-controller permission constants (e.g. `AgentTaskCreatePermission`, `AgentTaskUpdatePermission`, `AgentTaskIncrementPermission`)
- All four current executors (`task_create_task_executor`, `task_update_frontmatter_executor`, `task_increment_frontmatter_executor`, `task_result_executor`) gated, plus the executor wiring in `factory.go` updated to inject the `PermissionChecker` dependency
- Default identity policy: `agent` initiator → all task ops; `executor` initiator → result + increment; `pr-watcher` initiator → create only; unknown initiator → denied

## Problem

Today the controller in `bborbe/agent/task/controller/pkg/command/` accepts and applies any command regardless of the publisher's identity. The `cdb.Command.Initiator` field is set by every publisher (`agent`, `executor`, future `pr-watcher`) but the executors never read it. Anyone with Kafka write access to the topic can create, update, increment, or replace any task. The pattern is solved in the trading codebase via `cqrsiam.PermissionChecker.Check(ctx, tx, command.Initiator, permissionCheck)` — the agent controller is the only first-party Kafka consumer that hasn't adopted it. The upcoming `pr-watcher` service (`bborbe/code-reviewer` spec `pr-watcher.md`) and any future producer will inherit the same hole.

## Goal

After completion, every controller command executor checks the publisher's `Initiator` against a declared permission before executing. Unauthorized commands are rejected with a wrapped error and never mutate state. New executors added later cannot accidentally bypass the check because the wiring (factory + executor signature) makes the `PermissionChecker` a required constructor argument.

## Non-goals

- Cluster-wide IAM rollout — scope is the agent task controller's executors only; the executor binary and other agent services are out of scope
- Per-task ACLs (e.g. "only the task's `assignee` can mutate it") — coarse per-operation permission only; per-resource auth is a future spec
- Authentication of `Initiator` value at the Kafka transport layer — we trust the publisher to set it correctly; this spec adds authorization on top of that trust assumption
- Token/secret rotation for IAM principals — out of scope; existing trading IAM infra handles credential lifecycle
- Rewriting how `Initiator` is set by publishers (already established convention in `lib/delivery/result-deliverer.go` and `task/executor/pkg/result_publisher.go`)

## Desired Behavior

1. A new package (`lib/iam/`) declares typed permission constants for each task operation: create, update, increment, result-write
2. Each command executor takes a `cqrsiam.PermissionChecker` as a constructor argument and calls `Check(ctx, tx, command.Initiator, permissionCheck)` as the first step inside the handler
3. Permission denial returns a wrapped error from the executor — the cqrs framework drops the command and surfaces the failure in logs
4. The factory function `CreateCommandConsumer` (or equivalent) requires the permission checker — callers that fail to inject it get a compile error, not a silent bypass
5. Default permission grants are encoded in code (a single `iam.PermissionPolicy` style table) so the audit trail of "who can do what" lives in source, not in a runtime config
6. Tests for each executor cover the new authorized + unauthorized paths via a Counterfeiter fake of `PermissionChecker`

## Constraints

- Mirror the trading pattern exactly: `cqrsiam.PermissionChecker`, `iam.NewAnyPermissionCheck`, `iam.<Domain><Operation>Permission` naming
- New `lib/iam/` package — don't put permission constants in `lib/agent_task-commands.go` (separation of concerns)
- Tests use Ginkgo/Gomega + Counterfeiter for the `PermissionChecker` boundary
- Errors wrapped via `github.com/bborbe/errors`; permission-denied errors include the rejected `Initiator` and the required permission for log-grep
- Backwards compatibility: existing publishers (`agent`, `executor`) MUST be granted permissions matching their current behavior so the change is invisible from the publisher side after deployment — no Kafka command in production should start failing
- Initial permission policy (suggested): `Initiator("agent")` → all task ops; `Initiator("executor")` → increment + result-write; `Initiator("pr-watcher")` → create only; everything else → denied
- The permission policy table lives in `lib/iam/` so any service can reuse the same constants; controller code references the constants, not string literals
- Mocks/fakes generated under `task/controller/mocks/` per repo convention

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Command with empty `Initiator` | Wrapped error (no permission match for empty principal); command dropped | Publisher fixes payload |
| Command with unknown `Initiator` (e.g. typo, new service not added to policy) | Wrapped error naming the unrecognized principal + missing permission; command dropped | Operator adds principal to policy + redeploys controller |
| Command with valid `Initiator` but insufficient permission (e.g. `executor` trying to send `CreateTaskCommand`) | Wrapped permission-denied error; command dropped | Publisher fixes its initiator OR operator grants missing permission if intentional |
| Command with valid `Initiator` and matching permission | Executor proceeds normally; behavior identical to today | n/a |
| `PermissionChecker.Check` itself returns a non-permission error (e.g. underlying KV failure) | Wrapped error; command dropped; cqrs framework retries per its existing semantics | Operator investigates KV/IAM service |
| Test/dev with unset policy | Fail-fast: factory constructor returns error if any executor's required permission isn't in the policy | Operator wires policy explicitly |

## Security / Abuse Cases

- Malicious or buggy publisher with Kafka write access can no longer mutate arbitrary tasks — they must hold the permission for the operation they invoke
- Initiator string remains caller-asserted (no cryptographic identity); this spec assumes Kafka topic ACLs control who can publish at all, and `Initiator` is a distinguishing label among trusted-publisher services
- Permission-denied logs include initiator + permission so attempted privilege escalations are auditable via log aggregation
- A future "transport-level identity" spec could replace caller-asserted `Initiator` with a JWT or mTLS-derived principal; this spec doesn't preclude that and uses the same `PermissionChecker` interface

## Acceptance Criteria

- [ ] New `lib/iam/` package exposes typed permission constants for create / update / increment / result-write task operations
- [ ] All four current command executors require a `cqrsiam.PermissionChecker` constructor argument and call `Check` as their first handler step
- [ ] Default permission policy encoded in `lib/iam/` covers `agent`, `executor`, and `pr-watcher` initiators with operation grants matching today's behavior (zero regression on currently-published commands)
- [ ] Counterfeiter fake for `cqrsiam.PermissionChecker` (or reuse from trading lib if applicable)
- [ ] Each executor test covers authorized + unauthorized paths
- [ ] Factory wiring (`task/controller/pkg/factory/factory.go`) injects the checker and policy; missing policy at startup is a fail-fast error
- [ ] All tests + precommit green
- [ ] Released as next minor `lib/v0.NN.0` + `task/controller/v0.NN.0`
- [ ] **E2E scenario** (new authorization seam on the cqrs dispatch path — required per spec-writing.md): in dev, publish a `CreateTaskCommand` with `Initiator: "pr-watcher"` and verify the file is created; publish the same command with `Initiator: "executor"` and verify it's rejected with a permission-denied log line naming the missing permission; publish with `Initiator: ""` and verify rejection. Confirms the seam is reachable end-to-end and that denials produce greppable audit lines.

## Verification

```sh
make precommit
```

Manual verification in dev (after deploy):

```sh
# Publish a CreateTaskCommand from the watcher's identity — should succeed
# Publish the same command from "executor" identity — should be rejected
# Inspect controller logs for permission-denied entries with grep
kubectlquant -n dev logs deploy/agent-task-controller --tail=100 | grep "permission denied"
```

## Do-Nothing Option

Continue accepting any command from any publisher. Acceptable while the agent ecosystem has only first-party trusted producers (today: agent + executor; soon: pr-watcher), but every new producer adds blast radius. The first time a third-party system, a misconfigured pod, or a forgotten test harness gets Kafka write access, the controller will faithfully apply whatever it sends. The trading codebase already paid this cost; copying the established pattern across is a small lift compared to the long-term debt of a shared trust assumption that doesn't scale.

A weaker alternative — Kafka topic ACLs only — gates *who* can publish but not *what* each publisher can publish. The desired model is "publisher X can publish operation Y but not operation Z", which requires application-layer authorization.

## Dependent / Related Specs

- `017-create-task-command.md` (in flight) — the new `CreateTaskCommand` executor will inherit the permission-check requirement; coordinate so the new executor lands with the check from day one
- `bborbe/code-reviewer` `specs/pr-watcher.md` — first non-trivial third-party publisher; needs to be granted the `create` permission in the policy
