---
spec: ["062-sandbox-domains"]
status: draft
created: "2026-10-24T20:38:48Z"
---

# feat(helm): grant the executor its sandbox rights in every declared domain

<summary>
- The executor can create, watch and delete Jobs in every declared sandbox namespace, not just the single one
- Each declared domain contains its own Role and RoleBinding named `agent-task-executor-sandbox`, binding the executor's ServiceAccount in the control namespace
- The grant is unchanged in scope: no wildcard verbs, no cluster scope, no widening of the executor's blast radius
- An install with no declared domains renders exactly one pair in the single sandbox namespace, as today
</summary>

<objective>
Make the sandbox-scoped Role and RoleBinding in `helm/templates/executor-rbac.yaml` render once per namespace returned by `agent.sandboxNamespaces`, instead of once in `agent.sandboxNamespace`. Nothing else in the file changes.
</objective>

<context>
This is prompt 3 of 6 for spec `062-sandbox-domains`; it depends on prompt 1 only (the `agent.sandboxNamespaces` helper). It is independent of prompts 2, 4 and 5. Do not edit `helm/templates/sandbox.yaml`, `egress-proxy*.yaml`, `agents.yaml`, `helm/values.yaml`, the docs, the changelog or the render test script.

Read fully before editing:

- `helm/templates/executor-rbac.yaml` — the file you are editing. It opens with `{{- if .Values.executor.enabled }}` and `{{- $ns := include "agent.namespace" . }}` (the control namespace). It renders a ServiceAccount, a ClusterRole/ClusterRoleBinding, a control-namespace Role/RoleBinding, and finally the sandbox-scoped pair (`agent-task-executor-sandbox`) at the end of the file. The sandbox Role grants `batch/jobs` create/get/list/watch/delete, `pods` get/list/watch, `secrets` get and `persistentvolumeclaims` get. The RoleBinding subject is `ServiceAccount agent-task-executor` in `{{ $ns }}` — the control namespace, not the sandbox.
- `helm/templates/_helpers.tpl` — `agent.sandboxNamespaces` returns a comma-separated list of the namespaces that host agent pods: the sorted declared domains, or the single sandbox namespace when no domains are declared. It is a plain `include`, so consume it with `| splitList ","`.

Current shape of the sandbox pair (namespace interpolated in two places per document):

```gotemplate
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-task-executor-sandbox
  namespace: {{ include "agent.sandboxNamespace" . }}
  labels:
    {{- include "agent.labels" . | nindent 4 }}
...
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-task-executor-sandbox
  namespace: {{ include "agent.sandboxNamespace" . }}
...
subjects:
  - kind: ServiceAccount
    name: agent-task-executor
    namespace: {{ $ns }}
```
</context>

<requirements>
1. **Wrap the sandbox Role and RoleBinding in a range over the namespace list.** Immediately before the `# Sandbox-scoped grants:` comment (the one that introduces the Role), add:

   ```gotemplate
   {{- range $sandboxNS := (include "agent.sandboxNamespaces" . | splitList ",") }}
   ```

   and close it with `{{- end }}` after the RoleBinding document, before the file's closing `{{- end }}` that closes `{{- if .Values.executor.enabled }}`.

2. **Start each document with a bare `---` inside the loop.** The current file separates the Role and the RoleBinding with a `---` line; after wrapping, each loop iteration must emit both documents with their own separators, matching the `agents.yaml` pattern (`{{- range ... }}` on its own line, then `---`). The result must be:

   ```gotemplate
   {{- range $sandboxNS := (include "agent.sandboxNamespaces" . | splitList ",") }}
   ---
   # Sandbox-scoped grants: the executor, still in the control namespace, gets exactly
   # the cross-namespace rights it needs inside this environment's sandbox namespaces
   # and no more. No wildcard verbs, no cluster-scope — a widened grant here would
   # widen the executor's blast radius past the sandbox boundary. One pair per declared
   # domain (or the single sandbox namespace when no domains are declared).
   apiVersion: rbac.authorization.k8s.io/v1
   kind: Role
   metadata:
     name: agent-task-executor-sandbox
     namespace: {{ $sandboxNS }}
     labels:
       {{- include "agent.labels" $ | nindent 4 }}
   rules:
     ... (unchanged rule list) ...
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: RoleBinding
   metadata:
     name: agent-task-executor-sandbox
     namespace: {{ $sandboxNS }}
     labels:
       {{- include "agent.labels" $ | nindent 4 }}
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: Role
     name: agent-task-executor-sandbox
   subjects:
     - kind: ServiceAccount
       name: agent-task-executor
       namespace: {{ $ns }}
   {{- end }}
   ```

   - Replace both `namespace: {{ include "agent.sandboxNamespace" . }}` occurrences inside this pair with `namespace: {{ $sandboxNS }}`.
   - `{{ $ns }}` in the RoleBinding subject stays the control namespace — do NOT replace it with `$sandboxNS`. The binding crosses the namespace boundary on purpose.
   - Inside the range the root context is `$`, so `include "agent.labels" $` replaces `include "agent.labels" .`. The existing `$` reference at file scope still works, but use `$` consistently inside the loop.
   - The rule list is copied verbatim: `batch/jobs` create/get/list/watch/delete, `""/pods` get/list/watch, `""/secrets` get, `""/persistentvolumeclaims` get. Do not add or remove a verb.

3. **Leave the rest of the file untouched.** The ServiceAccount, ClusterRole, ClusterRoleBinding and the control-namespace Role/RoleBinding keep their current names, namespaces and rules. The `{{- $ns := include "agent.namespace" . }}` declaration stays where it is.

4. **Do not add render tests.** Prompt 6 owns `hack/test-helm-render.sh`. The existing `sandbox-dev`, `sandbox-prod` and `sandbox-explicit` cases must stay green: in legacy mode the range yields exactly one namespace, so the same two documents render.
</requirements>

<constraints>
- No wildcard verbs, no cluster-scope grant — the executor's sandbox blast radius must not widen.
- The binding's subject ServiceAccount lives in the control namespace, not in the sandbox namespace.
- Legacy render (no `domains`) is unchanged: one Role and one RoleBinding in the single sandbox namespace.
- Do NOT commit — dark-factory handles git.
- Do not edit `specs/`, `helm/values.yaml`, `helm/templates/sandbox.yaml`, `helm/templates/egress-proxy.yaml`, `helm/templates/egress-proxy-allowlist.yaml`, `helm/templates/agents.yaml`, `hack/test-helm-render.sh`, `CHANGELOG.md`, `helm/Chart.yaml`, `helm/README.md` or `docs/`.
</constraints>

<verification>
```bash
bash hack/test-helm-render.sh   # legacy cases must stay green
command -v helm >/dev/null 2>&1 || go install helm.sh/helm/v3/cmd/helm@v3.16.4
HELM_BIN="$(command -v helm || echo "$(go env GOPATH)/bin/helm")"

# Domains mode: one Role and one RoleBinding named agent-task-executor-sandbox in
# every declared domain, each subject in the control namespace.
"$HELM_BIN" template agent ./helm \
  --set namespace=dev \
  --set 'sandbox.domains.agent-sandbox.peers={router}' \
  --set 'sandbox.domains.trading-sandbox.peers={router}' \
  --set sandbox.proxyNamespace=agent-sandbox \
  --show-only templates/executor-rbac.yaml > /tmp/rbac.out

for d in agent-sandbox trading-sandbox; do
  for kind in Role RoleBinding; do
    awk -v K="$kind" -v NS="$d" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: agent-task-executor-sandbox\n") && index($0,"  namespace: " NS "\n"){f=1} END{exit(f?0:1)}' /tmp/rbac.out \
      || echo "FAIL: missing $kind agent-task-executor-sandbox in $d"
  done
  awk -v NS="$d" 'BEGIN{RS="---"} index($0,"kind: RoleBinding\n") && index($0,"  name: agent-task-executor-sandbox\n") && index($0,"  namespace: " NS "\n") && index($0,"    namespace: dev\n"){f=1} END{exit(f?0:1)}' /tmp/rbac.out \
    || echo "FAIL: $d binding subject is not in the control namespace dev"
done
[ "$(grep -c 'name: agent-task-executor-sandbox$' /tmp/rbac.out)" -eq 4 ] && echo "OK: 2 domains x Role+RoleBinding"

# Legacy mode: exactly one pair, in dev-agents-sandbox.
"$HELM_BIN" template agent ./helm --set namespace=dev \
  --show-only templates/executor-rbac.yaml > /tmp/rbac-legacy.out
[ "$(grep -c 'name: agent-task-executor-sandbox$' /tmp/rbac-legacy.out)" -eq 2 ] && echo "OK: legacy pair"
awk 'BEGIN{RS="---"} index($0,"kind: Role\n") && index($0,"  name: agent-task-executor-sandbox\n") && index($0,"  namespace: dev-agents-sandbox\n"){f=1} END{exit(f?0:1)}' /tmp/rbac-legacy.out && echo "OK: legacy placement"
```

Every `OK` line must print and no `FAIL` line may appear. `bash hack/test-helm-render.sh` must exit 0.
</verification>
