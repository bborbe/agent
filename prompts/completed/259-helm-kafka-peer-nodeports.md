---
status: completed
summary: Made the sandbox Kafka nodeport list configurable via sandbox.kafka.nodePorts (default [32159]) in both nodeport branches through one shared helper, with validation, tests, chart bump, and docs.
execution_id: agent-kafka-peer-ports-exec-259-helm-kafka-peer-nodeports
dark-factory-version: v0.198.7
created: "2026-10-25T16:15:00Z"
queued: "2026-10-25T16:03:15Z"
started: "2026-10-25T16:03:39Z"
completed: "2026-10-25T16:12:53Z"
---

# feat(helm): make the sandbox kafka peer's nodeport list configurable

<summary>
- The sandbox `kafka` peer's nodeport branch hardcodes a single port (`32159`). A Kafka client bootstraps on that port and then follows the broker's advertised metadata to a **different** nodeport, so the policy admits the bootstrap connection and rejects the one the client actually uses
- The chart gains `sandbox.kafka.nodePorts`, a list. It renders one `ports:` entry per value, so a stage can allow both the bootstrap and the advertised port
- The default is `[32159]`, which renders byte-identically to today. An install that sets nothing keeps exactly its current policy
- The same hardcoded peer is duplicated inline in the legacy single-sandbox policy, so **both** sites change; they share one helper so they cannot drift again
- The existing in-cluster branch (`sandbox.kafka.namespace` + `servicePort`) is unchanged and still takes precedence
- Measured on nuke dev: a sandboxed Job logged `KafkaBrokers [3]: …:32159` — the bootstrap was correct — and then died on `dial tcp 192.168.178.41:32706: connect: connection refused`. Both ports answer from outside the cluster, so the broker is healthy and the reject comes from the policy
- The chart version, the values comment, both READMEs, the network-security doc and the changelog are updated
</summary>

<objective>
Replace the hardcoded `port: 32159` in both Kafka nodeport peer branches — the shared helper and the inline legacy block — with a loop over `sandbox.kafka.nodePorts` (default `[32159]`), so a values overlay can allow every nodeport a client may be sent to. A stage that does not set the key must render exactly what it renders today.
</objective>

<context>
Read `CLAUDE.md` for project conventions: a single global `CHANGELOG.md` at the repo root, and the root `Makefile` is the only Makefile (there are no per-dir Makefiles). Read `docs/dod.md`: a config change updates the README and adds a CHANGELOG entry under `## Unreleased`.

Files to read before editing (anchor on the names given, not on line numbers):

- `helm/templates/_helpers.tpl` — the sandbox peer template. The `kafka` branch begins at `{{- else if eq .name "kafka" -}}`. Its in-cluster form (guarded by `$sandbox.kafka.namespace`) renders a `namespaceSelector` peer on `$sandbox.kafka.servicePort`. Its fallback form renders three `ipBlock` peers (`192.168.178.41/32`, `.42/32`, `.43/32`) followed by a single:
  ```yaml
  ports:
    - { protocol: TCP, port: 32159 }
  ```
- `helm/templates/sandbox.yaml` — the legacy single-sandbox `agent-sandbox-egress` policy, whose own inline Kafka block repeats the same three `/32` ipBlocks and the same `- { protocol: TCP, port: 32159 }`. That block is what an install with no `sandbox.domains` renders. **Both sites change.**
- `helm/values.yaml` — the `sandbox.kafka` block documents `namespace`, `podLabels` and `servicePort`. The new key belongs beside them.
- `helm/README.md` — the sandbox values table and the section describing the Kafka nodeport default.
- `docs/agent-network-security.md` — describes both Kafka branches and the `32159` default.
- `hack/test-helm-render.sh` — the chart render test, run by `make test` via the `test-helm` target. It resolves helm itself and supplies the `required` base values (`namespace`, `executor.kafkaBrokers`, `executor.sentry.dsn` or `existingSecret`). It already has a `kafka-default` case and a `domains-peers` case asserting `port: 32159`; reuse its `run_policy_case` / `policy_doc` helpers and its peer-extraction idiom rather than writing a new harness.

<requirements>

1. `sandbox.kafka.nodePorts` is a list of integers. Declare `nodePorts: [32159]` in `helm/values.yaml` under `sandbox.kafka`.
2. In BOTH nodeport branches — `_helpers.tpl`'s `agent.sandboxPeer` and `sandbox.yaml`'s inline legacy peer — render `ports:` with one `- { protocol: TCP, port: <p> }` entry per element, in the order given. Add ONE shared helper named `agent.sandboxKafkaNodePorts` that returns the list comma-joined, and have both call sites consume it as `include "agent.sandboxKafkaNodePorts" $ | splitList ","`. **Put the `default (list 32159)` fallback inside that helper**, so both call sites inherit it from one place and the byte-identical default cannot drift between the two branches. The indentation differs by four spaces between the two files (the helper emits at zero indent for `nindent 4`, the legacy block sits four spaces inside the egress list), so adapt the block per call site rather than pasting it.
3. An empty or unset `nodePorts` must fall back to `[32159]` — never render an empty `ports:` list, because a peer with no ports opens every port on those CIDRs. Never gate the list on truthiness (`{{- if $sandbox.kafka.nodePorts }}`): an empty list is falsy and would render a `ports:` key with no entries.
4. The in-cluster branch (`sandbox.kafka.namespace` + `servicePort`) is untouched.
5. Validate the list the way the sibling key is validated: `agent.sandboxValidate` already fails the render for a non-positive `servicePort`, with a message naming the key. Add the same unconditional check for every element of the nodeport list, failing with exactly `sandbox.kafka.nodePorts must contain only positive integers`, and have the render-test case assert on that string.
6. Extend `hack/test-helm-render.sh`. Keep the existing `kafka-default` case green (one `port: 32159`, three `/32` CIDRs). Add: (a) a legacy-path case setting `--set namespace=dev --set executor.existingSecret=agent-secret --set 'sandbox.kafka.nodePorts={32159,32706}'` and asserting both `port:` lines inside the extracted Kafka peer of `agent-sandbox-egress`; (b) the same `--set` against a domains values file asserting `<domain>-egress` carries both ports; (c) a guard on the extracted peer being non-empty before asserting, following the script's own anti-vacuous pattern. Add a render-test case for requirement 5's failure message. Carry the base `--set` flags the adjacent `kafka-default` case uses in both new cases, so a render that fails for an unrelated reason cannot pass as a real assertion.
7. `helm/Chart.yaml` is `version: 0.23.0` today — set `0.23.1` (patch, matching the `peerNamespace` 0.22.1→0.22.2 and `rerunOnLostClaim` 0.14.0→0.14.1 precedents for a new optional value). Leave `appVersion` unchanged.
8. `CHANGELOG.md` has NO `## Unreleased` section today; it starts at `## v0.129.0`. Create `## Unreleased` above that heading (one blank line either side, exactly one such heading in the file) and put one `feat(helm):` bullet under it, in the repo's established long-narrative style, ending `Chart 0.23.0→0.23.1`. Do not append to `## v0.129.0`.
9. Document the key in `helm/values.yaml` (a comment explaining that a NetworkPolicy port matches the destination pod and that Kafka clients follow advertised metadata, so the bootstrap port alone is not enough), in `helm/README.md` (both the values table and the Kafka nodeport paragraph), and add one sentence to `docs/agent-network-security.md` beside the Kafka branch description.

</requirements>

<constraints>
- Do NOT commit. dark-factory handles git.
- Existing tests must still pass.
- Change exactly two template sites: the Kafka nodeport branch in `helm/templates/_helpers.tpl` and the Kafka nodeport branch in `helm/templates/sandbox.yaml`. No other peer, no in-cluster Kafka branch, no Go code, no `helm/crds/`, no `tools.env`.
- The default render must stay byte-identical to today's for a values file that does not set the key.
- All paths in this prompt are repo-relative. Scratch files go in a `mktemp -d` dir only.
</constraints>

<verification>
`helm` is NOT on PATH in the container, and a bare `helm template` of this chart fails its `required` gates (`namespace`, `executor.kafkaBrokers`, `executor.sentry.dsn`/`existingSecret`). Go through `hack/test-helm-render.sh`, which resolves helm itself and supplies base values.

```bash
set -o pipefail; trap 'echo "VERIFY FAIL at line $LINENO"' ERR

# 1. the extended render test passes and covers both paths
bash hack/test-helm-render.sh            # must exit 0 and print "OK: helm render tests passed"
grep -qE '^test: test-helm' Makefile     # make test still wires it in

# 2. BOTH kafka nodeport branches read the value, not just the helper
grep -q 'nodePorts' helm/templates/_helpers.tpl
grep -q 'nodePorts' helm/templates/sandbox.yaml
# both call the one shared helper, so the default cannot drift
test "$(grep -c 'agent.sandboxKafkaNodePorts' helm/templates/_helpers.tpl)" -ge 1
test "$(grep -c 'agent.sandboxKafkaNodePorts' helm/templates/sandbox.yaml)" -ge 1

# 3. no truthiness gate on the list
! grep -q 'if .*kafka\.nodePorts' helm/templates/_helpers.tpl
! grep -q 'if .*kafka\.nodePorts' helm/templates/sandbox.yaml

# 4. bookkeeping
grep -q 'nodePorts' helm/values.yaml
grep -q 'nodePorts' helm/README.md
grep -q 'nodePorts' docs/agent-network-security.md
grep -q '^version: 0.23.1$' helm/Chart.yaml
test "$(grep -c '^## Unreleased$' CHANGELOG.md)" = 1
awk '/^## /{sec=$0} /nodePorts/{print sec}' CHANGELOG.md   # must print exactly one line: "## Unreleased"
```

Then run `make precommit`. It must pass and its output must include `OK: helm render tests passed`.
</verification>
