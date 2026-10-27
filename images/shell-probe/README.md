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

Only that fence is executed. Prose in the task body is never run. A task with no
`probe-sh` fence exits `2` without executing anything.

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

- The pod carries `app: agent` (the executor sets it unconditionally), so the live
  `*-sandbox-egress` policy selects it and it reaches only what that policy
  already admits. This image widens no egress.
- It carries no model, no credentials and no task authority.
- The Config that runs it accepts only `task_type: probe`, so a probe Job runs
  only for a task deliberately filed as one.

## Building

```bash
make probe-image-build VERSION=vX.Y.Z      # tag it with a released repo tag
make probe-image-upload VERSION=vX.Y.Z     # docker push
```

Then add `agent-shell-probe:vX.Y.Z` to `MIRROR_IMAGES` in `bborbe/nuke`
`agent/Makefile` and set the agent's `image:` / `tag:` in `values-<stage>.yaml`.
