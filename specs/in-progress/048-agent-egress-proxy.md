---
status: verifying
approved: "2026-08-08T20:38:43Z"
generating: "2026-08-21T08:13:02Z"
prompted: "2026-08-21T08:45:02Z"
verifying: "2026-10-23T09:12:32Z"
branch: dark-factory/agent-egress-proxy
---

## Summary

- Agent Jobs reach the public internet only through a domain-allowlisting proxy, never directly
- A `tinyproxy` Deployment in the per-environment sandbox namespace filters on the `CONNECT host:443` line — no TLS interception, no certificate injection
- The allowlist lives in a ConfigMap, seeded from claude-yolo's curated list, so changing it is an edit rather than an image rebuild
- Agents receive proxy configuration through `ConfigSpec.Env`, which is already a plain string map; the code changes are six names added to the Claude runner's child-env allowlist, without which the Bash tool never sees them, plus the redaction that keeps a credential-bearing proxy URL out of the child-env log
- A startup self-test proves both directions: a non-allowlisted domain must fail *and* an allowlisted one must succeed

## Problem

Agent Jobs carry real credentials: `github-dark-factory-agent` holds `ANTHROPIC_AUTH_TOKEN` and a GitHub App private key (`PEM_KEY`). With unrestricted egress, a prompt-injected agent can exfiltrate them to any host or use them for command-and-control. Tasks increasingly originate from untrusted content (PR titles, bodies, diffs), which is exactly the injection surface.

NetworkPolicy alone cannot close this: it is L3/L4, and `api.github.com` is a rotating CDN IP set. Domain-level control requires an L7 hop. `agent-sandbox-namespace.md` denies internal access and denies the internet outright; this spec restores the internet through a filtered path.

Background, prior art, and the traps this inherits: `docs/agent-network-security.md`.

## Goal

An agent in `<env>-agents-sandbox` can reach exactly the domains on the allowlist and nothing else. A tool that ignores proxy configuration fails closed rather than silently egressing. Changing the allowlist requires no image rebuild and no agent restart beyond the proxy's own.

## Non-goals

- TLS interception / `ssl_bump` — filtering on the CONNECT host is sufficient
- Per-agent allowlists — one shared allowlist first; the per-class split is cheap later because `ConfigSpec.Env` is already per-agent
- Replacing the internal-egress rules from `agent-sandbox-namespace.md`
- Proxying SSH — see Failure Modes; git over HTTPS is the supported path
- Egress auditing / alerting on blocked attempts (worth doing, separate spec)

## Acceptance Criteria

All build-time ACs use a render that actually succeeds — a bare `helm template helm/` exits 1 (no defaults for `namespace`, `executor.kafkaBrokers`, `executor.existingSecret`), and every grep against its empty stdout would pass or fail vacuously:

```bash
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
```

- [ ] `make precommit` exits 0
- [ ] The Claude runner's child-env allowlist admits **all six** proxy names — a unit test in `claude/claude-runner_test.go` sets `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `http_proxy`, `https_proxy` and `no_proxy` in the parent environment, asserts all six appear in the child's environment, and asserts an arbitrary parent var (e.g. `AWS_SECRET_ACCESS_KEY`) still does **not** (the allowlist must stay an allowlist, not become a pass-through). Evidence: `go test ./claude/...` exits 0. Testing only the uppercase trio would leave lowercase-only consumers (`curl`, some Go clients) silently without a proxy
- [ ] `eval "$RENDER"` exits 0
- [ ] The allowlist ConfigMap contains `api.minimax.io` — `$RENDER | grep -c 'api\.minimax\.io'` returns ≥1 (prod `github-dark-factory-agent` runs `ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic`; allowlisting only `api.anthropic.com` breaks it)
- [ ] The self-test actually probes both directions rather than merely naming the domains — in the rendered chart, all four hold: `$RENDER | grep -c 'example\.com'` ≥1, `$RENDER | grep -c 'api\.github\.com'` ≥1, `$RENDER | grep -c 'curl --proxy'` ≥2, and `$RENDER | grep -c 'exit 1'` ≥1 (a self-test that greps green without issuing requests is the laziest passing implementation; the `curl` and `exit 1` counts close it)
- [ ] The proxy runs ≥2 replicas — `$RENDER | grep -A5 'name: egress-proxy' | grep -c 'replicas: 2'` returns ≥1
- [ ] **Post-Deploy (Rung-2):** a non-allowlisted domain is blocked — from inside a sandbox agent pod, `wget -qO- --timeout=5 https://example.com` exits non-zero and prints no body. Negative evidence: exit code ≠ 0, stdout empty
  - `deploy_check:` `kubectlquant -n dev-agents-sandbox get deploy/egress-proxy -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `$(git rev-parse --short HEAD)`
- [ ] **Post-Deploy (Rung-2):** an allowlisted domain succeeds — from the same pod, `wget -qO- --timeout=5 https://api.github.com/zen` exits 0 and stdout is non-empty
  - `deploy_check:` `kubectlquant -n dev-agents-sandbox get cm egress-proxy-allowlist -o jsonpath='{.data}' | grep -c 'api\.github\.com'`
  - `deploy_target:` `1`
- [ ] **Post-Deploy (Rung-2):** direct egress bypassing the proxy is denied — from the same pod with proxy env unset, `wget -qO- --timeout=5 https://api.github.com/zen` exits non-zero. This proves the proxy is enforced by policy, not merely configured. Negative evidence: exit code ≠ 0
  - `deploy_check:` `kubectlquant -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].to[*].podSelector.matchLabels.app}' | grep -c egress-proxy`
  - `deploy_target:` `1`
- [ ] **Post-Deploy (Rung-2):** the proxy refuses to become ready with a broken-open allowlist — set the ConfigMap to allow `.*`, restart the proxy, and `kubectlquant -n dev-agents-sandbox get pod -l app=egress-proxy -o jsonpath='{.items[0].status.containerStatuses[0].ready}'` returns `false`. Restore the ConfigMap afterwards
  - `deploy_check:` `kubectlquant -n dev-agents-sandbox get deploy/egress-proxy -o jsonpath='{.spec.template.spec.containers[0].readinessProbe.exec.command}' | grep -c selftest`
  - `deploy_target:` `1`
- [ ] **Post-Deploy (Rung-2):** one real agent completes end-to-end through the proxy — a `github-dark-factory-agent` Job runs to completion and its task file reaches `phase: human_review`
  - `deploy_check:` `kubectlquant -n dev get config.agent.benjamin-borbe.de github-dark-factory-agent -o jsonpath='{.spec.env.HTTPS_PROXY}'`
  - `deploy_target:` `http://egress-proxy.dev-agents-sandbox.svc.cluster.local:8888`
- [ ] **Post-Deploy (Rung-3):** the same block/allow pair holds in prod — `wget https://example.com` fails and `wget https://api.github.com/zen` succeeds from a prod sandbox pod
  - `deploy_check:` `kubectlquant -n prod get config.agent.benjamin-borbe.de github-dark-factory-agent -o jsonpath='{.spec.env.HTTPS_PROXY}'`
  - `deploy_target:` `http://egress-proxy.prod-agents-sandbox.svc.cluster.local:8888`

## Verification

```bash
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered.yaml   # must exit 0
grep -c 'api\.minimax\.io' /tmp/rendered.yaml   # ≥1
grep -c 'example\.com'     /tmp/rendered.yaml   # ≥1
grep -c 'api\.github\.com' /tmp/rendered.yaml   # ≥1
grep -c 'curl --proxy'     /tmp/rendered.yaml   # ≥2
grep -c 'exit 1'           /tmp/rendered.yaml   # ≥1

# The one permitted code change (prompt 4)
go test ./claude/... 2>&1 | tail -20            # must pass, including the six-name allowlist test
grep -c '"HTTP_PROXY"' claude/claude-runner.go  # ≥1 (quoted slice entry)
grep -c '"NO_PROXY"'   claude/claude-runner.go  # ≥1
```

Each grep is a separate assertion — an OR across them would pass with half the self-test wired. `make precommit` covers the Go half as well; it also runs `test-helm`, so a chart render failure fails it too.

Post-Deploy ACs are operator-executed against dev first, then prod, per `docs/agent-network-security.md`.

**No dark-factory scenario:** a scenario harness building a fresh binary in `/tmp/` cannot reproduce live kube-router policy enforcement or real CONNECT filtering. The Post-Deploy AC ladder against real dev/prod clusters is the deliberate substitute.

## Desired Behavior

1. A `tinyproxy` Deployment (`replicas: 2`) and Service named `egress-proxy`, rendered per environment alongside the sandbox namespace, run in the environment's sandbox namespace (`<env>-agents-sandbox`), listening on 8888, filtering CONNECT requests against a domain allowlist. Two replicas because every agent's internet egress flows through this one component.
2. The allowlist is a ConfigMap (`egress-proxy-allowlist`), seeded from claude-yolo's `files/tinyproxy-allowlist` and extended with `api.minimax.io`, `api.gpt.seibert.ai` and `api.seibert.ai` (32 anchored entries). All resources render from `helm/templates/egress-proxy.yaml`; the self-test is carried in the ConfigMap and referenced by the readiness probe, so nothing depends on a loose file outside the chart.
3. The proxy's readiness probe runs the two-direction self-test; a broken-open or over-tight allowlist keeps the pod un-ready rather than serving traffic.
4. `agent-sandbox-egress` permits agent pods to reach `egress-proxy:8888`; direct internet egress remains denied.
5. The proxy pod carries its own policy allowing egress to `0.0.0.0/0` minus private CIDRs.
6. Agent Configs set `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and the lowercase `http_proxy`/`https_proxy`/`no_proxy` (in-cluster destinations) via `ConfigSpec.Env`, **and the Claude runner passes those names through to the CLI child process** — the library's child-env allowlist (`claude/claude-runner.go`) admits `HOME, PATH, USER, TZ, ZONEINFO, TMPDIR, LANG, LC_ALL` today and must also admit the six proxy names, or the Bash tool inside the agent sees no proxy at all and every outbound call fails closed.

## Constraints

- Proxy configuration is carried in `ConfigSpec.Env`. **Two** code changes are permitted, and only these: (1) a six-name addition to the Claude runner's child-env allowlist in `bborbe/agent` `claude/claude-runner.go` (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, plus their lowercase forms) — without it the library's fixed allowlist drops the proxy vars before the Claude CLI starts and the Bash tool never sees them; and (2) the companion redaction in `envparse/redact.go`, because admitting those names makes them the first allowlisted variables that can embed credentials (`http://user:pass@host`), so `PROXY` joins the sensitive-key markers and the child-env log line shows `***`. No Dockerfile or entrypoint change and no new dependency, but the `bborbe/agent-claude` image **must be rebuilt and redeployed** for the allowlist to take effect — a chart-only rollout leaves the pod env set and the Bash tool still without a proxy. No behaviour change when the vars are unset.
- kube-dns and sentry-proxy stay on the direct path via `NO_PROXY`, and so do the Kafka brokers — whose hosts are derived from the per-install `executor.kafkaBrokers` value because nuke's Kafka is an external nodeport, not an in-cluster Service. None of them may be routed through the proxy
- Agent pods keep `drop: ALL`; no `NET_ADMIN` is introduced
- The internal-egress rules from `agent-sandbox-namespace.md` must not be loosened to accommodate the proxy
- Changing the allowlist must not require rebuilding any image

## Assumptions

- The allowlist is claude-yolo's curated 30-entry list plus the two claude-code-router upstreams `api.gpt.seibert.ai` and `api.seibert.ai` (32 entries). `vllm.seibert.tools` is kept although no workload in the goal's egress matrix calls it — it is still referenced by claude-code-router configs across the fleet, and the list is not pruned to what a config-derived matrix can see, because runtime tool calls reach hosts no static matrix records.
- The `agent-sandbox-egress` peer list is owned by spec 047 and its shape has already changed once (from a two-rule policy to a multi-peer list including a Kafka `ipBlock`). This spec only **appends** one peer; prompt 2 is written against "the end of the `egress:` list", never a positional anchor, and its verification compares peer counts before and after rather than naming rules.
- The two Kafka controls derive from different sources and can drift: this spec reads `executor.kafkaBrokers` at render time, while spec 047's NetworkPolicy hardcodes the broker `/32`s. If a broker address changes, `NO_PROXY` follows automatically and the policy does not — the symptom would be Kafka egress denied for job pods while the proxy itself looks healthy. Deriving 047's `ipBlock`s from the same value is the preferred follow-up on that side.
- Spec 047 (`agent-sandbox-namespace`) is merged on the branch before any prompt of this spec runs — prompts 1-3 hard-stop without `helm/templates/sandbox.yaml` and the `agent.sandboxNamespace` helper.
- `monokal/tinyproxy` reads `Filter`/`FilterDefaultDeny`/`FilterURLs` from environment variables, defaults to port 8888, and takes its client ACL as the first positional arg; `MaxClients` is baked into the image, not ConfigMap-editable.
- The `bborbe/agent-claude` image is rebuilt and redeployed after prompt 4 — the runner change has no effect on a stale image.
- Kafka on nuke is reachable at an external nodeport (`192.168.178.41:32159`), not in-cluster, so `.svc.cluster.local` does not cover it. **Resolved (2026-10-23):** the proxy env derives the Kafka hosts from the per-install `executor.kafkaBrokers` value and appends them to `NO_PROXY`, so dev, prod and sun each get their own list with no hardcoded address in the template. Go Kafka clients use raw TCP and ignore proxy env anyway; the explicit entry covers any agent tool that does honour it.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Agent needs a domain not on the allowlist | Job fails with a proxy 403; tinyproxy logs `Filtered request from <ip>: host` | Add the domain to the ConfigMap, restart the proxy |
| Tool does not honour proxy env (raw TCP client) | Connection denied by NetworkPolicy — fails closed | Intended. Either allow the destination explicitly or fix the tool |
| The running `agent-claude` image predates the runner allowlist change | Pod env carries the proxy vars; the Bash tool still has none; every outbound call fails closed | Rebuild and redeploy `agent/claude` (dev first), then confirm the child process env contains `HTTPS_PROXY` |
| Agent uses git over SSH | Blocked — SSH is not proxied and 22/7999 is not in the policy | Switch that agent to HTTPS with a token; claude-yolo's SSH exemption does not carry over |
| Allowlist edited to `.*` | Readiness self-test negative check fails; pod never becomes ready; old pod keeps serving | Revert the ConfigMap. Failing ready is the designed outcome |
| Proxy Deployment down | All agent internet egress stops; Jobs fail with connection refused | `kubectlquant -n dev-agents-sandbox rollout restart deploy/egress-proxy`; confirm `kubectlquant -n dev-agents-sandbox get deploy/egress-proxy -o jsonpath='{.status.readyReplicas}'` returns ≥1 within 60s |
| N concurrent agent Jobs exceed tinyproxy's `MaxClients` | New connections refused or queued; agents see intermittent proxy errors under load | Scale replicas: `kubectlquant -n dev-agents-sandbox scale deploy/egress-proxy --replicas=3`. `MaxClients` is baked into the `monokal/tinyproxy` image and is **not** ConfigMap-editable — raising it would need a different image and a separate spec. The shared chokepoint is inherent to the central-proxy design |
| Agent reaches an allowlisted host that is itself attacker-controlled | Not detected — allowlist is host-level, not content-level | Out of scope; keep the allowlist minimal |

## Security / Abuse Cases

- **Credential exfiltration to an arbitrary host.** Blocked: the host is not on the allowlist, and direct egress is denied by policy.
- **Bypass by using a raw IP literal instead of a domain.** Blocked at the network layer — the only permitted egress target is the proxy pod, regardless of what the agent asks for.
- **Bypass by connecting on a non-standard port.** Same: the policy permits only the named peers — 8888 to the proxy, 53 to kube-dns, and the Kafka nodeport listener — and nothing else.
- **Exfiltration through an allowlisted host** (gist, PR comment on an attacker's repo via `api.github.com`). **Not mitigated by this spec** — host-level allowlisting cannot see content. Recorded as residual risk; the mitigations are least-privilege credentials and the repo allowlist, not the proxy.
- **DNS tunnelling via kube-dns.** Not mitigated; kube-dns is reachable by necessity. Residual risk.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs (by description, not number — the AC list is unnumbered) | Depends on |
|---|---|---|---|---|
| 1 | tinyproxy Deployment (2 replicas) + Service + allowlist ConfigMap + two-direction self-test wired as the readiness probe, in `helm/templates/egress-proxy.yaml` | 1, 2, 3 | render exits 0; `api.minimax.io` present; self-test four-grep set; `replicas: 2` | — |
| 2 | NetworkPolicy updates — agent→proxy allow, proxy→internet allow | 4, 5 | the post-deploy rung-2/rung-3 block/allow/deny probes | 1 |
| 3 | Proxy env on agent Configs (`ConfigSpec.Env`), dev first | 6 | the `spec.env.HTTPS_PROXY` deploy_checks on the dev and prod Configs | 2 |
| 4 | Claude runner child-env allowlist admits the six proxy names (`claude/claude-runner.go` + its test) | 6 | the runner allowlist unit test (six names present, control absent) | — |

Rationale: the proxy must exist and prove itself healthy (1) before any policy points at it (2), and no agent is pointed at it (3) until the block/allow pair is demonstrably enforced. Prompt 4 is independent of the chart and may land first — until it does, prompt 3's env reaches the pod but not the Bash tool, so nothing actually egresses through the proxy.

## Do-Nothing Option

`agent-sandbox-namespace.md` alone leaves agents with no internet at all, which breaks every agent that calls an LLM API or clones from GitHub — so doing nothing here effectively blocks that spec from rolling out past its scaffolding phase. The alternative to a proxy is restoring unrestricted internet egress, which leaves an `ANTHROPIC_AUTH_TOKEN` and a GitHub App private key one prompt injection away from any host on the internet.
