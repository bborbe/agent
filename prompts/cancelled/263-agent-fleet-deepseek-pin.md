---
status: cancelled
execution_id: agent-fleet-deepseek-exec-263-agent-fleet-deepseek-pin
dark-factory-version: v0.198.7
created: "2026-11-03T14:45:00Z"
queued: "2026-11-03T15:20:33Z"
started: "2026-11-03T15:20:51Z"
cancelled: "2026-11-03T15:23:57Z"
---

<summary>
- A newly scaffolded agent currently points at a model provider that is being shut down, so an agent built from the template today would call a dead endpoint.
- Most live agents already route through the fleet's shared in-cluster model router, which holds the upstream provider credentials.
- The template's LLM block now points at that router, so a new agent carries no provider credential of its own for model access.
- The interview guidance that tells a reader which values to render into the template is corrected in the same change, so the two files cannot disagree.
- A newly scaffolded agent therefore points at a live endpoint instead of one that expires on 2026-11-16.
- The change is recorded in the changelog so the next release carries it.
</summary>

<objective>
Make the `launch-agent` Config CRD template render an agent that routes through the in-cluster claude-code-router on `deepseek-v4-flash-max[1m]`, and remove the retired MiniMax default from the interview text that selects the template's LLM block.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these files in full before editing:
- `skills/launch-agent/references/config-crd-template.yaml` — the template. The LLM block is at lines 28-36: a comment block, then `ANTHROPIC_BASE_URL` and `ANTHROPIC_MODEL`. Every placeholder is `<ANGLE_BRACKETED>` and the file's own header says none may survive into final output; keep that convention.
- `skills/launch-agent/references/interview.md` — two places matter. Part 2 question 4 asks which runtime tier the new agent uses and names MiniMax as the recommended default; its preamble says every non-Max tier renders the stateless `ANTHROPIC_AUTH_TOKEN` Secret. Part 7 question 3 (Security boundaries) says to render `ANTHROPIC_AUTH_TOKEN` from inline TeamVault ID `MOPmQL` for "the shared MiniMax token".
- `skills/launch-agent/references/config-only.md` — the `--config-only` mode's env block already uses provider-agnostic `<ROUTER_OR_PROVIDER_URL>` / `<MODEL>` placeholders and keeps `ANTHROPIC_AUTH_TOKEN` in `secretEnv`. Do not change it.

Key facts, verified against the live cluster on 2026-11-03:
- The router Service is `claude-code-router-<stage>` in namespace `agent`, so the host is `claude-code-router-<stage>.agent.svc.cluster.local:8788`. The chart builds it as `<routerInstance>.<agent.namespace>` from `helm/templates/_helpers.tpl`, and nuke's `agent/values-<stage>.yaml` sets `namespace: agent` with `sandbox.routerInstance: claude-code-router-<stage>`. Live Config CRs on nuke dev carry exactly this host.
- Every Config in `agent/values-{dev,prod}.yaml` on `origin/master` (post-`bborbe/nuke#1011`) pins `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]` — 17 on dev, 14 on prod. The router's `seibert-api` provider serves the `deepseek-v4-*` glob and strips the `[1m]` suffix before the glob walk, so the upstream receives the bare name it answers. The suffix is also the CLI's own 1M-context marker; without it the CLI assumes a 200k window and auto-compacts early. Keep it.
- `ANTHROPIC_MODEL` is validated client-side against a catalog baked into the CLI image, but only as an advisory: an unknown name logs a catalog notice and the request proceeds. Do not add a workaround for it.
- The in-cluster router currently runs with **no inbound auth** — `claude-code-router/values-<stage>.yaml` in the nuke repo records "RISK ACCEPTED: the router has no auth (no allowedApiKeys)". So the router path needs no *valid* credential today — but the CLI still refuses to start with an empty token (`config-only.md` documents the 0-byte failure), so the Secret must carry a non-empty placeholder. Keep `ANTHROPIC_AUTH_TOKEN` in the template's Secret: the chart renders it, a gated router (the laptop one uses `allowedApiKeys`) would require a real one, and `config-only.md` keeps it. Only the comment that calls it "the LLM token" should stop implying it is always a provider credential.
- This repo is a git worktree, and dark-factory is normally started with `--set hideGit=true` (the project `.dark-factory.yaml` sets `workflow: direct` and leaves `hideGit` unset, so the mask comes from the invocation), which masks `.git`. `Makefile:3` derives `ROOTDIR` from `git rev-parse --show-toplevel`, so inside the container it must be passed explicitly (see `<verification>`).
</context>

<requirements>
1. In `skills/launch-agent/references/config-crd-template.yaml`, replace the LLM block's two values:
   - `ANTHROPIC_BASE_URL: http://claude-code-router-<STAGE>.agent.svc.cluster.local:8788   # <STAGE> = dev | prod`
   - `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`
2. Rewrite the comment above them. It must say that the in-cluster router is the fleet default and holds the provider tokens, so the agent itself carries no provider credential for model access. It must not mention MiniMax or `api.minimax.io` at all — `<verification>` asserts the string is absent from the whole file, so even a "retired" mention fails.
3. In the same file, adjust the `secretName` comment so `ANTHROPIC_AUTH_TOKEN` is described as present in the Secret for every tier, with a different value per tier: on the router tier it is a non-empty placeholder the in-cluster router ignores (the router gates nothing), the direct-provider tiers carry their own provider token, and the Max tier ignores it in favour of the OAuth PVC.
4. In `skills/launch-agent/references/interview.md`, Part 2 question 4: replace the MiniMax option and the "Default = MiniMax" recommendation with a router tier that names the model exactly as the template does (`deepseek-v4-flash-max[1m]`, served by `claude-code-router-<stage>.agent.svc.cluster.local:8788`) and mark it the default. Keep the option list at four entries. Leave the preamble's statement that every non-Max tier renders the stateless `ANTHROPIC_AUTH_TOKEN` Secret as it stands — it is correct — and add that on the router tier that value is a placeholder, because the router holds the provider tokens. Keep the OAuth-PVC exception text for the Max tier.
5. In the same file, Part 7 question 3 (`Security boundaries`): delete the "inline TeamVault ID `MOPmQL` for the shared MiniMax token" clause outright — after requirement 4 no remaining tier uses a MiniMax token, so re-scoping it would leave a dead reference. Keep `SENTRY_DSN`, the env-indirection guidance and the GitHub App paragraph unchanged.
6. Add a `## Unreleased` section to `CHANGELOG.md` above the current top version heading (`## v0.138.0`), with one `feat(skills):` bullet describing the change and naming the 2026-11-16 deadline as the reason.
7. Before finishing, re-run every command in `<verification>` and confirm each passes; then walk each `<acceptance>` line against the change.
</requirements>

<constraints>
- Do not touch `skills/launch-agent/references/shapes.md`, `skills/launch-agent/references/config-only.md`, or `pi/`.
- Do not change the `pi` runner's model wiring: this repo's `pi/pi-runner.go` passes `MINIMAX_API_KEY` through, and `agent-pi` hardcodes the same key in its own repo; that is a separate change.
- Do not change the template's `namespace: <NAMESPACE>` line or its `dev | prod` comment. That comment is stale (the fleet's Configs live in `agent`), but it is a separate correction.
- Leave `skills/launch-agent/SKILL.md` alone. Its line about the fleet norm being a stateless `ANTHROPIC_AUTH_TOKEN` Secret describes the Agent Design Guide's convention, which still holds.
- Leave `helm/README.md`, `docs/agent-network-security.md` and `helm/templates/egress-proxy-allowlist.yaml` alone — they still describe the MiniMax provider and are a separate cleanup.
- No Go code changes. This is a template and guidance change only.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repo root:

```bash
grep -qF 'ANTHROPIC_BASE_URL: http://claude-code-router-<STAGE>.agent.svc.cluster.local:8788' skills/launch-agent/references/config-crd-template.yaml
grep -qF 'ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]' skills/launch-agent/references/config-crd-template.yaml
! grep -qni 'minimax' skills/launch-agent/references/config-crd-template.yaml
! grep -qni 'minimax' skills/launch-agent/references/interview.md
grep -q '^## Unreleased' CHANGELOG.md
awk '/^## /{print; exit}' CHANGELOG.md | grep -qx '## Unreleased'
awk '/^## /{sec=$0} /^- feat\(skills\):/{print "sits under: " sec}' CHANGELOG.md | grep -q 'sits under: ## Unreleased'
ROOTDIR=/workspace make precommit
```

The two `grep -qF` lines must exit 0 — the template carries exactly that router host and model (`-F` because `[1m]` is a bracket expression otherwise). Both `! grep` lines must exit 0 — no MiniMax text may remain in either file. The first `## ` heading must be `## Unreleased`, and the `awk` walk must print `sits under: ## Unreleased`, proving the bullet landed in the new section rather than a released one. `ROOTDIR=/workspace make precommit` must exit 0.
</verification>

<acceptance>
- `config-crd-template.yaml` carries `ANTHROPIC_BASE_URL: http://claude-code-router-<STAGE>.agent.svc.cluster.local:8788` and `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`.
- Neither `config-crd-template.yaml` nor `interview.md` contains the string `minimax` in any case.
- `interview.md` recommends the router-routed deepseek tier as the default, still offers four tiers, and no longer mentions `MOPmQL`.
- `CHANGELOG.md` has a non-empty `## Unreleased` section as its first version heading, above `## v0.138.0`, holding a `feat(skills):` bullet.
- `ROOTDIR=/workspace make precommit` exits 0.
</acceptance>
