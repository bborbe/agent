---
status: prompted
approved: "2026-08-08T20:38:38Z"
generating: "2026-08-21T07:52:06Z"
prompted: "2026-08-21T08:13:02Z"
branch: dark-factory/agent-sandbox-namespace
---

## Summary

- A dedicated per-environment `<env>-agents-sandbox` namespace is added to the helm chart, with a default-deny egress NetworkPolicy
- Agent Jobs spawned into it can reach kube-dns, `sentry-proxy` and the three Kafka nodeport IPs, and nothing else — no other internal services, no other private ranges, no cloud metadata endpoint
- The executor gets cross-namespace RBAC so it can create Jobs, read Secrets, and bind PVCs there
- Public-internet egress stays denied here; `agent-egress-proxy.md` restores it through a filtered path
- The executor-side mechanism (`ConfigSpec.JobNamespace`, pod labelling) is specified separately in `bborbe/agent-task-executor:specs/config-job-namespace.md` and must ship first

## Problem

Agent Jobs run in the same namespace as the executor and inherit that namespace's network access. There is no boundary between an agent reasoning about a markdown task and the production trading stack.

Verified 2026-08-08: zero NetworkPolicies exist in `dev`/`prod` on either cluster, and strimzi's Kafka client listeners (`:9092`–`:9095`) accept `FROM: ANY`. A prompt-injected agent can reach any internal service, publish to any Kafka topic including the trading ones, and query `169.254.169.254`. With 10 `Config` CRs per namespace and tasks increasingly sourced from untrusted content, that blast radius keeps growing.

Background, manifests, and the traps this inherits: `docs/agent-network-security.md`.

## Goal

`<env>-agents-sandbox` exists in both dev and prod with a policy that denies egress by default and permits only kube-dns, `sentry-proxy` and the Kafka nodeports. Agent Jobs land there and provably cannot reach an internal service. The executor, still in the control namespace, retains exactly the cross-namespace rights it needs and no more.

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
- [ ] The policy declares egress only, no ingress — `$RENDER | grep -A30 'name: agent-sandbox-egress' | grep -c 'policyTypes'` returns 1, and the rendered `policyTypes` list contains only `Egress`. Negative evidence: `$RENDER | grep -A30 'name: agent-sandbox-egress' | grep -c 'ingress:'` returns 0
- [ ] The sandbox namespace is per-environment, not a shared literal — `helm template helm/ --set namespace=dev ... | grep -c 'dev-agents-sandbox'` returns ≥1 **and** `helm template helm/ --set namespace=prod ... | grep -c 'prod-agents-sandbox'` returns ≥1. Negative evidence: neither render contains a bare `name: agents-sandbox` line
- [ ] **Post-Deploy (Rung-2):** the namespace and policy exist in dev — `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.metadata.name}'` returns `agent-sandbox-egress`
  - `deploy_check:` `kubectlnukedev get ns dev-agents-sandbox -o jsonpath='{.status.phase}'`
  - `deploy_target:` `Active`
- [ ] **Post-Deploy (Rung-2):** the policy selects the probe pod — `kubectlnukedev -n dev-agents-sandbox get pods -l app=agent -o name | wc -l` returns ≥1. The pod is hand-created (`busybox:1.36`); no agent Job runs here until `config-job-namespace.md` ships
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.podSelector.matchLabels.app}'`
  - `deploy_target:` `agent`
- [ ] **Post-Deploy (Rung-2):** an internal service on an allowed port is still blocked — from inside the probe pod, `wget -qO- --timeout=3 http://vault-obsidian-openclaw.dev.svc.cluster.local:9090` exits non-zero within 5s and prints no body. This is the check that proves the `sentry-proxy` peer is narrow: a bare namespace+port rule would let this through. Negative evidence: exit code ≠ 0, stdout empty
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].ports[*].port}'`
  - `deploy_target:` `53 53 32159 9090`
- [ ] **Post-Deploy (Rung-2):** the metadata endpoint is blocked — from the same pod, `wget -qO- --timeout=3 http://169.254.169.254/` exits non-zero. Negative evidence: exit code ≠ 0
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.podSelector.matchLabels.app}'`
  - `deploy_target:` `agent`
- [ ] **Post-Deploy (Rung-2):** Kafka and `sentry-proxy` are reachable — from the same probe pod, `nc -z 192.168.178.41 32159` and `nc -z sentry-proxy.dev.svc.cluster.local 9090` both exit 0. Positive evidence: both exit codes 0
  - `deploy_check:` `kubectlnukedev -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].ports[*].port}'`
  - `deploy_target:` `53 53 32159 9090`
- [ ] **Post-Deploy (Rung-3):** the same block/allow pair holds in prod — from a pod in `prod-agents-sandbox`, `vault-obsidian-openclaw.prod.svc.cluster.local:9090` is blocked (exit ≠ 0), `nc -z 192.168.178.41 32159` exits 0, and `nc -z sentry-proxy.prod.svc.cluster.local 9090` exits 0
  - `deploy_check:` `kubectlnukeprod get ns prod-agents-sandbox -o jsonpath='{.status.phase}'`
  - `deploy_target:` `Active`

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
3. The policy permits egress to exactly three destinations, and no others:
   - kube-dns, 53 UDP+TCP
   - `sentry-proxy` in the environment's control namespace, 9090 TCP — a **single** peer carrying both `namespaceSelector` (the release namespace, templated, not a literal) and `podSelector` (`sentry-proxy`). Separate peers would OR and open every service on 9090 in that namespace, which is why the `vault-obsidian-openclaw` block AC exists
   - Kafka, TCP 32159, as three `ipBlock`s — `192.168.178.41/32`, `192.168.178.42/32`, `192.168.178.43/32`. Kafka on nuke is the shared cluster's **external nodeport listener**, not an in-cluster service, so these are LAN IPs; an `ipBlock` of `192.168.178.0/24` would allow every host on that subnet on that port

   Everything else — other internal services, other private CIDRs, `169.254.0.0/16`, direct internet — is denied by omission.
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
| Policy applied but `podSelector` matches no pods | AC "policy selects real pods" fails; `get pods -l app=agent` returns 0 | Ship `config-job-namespace.md` first — the label comes from the executor. The policy is inert until it matches |
| Agent needs an internal service not in the allowlist | Job fails with a connection timeout, visible in Job logs | Add a named exception to the policy; never revert to allow-all |
| PVCs absent in the new namespace | Job fails to start; `kubectl describe pod` shows unbound PVC | Re-create PVCs and re-seed Secrets in the environment's sandbox namespace before cutover |
| Executor lacks cross-namespace RBAC | Job spawn fails with 403 in executor logs | Re-run `helm upgrade --install`; confirm with `kubectlnukedev -n dev-agents-sandbox get role agent-task-executor-sandbox -o jsonpath='{.rules[*].resources}'` returning `jobs secrets persistentvolumeclaims` |
| Chart deployed while executor is pre-`JobNamespace` | Nothing spawns into the sandbox; namespace sits empty | Expected no-op; deploy order is chart-then-executor or either order |
| Both dev and prod releases create the same sandbox namespace | Second `helm upgrade --install` fails a Helm ownership check, or silently shares one namespace and collapses env isolation | Namespace is parameterized per env (DB 1). If a shared `agents-sandbox` already exists from an earlier install, delete it before re-installing |
| Prometheus can no longer scrape agent pods | Scrape targets go stale for sandbox pods | Ingress is undeclared, so scraping is unaffected — but confirm during dev soak; if broken, add a monitoring-namespace ingress rule |

## Security / Abuse Cases

- **Agent probes internal services.** Denied by default; an agent scanning `10.0.0.0/8` gets timeouts.
- **Agent queries the cloud metadata endpoint.** Denied by omission — link-local is not in the allowlist. Covered by its own Post-Deploy AC.
- **Agent publishes to a trading Kafka topic.** Still possible — the policy allows Kafka at the network layer and strimzi has no per-topic authz here. Explicit residual risk; the fix is Kafka ACLs, out of scope.
- **Agent escapes via an unlabelled Job.** A Job without `app: agent` is unselected and unrestricted. Mitigated in the sibling spec, where the spawner sets the label unconditionally.
- **Executor's widened RBAC is abused.** Grants are namespace-scoped to `agents-sandbox` and enumerated; no `*` verbs, no cluster-scope.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Namespace + NetworkPolicy helm templates and values wiring | 1, 2, 3, 4 | 1, 2, 6, 7, 8 | — |
| 2 | Sandbox-scoped Role + RoleBinding (`agent-task-executor-sandbox`) for the executor ServiceAccount | 5 | 3, 4, 5 | 1 |

Rationale: the policy and namespace are one coherent template change; RBAC is separable and depends on the namespace existing. Post-Deploy ACs are operator-executed after both land.

## Do-Nothing Option

Agent Jobs keep unrestricted access to the trading stack, Kafka, and the metadata endpoint. Today's only mitigation is that tasks are small and agents are well-behaved — scope discipline, not a security boundary, and it degrades as the fleet grows. Doing nothing also strands `agent-egress-proxy.md`, which has no namespace to deploy into, and strands `config-job-namespace.md`, whose default target would not exist.
