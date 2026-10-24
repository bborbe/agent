---
status: draft
---

## Summary

- The chart renders exactly one sandbox namespace today, named by `sandbox.namespace` or derived as `<namespace>-agents-sandbox`.
- An install needs several: `agent-sandbox`, `development-sandbox`, `sentry-sandbox`, `trading-sandbox`, each with its own default-deny egress policy.
- The chart gains a `sandbox.domains` map; each entry renders its own `Namespace` and its own policy, named `<domain>-egress`.
- Each agent's chart-rendered Secret and PVC is copied into the sandbox that agent's `jobNamespace` names.
- An install that declares no domains renders exactly what it renders today, so the version bump lands inert until a values file opts in.

## Problem

The single sandbox namespace is a shared boundary. A Job pod carrying an LLM token and a GitHub App key sits in the same namespace as every other domain's Job pods, so the one default-deny policy has to admit the union of every destination any agent needs — the vaults, the Sentry proxy, the two trading frontends and Kafka, all at once. A per-domain policy cannot be expressed against a single namespace, and the chart has no mechanism to render more than one: the sandbox name is a single helper, the policy is a single document, and the storage copy targets that one name.

⚠️ **Four domain namespaces are not by themselves isolation.** An install that declares four domains and narrows none of their peer sets renders four identical full-union policies — a namespace boundary with the same reach on both sides. The narrowing is the `peers` key, and making it expressible is the point of this spec.

## Goal

An install can declare an arbitrary set of sandbox namespaces in its values. Each renders its own `Namespace` and its own default-deny egress policy, built from a named peer catalog. The egress proxy and every object it owns render once, in a namespace the values name explicitly, and **every domain can reach that proxy across the namespace boundary** — a peer that matches nothing is a denial, and the chart's proxy is the only route to the public internet. Each agent's chart-rendered Secret and PVC lands in the sandbox its `jobNamespace` names. An install that declares nothing is unchanged.

## Non-goals

- Moving Config CRDs between namespaces, or changing which namespace the controller watches.
- Narrowing egress per agent (label-level) inside a domain.
- Changing the egress proxy's allowlist contents, or turning `egressProxy.injectAgentEnv` on by default.
- Rendering or relocating `type: service` StatefulSets — the executor owns those.
- Per-domain `ResourceQuota`. The per-agent quota in `agents.yaml` renders in the control namespace and does not bind Job pods in a domain namespace; that gap is pre-existing and this change multiplies it rather than closing it.
- **A domain with no internet egress.** `dns` and `proxy` are unconditional (DB3); an install that wants a domain cut off from the public internet is a separate spec with its own argument, not a values key here.
- **Requiring `peers` on every declared domain.** Omitting it renders today's full union, deliberately: the first apply of a domains install must not change any agent's reach, and the narrowing then lands as a separate, reviewable values change. Making `peers` mandatory would force the partition and the narrowing into one all-or-nothing step — and a mistake there is the silent-egress-loss row in Failure Modes.
- Publishing the chart or cutting a release tag.

## Assumptions

- The executor already spawns Jobs into `Config.spec.jobNamespace` — shipped in chart 0.22.1, and the chart is at 0.22.3 today. This spec moves no Job by itself; it makes the namespace set configurable.
- One egress-proxy instance can serve every domain: the proxy filters destination hosts, not clients, and its allowlist is domain-agnostic.
- An operator accepts placing an externally-managed Secret or PVC by hand in each domain namespace; the chart copies only objects it renders itself.

## Acceptance Criteria

- [ ] **AC1 — a domain set renders one Namespace per entry.** `helm template` with a values file declaring N domains emits exactly N `kind: Namespace` documents, named exactly those N keys, **each carrying `helm.sh/resource-policy: keep`**. Evidence: the set of `metadata.name` over `kind: Namespace` documents equals the declared names exactly (the Verification block declares five, so the criterion is exercised at a count the spec never hardcodes) (set equality, not a count — a dropped Namespace still reaches the same `kind:` tally), and each of those four documents carries `helm.sh/resource-policy: keep` — checked per document, not by a bare `grep -c` over the whole render, which also counts the identical annotation on every PVC.
- [ ] **AC2 — each domain gets its own Egress-only policy, from its own peer set.** Every declared domain `<D>` renders a `NetworkPolicy` **named `<D>-egress`** in namespace `<D>`, with `podSelector.matchLabels.app: agent` and `policyTypes` exactly `[Egress]`. The name is per-domain on purpose: the legacy policy is `agent-sandbox-egress` and the suite's `policy_doc` helper matches by name only (ten suite cases pin that name: `sandbox-dev`, `sandbox-prod`, `sandbox-explicit`, `kafka-incluster`, `kafka-incluster-nolabels`, `kafka-default`, `router-override`, `vault-api`, `peer-namespace`, `peer-namespace-default`), so four domains sharing one name would concatenate into a blob and every per-domain assertion would be meaningless. With `development-sandbox: {peers: [router, pushgateway, sentry-proxy, kafka]}` and `trading-sandbox: {peers: [router, pushgateway, frontend-gateway, frontend-strategy]}`, the `trading-sandbox-egress` document contains **zero** `app: sentry-proxy` selector lines and **zero** `app: vault-obsidian-` selector lines, while `development-sandbox-egress` carries `app: sentry-proxy` and an in-cluster `kafka` peer. Evidence: `--show-only templates/sandbox.yaml`; each policy document selected by `kind` + `metadata.name` + `metadata.namespace`; then `policy_doc "$out" <D>-egress | grep -q 'app: sentry-proxy'` (and one per listed peer — `doc_has` is an object-identity helper and cannot assert a peer), a negative `grep -c` per foreign peer returning 0, and an explicit assertion of the `app: agent` pod selector.
- [ ] **AC3 — the executor's sandbox RBAC renders once per domain.** Each declared domain namespace contains a `Role` and a `RoleBinding` named `agent-task-executor-sandbox`, the binding's subject being the executor ServiceAccount in the control namespace. Evidence: `doc_has <render> Role agent-task-executor-sandbox <domain>` and the RoleBinding equivalent, once per declared domain, and the subject's `namespace` equal to the control namespace.
- [ ] **AC4 — one egress proxy, in a declared domain, reachable from every domain.** Exactly one `egress-proxy` Deployment, one Service, one PodDisruptionBudget, one allowlist ConfigMap, one `egress-proxy-ingress` and one `egress-proxy-egress` policy render, **each identified by kind + name + namespace** (not by a per-kind count) in `sandbox.proxyNamespace`, and `proxyNamespace` must be one of the declared domains — a proxy in a namespace nothing creates is a silent outage. `egress-proxy-ingress` admits `app: agent` pods from every rendered sandbox namespace. **And the egress side:** every domain's policy carries a `proxy` peer whose `to:` entry holds `namespaceSelector` for `proxyNamespace` **and** `podSelector: app: egress-proxy` **in the same entry** on port 8888 — a bare `podSelector` matches only same-namespace pods, so in every other domain it would match nothing, default-deny would hold, and all internet egress would be lost with no render failure. Evidence: identity per object, one `kubernetes.io/metadata.name: <domain>` entry per declared domain in the ingress policy, and the `proxy` peer's two selectors asserted inside one `to:` block in every `<D>-egress` document.
- [ ] **AC5 — the storage copy follows `jobNamespace` in domains mode, and only then.** With `sandbox.renderAgentStorage: true` and domains declared, an agent whose `jobNamespace` names domain D has its Secret and PVC rendered in D; an agent whose `jobNamespace` is absent or names a namespace that is not a declared domain renders **no** sandbox copy. Evidence: `--show-only templates/agents.yaml` — positive `doc_has` per declared domain for the mapped agent, and `grep -c 'namespace: <other-domain>'` returning 0 for each non-target domain.
- [ ] **AC6 — no install changes its render unless it opts in.** With no `domains` key, the render's object identities — the set of `(kind, metadata.name, metadata.namespace)` over every document — equal those of the branch point. Evidence: `BASE` is the fork SHA recorded when the change was implemented — not a fresh `git merge-base`, which moves as master advances — then render that commit's chart (`git archive "$BASE" | tar -x -C "$tmp/base"`, then `helm template … "$tmp/base/helm"`) and the worktree's (`helm template … ./helm`) with the same values, extract the identity triples from both and `diff` them — empty. Identities carry no labels, so the chart-version bump cannot perturb this comparison, which is why the check is on triples and not on bytes. ⚠️ **No `grep` for a `<domain>-sandbox` string here:** the legacy policy is named `agent-sandbox-egress` and must stay that name — ten existing suite cases pin it — so such a grep returns 3 on a *correct* render (the policy name plus two comment lines) and could only pass by breaking the identity clause above. The absence of a domain `Namespace` is already proved by the triple diff. Content changes the triples cannot see are covered by the suite's existing cases, which must stay green: `sandbox-dev`, `sandbox-prod`, `kafka-incluster`, `kafka-default`, `peer-namespace`, `peer-namespace-default`, `router-override`, `vault-api`.
- [ ] **AC7 — a domain named as the control namespace fails the render.** A values file whose `sandbox.domains` contains the control namespace's name fails `helm template` with a non-zero exit and a message naming the collision, rather than rendering a second `Namespace` object for the control namespace. Evidence: exit code non-zero, and stderr contains the control namespace's name.
- [ ] **AC8 — `sandbox.namespace` and `domains` together fail the render.** Declaring both is ambiguous — a fifth namespace, or one silently ignored — so the render fails naming the conflict, mirroring AC7. Evidence: exit code non-zero, stderr names both keys.
- [ ] **AC9 — the proxy env points at the proxy's namespace.** With `egressProxy.injectAgentEnv: true`, domains declared and `proxyNamespace: P`, the rendered agent Config carries `http://egress-proxy.P.svc.cluster.local:8888` for `HTTP_PROXY`/`HTTPS_PROXY` and their lowercase twins, and **no** other domain's host. Evidence: `--show-only templates/agents.yaml`, positive match on `egress-proxy.P.svc.cluster.local:8888`, and `grep -c 'egress-proxy.<any-other-domain>'` returning 0.
- [ ] **AC10 — the peer set comes from values, including for a domain the spec never names.** Declare `research-sandbox: {peers: [router, vault-trading]}` — a name that appears nowhere else — and assert its document contains the **rendered selector** `app: vault-obsidian-trading` (the catalog name `vault-trading` never appears in the output; grepping it returns 0 on a correct render and would only pass by planting the literal in a comment) and the router's instance selector; contains **zero** `app: sentry-proxy`, `app: pushgateway`, `app: frontend-` and `metadata.name: kafka`; and **does** contain `k8s-app: kube-dns` (DB3's unconditional peer). Separately, declare one domain with the `peers` key **omitted** and assert its document carries the full union. Evidence: `--show-only templates/sandbox.yaml`, every grep targeting a **selector line** (`app: …`, `k8s-app: …`, `metadata.name: …`) rather than a bare catalog word — the policy record includes its header comment block, which contains those words and would make a bare grep pass on any implementation, correct or not. Without this criterion, an implementation that hardcodes the example domain names and never reads `peers` passes every other one.
- [ ] **AC11 — the fail-fast rules are implemented, not just documented.** Four bad values files, each asserting a non-zero `helm template` exit and a message naming the offending key: (a) a domain whose `peers` names `not-a-peer`; (b) two domains declared with no `proxyNamespace`; (c) `peers: [vault-api]` with no `sandbox.vaultApiApp`; (d) `proxyNamespace: not-a-domain` with domains declared; (e) `proxyNamespace` set with no `domains`. Evidence: five non-zero exits with the named key in stderr, in the same shape as the suite's existing `sandbox-ns-collision` and `kafka-missing-port` cases. Case (d) is the one that keeps a proxy out of a namespace nothing creates — without it, the check DB4 requires can be omitted and every other criterion still passes.
- [ ] **AC12 — the chart's own suite passes.** `make test` exits 0 in this worktree, including new `hack/test-helm-render.sh` cases covering AC1–AC11, plus a domains-mode `executor.enabled: false` case asserting no sandbox object renders. That last one is not covered by the existing `sandbox-disabled` case: it greps for `agents-sandbox`, which a domain literally named `agent-sandbox` does not match, so it would pass while domain namespaces rendered anyway. Evidence: exit code 0.

## Verification

Container-executable — no operator rung, no cluster access, no deployed-system check. The container mounts the repo as its working directory, so **no `cd` is needed**; a `cd ~/…` would fail there, where `~` is `/home/node`.

```bash
make test          # AC12, plus the existing cases AC6 names

cat > /tmp/domains.yaml <<'EOF'
namespace: dev
executor:
  kafkaBrokers: 192.168.178.41:32159
  sentry:
    dsn: dummy
egressProxy:
  injectAgentEnv: true
sandbox:
  proxyNamespace: agent-sandbox
  renderAgentStorage: true
  kafka:
    namespace: kafka
    servicePort: 9092
    podLabels:
      strimzi.io/cluster: sun
  domains:
    agent-sandbox: {}
    development-sandbox:
      peers: [router, pushgateway, sentry-proxy, kafka]
    sentry-sandbox:
      peers: [router, pushgateway, kafka, vault-personal]
    trading-sandbox:
      peers: [router, pushgateway, frontend-gateway, frontend-strategy]
    research-sandbox:
      peers: [router, vault-trading]
# Two agents on purpose: one whose jobNamespace names a domain (AC5 positive, AC9)
# and one with no jobNamespace (AC5 negative). Without an agents: list the render
# emits zero Config documents and neither criterion has anything to observe.
agents:
  - name: mapped-agent
    enabled: true
    assignee: mapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: trading-sandbox
  - name: unmapped-agent
    enabled: true
    assignee: unmapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF

helm template agent ./helm -f /tmp/domains.yaml --show-only templates/sandbox.yaml
helm template agent ./helm -f /tmp/domains.yaml --show-only templates/agents.yaml | grep -c 'egress-proxy.agent-sandbox.svc'
```

Expected: five `Namespace` documents; five policies named `<domain>-egress`; `trading-sandbox-egress` with no `sentry-proxy` and no `vault-obsidian-` peer; `development-sandbox-egress` with both; `research-sandbox-egress` with only `router`, `vault-trading`, `kube-dns` and the unconditional `proxy`; every policy's `proxy` peer carrying a namespace selector for `agent-sandbox` in the same `to:` as `app: egress-proxy`; `mapped-agent`'s Secret and PVC rendered in `trading-sandbox` and in no other sandbox; `unmapped-agent` rendering **no** sandbox copy at all; and both agents' Configs carrying `http://egress-proxy.agent-sandbox.svc.cluster.local:8888`. With `sandbox.kafka.namespace` set the `kafka` peer renders as a namespace selector — leave it unset and it renders as three `/32` blocks on 32159, where the string `kafka` never appears and a grep for it passes vacuously. `--version` is deliberately absent: against a local path it is ignored, and adding it would look like it pinned a release it did not.

## Desired Behavior

1. `sandbox.domains` is a map of namespace name to a block; an empty or absent map falls back to today's single sandbox exactly. Declaring `domains` **and** `sandbox.namespace` fails the render.
2. Each domain's policy is built from a **fixed catalog of named peers**: `dns`, `router`, `pushgateway`, `sentry-proxy`, `vault-personal`, `vault-openclaw`, `vault-trading`, `frontend-gateway`, `frontend-strategy`, `vault-api`, `kafka`, `proxy`. A peer entry is a **name only**; an unknown name fails the render. Each name resolves its namespace from a fixed rule, so a values file cannot point a peer at an arbitrary namespace:
   - `dns` → `kube-system`, `k8s-app: kube-dns`;
   - `router` → the control namespace, instance label from `sandbox.routerInstance`;
   - `pushgateway` → `monitoring`, `app: pushgateway`;
   - `sentry-proxy`, `vault-personal`, `vault-openclaw`, `vault-trading`, `frontend-gateway`, `frontend-strategy` → `sandbox.peerNamespace`, defaulting to the control namespace — the existing key keeps its meaning and stays load-bearing;
   - `vault-api` → `sandbox.peerNamespace`, `app` from `sandbox.vaultApiApp`; listing the peer without that value fails the render;
   - `kafka` → `sandbox.kafka.namespace` / `servicePort` / `podLabels`, or the built-in nodeport addresses when unset;
   - `proxy` → `sandbox.proxyNamespace`, `app: egress-proxy`, selectors in one `to:` entry.
3. **`dns` and `proxy` are added to every domain unconditionally**, whether or not `peers` lists them. Both are load-bearing in a way the other peers are not: `dns` is how a pod resolves anything, and `proxy` is the chart's only route to the public internet — making it opt-in would be an off-switch for egress that fails silently, which is the exact failure class this spec's Failure Modes table exists to name. Every other peer is opt-in, and **omitting a domain's `peers` entirely renders today's full union**, which keeps the default behaviour-preserving — and is why four domains alone buys no narrowing.
4. The egress proxy and **every object it owns** — Deployment, Service, PodDisruptionBudget, allowlist ConfigMap, `egress-proxy-ingress`, `egress-proxy-egress` — render exactly once, in `sandbox.proxyNamespace`, which must be one of the declared domains. `proxyNamespace` is **required when more than one domain is declared** and fails the render when absent: a default would have to pick a map key, and Helm ranges a map alphabetically, so "the first domain" would silently mean `agent-sandbox` and silently change if a domain were added. With exactly one domain it defaults to that domain. Setting it with **no** domains declared fails the render — it would otherwise name a namespace nothing creates.
5. With `domains` declared, `sandbox.renderAgentStorage` copies each agent's Secret and PVC into the sandbox its `jobNamespace` names, and only when that name is a declared domain. With **no** `domains`, the copy keeps targeting the single sandbox **regardless of `jobNamespace`** — the current behaviour, and the one the existing `storage-enabled` render case pins.
6. The proxy env helper resolves to `sandbox.proxyNamespace`, not to "the" sandbox, so an agent in any domain reaches the one proxy.
7. `sandbox.namespace` keeps working and its render is unchanged.
8. With `executor.enabled: false`, no sandbox object renders at all in either mode — the current gate, unchanged.

## Constraints

- `sandbox.namespace` remains supported; removing it would rename a live namespace and orphan its PVCs.
- `helm.sh/resource-policy: keep` stays on every rendered `Namespace` and PVC.
- Every new values key is optional except `proxyNamespace` when more than one domain is declared; no existing install gains a required value.
- `hack/test-helm-render.sh`'s existing cases encode the current render and must stay green — including `storage-enabled`, which asserts a copy for an agent with **no** `jobNamespace`, and the four values-driven peer cases (`peer-namespace`, `peer-namespace-default`, `router-override`, `vault-api`), which must keep working in domains mode too.
- `helm/Chart.yaml` version bumps. `CHANGELOG.md` has no `## Unreleased` section today (its top section is `## v0.127.0`), so add one at the top and let the releaser fold it.
- No `values.schema.json` exists; every new key must therefore be asserted by a render test, or a typo in it is accepted and dropped silently.
- `docs/agent-network-security.md` §"Target topology" and §"NetworkPolicy reference" describe the single-sandbox shape, including a proxy peer with a bare `podSelector`; both must be updated in the same change — a spec dies, that doc is read.
- `helm/values.yaml`'s own comment on `agents[].jobNamespace` says *"renderAgentStorage never follows jobNamespace to a different name"*. DB5 makes it follow `jobNamespace` in domains mode, so that comment becomes false and must be corrected in the same change — a stale values comment is read as the interface.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| `sandbox.domains` names the control namespace | render fails, message names the collision | fix the values file; `helm template … -f values.yaml` exits 0 |
| `sandbox.domains` and `sandbox.namespace` both set | render fails, message names both keys | remove one; render exits 0 |
| `proxyNamespace` set with no `domains` declared | render fails naming the key | remove it, or declare the domains it belongs to; render exits 0 |
| A domain's `peers` names an unknown peer | render fails naming the peer | fix the values file; render exits 0 |
| More than one domain declared, `proxyNamespace` absent | render fails naming the missing key | set `proxyNamespace`; render exits 0 |
| `proxyNamespace` names a namespace that is not a declared domain | render fails naming it | declare the domain or fix the key; render exits 0 |
| `vault-api` listed without `sandbox.vaultApiApp` | render fails naming the missing value | set `vaultApiApp` or drop the peer; render exits 0 |
| A declared domain already exists in the cluster as a non-chart namespace | `helm install` aborts with `already exists`; the chart cannot detect this | rename the domain, or adopt the namespace (`kubectl annotate` the ownership metadata Helm expects) and re-apply |
| `renderAgentStorage: true` and an agent's `jobNamespace` names an undeclared namespace | no copy rendered — the Job fails `CreateContainerConfigError` in that namespace | declare the domain, or place the Secret by hand; `kubectl -n <ns> get events --field-selector reason=Failed` stops reporting `secret … not found` |
| **A domain's peer set omits a destination its agents use** | **render succeeds and the Job runs, but that egress is dropped at runtime — the dangerous row: nothing fails the render** | add the peer name and re-apply; before applying, `helm template … --show-only templates/sandbox.yaml \| grep -c '<peer>'` returns ≥1 for the intended domain |
| **The `proxy` peer loses its namespace selector** | **render succeeds; only the proxy's own domain keeps internet egress and every other domain loses it silently** | restore both selectors in one `to:` entry; the AC4 assertion is the guard |
| **`egress-proxy-ingress` is not extended for a newly added domain** | **render succeeds, the domain's agents reach the proxy's Service and the proxy refuses them — the mirror image of the row above, equally silent** | add the domain to the ingress policy's peer list; AC4 asserts one entry per declared domain |
| Neither `domains` nor `namespace` set | derived `<namespace>-agents-sandbox`, as today | none needed |

## Security / Abuse Cases

The values file renders a NetworkPolicy, so a wrong shape in the template is a security regression, not a formatting bug. The invariants below are already documented in the templates and must survive this change:

- `namespaceSelector` and `podSelector` stay in the **same** `to:` peer. As two separate peers they OR, which opens every service on that port in that namespace. This applies to the new `proxy` peer as much as to the existing ones.
- Kafka stays three `/32` addresses (or an in-cluster Service with a namespace selector) — never a `/24`, which would admit every host on the subnet.
- `egress-proxy-ingress` never gains a namespace-wide peer: its clients are `app: agent` pods in the rendered sandbox namespaces, and nothing else.
- A narrowed domain cannot reach another domain's peers — the catalog makes the peer set closed, and each name resolves its namespace by a fixed rule, so a values file cannot name an arbitrary namespace or port.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Values + helpers: `sandbox.domains`, the peer catalog with its namespace rules, the namespace list, per-agent sandbox resolution, `proxyNamespace` | 1, 2, 3, 6 | AC6, AC7, AC8, AC9 | — |
| 2 | `templates/sandbox.yaml`: one Namespace + one `<domain>-egress` policy per domain — including each policy's `proxy` peer — **and** the proxy's own `egress-proxy-egress` policy that lives in this file today | 2, 3, 4 | AC1, AC2, AC4, AC10 | 1 |
| 3 | `templates/executor-rbac.yaml`: one Role/RoleBinding pair per domain | — | AC3 | 1 |
| 4 | `templates/egress-proxy.yaml` + `egress-proxy-allowlist.yaml`: Deployment, Service, PDB, ConfigMap and `egress-proxy-ingress` once, in `proxyNamespace`, with a multi-namespace ingress | 4 | AC4 | 1, 2 |
| 5 | `templates/agents.yaml`: the storage copy, domains mode vs legacy mode | 5 | AC5 | 1 |
| 6 | Tests (`hack/test-helm-render.sh` cases for AC1–AC11), `helm/README.md` values reference, `helm/values.yaml`'s `jobNamespace` comment, `docs/agent-network-security.md`, CHANGELOG + Chart.yaml bump | — | AC11, AC12 | 2, 3, 4, 5 |

Rationale: prompt 1 establishes the helpers every later template resolves through; prompts 3 and 5 are independent of everything after 1. **Prompts 2 and 4 both touch the proxy's policy set** — `egress-proxy-egress` lives in `templates/sandbox.yaml` while the rest of the proxy lives in `templates/egress-proxy.yaml` — so prompt 4 depends on 2 for the final placement of that document and must not run concurrently with it. Prompt 6 depends on all of them because it encodes their expected output.

## Do-Nothing Option

The chart keeps rendering one sandbox per cluster. Every agent's Job pods continue to share one namespace, so the per-domain egress policies the isolation goal needs cannot be attached, and the one policy must keep admitting the union of every destination — exactly the "allows every peer" shape the egress work exists to remove. The four domain namespaces can be hand-created with `kubectl`, but then they are not reproducible from values, not covered by the chart's tests, and drift on the next apply.
