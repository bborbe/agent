---
status: draft
summary: Point the launch-agent Config CRD template at the in-cluster claude-code-router with a deepseek-v4-flash-max[1m] pin, and drop the retired MiniMax default from the interview guidance
created: "2026-11-03T14:45:00Z"
---

<summary>
- A new agent rendered from the Config CRD template inherits `ANTHROPIC_BASE_URL: https://api.minimax.io/anthropic` and `ANTHROPIC_MODEL: MiniMax-M2.7-highspeed`.
- MiniMax was retired fleet-wide on 2026-11-03: the subscription is cancelled and expires 2026-11-16, and every cluster router has dropped the provider. A new agent built from this template would call a dead endpoint.
- The template's LLM block moves to the in-cluster router, which is how every live agent already routes, so a new agent is keyless against the provider and needs no per-agent token for model access.
- The interview guidance that names MiniMax as the recommended default is corrected in the same change, because it is what tells a reader which value to render into the template.
</summary>

<objective>
Make the `launch-agent` Config CRD template render an agent that routes through the in-cluster claude-code-router on `deepseek-v4-flash-max[1m]`, and remove the retired MiniMax default from the interview text that selects the template's LLM block.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these files in full before editing:
- `skills/launch-agent/references/config-crd-template.yaml` — the template. The LLM block is at lines 30-38: a comment block, then `ANTHROPIC_BASE_URL` and `ANTHROPIC_MODEL`. Every placeholder is `<ANGLE_BRACKETED>` and the file's own header says none may survive into final output; keep that convention.
- `skills/launch-agent/references/interview.md` — Part 2 question 4 asks which runtime tier the new agent uses and names MiniMax as the recommended default. That text is what an operator follows when filling the template in, so it must agree with the template.
- `skills/launch-agent/references/shapes.md` — the `pi` shape's description mentions MiniMax as the model. Leave it: `pi` is a different runtime with its own provider wiring, and this change does not touch it.

Key facts:
- Every live agent Config routes through the in-cluster router: `ANTHROPIC_BASE_URL: http://claude-code-router-<stage>.agent.svc.cluster.local:8788` with `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`. The router holds the upstream provider tokens, so the caller is keyless.
- The router strips the `[1m]` suffix after alias resolution and rewrites the request body, so the upstream receives the bare name it answers. The suffix is also the CLI's own 1M-context marker; without it the CLI assumes a 200k window and auto-compacts early. Keep it.
- `ANTHROPIC_MODEL` is validated client-side against a catalog baked into the CLI image, but only as an advisory: an unknown name logs a catalog notice and the request proceeds. Do not add a workaround for it.
- The `secretName` block below the env block still lists `ANTHROPIC_AUTH_TOKEN`. Keep it: the chart renders the Secret and some tiers (the Max OAuth-PVC exception, and any direct-provider tier) still need it. Only the comment that calls it "the LLM token" should stop implying it is required for router routing.
- Stage is not knowable at template time, so the host keeps a `<STAGE>` placeholder in the same style as the file's other placeholders.
</context>

<requirements>
1. In `skills/launch-agent/references/config-crd-template.yaml`, replace the LLM block's two values:
   - `ANTHROPIC_BASE_URL: http://claude-code-router-<STAGE>.agent.svc.cluster.local:8788`
   - `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`
2. Rewrite the comment above them. It must say that the in-cluster router is the fleet default and holds the provider tokens, so the agent itself carries no provider key for model access. It must not mention MiniMax or `api.minimax.io` as a default.
3. In the same file, adjust the `secretName` comment so `ANTHROPIC_AUTH_TOKEN` is described as required only by the direct-provider and Max tiers, not by the router path.
4. In `skills/launch-agent/references/interview.md`, Part 2 question 4: replace the MiniMax option and the "Default = MiniMax" recommendation with the router-routed deepseek tier as the default. Keep the option list at four entries and keep the stateless-token versus OAuth-PVC distinction intact.
5. Add a `## Unreleased` section to `CHANGELOG.md` above the current top version heading, with one `feat(agent):` bullet describing the change and naming the 2026-11-16 deadline as the reason.
</requirements>

<constraints>
- Do not touch `skills/launch-agent/references/shapes.md` or `pi/`.
- Do not change the `pi` runner's model wiring: `agent-pi` hardcodes the provider key as `MINIMAX_API_KEY` in its own repo, and that is a separate change.
- Do not introduce a new placeholder name beyond `<STAGE>`; the file's header forbids leaving placeholders in rendered output, so `<STAGE>` must be substituted like the others.
- No Go code changes. This is a template and guidance change only.
</constraints>

<verification>
Run from the repo root:

```bash
grep -n 'ANTHROPIC_BASE_URL\|ANTHROPIC_MODEL' skills/launch-agent/references/config-crd-template.yaml
grep -rn 'api.minimax.io' skills/launch-agent/references/config-crd-template.yaml skills/launch-agent/references/interview.md
grep -n '## Unreleased' CHANGELOG.md
make precommit
```

The first command must print the router URL and `deepseek-v4-flash-max[1m]`. The second must print nothing (exit 1). The third must print one match. `make precommit` must exit 0.
</verification>

<acceptance>
- `config-crd-template.yaml` carries `ANTHROPIC_BASE_URL: http://claude-code-router-<STAGE>.agent.svc.cluster.local:8788` and `ANTHROPIC_MODEL: deepseek-v4-flash-max[1m]`.
- No MiniMax or `api.minimax.io` reference remains in `config-crd-template.yaml` or `interview.md`.
- `interview.md` recommends the router-routed deepseek tier as the default and still offers four tiers.
- `CHANGELOG.md` has a non-empty `## Unreleased` section above the current top version heading, with a `feat(agent):` bullet.
- `make precommit` exits 0.
</acceptance>
