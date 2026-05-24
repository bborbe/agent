---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Makes image pull secret name configurable via AgentConfiguration
- Removes hardcoded "docker" literal from job_spawner.go
- Default remains "docker" if not specified in Config CR
</summary>

<objective>
The image pull secret name "docker" is hardcoded in job_spawner.go at line 104. If a namespace lacks a Secret named "docker" or if that Secret is compromised, Jobs silently fall back to unauthenticated pulls. After this change, the secret name is sourced from the AgentConfiguration Config CR, with "docker" as the default fallback.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/spawner/job_spawner.go (~line 104, SetImagePullSecrets call)
- task/executor/pkg/agent_configuration.go (AgentConfiguration struct)
- task/executor/pkg/k8s_connector.go (~line 193, CRD schema for env)
</context>

<requirements>
### 1. Add ImagePullSecret field to AgentConfiguration

In `pkg/agent_configuration.go`, add a field:
```go
ImagePullSecret string
```

### 2. Update job_spawner.go to use configurable secret name

In `pkg/spawner/job_spawner.go`, change the `SetImagePullSecrets` call to use the config value with "docker" as default:

```go
secretName := "docker"
if config.ImagePullSecret != "" {
    secretName = config.ImagePullSecret
}
podSpecBuilder.SetImagePullSecrets([]string{secretName})
```

### 3. Update build function signature if needed

If the `build` function in job_spawner.go needs to receive the AgentConfiguration to access ImagePullSecret, ensure it does. Pass the full config struct if not already.

### 4. Add test for custom secret name

Add a test case in job_spawner_test.go that verifies a custom ImagePullSecret value is used when set.

### 5. Run make generate and make test

```bash
cd task/executor && make generate && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, factory pattern
- ImagePullSecret field should be optional — default to "docker" if empty
</constraints>

<verification>
cd task/executor && make precommit
</verification>
