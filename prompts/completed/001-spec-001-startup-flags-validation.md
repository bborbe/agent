---
status: completed
spec: ["001"]
summary: Added GitClient interface and implementation with git clone/validate via os/exec subprocess, added CLI flags to task/controller application struct, and fixed osv-scanner Makefile to use ROOTDIR for mono-repo compatibility
container: agent-001-spec-001-startup-flags-validation
dark-factory-version: v0.67.3-dirty
created: "2026-03-26T09:00:00Z"
queued: "2026-03-26T18:33:16Z"
started: "2026-03-26T18:33:23Z"
completed: "2026-03-26T18:49:50Z"
branch: dark-factory/git-to-kafka-task-sync
---

<summary>
- Task controller accepts git repository connection settings and polling configuration at startup
- On first launch, the service automatically clones the vault repository
- On subsequent launches, the service validates the existing local clone
- Git operations use the git binary via subprocess for future GPG signing compatibility
- Authentication token is embedded in the clone URL and never logged
- Existing HTTP endpoints and flags continue to work unchanged
</summary>

<objective>
Add the required CLI flags to task/controller and implement startup validation with git clone/open logic. On first start, clone the repo from git-url. On subsequent starts, validate the existing clone. All git operations use `os/exec` subprocess (the `git` binary), not go-git or other embedded libraries — this ensures future GPG signing compatibility. This establishes the foundation that subsequent prompts (polling loop, Kafka publishing) will build on.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `/home/node/.claude/docs/go-patterns.md` for interface/constructor/struct patterns.
Read `/home/node/.claude/docs/go-testing.md` for Ginkgo/Gomega and counterfeiter conventions.
Read `/home/node/.claude/docs/go-factory-pattern.md` for factory function rules (zero logic, Create* prefix).

Key files to read before making changes:
- `task/controller/main.go` — existing application struct, Run(), createHTTPServer()
- `lib/agent_cdb-schema.go` — TaskV1SchemaID definition
- `task/controller/Makefile` — build targets (make test, make precommit)
</context>

<requirements>
1. Add the following fields to the `application` struct in `task/controller/main.go`:

   ```go
   GitURL       string        `required:"true"  arg:"git-url"        env:"GIT_URL"        usage:"vault git repository URL"`
   GitToken     string        `required:"true"  arg:"git-token"      env:"GIT_TOKEN"      usage:"git authentication token" display:"length"`
   KafkaBrokers string        `required:"true"  arg:"kafka-brokers"  env:"KAFKA_BROKERS"  usage:"comma-separated Kafka broker addresses"`
   GitBranch    string        `required:"false" arg:"git-branch"     env:"GIT_BRANCH"     usage:"git branch to track" default:"main"`
   PollInterval time.Duration `required:"false" arg:"poll-interval"  env:"POLL_INTERVAL"  usage:"vault polling interval" default:"60s"`
   TaskDir      string        `required:"false" arg:"task-dir"       env:"TASK_DIR"       usage:"task directory within vault" default:"24 Tasks"`
   ```

   Note: `display:"length"` on GitToken prevents logging the secret (only shows length).

2. Add a package-level constant for the hardcoded local clone path:

   ```go
   const vaultLocalPath = "/data/vault"
   ```

3. Create `task/controller/pkg/gitclient/git_client.go` with:
   - Interface:
     ```go
     //counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient
     type GitClient interface {
         // EnsureCloned clones the repo if not present, validates if already cloned.
         EnsureCloned(ctx context.Context) error
         // Pull runs git pull on the local clone.
         Pull(ctx context.Context) error
         // Path returns the local clone path.
         Path() string
     }
     ```
   - Private struct with fields: `repoURL string`, `localPath string`, `branch string`.
   - Constructor:
     ```go
     func NewGitClient(gitURL string, gitToken string, localPath string, branch string) GitClient
     ```
   - The constructor builds the authenticated URL by embedding the token:
     `https://x-access-token:TOKEN@github.com/owner/repo.git` (replace the scheme+host portion of gitURL).
   - All git operations use `os/exec` subprocess — `exec.CommandContext(ctx, "git", args...)`.
   - `EnsureCloned(ctx)`:
     - If `localPath` does not exist: run `git clone --branch <branch> --single-branch <authURL> <localPath>`
     - If `localPath` exists and has `.git/`: validate with `git -C <localPath> rev-parse --git-dir` — if it fails, return error
     - If `localPath` exists but no `.git/`: return error
   - `Pull(ctx)`: run `git -C <localPath> pull`
   - `Path()`: return `localPath`
   - Import: `github.com/bborbe/errors` for error wrapping

4. Create `task/controller/pkg/gitclient/git_client_test.go` with a Ginkgo suite:
   - Suite bootstrap: `task/controller/pkg/gitclient/gitclient_suite_test.go`
   - Tests use `os.MkdirTemp` for temp dirs, `exec.Command("git", "init")` to create test repos.
   - Test cases:
     - `EnsureCloned` with non-existent path → runs `git clone` (mock via checking the command was attempted — or use a local bare repo as remote)
     - `EnsureCloned` with valid git repo → succeeds without cloning
     - `EnsureCloned` with existing dir but no `.git/` → returns error
     - `Pull` on valid repo → succeeds
     - `Path()` returns the configured local path
   - Use `package gitclient_test` (external test package).

5. In `task/controller/main.go`, update `Run()`:
   - Create git client: `gitClient := gitclient.NewGitClient(a.GitURL, a.GitToken, vaultLocalPath, a.GitBranch)`
   - Call `gitClient.EnsureCloned(ctx)` — if error, return immediately (fatal)
   - Pass `gitClient` to downstream components (scanner will use it in prompt 2)

6. Run `make generate` in `task/controller/` to regenerate mocks.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib.Task`, `lib.TaskV1SchemaID`, or `lib.TaskIdentifier` — these are frozen
- Do NOT use `vault-cli/pkg/domain` types in this prompt — they are used in the scanner prompt
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have counterfeiter annotations; mocks must be generated
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Use external test packages (`package gitclient_test`)
- Existing healthz/readiness/metrics endpoints must continue to work unchanged
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
</verification>
