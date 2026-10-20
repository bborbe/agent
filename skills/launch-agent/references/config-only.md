# Config-only mode — a lightweight agent with no repo

`--config-only` scaffolds an agent that runs the **stock `agent-claude` image** with its
system prompt shipped beside the Config. Nothing is cloned, nothing is built, no GitHub repo
is created: three artifacts go into an existing deploy repo and the vault.

Pick it when the agent is **pure prompt + tools**. Pick one of the four template shapes
instead when the agent needs Go code of its own.

## Why it works (verified 2026-10-20)

- `agent-claude` spawns `claude` with `cmd.Dir = $AGENT_DIR`
  (`claude/claude-runner.go:184` in `bborbe/agent`), and the CLI loads
  `.claude/CLAUDE.md` from its working directory as project memory.
- The Config CRD carries `configMapName` / `configMapMountPath` / `configMapItems`
  (`helm/templates/agents.yaml:34-41`), which the executor mounts into the Job.
- So a ConfigMap holding `CLAUDE.md` at `.claude/CLAUDE.md` under `/agent`, plus
  `AGENT_DIR=/agent`, **is** the system prompt — with no image build and no release.

⚠️ Read the chart from `origin/master`. A worktree on an older branch can predate the
`configMap*` render and make the whole approach look unsupported.

## Interview — five questions, not forty-five

Ask these conversationally. Use `AskUserQuestion` only where the answers are enumerable.

1. **Purpose** — one or two sentences in the operator's own words: what does this agent do,
   and what does it never do? ⚠️ Carry the operator's wording into the emitted prompt
   **verbatim**. A generic template is a failed scaffold: the whole point of the mode is
   that the prompt *is* the agent.
2. **Tools** — the tool allowlist (`ALLOWED_TOOLS`), comma-separated. Default to read-only
   (`Read,Grep,Glob`) and widen only on an explicit request.
3. **Concurrency** — how many Jobs may run at once: `maxConcurrentJobs` on the Config
   `spec`, `concurrency` for the chart's per-agent ResourceQuota. Default 1.
4. **Phases** — which `taskTypes` and trigger phases/statuses the agent answers. Default
   `taskTypes: [llm]`, `triggerPhases: [planning, execution, ai_review]`,
   `triggerStatuses: [in_progress]` — the executor's own defaults.
5. **Name** — kebab-case, unique across the deploy repo's agents. The same name is used for
   the Config, the ConfigMap, the Secret and the values entry, so they cannot drift.

Capture without asking separately: the stage/namespace (dev unless told otherwise), and the
vault + task directory the agent reads.

## Artifact 1 — the Config values entry (`bborbe/nuke`, chart-rendered)

Add one entry to the `agents:` list in `agent/values-dev.yaml`. The chart renders the Config
CR **and** its Secret, so `make apply` owns both.

```yaml
  - name: <NAME>
    enabled: true
    assignee: <NAME>
    image: bborbe/agent-claude
    tag: <AGENT_CLAUDE_TAG>          # pin a released agent-claude tag; never :latest
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning, execution, ai_review]
    triggerStatuses: [in_progress]
    maxConcurrentJobs: <CONCURRENCY>
    concurrency: <CONCURRENCY>       # ResourceQuota pods for this agent's PriorityClass
    configMapName: <NAME>
    configMapMountPath: /agent
    configMapItems:
      - key: CLAUDE.md
        path: .claude/CLAUDE.md
    env:
      AGENT_DIR: /agent
      ALLOWED_TOOLS: <TOOL_LIST>
      ANTHROPIC_BASE_URL: <ROUTER_OR_PROVIDER_URL>
      ANTHROPIC_MODEL: <MODEL>
    secretEnv:
      SENTRY_DSN: ""
      ANTHROPIC_AUTH_TOKEN: ""       # filled by the Makefile from TeamVault
    resources:
      requests: {cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi}
      limits:   {cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi}
```

⚠️ Do **not** add `volumeClaim` / `volumeMountPath` unless the agent genuinely needs a PVC —
the chart renders a claim whenever `volumeMountPath` is set.

### The second edit — `SECRET_ENV` rows

**The values entry alone leaves the agent without credentials.** The `secretEnv` block above
is only a placeholder: the chart renders the Secret from it, and the values are filled by
`agent/Makefile`'s `SECRET_ENV` table. That table names agents explicitly, so a new agent with
no rows renders a Secret of **0-byte** values — and the failure is silent until a Job runs:

```
claude CLI failed: {"type":"subtype":"init","cwd":"/agent",…} exit status 1
```

The CLI starts, `cwd` is correct, and it has no token to send. (`kubectl describe secret <name>`
shows `ANTHROPIC_AUTH_TOKEN: 0 bytes` against a working agent's non-zero length.)

So add two rows beside the other agents in `agent/Makefile`:

```make
	vault-grooming-agent:SENTRY_DSN:url:$(SENTRY_DSN_KEY) \
	vault-grooming-agent:ANTHROPIC_AUTH_TOKEN:password:$(ANTHROPIC_AUTH_TOKEN_KEY) \
```

And when the agent is dev-only, add its name to `STAGE_ABSENT_prod` in the same file — without
it, a prod render fails on an index that does not exist there. No new credential is needed for
a Claude agent: `SENTRY_DSN_KEY` and `ANTHROPIC_AUTH_TOKEN_KEY` already exist.

⚠️ Adding an agent is **two** edits in `bborbe/nuke`: the values entry **and** its `SECRET_ENV`
rows. Emit both, or the agent deploys and cannot run.

## Artifact 2 — the ConfigMap manifest (`bborbe/nuke`, applied by `make apply`)

The chart renders the **reference**, never the object: a values entry alone leaves the
ConfigMap existing only on the cluster, so a rebuild loses the prompt. Emit the object into
the deploy repo.

Write `agent/<NAME>-configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: <NAME>
  namespace: <NAMESPACE>
data:
  CLAUDE.md: |
    <SYSTEM_PROMPT — the operator's own words, filled out into a usable prompt>
```

Then list it in `agent/Makefile`'s `SERVICE_MANIFESTS_dev` (the deliberately independent dev
list — **never** `SERVICE_MANIFESTS_prod`, which is what keeps a dev-only agent dev-only):

```make
SERVICE_MANIFESTS_dev := \
	claude-interactive-heartbeat-role.yaml \
	claude-interactive-heartbeat-rolebinding.yaml \
	claude-interactive-ing.yaml \
	claude-interactive-svc.yaml \
	<NAME>-configmap.yaml
```

## Artifact 3 — the KB agent page (vault)

Render `references/vault-page-template.md` into `50 Knowledge Base/<Name> Agent.md`, marking
the shape as **`claude` + config-only**. The template assumes a scaffolded repo, so substitute
in this mode:

- **`## Code`** — replace the `Repo:` / `Template:` / `Deploy: … make buca` lines with the
  three artifact paths (values entry, `agent/<NAME>-configmap.yaml`, this page) and
  `Deploy: cd <nuke-dev>/agent && BRANCH=dev make apply`.
- **`## Tracked goal`** — delete the section. No goal page is created, so the link would
  dangle.

No goal page and no scenario stub are generated in this mode — the agent is an addition to an
existing deploy repo, not a new project.

## What this mode never does

- `gh repo create` — there is no repo.
- `make buca`, an image build, or a release — the image is the stock `agent-claude`.
- `kubectl apply` — print the apply command, let the operator run it.

## Deploy checklist (print, never execute)

`<nuke>` is the `bborbe/nuke` feature worktree the artifacts were written into; `<nuke-dev>`
is that repo's `nuke-dev` deploy worktree; `<vault>` is the vault the KB page was written to.

```
1. Review the values entry:   <nuke>/agent/values-dev.yaml
2. Review the SECRET_ENV rows: <nuke>/agent/Makefile — BOTH edits, not just the values entry;
                              without these rows the Secret renders with 0-byte values
3. Review the ConfigMap:      <nuke>/agent/<NAME>-configmap.yaml
4. Review the KB page:        <vault>/50 Knowledge Base/<Name> Agent.md
5. Merge the nuke PR, then:   cd <nuke-dev> && git fetch && git merge --ff-only origin/master
6. Apply:                     cd <nuke-dev>/agent && BRANCH=dev make apply
7. Verify the Secret:         kubectlnukedev -n dev describe secret <NAME>
                              — ANTHROPIC_AUTH_TOKEN must be non-zero bytes
8. Verify the objects:        kubectlnukedev -n dev get configs.agent.benjamin-borbe.de <NAME>
                              kubectlnukedev -n dev get configmap <NAME>
9. File one task assigned to <NAME> and confirm its `## Result` carries the prompt's canary line.
```

Steps 4–7 are ops commands the auto-mode classifier soft-denies — the operator switches to
`accept edits` before running them.
