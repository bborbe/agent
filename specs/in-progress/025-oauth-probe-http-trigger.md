---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-13T19:05:10Z"
generating: "2026-05-13T19:05:10Z"
prompted: "2026-05-13T19:37:44Z"
verifying: "2026-05-13T20:08:44Z"
branch: dark-factory/oauth-probe-http-trigger
---

## Summary

- Operators currently can only fire the OAuth probe loop by waiting for the weekly cron tick or by overriding the cron expression env var and restarting the pod.
- Ad-hoc verification (e.g. "did the OAuth refresh I just performed take?") needs a faster path that does not require a pod restart.
- This spec adds an HTTP endpoint on the executor that fires the same probe loop the cron fires, on demand.
- The HTTP endpoint and the cron share a single probe runner — two invocation paths, one behavior.
- No new auth, no new metric, no new alert; outcomes still surface as `phase: human_review` vault tasks per the prior probe spec.

## Problem

The OAuth probe loop (introduced by spec 024) only runs on a cron schedule. When an operator refreshes an OAuth token and wants to confirm the new credential is in effect, they must either wait up to a week for the next cron tick or override the cron expression env var and restart the executor pod. The restart costs roughly half a minute of downtime, requires elevated cluster privileges, and is awkward to repeat. There is no ad-hoc invocation path that lets an operator verify token health in seconds.

## Goal

After this work, an operator with admin-gateway access can fire the OAuth probe loop on demand by hitting an HTTP endpoint on the executor — path `/oauth-probe/trigger`, method POST, fire-and-forget — without restarting the pod. The endpoint exercises the exact same probe behavior the weekly cron exercises — same runner, same probed Configs, same outcome reporting.

## Assumptions

- Spec 024 (the weekly probe cron) is deployed and the `probe.OAuthProbeRunner` interface in `task/executor/pkg/probe/` exists and is stable.
- Today the runner is constructed inside `CreateOAuthProbeCron` and is not exposed to other callers. Sharing the runner between the cron and the new HTTP handler requires either (a) hoisting runner construction up to `main.go` so both the cron factory and the HTTP server registration accept the same runner instance, or (b) exposing the runner from the factory via a small extraction. The implementation prompt chooses the shape; this spec only pins the invariant (single instance, two invocation paths).
- The admin gateway in dev and prod already proxies executor HTTP endpoints under `/admin/agent-task-executor/*` with Google OAuth at the gateway layer. No executor-side auth changes are required.
- `libhttp.NewBackgroundRunHandler` (used by the precedent in `~/Documents/workspaces/trading/capitalcom/marketdetail/fetcher/main.go:214-234`) is reusable here. Method semantics follow the wrapper's default.

## Non-goals

- No authentication beyond what the admin gateway already provides.
- No new metric, no new alert, no probe outcome in the HTTP response body.
- No HTTP-side scheduling — the cron remains the sole scheduled invoker; HTTP is ad-hoc only.
- No filtering ("probe only agent-X") — the endpoint fires the same all-Configs loop as the cron.
- No change to probe behavior itself, no change to shared libraries, no change to the existing vault-task reporting path.

## Desired Behavior

1. The executor exposes an HTTP endpoint at path `/oauth-probe/trigger`, method POST, that kicks off one full OAuth probe loop (the same loop the weekly cron triggers) and returns success (HTTP 200, empty or fixed acknowledgement body — no probe outcome details) immediately without waiting for the loop to finish.
2. The probe loop fired via HTTP is functionally indistinguishable from a cron-fired probe loop — same Configs probed, same outcome paths (success silent, failure surfaces as a `phase: human_review` vault task).
3. Cron-fired probes continue to behave exactly as before; adding the HTTP path does not change any cron behavior.
4. The endpoint is reachable through the admin gateway in dev and prod and inherits the gateway's existing access controls; no executor-side auth is added.
5. Invoking the endpoint while a probe loop is already running is **single-flight**: the second invocation returns success immediately as a no-op and does NOT start a second concurrent loop. The in-flight loop runs to completion uninterrupted. This bounds OAuth quota consumption regardless of caller behavior.

## Constraints

- The probe runner instance must be shared between the cron path and the HTTP path. Two invocation paths, one runner — no duplicate construction.
- Probe behavior itself is frozen: this spec does not modify probe logic, the runner's contract, or the vault-task reporting format from the prior probe spec.
- Change is confined to the executor module and the root changelog. Shared libraries and the probe package are not modified.
- The HTTP endpoint must follow the same fire-and-forget shape already used elsewhere in the broader codebase for cron-equivalent HTTP triggers — wrap the runner with `libhttp.NewBackgroundRunHandler` (the existing market-detail fetcher's `/trigger/*` endpoints at `~/Documents/workspaces/trading/capitalcom/marketdetail/fetcher/main.go:214-234` are the reference pattern).
- Existing executor HTTP handlers (`/healthz`, `/readiness`, `/metrics`, `/agents`, `/setloglevel/{level}`) continue to be registered and behave unchanged.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Caller invokes the endpoint while a probe loop is already running | Endpoint returns success immediately (HTTP 200); the in-flight loop is not disrupted; the second invocation is a single-flight no-op (does NOT start a concurrent loop) | None — first invocation completes on its own |
| Probe runner returns an error during an HTTP-triggered loop | Error is logged and surfaced via the same vault-task path the cron uses; the HTTP response status is unaffected because the response was already returned | Operator follows the existing `phase: human_review` recovery flow |
| Endpoint receives a malformed request (wrong method, unexpected body) | Behavior matches the framework default for the wrapper handler used; no panic, no leaked goroutine | None |
| Executor is mid-shutdown when the endpoint is invoked | Either the probe loop is not started, or it is started and cancelled cleanly via context — no orphaned goroutines after shutdown completes | None |

## Security / Abuse Cases

- Trust boundary: the endpoint sits behind the admin gateway, which already enforces Google OAuth in dev and prod. Local development has no gateway auth, matching every other admin-gateway endpoint on this service.
- Attacker-controlled inputs: none — the endpoint takes no parameters and probes the same set of Configs regardless of caller.
- Denial of service: single-flight invocation (Desired Behavior #5) caps in-flight loops at one. A flood of requests collapses into one loop per loop-duration window. The probe loop itself is bounded (iterates the known set of Configs and returns), so OAuth quota waste is naturally bounded by loop duration × Config count. Acceptable risk given the auth boundary.
- No path traversal, no file write, no shell-out — the handler only triggers an in-process function call.

## Acceptance Criteria

- [ ] An HTTP endpoint at path `/oauth-probe/trigger`, method POST, fires the OAuth probe loop on demand and returns HTTP 200 immediately with an empty or fixed acknowledgement body.
- [ ] The HTTP path and the cron path use the same `probe.OAuthProbeRunner` instance — verified by inspecting the wiring code (no duplicate construction).
- [ ] A unit test exercises the HTTP handler via `net/http/httptest`: invoking the endpoint causes the runner's probe entry point to be called exactly once per invocation.
- [ ] A unit test exercises the single-flight guarantee: two invocations while the first is in flight result in only one runner call until the first completes.
- [ ] The cron-fired probe path continues to work; spec 024's test coverage still passes (no regression).
- [ ] All previously-registered executor HTTP handlers (`/healthz`, `/readiness`, `/metrics`, `/agents`, `/setloglevel/{level}`) remain registered and functional.
- [ ] Changelog entry added under `## Unreleased` in the root changelog.
- [ ] `make precommit` is clean in the executor module.

**Scenario coverage — NO new scenario.** The pattern is already validated by the prior probe spec plus the existing market-detail fetcher precedent that uses the same dual-trigger shape. The new behavior is fully reachable via unit tests on the HTTP handler wiring and the factory wiring, and the operator-facing smoke test (curl the endpoint, observe a vault-task update) is a manual step gated on dev deploy, not an automated E2E.

## Verification

```
cd task/executor
make precommit
```

Manual smoke (gated on dev deploy):

```
curl -X POST https://dev.quant.benjamin-borbe.de/admin/agent-task-executor/oauth-probe/trigger
```

Expected: HTTP 200 returned promptly; within a few seconds the probe loop completes; vault tasks tied to probed Configs update as they do for cron-fired probes.

## Do-Nothing Option

Operators continue to override the cron expression env var and restart the executor pod for ad-hoc probes. This costs roughly thirty seconds of pod-restart downtime per ad-hoc probe and requires `kubectl set env` privileges on the executor deployment. Acceptable for rare cases; poor fit for the common "I just rotated an OAuth token, did it take?" verification that operators want to do on a seconds-to-minutes cadence.
