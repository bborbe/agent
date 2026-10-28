# shell-probe

A shell-entrypoint image for sandboxed egress assertions.

## Why this exists

The agent platform has no vehicle that can run a shell command inside a sandbox:

- `Config.spec` carries no `command`/`args` field, and the executor's spawner
  (`agent-task-executor` `pkg/spawner/job_spawner.go`) sets only `Image`, `Env`
  and resources on the container — it never sets `Command`/`Args`. So the shell
  must come from the image.
- Every other image in the fleet is either an LLM agent whose own guardrail
  forbids private IPs (`192.168.x`) and cluster DNS (`*.svc`), or a Go binary
  running a fixed workload. Neither can be asked to `nc` a Kafka nodeport.

So egress assertions that name a private IP or an in-cluster Service had no
instrument. This image is that instrument.

## How it works

The entrypoint reads a fenced block out of `TASK_CONTENT` — the task markdown the
executor passes to every Job — and runs it with `sh -x`. The fence info string
must be exactly `probe-sh`:

````markdown
```probe-sh
nc -w 3 192.168.178.41 32159 </dev/null; echo "kafka-41=$?"
curl -x http://egress-proxy.agent-sandbox.svc.cluster.local:8888 \
  -sS -o /dev/null -w 'proxy-github=%{http_code}\n' https://github.com
```
````

Only that fence is executed. Prose in the task body is never run.

Extraction contract, all cases covered by `hack/test-shell-probe.sh` (`make
test-shell-probe`):

- leading/trailing whitespace on the fence line is tolerated, and CRLF line
  endings are normalised
- the **first** `probe-sh` block wins; later blocks are ignored
- an **unclosed** fence is refused (exit `2`) rather than executed to EOF — the
  dangerous case, since it would run the rest of the task body as shell
- an empty block, no fence at all, and a parser failure each exit `2` with their
  own message, so "the vehicle could not run the probe" is never reported as
  "the probe found nothing"

`PROBE_COMMAND` overrides the fence; the probe Config deliberately does not set
it. It exists so the image can be exercised with `kubectl run` before any Config
exists.

## The exit status is always 0

Once a block was found the entrypoint exits `0`, whatever the block returned. A
refused connect is the *result* this vehicle exists to record. A non-zero exit
would make the executor publish a synthetic failure, mark the task failed, and
hide the very denial the probe was sent to find. Read the individual exit codes
out of the log, not the Job's phase.

## Safety posture

The in-image control is only the `probe-sh` fence. Every compensating control
lives outside this repo, so it is named here rather than assumed:

| Control | Owner | What it does |
|---|---|---|
| `app: agent` pod label | `bborbe/agent-task-executor` `pkg/spawner/job_spawner.go` (`jobBuilder.SetApp("agent")`) | makes the live `*-sandbox-egress` policy select the pod, so it reaches only what the policy already admits. The chart side is `bborbe/agent` `helm/templates/sandbox.yaml` |
| `task_type: probe` gate | `bborbe/nuke` `agent/values-dev.yaml` → `agents[]`, `taskTypes: [probe]` | the executor claims a task only when its `task_type` is in that set (`pkg/task_type_filter.go`), so a probe Job runs only for a task deliberately filed as one |
| no mounted secret | same `bborbe/nuke` `agents[]` entry — no `secretName`, no `secretEnv` | the pod carries no credential for the shell to reach |
| image pin | `bborbe/nuke` `agent/Makefile` `MIRROR_IMAGES` | makes the tag that runs in-cluster traceable to a released commit |

**Who may author `TASK_CONTENT`:** anyone who can write a task file into the
vault that the probe's executor serves, with `task_type: probe` and the probe
assignee. That is the same population that can already direct an LLM agent — and
a probe Job is strictly less capable than one: no model, no credentials, no
tools, and the same NetworkPolicy. The fence is not an authorisation boundary and
is not claimed as one; it exists so that a task body's *prose* can never be
executed by accident.

The image widens no egress. It carries no model, no credentials and no task
authority.

## Building

```bash
make probe-image-build VERSION=vX.Y.Z      # tag it with a released repo tag
make probe-image-upload VERSION=vX.Y.Z     # docker push
```

Then add `agent-shell-probe:vX.Y.Z` to `MIRROR_IMAGES` in `bborbe/nuke`
`agent/Makefile` and set the agent's `image:` / `tag:` in `values-<stage>.yaml`.
