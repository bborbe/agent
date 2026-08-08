# Agent Network Security

Reference for the network posture of agent Jobs: what the platform enforces, what the policies look like, and the traps that have already cost time. Specs reference this doc rather than inlining manifests.

## The platform enforces NetworkPolicy — no CNI change needed

**A `kind: NetworkPolicy` applied to these clusters takes effect immediately.**

K3s embeds a NetworkPolicy controller (kube-router's `netpol` package) in the `k3s server` process. It is enabled by default unless the server is started with `--disable-network-policy`, and it is **independent of the CNI** — Flannel provides the dataplane, kube-router programs the policy rules.

### Why this is written down

From 2026-05-02 to 2026-08-08 the opposite was believed. A NetworkPolicy (`vault-obsidian-openclaw-ingress`, spec 018) applied cleanly but did not block, and the conclusion drawn was "K3s + Flannel doesn't enforce NetworkPolicy." That inference is wrong. The observations behind it were all true — K3s 1.32.5, Flannel, no Calico daemonset — only the conclusion was false, which is why it survived: a stale conclusion doesn't look out of date the way a stale fact does.

The cost: a hold-the-line rule forbidding new NetworkPolicy YAML "until the CNI is replaced" blocked all policy work for three months, and a multi-week CNI-migration project was planned that was never necessary.

### Verify it yourself (read-only)

```bash
# 1. Is the controller disabled? (absence of the flag = enabled)
sudo cat /etc/rancher/k3s/config.yaml | grep -i disable-network-policy
ps -eo args | grep '[k]3s server'

# 2. Are kube-router's rules actually in the kernel?
sudo iptables-save | grep -cE 'KUBE-ROUTER|KUBE-NWPLCY'
sudo iptables-save | grep -oE 'KUBE-(ROUTER-[A-Z]+|NWPLCY-[A-Z0-9]+)' | sort -u
```

Expect `KUBE-ROUTER-INPUT`, `KUBE-ROUTER-OUTPUT`, `KUBE-ROUTER-FORWARD`, `KUBE-NWPLCY-COMMON`, `KUBE-NWPLCY-DEFAULT`. Rule count scales with how many policies exist. Measured 2026-08-08: nukedev 20, quant 100.

**Corollary:** a policy that applies cleanly but does not block is a **bug in the policy** — selector mismatch, wrong `policyTypes`, wrong direction. Never conclude the platform is at fault. Verify every policy by curling a denied destination from inside a pod; `kubectl apply` succeeding proves nothing.

## Threat model — two directions

| Direction | Threat | Control |
|---|---|---|
| **Inward** — agent → cluster | Prompt-injected agent reaches trading APIs, Kafka, Postgres, `169.254.169.254` | NetworkPolicy default-deny egress + named exceptions |
| **Outward** — agent → internet | Credential exfiltration (`ANTHROPIC_AUTH_TOKEN`, GitHub App `PEM_KEY`), C2 | Egress proxy with domain allowlist; direct internet denied |

Both were fully open as of 2026-08-08: zero NetworkPolicies in `dev`/`prod`, and strimzi's Kafka client listeners (`:9092`–`:9095`) accept `FROM: ANY`.

## Why a central proxy, not a sidecar

A sidecar shares the pod's network namespace — one pod, one IP — so NetworkPolicy cannot distinguish agent traffic from proxy traffic. The agent can ignore `HTTPS_PROXY` and dial out directly. Enforcing a sidecar would require programming iptables inside the pod netns via a `NET_ADMIN` initContainer (the Istio pattern), reintroducing `NET_ADMIN` into pods that currently run `drop: ALL`.

A separate proxy pod has a **separate IP**, which is exactly what NetworkPolicy enforces well. The kernel becomes the boundary instead of in-container UID separation — strictly stronger, since a root exploit inside the container defeats a UID rule but not a policy applied to the pod.

### Prior art: claude-yolo

`~/Documents/workspaces/claude-yolo` (`docs/network-firewall.md`) solves this locally with two layers:

| Layer | Runs as | Job | File |
|---|---|---|---|
| tinyproxy `127.0.0.1:8888` | `root` (UID 0) | domain allowlist regex, CONNECT-only on 443/80/7999 | `files/tinyproxy.conf`, `files/tinyproxy-allowlist` |
| iptables | kernel | only UID 0 egresses directly; all other UIDs REJECTed | `files/init-firewall.sh` |

Its own doc: *"tinyproxy alone could be bypassed by Claude making direct outbound."* **The allowlist is advice; the enforcement layer is what makes it real.** In k8s that enforcement layer is NetworkPolicy instead of the UID rule.

Reusable directly:
- `files/tinyproxy-allowlist` — curated allowlist, seed the ConfigMap from it
- `files/init-firewall.sh:75-85` — the startup self-test (see below)

### The self-test is not optional

```bash
# Negative: a non-allowlisted domain MUST fail
curl --proxy http://<proxy>:8888 --connect-timeout 5 https://example.com   # must fail
# Positive: an allowlisted domain MUST succeed
curl --proxy http://<proxy>:8888 --connect-timeout 5 https://api.github.com/zen  # must succeed
```

Without the **negative** check a broken-open allowlist ships silently and looks perfectly healthy. Without the **positive** check an over-tight allowlist looks identical to a working one until an agent fails hours later.

## Cluster topology matters for naming

| Cluster | dev/prod separation | Consequence |
|---|---|---|
| **quant** (agents run here today) | namespaces `dev` / `prod` on **one** cluster | a cluster-scoped name must be per-env prefixed or the two helm installs collide |
| **nukedev** `192.168.178.30` / **nukeprod** `192.168.178.37` | **separate clusters** | no collision possible; a plain `agents-sandbox` would be fine |

The chart prefixes the sandbox namespace (`{{ .Values.namespace }}-agents-sandbox`) because it must work on quant. That prefix is redundant — but harmless — on the nuke clusters. Once the migration completes and nothing deploys to a shared-namespace cluster, the prefix can be dropped.

## Target topology

```
namespace: agents              (control plane)
  - executor Deployment
  - AgentConfig CRDs
  - Kafka SASL config

namespace: agents-sandbox      (data plane)
  - Job pods (agent-claude-*, backtest-agent-*, github-dark-factory-agent-*, ...)
  - egress-proxy Deployment + Service + allowlist ConfigMap
  - PVCs, Secrets
  - NetworkPolicy: default-deny egress
```

## NetworkPolicy reference

Egress from sandbox agent pods. Everything not listed is denied by omission — including private CIDRs, `169.254.169.254`, and direct internet.

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
    # DNS
    - to:
        - namespaceSelector:
            matchLabels: { kubernetes.io/metadata.name: kube-system }
          podSelector:
            matchLabels: { k8s-app: kube-dns }
      ports:
        - { protocol: UDP, port: 53 }
        - { protocol: TCP, port: 53 }
    # The only path to the public internet
    - to:
        - podSelector:
            matchLabels: { app: egress-proxy }
      ports:
        - { protocol: TCP, port: 8888 }
    # Kafka (result publishing)
    - to:
        - namespaceSelector:
            matchLabels: { name: strimzi }
          podSelector:
            matchLabels: { strimzi.io/cluster: my-cluster }
      ports:
        - { protocol: TCP, port: 9092 }
```

**Ingress is denied entirely.** There is no executor→Job ingress path: the executor "does NOT watch Jobs, read stdout, or publish results" (`docs/agent-job-interface.md`), and "the agent publishes its result directly to Kafka" (`docs/agent-job-lifecycle.md`). No ingress rule is needed.

The proxy pod carries its own policy allowing egress to `0.0.0.0/0` minus private ranges.

## Executor changes

```go
// Spawner uses the resolved namespace rather than its own.
clientset.BatchV1().Jobs(jobNamespace).Create(ctx, job, metav1.CreateOptions{})
```

- `CONFIG_NAMESPACES` (comma-separated) so the Config informer watches multiple namespaces
- `ConfigSpec.JobNamespace` — see the default-direction trap below
- RBAC: cross-namespace `jobs`, `secrets`, `persistentvolumeclaims` in the sandbox namespace
- Spawned Job pods must carry `app: agent` or the policy's `podSelector` matches nothing

## Traps

| Trap | Why it bites |
|---|---|
| **A policy selecting zero pods looks exactly like a working policy** | No error, no event, no traffic blocked. Always confirm the `podSelector` matches real Job pods *and* prove the block from inside a pod. |
| **`JobNamespace` default direction** | If it defaults to the Config's namespace and Configs live in the control namespace `agents`, the default spawns Jobs *next to the executor and Kafka SASL config*, unprotected. The default must resolve to the sandbox. |
| **Allowlist `api.minimax.io`, not just Anthropic** | Prod `github-dark-factory-agent` runs `ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic`. Allowlisting only `api.anthropic.com` breaks every agent on that config. |
| **SSH bypass** | claude-yolo deliberately allows SSH on 22/7999 outside the proxy so git works, accepting the risk because no keys are mounted. A default-deny policy blocks it. Agents using GitHub App `PEM_KEY` over HTTPS are unaffected; verify per agent. |
| **Fails closed by design** | Any tool not honouring proxy env simply breaks. Desired property, main rollout risk — one agent at a time in dev, never a blanket apply. |
| **Unresolved: the 2026-05-02 failure** | Why did `vault-obsidian-openclaw-ingress` not block when enforcement was live? Reconstruct from git history before writing new policies — the same bug can recur, and "the platform doesn't enforce" is no longer available as an explanation. |
