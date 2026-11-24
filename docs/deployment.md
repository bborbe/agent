# Deployment

## Production runs in the cluster — `cmd/run-task` is a local harness

Every agent image carries a `cmd/run-task` entrypoint: it takes one task and runs it to a
result. **That entrypoint is for local verification and debugging, never for production.**

An agent "deployed" by running `cmd/run-task` on a machine is not deployed: no scheduler, no
retry, no restart, no metrics, no liveness — and it dies with the host it happens to be on.
Production means a Kubernetes workload on the fleet cluster, dispatched by the task
controller and run by the executor as a Job.

Measured 2026-11-13: a rebase agent shipped as a **launchd job on a Mac** running a Python
script. Its goal closed 5/5 because the criterion asked only for *"identified within one poll
interval with no human starting the run"* — which a local scheduler satisfies exactly. The
gap surfaced only when the operator asked why the laptop was involved. Design rule and the
full contrast table: `private-personal` vault, `[[Agent Design Guide]]` § *Norm: production
runs in the cluster*.

## What this repo deploys: no component image

`bborbe/agent` is the **library plus the Helm chart**. It has no `.goreleaser` and publishes
no deployable component — the components publish their own images from their own repos. Its
only image is the diagnostic **shell-probe**: `images/shell-probe/Dockerfile` plus
`Makefile.docker`'s `probe-image-build` / `probe-image-upload` targets publish
`bborbe/agent-shell-probe`, whose `MIRROR_IMAGES` pin lives in `bborbe/nuke`. That is a probe,
not a component, so it is not part of the component deploy flow below — but a pin bump that
omits it still hard-fails the apply, exactly as step 2 warns.

| Repo | Role |
|---|---|
| `bborbe/agent` | Go library + Helm chart. One image only: `agent-shell-probe`, a probe rather than a deployable component. |
| `bborbe/agent-task-controller` | The controller. Own repo, own image. |
| `bborbe/agent-task-executor` | The executor. Own repo, own image. |
| `bborbe/agent-claude` / `agent-pi` | Agent images. Each pins `bborbe/agent` independently. |

⚠️ A library release reaches the fleet only when a component repo bumps its pin.

## The deploy flow

1. **Publish the image** — from the component repo at its release tag:
   `make build BUILD_TARGET=sun REF=<sha> VERSION=vX.Y.Z`. Local build + `make upload` is the
   fallback while sun is down; agents are refused `docker push`, so the operator runs it.
2. **Bump the pin in `bborbe/nuke`** and merge to `master` — update the image tag in the
   component's `values-<stage>.yaml` **and** its `MIRROR_IMAGES` entry. A bump without the
   mirror entry hard-fails the apply.
3. **Mirror** by hand where the component's `apply:` target does not (`make mirror` from a
   synced `bborbe/nuke` checkout, `BRANCH=<stage>`); skip when the `apply:` target already
   mirrors.
4. **Apply** — `/nuke-deploy <stage> <component>`. Never a hand-run apply except the fallback
   for a component the apply service refuses.

⚠️ Never bump the pin or run the fallback from this worktree — `~/Documents/workspaces/agent`
is a development checkout where dark-factory commits, tags and releases. Work from the
stage's `nuke-*` worktree, and always `git fetch && git merge origin/master` (local
`refs/heads/master` is stale), then verify the change is present before mirroring or applying.

## Where the fleet runs

The agent fleet runs on **nuke**, not quant — `kubectlnukeprod -n prod` for the fleet,
`kubectlnukedev -n dev` for dev. `kubectlquant` is for Kafka and the topic-controllers only,
until the per-env Kafka split lands.

```bash
# Check pod status
kubectlnukeprod -n prod get pods | grep -i agent
kubectlnukedev  -n dev  get pods | grep -i agent

# Logs
kubectlnukeprod -n prod logs <pod> --tail=50
```

## What this page replaced

The previous version deployed `task/controller` from this worktree with `make buca` against
`kubectlquant`, and proposed `agent-dev`/`agent-prod` worktrees. All three are gone from the
deploy path: the controller and executor are standalone repos, agent workloads moved off
quant, and `agent-dev` holds the flattened library with no deployable component while
`agent-prod` was never created.

⚠️ `CLAUDE.md` and `skills/launch-agent/` still instruct `make buca` / `/make-buca` and
`kubectlquant` against those worktrees. Those are pre-existing references outside this
change, and they now contradict this page — tracked separately rather than rewritten here.
