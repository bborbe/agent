---
status: prompted
approved: "2026-08-08T20:38:38Z"
generating: "2026-08-21T07:52:06Z"
prompted: "2026-08-21T08:13:02Z"
branch: dark-factory/agent-sandbox-namespace
---

## Summary

- A dedicated per-environment `<env>-agents-sandbox` namespace is added to the helm chart, with a default-deny egress NetworkPolicy
- Agent Jobs spawned into it can reach kube-dns, the nine destinations in the parent goal's egress matrix (sentry-proxy, claude-code-router-dev, pushgateway, the three vaults, the two trading frontends, Kafka) and nothing else — no other internal services, no other private ranges, no cloud metadata endpoint
- The executor gets cross-namespace RBAC so it can create Jobs, read Secrets, and bind PVCs there
- Public-internet egress stays denied here; `agent-egress-proxy.md` restores it through a filtered path
- The executor-side mechanism (`ConfigSpec.JobNamespace`, pod labelling) is specified separately in `bborbe/agent-task-executor:specs/config-job-namespace.md` and must ship first

## Problem

Agent Jobs run in the same namespace as the executor and inherit that namespace's network access. There is no boundary between an agent reasoning about a markdown task and the production trading stack.

Verified 2026-08-08: zero NetworkPolicies exist in `dev`/`prod` on either cluster, and strimzi's Kafka client listeners (`:9092`–`:9095`) accept `FROM: ANY`. A prompt-injected agent can reach any internal service, publish to any Kafka topic including the trading ones, and query `169.254.169.254`. With 10 `Config` CRs per namespace and tasks increasingly sourced from untrusted content, that blast radius keeps growing.

Background, manifests, and the traps this inherits: `docs/agent-network-security.md`.

## Goal

`<env>-agents-sandbox` exists in both dev and prod with a policy that denies egress by default and permits only kube-dns plus the nine destinations in DB 3 — `sentry-proxy`, `claude-code-router`, `pushgateway`, the three `vault-obsidian-*`, `frontend-gateway`, `frontend-strategy` and Kafka. Agent Jobs land there and provably cannot reach anything outside that list. The executor, still in the control namespace, retains exactly the cross-namespace rights it needs and no more.

## Non-goals

- Public-internet egress — `agent-egress-proxy.md` owns it; until that lands, sandbox agents have no internet
- Executor Go changes — `bborbe/agent-task-executor:specs/config-job-namespace.md` owns them
- Pod-to-pod isolation between concurrent agent Jobs
- Per-agent egress policies — one sandbox policy for all agents
- Kafka topic-level authorization (agents can still publish to any topic; see Security)
- Runtime sandboxing (gVisor, Kata)

## Acceptance Criteria

All build-time ACs render the chart with the minimum required values — the chart has no defaults for `namespace`, `executor.kafkaBrokers`, or `executor.existingSecret`, so a bare `helm template helm/` exits 1 and every grep against it would trivially return 0:

```bash
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
```

- [ ] `eval "$RENDER"` exits 0
- [ ] The chart renders the sandbox namespace — `$RENDER | grep -c 'name: dev-agents-sandbox'` returns ≥1
- [ ] The chart renders a sandbox-scoped Role named `agent-task-executor-sandbox` **in the sandbox namespace**, not the control namespace — `$RENDER | grep -A3 'name: agent-task-executor-sandbox' | grep -c 'namespace: dev-agents-sandbox'` returns ≥1. (A bare `grep -cE 'kind: (Namespace|NetworkPolicy|Role|RoleBinding)' >= 4` does NOT work here: the pre-existing chart already renders 3 such lines from `executor-rbac.yaml`, so a Namespace + NetworkPolicy alone would reach 4 and let the RBAC be skipped entirely.)
- [ ] That Role carries exactly the verbs in DB 5 — `$RENDER | grep -A15 'name: agent-task-executor-sandbox' | grep -cE 'jobs|secrets|persistentvolumeclaims'` returns ≥3, and `$RENDER | grep -A15 'name: agent-task-executor-sandbox' | grep -c '\*'` returns 0. Negative evidence: no wildcard verb or resource
- [ ] A RoleBinding binds it to the executor's ServiceAccount in the sandbox namespace — `$RENDER | grep -A6 'kind: RoleBinding' | grep -c 'agent-task-executor-sandbox'` returns ≥1
- [ ] The rendered policy selects `app: agent` — `$RENDER | grep -A5 'name: agent-sandbox-egress' | grep -c 'app: agent'` returns ≥1
- [ ] The policy permits exactly the intended destinations, and no others — **against both renders**, `/tmp/rendered-dev.yaml` and `/tmp/rendered-prod.yaml`: `grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32'` returns `3`; `grep -c 'cidr: 192.168.178.0/24'` returns `0`; `grep -c 'port: 9092'` returns `0`; and every in-cluster destination's **selector line** appears under a `podSelector` — `grep -cE '^\s+app(\.kubernetes\.io/instance)?: (sentry-proxy|claude-code-router-<env>|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy))$'` returns `8`. Assert the selector, not the bare service name: the template comments name every destination and Helm preserves comments, so a bare-name grep stays green with the peer deleted. The prod half is what catches a `-dev` literal in a both-env template
- [ ] The `sentry-proxy` peer keeps `namespaceSelector` and `podSelector` in **one** `- to:` entry — a split peer renders identically under every grep above and ORs into "every service on 9090 in that namespace". Assert the structure: the nearest `- to:` before `app: sentry-proxy` must precede its `namespaceSelector`. `awk '/- to:/{t=NR} /namespaceSelector:/{n=NR} /app: sentry-proxy/{print (n>t)?"SAME_PEER_OK":"SPLIT_PEER_FAIL"}' /tmp/rendered-dev.yaml` prints `SAME_PEER_OK`
- [ ] The policy declares egress only, no ingress — `$RENDER | grep -A30 'name: agent-sandbox-egress' | grep -c 'policyTypes'` returns 1, and the rendered `policyTypes` list contains only `Egress`. Negative evidence: `$RENDER | grep -A30 'name: agent-sandbox-egress' | grep -c 'ingress:'` returns 0
- [ ] The sandbox namespace is per-environment, not a shared literal — `helm template helm/ --set namespace=dev ... | grep -c 'dev-agents-sandbox'` returns ≥1 **and** `helm template helm/ --set namespace=prod ... | grep -c 'prod-agents-sandbox'` returns ≥1. Negative evidence: neither render contains a bare `name: agents-sandbox` line
- [ ] **Post-Deploy (Rung-2):** the namespace and policy exist in dev — `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.metadata.name}'` returns `agent-sandbox-egress`
  - `deploy_check:` `kubectlnukedev get ns dev-agents-sandbox -o jsonpath='{.status.phase}'`
  - `deploy_target:` `Active`
> **Every probe below runs inside the pod via `kubectl exec`.** Run from the workstation they prove nothing: `32159` is a cluster-wide NodePort, so a bare `nc` from the LAN answers regardless of any NetworkPolicy, and `vault-obsidian-openclaw.dev` may not even resolve there.

- [ ] **Post-Deploy (Rung-2):** the policy selects the probe pod — create it and wait for Ready. No agent Job runs here until `config-job-namespace.md` ships, so the pod is created for these checks and removed by the teardown AC **after** them:
  - `kubectlnukedev -n dev-agents-sandbox run agent-egress-probe --image=busybox:1.36 --labels app=agent --restart=Never -- sleep 3600`
  - `kubectlnukedev -n dev-agents-sandbox wait --for=condition=Ready pod/agent-egress-probe --timeout=60s`
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.podSelector.matchLabels.app}'`
  - `deploy_target:` `agent`
- [ ] **Post-Deploy (Rung-2):** an internal service on an allowed port is still blocked — precondition `kubectlnukedev -n dev get svc vault-obsidian-openclaw` returns the service, otherwise the negative passes vacuously on DNS failure. Then `kubectlnukedev -n dev-agents-sandbox exec agent-egress-probe -- wget -qO- --timeout=3 http://vault-obsidian-openclaw.dev.svc.cluster.local:9090` exits non-zero within 5s and prints no body. This is the check that proves the `sentry-proxy` peer is narrow: a bare namespace+port rule would let this through. Negative evidence: exit code ≠ 0, stdout empty
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].to[*].podSelector.matchLabels.app}'`
  - `deploy_target:` `sentry-proxy vault-obsidian-personal vault-obsidian-openclaw vault-obsidian-trading frontend-gateway frontend-strategy pushgateway`
- [ ] **Post-Deploy (Rung-2):** the metadata endpoint is blocked — `kubectlnukedev -n dev-agents-sandbox exec agent-egress-probe -- wget -qO- --timeout=3 http://169.254.169.254/` exits non-zero. Negative evidence: exit code ≠ 0
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.podSelector.matchLabels.app}'`
  - `deploy_target:` `agent`
- [ ] **Post-Deploy (Rung-2):** all nine permitted destinations answer — from the probe pod, each of `192.168.178.41:32159`, `sentry-proxy.dev.svc.cluster.local:9090`, `claude-code-router-dev.dev.svc.cluster.local:8788`, `pushgateway.monitoring.svc.cluster.local:9090`, `vault-obsidian-{personal,openclaw,trading}.dev.svc.cluster.local:9090`, `frontend-gateway.dev.svc.cluster.local:9090` and `frontend-strategy.dev.svc.cluster.local:9090` exits 0 under `kubectlnukedev -n dev-agents-sandbox exec agent-egress-probe -- nc -w 3 <host> <port> </dev/null`. Positive evidence: all nine exit codes 0. Probing only Kafka and sentry proves two of nine, and a policy missing the other seven breaks the agents that need them while this AC stays green. A bare connect is used deliberately, with no `-z`: `-z` is a busybox build variant, and an HTTP probe is wrong for Kafka, which does not speak HTTP
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].to[*].podSelector.matchLabels.app}'`
  - `deploy_target:` `sentry-proxy vault-obsidian-personal vault-obsidian-openclaw vault-obsidian-trading frontend-gateway frontend-strategy pushgateway`
- [ ] **Post-Deploy (Rung-2):** tear down the dev probe pod — **only after every probe above has run**: `kubectlnukedev -n dev-agents-sandbox delete pod agent-egress-probe`. Deleting it earlier makes the two negative ACs pass vacuously, because a `kubectl exec` against a missing pod exits non-zero for the wrong reason
- [ ] **Post-Deploy (Rung-3):** create the prod probe pod — `kubectlnukeprod -n prod-agents-sandbox run agent-egress-probe --image=busybox:1.36 --labels app=agent --restart=Never -- sleep 3600`, then `kubectlnukeprod -n prod-agents-sandbox wait --for=condition=Ready pod/agent-egress-probe --timeout=60s`
  - `deploy_check:` `kubectlnukeprod get ns prod-agents-sandbox -o jsonpath='{.status.phase}'`
  - `deploy_target:` `Active`
- [ ] **Post-Deploy (Rung-3):** the same block/allow pair holds in prod — the same three `exec` probes from `agent-egress-probe`, against `sentry-proxy.prod.svc.cluster.local` and `vault-obsidian-openclaw.prod.svc.cluster.local` (blocked: exit ≠ 0; Kafka on `192.168.178.41:32159` and sentry on 9090: exit 0)
- [ ] **Post-Deploy (Rung-3):** tear down the prod probe pod — `kubectlnukeprod -n prod-agents-sandbox delete pod agent-egress-probe`, after the probes above

## Verification

```bash
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered.yaml            # must exit 0
grep -cE 'kind: (Namespace|NetworkPolicy|Role|RoleBinding)' /tmp/rendered.yaml   # >=4
grep -c 'dev-agents-sandbox' /tmp/rendered.yaml                                  # >=1
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret | grep -c 'prod-agents-sandbox'     # >=1
```

A bare `helm template helm/` exits 1 — the chart has no defaults for `namespace`, `executor.kafkaBrokers`, or `executor.existingSecret`. Every grep must run against a render that actually succeeded, or it passes vacuously against empty stdout.

Post-Deploy ACs are operator-executed against dev, then prod, per `docs/agent-network-security.md`.

**Ordering:** no AC here requires an agent Job. The Kafka/sentry-reachability and policy-selects-the-pod ACs run against a hand-created probe pod labelled `app: agent`, so this chart is verifiable on its own, before `bborbe/agent-task-executor:specs/config-job-namespace.md` ships. Until that lands the namespace carries no real work (Failure Modes row 5), which is exactly why the probe pod exists.

**No dark-factory scenario:** live kube-router enforcement cannot be reproduced by a scenario harness building a fresh binary in `/tmp/`. The Post-Deploy AC ladder against real dev/prod clusters is the substitute, deliberately.

## Desired Behavior

1. The chart renders a namespace named `{{ .Values.namespace }}-agents-sandbox`, so the dev and prod installs own distinct objects (`dev-agents-sandbox`, `prod-agents-sandbox`) rather than colliding on one cluster-scoped name.
2. It renders a NetworkPolicy `agent-sandbox-egress` selecting pods labelled `app: agent`, with `policyTypes: [Egress]` only.
3. The policy permits egress to exactly the destinations the fleet's job pods actually use, and no others. Every row below is derived from the parent goal's `## Egress Matrix` (2026-10-23, `nuke-dev@64abe7e` + 19 live Configs), not from memory:

   | Destination | Namespace | Port | Selector |
   |---|---|---|---|
   | kube-dns | `kube-system` | 53 UDP+TCP | `k8s-app: kube-dns` |
   | `sentry-proxy` | release ns | 9090 | `app: sentry-proxy` |
   | `claude-code-router-dev` | release ns | 8788 | `app.kubernetes.io/instance: claude-code-router-dev` |
   | `pushgateway` | `monitoring` | 9090 | `app: pushgateway` |
   | `vault-obsidian-personal` | release ns | 9090 | `app: vault-obsidian-personal` |
   | `vault-obsidian-openclaw` | release ns | 9090 | `app: vault-obsidian-openclaw` |
   | `vault-obsidian-trading` | release ns | 9090 | `app: vault-obsidian-trading` |
   | `frontend-gateway` | release ns | 9090 | `app: frontend-gateway` |
   | `frontend-strategy` | release ns | 9090 | `app: frontend-strategy` |
   | Kafka ×3 | external | 32159 | `ipBlock` `/32` ×3 |

   - Every in-cluster peer carries **both** `namespaceSelector` (the release namespace, or `monitoring` / `kube-system`) and a `podSelector` in the **same** `to:` entry. Separate peers OR, which would open every service on that port in that namespace — the trap in [[A Namespace-Port Egress Rule Opens Every Service on That Port]].
   - Kafka is three `ipBlock`s — `192.168.178.41/32`, `192.168.178.42/32`, `192.168.178.43/32` — on TCP 32159. Kafka on nuke is the shared cluster's **external nodeport listener**, not an in-cluster service, so these are LAN IPs; an `ipBlock` of `192.168.178.0/24` would allow every host on that subnet on that port.

   Everything else — other internal services, other private CIDRs, `169.254.0.0/16`, direct internet — is denied by omission.

   **What is deliberately NOT permitted:** `attention-controller:18081` (needed only by `claude-interactive`, a long-running Deployment that stays outside the sandbox) and `api.minimax.io:443` (public — spec 048's proxy owns it). The controllers, executors, `a2a-worker-controller` and `vault-api-*` are Deployments in the control namespace and are not subject to this policy at all.

   **One shared allowlist, by design.** Non-goals lists per-agent egress policies: every sandbox pod gets this union, so `trading-backtest-agent` can also reach `vault-obsidian-*` even though its own config does not. Tightening to per-agent `podSelector` rules is the follow-up once the executor labels pods per agent type.

   **Lockstep with spec 048.** `agent-egress-proxy`'s prompt 2 extends this same `agent-sandbox-egress` policy with an **additive** `to:` entry for `app: egress-proxy` on 8888 — it inserts one rule and does not replace the peer list, so all nine peers above survive that change, and it adds a separate `egress-proxy-egress` policy for the proxy pod itself. **That prompt's description of this policy is stale as of this amendment** and must be corrected before it runs: it states the policy "currently has exactly two egress rules (kube-dns, strimzi Kafka)" and instructs the executor to insert "between the kube-dns rule and the strimzi Kafka rule". There is no strimzi rule any more, and eight peers sit in that gap — the insertion point is now the end of the `egress:` list, after the Kafka `ipBlock` rule.

   **Lockstep invariant:** the three IPs duplicate `executor.kafkaBrokers`, whose value is set per install (`values-dev.yaml` / `values-prod.yaml` in the nuke repo — `helm/values.yaml` leaves it empty and required). Changing a broker address in `values-dev.yaml` / `values-prod.yaml` without changing the policy silently denies Kafka egress, and the only symptom is a Job whose result never reaches its topic. Deriving the `ipBlock`s from `executor.kafkaBrokers` (a `range` over `splitList ","` then `splitList ":"`) removes the drift and is the preferred follow-up; until then the two must move together, and Failure Modes records it.

   **NodePort residual:** TCP 32159 is a cluster-wide NodePort, so permitting the three node IPs on that port also reaches **any other service** bound to NodePort 32159 on those nodes — not just Kafka. The exposure is bounded to three hosts and one port, and to whatever else claims that port. Recorded as residual risk in Security.
4. Ingress is not declared at all, because no executor→Job ingress path exists: the executor "does NOT watch Jobs, read stdout, or publish results" and the agent publishes directly to Kafka.
5. The chart renders a Role and RoleBinding named `agent-task-executor-sandbox`, in the sandbox namespace, granting the executor's ServiceAccount `create`/`get`/`list`/`watch`/`delete` on `jobs`, `get` on `secrets`, and `get` on `persistentvolumeclaims` within the environment's sandbox namespace — and nothing else.

## Constraints

- Existing workloads in `dev`/`prod` must not be affected — the policy is scoped to the sandbox namespace and selects only `app: agent`
- Kafka result publishing must not regress; it is the only way a task ever completes
- No capability, securityContext, or privilege change to any pod; agent pods keep `drop: ALL`
- The executor's rights outside its environment's sandbox namespace must not widen
- The dev and prod installs must never share a sandbox namespace, NetworkPolicy, Role, or RoleBinding. On **quant** — where agents run today — `dev` and `prod` are namespaces on one cluster, so any unparameterized cluster-scoped name collides (the chart's `executor-rbac.yaml` already suffixes its ClusterRole for this reason).

  **Transitional, by design:** the new **nukedev** (`192.168.178.30`) and **nukeprod** (`192.168.178.37`) clusters are *separate clusters*, where a plain `agents-sandbox` could not collide and the `{{ .Values.namespace }}-` prefix is redundant. The prefix stays because these specs may still deploy to quant, and it is harmless on separate clusters. **Removal trigger:** once the quant → nuke migration completes and no install targets a shared-namespace cluster, drop the prefix and simplify to `agents-sandbox`
- Deploying this chart before `config-job-namespace.md` ships must be a no-op, not a breakage — nothing spawns into the namespace yet

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Policy applied but `podSelector` matches no pods | The "policy selects the probe pod" AC fails — `get pods -l app=agent` returns 0 | Check the probe pod was created with `--labels app=agent` and reached Ready. A real agent Job carries the label once `config-job-namespace.md` ships, but the probe pod does not depend on it |
| Agent needs an internal service not in the allowlist | Job fails with a connection timeout, visible in Job logs | Add a named exception to the policy; never revert to allow-all |
| A Kafka broker address changes in `values-*` but not in the policy | Kafka egress denied; the Job runs but its result never reaches its topic, and the task never completes | Update the three `ipBlock`s in lockstep with `executor.kafkaBrokers`; better, derive them from that value so the two cannot drift |
| PVCs absent in the new namespace | Job fails to start; `kubectl describe pod` shows unbound PVC | Re-create PVCs and re-seed Secrets in the environment's sandbox namespace before cutover |
| Executor lacks cross-namespace RBAC | Job spawn fails with 403 in executor logs | Re-run `helm upgrade --install`; confirm with `kubectlnukedev -n dev-agents-sandbox get role agent-task-executor-sandbox -o jsonpath='{.rules[*].resources}'` returning `jobs secrets persistentvolumeclaims` |
| Chart deployed while executor is pre-`JobNamespace` | Nothing spawns into the sandbox; namespace sits empty | Expected no-op; deploy order is chart-then-executor or either order |
| Both dev and prod releases create the same sandbox namespace | Second `helm upgrade --install` fails a Helm ownership check, or silently shares one namespace and collapses env isolation | Namespace is parameterized per env (DB 1). If a shared `agents-sandbox` already exists from an earlier install, delete it before re-installing |
| Prometheus can no longer scrape agent pods | Scrape targets go stale for sandbox pods | Ingress is undeclared, so scraping is unaffected — but confirm during dev soak; if broken, add a monitoring-namespace ingress rule |

## Security / Abuse Cases

- **Agent probes internal services.** Denied by default; an agent scanning `10.0.0.0/8` gets timeouts. **Except** the nine non-DNS destinations in DB 3 — six are internal services on port 9090 (sentry-proxy, the three vaults and the two frontends), plus the router on 8788 and pushgateway on 9090 in `monitoring`. A prompt-injected agent can therefore read and write the vaults it is already trusted to write, and reach the trading frontends. That is a real reduction in isolation versus a three-destination policy, and it is the price of the fleet actually working: 15 of the 17 job Configs need at least one of them. Each peer is a single-service `podSelector`, so the exposure is exactly those six services — not "everything on 9090 in `dev`", which is what a namespace+port rule would have opened.
- **Agent queries the cloud metadata endpoint.** Denied by omission — link-local is not in the allowlist. Covered by its own Post-Deploy AC.
- **Agent publishes to a trading Kafka topic.** Still possible — the policy allows Kafka at the network layer and strimzi has no per-topic authz here. Explicit residual risk; the fix is Kafka ACLs, out of scope.
- **Agent escapes via an unlabelled Job.** A Job without `app: agent` is unselected and unrestricted. Mitigated in the sibling spec, where the spawner sets the label unconditionally.
- **Agent reaches a non-Kafka service on NodePort 32159.** The three node IPs are permitted on that port for Kafka, and a NodePort is cluster-wide, so any *other* service bound to 32159 on those hosts is reachable too. Bounded to three hosts and one port. Recorded, not mitigated: deriving the rule from `executor.kafkaBrokers` would not narrow it, because the port stays. The real fix is to stop fronting Kafka with a shared NodePort.
- **Executor's widened RBAC is abused.** Grants are namespace-scoped to `agents-sandbox` and enumerated; no `*` verbs, no cluster-scope.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Namespace + NetworkPolicy helm templates and values wiring | 1, 2, 3, 4 | 1, 2, 6, 7, 8 | — |
| 2 | Sandbox-scoped Role + RoleBinding (`agent-task-executor-sandbox`) for the executor ServiceAccount | 5 | 3, 4, 5 | 1 |

Rationale: the policy and namespace are one coherent template change; RBAC is separable and depends on the namespace existing. Post-Deploy ACs are operator-executed after both land.

## Do-Nothing Option

Agent Jobs keep unrestricted access to the trading stack, Kafka, and the metadata endpoint. Today's only mitigation is that tasks are small and agents are well-behaved — scope discipline, not a security boundary, and it degrades as the fleet grows. Doing nothing also strands `agent-egress-proxy.md`, which has no namespace to deploy into, and strands `config-job-namespace.md`, whose default target would not exist.
