---
status: draft
created: "2026-11-03T14:45:00Z"
---

<summary>
- A newly scaffolded agent currently points at a model provider that is being shut down, so an agent built from the template today would call a dead endpoint.
- Every live agent already routes through the fleet's shared in-cluster model router, which holds the upstream provider credentials.
- The template's LLM block now points at that router, so a new agent carries no provider key of its own for model access.
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
- `skills/launch-agent/references/config-crd-template.yaml` — the template. The LLM block is at lines 28-36: a comment block, then `ANTHROPIC_BASE_URL` and `ANTHROPIC_MODEL`. Every placeholder is `<ANGLE_BRACKETED>` and the file's own header says none may survive into final output; keep that convention. The file already uses `<NAMESPACE>` (`dev | prod`) for the stage, and the router Service is named `claude-code-router-<namespace>`, so the router host reuses that same placeholder rather than introducing a second one.
- `skills/launch-agent/references/interview.md` — two places matter. Part 2 question 4 asks which runtime tier the new agent uses and names MiniMax as the recommended default; its preamble says every non-Max tier renders the stateless `ANTHROPIC_AUTH_TOKEN` Secret. Part 7 question 3 (Security boundaries) says to render `ANTHROPIC_AUTH_TOKEN` from inline TeamVault ID `MOPmQL` for "the shared MiniMax token".
- `skills/launch-agent/references/config-only.md` — the `--config-only` mode's env block already uses provider-agnostic `<ROUTER_OR_PROVIDER_URL>` / `<MODEL>` placeholders. Do not change it.

Key facts:
- Every live agent Config routes through the in-cluster router: `ANTHROPIC_BASE_URL: http://claude-code-router-<stage>.agent.svc.cluster.local:8788` with `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`. The router holds the upstream provider tokens, so the caller is keyless.
- The router strips the `[1m]` suffix after alias resolution and rewrites the request body, so the upstream receives the bare name it answers. The suffix is also the CLI's own 1M-context marker; without it the CLI assumes a 200k window and auto-compacts early. Keep it.
- `ANTHROPIC_MODEL` is validated client-side against a catalog baked into the CLI image, but only as an advisory: an unknown name logs a catalog notice and the request proceeds. Do not add a workaround for it.
- The `secretName` block below the env block still lists `ANTHROPIC_AUTH_TOKEN`. Keep it: the chart renders the Secret and the direct-provider and Max tiers still need it. Only the comment that calls it "the LLM token" should stop implying it is required for router routing.
- This repo is a git worktree and dark-factory runs with `hideGit=true`, which masks `.git`. `Makefile:3` derives `ROOTDIR` from `git rev-parse --show-toplevel`, so inside the container it must be passed explicitly (see `<verification>`).
</context>

<requirements>
1. In `skills/launch-agent/references/config-crd-template.yaml`, replace the LLM block's two values:
   - `ANTHROPIC_BASE_URL: http://claude-code-router-<NAMESPACE>.agent.svc.cluster.local:8788`
   - `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`
2. Rewrite the comment above them. It must say that the in-cluster router is the fleet default and holds the provider tokens, so the agent itself carries no provider key for model access. It must not mention MiniMax or `api.minimax.io` as a default.
3. In the same file, adjust the `secretName` comment so `ANTHROPIC_AUTH_TOKEN` is described as required only by the direct-provider and Max tiers, not by the router path.
4. In `skills/launch-agent/references/interview.md`, Part 2 question 4: replace the MiniMax option and the "Default = MiniMax" recommendation with the router-routed deepseek tier as the default. Keep the option list at four entries. Change the question's preamble so it says the router tier is keyless (the router holds the provider tokens) and only the direct-provider and Max tiers render `ANTHROPIC_AUTH_TOKEN`. Keep the OAuth-PVC exception text for the Max tier.
5. In the same file, Part 7 question 3 (`Security boundaries`): remove the "inline TeamVault ID `MOPmQL` for the shared MiniMax token" clause. State that the router path needs no `ANTHROPIC_AUTH_TOKEN`, and that the inline `MOPmQL` reference applies only to the direct-provider tiers. Keep `SENTRY_DSN`, the env-indirection guidance and the GitHub App paragraph unchanged.
6. Add a `## Unreleased` section to `CHANGELOG.md` above the current top version heading (`## v0.138.0`), with one `feat(skills):` bullet describing the change and naming the 2026-11-16 deadline as the reason.
7. Before finishing, re-run every command in `<verification>` and confirm each passes; then walk each `<acceptance>` line against the change.
</requirements>

<constraints>
- Do not touch `skills/launch-agent/references/shapes.md`, `skills/launch-agent/references/config-only.md`, or `pi/`.
- Do not change the `pi` runner's model wiring: `agent-pi` hardcodes the provider key as `MINIMAX_API_KEY` in its own repo, and that is a separate change.
- Leave `skills/launch-agent/SKILL.md` alone. Its line about the fleet norm being a stateless `ANTHROPIC_AUTH_TOKEN` Secret describes the Agent Design Guide's direct-provider convention, which still holds for the non-router tiers.
- Leave `helm/README.md`, `docs/agent-network-security.md` and `helm/templates/egress-proxy-allowlist.yaml` alone — they still describe the MiniMax provider and are a separate cleanup.
- No Go code changes. This is a template and guidance change only.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repo root:

```bash
grep -n 'ANTHROPIC_BASE_URL\|ANTHROPIC_MODEL' skills/launch-agent/references/config-crd-template.yaml
! grep -qni 'minimax' skills/launch-agent/references/config-crd-template.yaml
! grep -qni 'minimax' skills/launch-agent/references/interview.md
grep -q '^## Unreleased' CHANGELOG.md
awk '/^## /{sec=$0} /^- feat\(skills\):/{print "sits under: " sec}' CHANGELOG.md | grep -q 'sits under: ## Unreleased'
ROOTDIR=/workspace make precommit
```

The first command must print the router URL and `deepseek-v4-flash-max[1m]`. Both `! grep` lines must exit 0 — no MiniMax text may remain in either file. The `## Unreleased` check must exit 0, and the `awk` line must print `sits under: ## Unreleased`, proving the bullet landed in the new section rather than a released one. `ROOTDIR=/workspace make precommit` must exit 0.
</verification>

<acceptance>
- `config-crd-template.yaml` carries `ANTHROPIC_BASE_URL: http://claude-code-router-<NAMESPACE>.agent.svc.cluster.local:8788` and `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`.
- Neither `config-crd-template.yaml` nor `interview.md` contains the string `minimax` in any case.
- `interview.md` recommends the router-routed deepseek tier as the default, still offers four tiers, and its Part 7 Q3 no longer ties `ANTHROPIC_AUTH_TOKEN` to a MiniMax token.
- `CHANGELOG.md` has a non-empty `## Unreleased` section above `## v0.138.0`, holding a `feat(skills):` bullet.
- `ROOTDIR=/workspace make precommit` exits 0.
</acceptance>
