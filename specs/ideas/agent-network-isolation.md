---
status: idea
---

## Summary

- Agent Jobs run in a dedicated, locked-down namespace (e.g. `agents-sandbox`) isolated by NetworkPolicy
- Agents are blocked from reaching internal cluster services, cloud metadata endpoints, and private IP ranges
- Agents retain public internet access (for LLM APIs, package registries, public data sources)
- `AgentConfig` CRDs live in a control namespace (e.g. `agents`); executor watches Configs across namespaces
- New `ConfigSpec.JobNamespace` field lets each agent optionally pin its Jobs to a different namespace (defaults to the Config's namespace)

## Problem

Agent Jobs currently run in the same namespace as the executor (`dev`/`prod`) and inherit that namespace's network access. A misbehaving or prompt-injected agent can reach any internal service (trading APIs, Kafka, Postgres, etc.), the cloud metadata endpoint (169.254.169.254), and all private IPs. There is no firewall between "Claude reasoning about a markdown task" and the production trading stack.

As agents multiply (backtest, trade-analysis, claude, hypothesis, upcoming...) and as tasks become less trusted (user-submitted prompts, agent-generated tasks), the blast radius of a single compromised agent grows. A generic agent-claude accepting arbitrary instructions makes this acute.

## Goal

After this work, agent Jobs run in a namespace where:

1. Egress to internal cluster CIDR (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, link-local `169.254.0.0/16`) is **denied** by default
2. Egress to public internet (`0.0.0.0/0` minus private ranges) is **allowed**
3. Ingress from anything except the executor's result-collection path (or nothing, if results flow via Kafka only) is **denied**
4. Optional allow-list exceptions for specific endpoints (e.g. Kafka, a read-only vault service) declared per-agent

The executor runs in a separate control-plane namespace and reaches across namespaces to spawn Jobs and mount Secrets/PVCs.

## Non-goals

- Per-agent fine-grained egress policy (one sandbox policy for all agents, refine later)
- Pod-to-pod isolation between concurrent agent Jobs
- Runtime sandboxing (gVisor, Kata) — NetworkPolicy only
- Replacing Kubernetes NetworkPolicy with a service mesh
- Auth/authz between agents and internal services (if an agent needs a service, allow it in policy)

## Desired Behavior

### Control plane vs. sandbox

```
namespace: agents              (control)
  - executor Deployment
  - AgentConfig CRDs
  - Kafka SASL config

namespace: agents-sandbox      (data plane)
  - Job pods (agent-claude-*, backtest-agent-*, trade-analysis-*)
  - PVCs (agent-claude, agent-trade-analysis, ...)
  - Secrets (agent-claude, agent-trade-analysis, ...)
  - NetworkPolicy: deny-internal-allow-internet
```

### NetworkPolicy (conceptual)

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: agent-sandbox-egress
  namespace: agents-sandbox
spec:
  podSelector:
    matchLabels:
      app: agent
  policyTypes:
    - Egress
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
              - 169.254.0.0/16
    # Kafka exception (result publishing)
    - to:
        - namespaceSelector:
            matchLabels: { name: strimzi }
          podSelector:
            matchLabels: { strimzi.io/cluster: my-cluster }
      ports:
        - protocol: TCP
          port: 9092
```

### Executor changes

1. **Multi-namespace Config watching:** Add `CONFIG_NAMESPACES` env (comma-separated) so the executor's Config informer watches multiple namespaces, not just its own.
2. **`JobNamespace` field in `ConfigSpec`:** Optional; defaults to the Config's namespace. Spawner uses it for `Jobs(jobNamespace).Create(...)`.
3. **RBAC:** Executor needs cross-namespace permissions for `jobs`, `secrets`, `persistentvolumeclaims` in the sandbox namespace.

### Migration

- Phase 1: Introduce `agents-sandbox` namespace, NetworkPolicy, RBAC — no behavior change (nothing spawns there yet)
- Phase 2: Add `JobNamespace` + multi-namespace Config watch — opt-in per agent
- Phase 3: Move agent-claude Jobs to sandbox first (most risk, least coupling) — validate
- Phase 4: Move remaining agents (trade-analysis, backtest) after adding any required egress exceptions

## Open Questions

- Do we need separate sandbox namespaces per trust tier (public-facing agents vs. internal-data agents)?
- Does trade-analysis need egress to trading APIs? If yes → per-agent egress exceptions (overrides the generic deny)
- Metrics scraping: Prometheus needs ingress to agent pods. Option: allow from `monitoring` namespace only
- PVC access: PVCs are namespace-local, so moving to sandbox means re-creating PVCs there. Credentials re-seed required.

## Related

- [[Build Generic Claude Agent]] — goal that triggered this
- `specs/ideas/agent-definition-crd.md` — CRD foundation this builds on
