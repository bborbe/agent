# Deployment

## Quick Deploy (current MVP workflow)

```bash
# Deploy to dev
cd ~/Documents/workspaces/agent/task/controller && BRANCH=dev make buca

# Deploy to prod
cd ~/Documents/workspaces/agent/task/controller && BRANCH=prod make buca
```

`make buca` = build → upload → clean → apply.

## What happens

1. **build** — Docker image tagged `$(DOCKER_REGISTRY)/agent-task-controller:$(BRANCH)`
2. **upload** — Push to registry
3. **clean** — Remove local image + prune
4. **apply** — Render K8s YAML with `teamvault-config-parser`, apply via `kubectlquant -n $(NAMESPACE)`

Env files loaded by BRANCH:
- `dev` → `dev.env` + `common.env`
- `prod` → `prod.env` + `common.env`

## Useful links

```bash
# Bump log level temporarily (auto-resets after 5 min)
curl https://dev.quant.benjamin-borbe.de/admin/agent-task-controller/setloglevel/3
curl https://prod.quant.benjamin-borbe.de/admin/agent-task-controller/setloglevel/3

# Check pod status
kubectlquant -n dev get pods -l app=agent-task-controller
kubectlquant -n prod get pods -l app=agent-task-controller

# Logs
kubectlquant -n dev logs agent-task-controller-0
kubectlquant -n prod logs agent-task-controller-0
```

## Future: worktree-based workflow

When the project matures past MVP, adopt the trading-style worktree setup:

```bash
cd ~/Documents/workspaces/agent
git worktree add ../agent-dev dev
git worktree add ../agent-prod prod
```

Then deploy from dedicated worktrees (like `trading-dev`/`trading-prod`).
