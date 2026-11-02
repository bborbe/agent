---
status: completed
summary: Added opt-in ClaudeRunnerConfig.RunAsUID privilege drop applied at both CLI spawn sites, with unix credential helper, range/group semantics, root- and non-root-gated tests, and a CHANGELOG Unreleased entry
execution_id: agent-run-as-uid-exec-262-claude-run-as-uid
dark-factory-version: v0.198.7
created: "2026-11-02T08:20:00Z"
queued: "2026-11-02T07:32:46Z"
started: "2026-11-02T07:32:59Z"
completed: "2026-11-02T07:47:44Z"
---

<summary>
- An agent's Go process can hold a credential in its own environment and still hand a Claude session no access to it — today it cannot, because the session runs under the same user id and can read the parent's environment through `/proc`.
- This adds an opt-in numeric user id to the runner configuration: when it is set, the spawned CLI runs as that id and the parent's environment becomes unreadable to it.
- The default is off, so every existing caller behaves exactly as before.
- The caller keeps responsibility for the directories the session writes to; this package only performs the privilege drop.
- Both spawn sites of the long-lived and one-shot CLI are covered, so the setting cannot be silently ignored by one of them.
</summary>

<objective>
Add an opt-in numeric-user-id privilege drop for the spawned Claude CLI process, so an agent can keep a credential in its own (root) environment while the session it drives runs as a different, unprivileged user id.
</objective>

<context>
Read `CLAUDE.md` for project conventions — in particular its rule that tests live in external `*_test` packages.

Read these files in full before editing:
- `claude/claude-runner-config.go` — the config struct. Every field carries a GoDoc comment; follow that style.
- `claude/claude-runner.go` — `buildCommand` (~line 269) constructs the one-shot CLI's `exec.Cmd`. `cmd.Env` is already an explicit allowlist built by `buildSubprocessEnv` (~line 492), so the child's environment is already stripped — this change is about the user id, not the environment.
- `claude/claude-session.go` — `startProcess` (~line 479) constructs the streaming session's `exec.Cmd` from the SAME `ClaudeRunnerConfig`, reached through `NewSession` / `NewSessionFactory`. It is the second spawn site and must apply the drop too, or the setting is silently inert for the session path.
- `claude/claude-runner_test.go` — the Ginkgo suite is in package `claude_test`. It defines a `writeShim` closure (line ~28, redefined per Describe block, e.g. line ~291) that puts a fake `claude` on `PATH` and drives the real `Run()` path; reuse that pattern rather than inventing a new harness.
- `docs/claude-streaming-protocol.md` — documents the CLI spawn and flag construction this change touches.
- `~/.claude/plugins/marketplaces/coding/docs/go-test-types-guide.md` — the unit/integration/e2e taxonomy (it does not govern external-vs-internal Go `*_test` packages; `CLAUDE.md` does).
- `interactive/export_test.go` — the repo's precedent for re-exporting an unexported symbol to an external `_test` package from a `_test.go` file that runs no specs.
- `CHANGELOG.md` — the top heading is `## v0.136.0`; there is no `## Unreleased` yet.

Key facts:
- On Linux, `/proc/<pid>/environ` is a `0400` file owned by the process's own user id. A process running as a different id gets `EACCES`. That is the whole mechanism — no `/proc` remount and no `hidepid` is needed for this vector.
- A spawned agent Job carries NO `securityContext`: `pkg/spawner/job_spawner.go` never sets one, `bborbe/k8s`'s `ContainerBuilder` has no default, and the Config CRD has no such field. The chart's hardened `securityContext` (`helm/values.yaml`, `capabilities.drop: [ALL]`) belongs to the executor Deployment, not to the Jobs it spawns. So a root container here still holds `CAP_SETUID`/`CAP_SETGID` and the drop works — do not add a capability grant to this change, and do not weaken the executor's own hardening.
</context>

<requirements>
1. In `claude/claude-runner-config.go`, add one field to `ClaudeRunnerConfig` with a GoDoc comment in the file's existing style:
   - `RunAsUID int` — when non-zero, the CLI subprocess runs as this numeric user id instead of inheriting the parent's. The parent must be root; when it is not, `cmd.Start()` fails with `EPERM` and `Run` returns that wrapped as `start claude CLI`. Zero means inherit.
   Document that the caller must make every directory the CLI writes to (`CLAUDE_CONFIG_DIR`, the working directory, and any scratch dir) owned by that user id, because this package only performs the privilege drop. Document the group behaviour too: the child's primary group is set to the same numeric value, and its supplementary groups are cleared, so it does not inherit the parent's.
2. Add `claude/claude-runner-credential_unix.go` with `//go:build unix` and the repo's BSD copyright header (every `.go` file carries it; `make precommit`'s `addlicense` would otherwise rewrite the file after the commit), exposing:
   ```go
   // credentialSysProcAttr returns the SysProcAttr that runs a child as uid.
   func credentialSysProcAttr(ctx context.Context, uid int) (*syscall.SysProcAttr, error)
   ```
   It returns `errors.Errorf(ctx, "uid %d out of range", uid)` (from `github.com/bborbe/errors`, matching the package's existing style) when `uid < 1 || uint64(uid) > math.MaxUint32` — the range check is what keeps the `int` → `uint32` conversion from tripping gosec G115, which is enabled in `.golangci.yml`. If gosec still reports G115 on the conversions, add `//nolint:gosec // range-checked above` rather than weakening the check. Otherwise it returns:
   ```go
   &syscall.SysProcAttr{Credential: &syscall.Credential{
       Uid: uint32(uid), Gid: uint32(uid), Groups: []uint32{},
   }}
   ```
   `Groups: []uint32{}` with `NoSetGroups` left false makes Go call `setgroups(2)` with an empty list, clearing the parent's supplementary groups; the root parent holds `CAP_SETGID`, so it succeeds. Do NOT set `NoSetGroups: true` — that would leave the parent's supplementary groups on the child.
   Add `claude/claude-runner-credential_other.go` with `//go:build !unix` returning `(nil, nil)`, so a non-unix build still compiles.
3. Apply the drop at BOTH spawn sites, only when `RunAsUID > 0`, and assign the result to `cmd.SysProcAttr` only when it is non-nil:
   - `claude/claude-runner.go`'s `buildCommand`, after `cmd.Env` is set.
   - `claude/claude-session.go`'s `startProcess`, after its `cmd.Env` assignment and before the pipes are created.
   Log at `V(2)` that the CLI will run as that user id. An error from the helper propagates out of the enclosing function.
4. Extend `claude/claude-runner_test.go` and `claude/claude-session_test.go` (both package `claude_test`), reusing the existing per-`Describe` `writeShim` / `writeSessionShim` closures — do NOT add an internal Ginkgo spec file (the repo's convention is external `*_test` specs only, and an internal Ginkgo file with no `RunSpecs` of its own silently runs zero specs). To reach the unexported helper, add `claude/export_test.go` in package `claude` with `var CredentialSysProcAttr = credentialSysProcAttr`, exactly as `interactive/export_test.go` does for `serverOptionFns` — a `_test.go` file, compiled only into test builds, runs no specs, and adds nothing to the public API.
   - Root-gated (`if os.Geteuid() != 0 { Skip("requires root") }`): a shim that prints `id -u` reports `1234` when `RunAsUID: 1234`. `GinkgoT().TempDir()` is `0700` and root-owned, so a child running as `1234` cannot traverse it — create the shim dir with `os.MkdirTemp("/tmp", "shim")` and `os.Chmod(dir, 0o755)`. Print `id -G` too and assert it is exactly the one gid: that is the only real check that the supplementary groups were cleared.
   - Non-root (the normal CI and container case): with `RunAsUID: 1234` and the shim on `PATH`, `Run` returns an error mentioning both `start claude CLI` and `operation not permitted` — the second substring is what separates the kernel rejecting the credential from a `LookPath` failure, which produces the same wrapper. This is the boundary assertion; a struct-shape test would not be.
   - With `RunAsUID: 0`, the shim reports the current user id — the zero value must not touch `cmd.SysProcAttr`.
   - In `claude/claude-session_test.go`, using the existing `writeSessionShim` helper, with a config that also sets `RunAsUID: 1234`: non-root, the session prompt returns an error mentioning `start claude process` — the session spawn site's own boundary assertion, so a drop wired only into `buildCommand` cannot pass the suite.
   - `claude.CredentialSysProcAttr` (the `export_test.go` alias) rejects `0`, `-1`, and `math.MaxUint32+1` with an error, and accepts `1` and `math.MaxUint32` without one.
   Use Ginkgo/Gomega, matching the repo's existing test style.
5. Add `## Unreleased` immediately above `## v0.136.0` in `CHANGELOG.md` (it does not exist yet) with one `feat(claude):` bullet describing the opt-in privilege drop, in the repo's existing entry style.
</requirements>

<constraints>
- Backwards compatible only: the zero value must leave `cmd.SysProcAttr` nil. No existing consumer changes behaviour.
- Do NOT chown anything, do NOT create users, do NOT touch `/proc`, and do NOT change `buildSubprocessEnv` or the env allowlist — the drop is the only behaviour added.
- Do NOT add the field to any other package or to the agent/step APIs.
- `claude/claude-plugin-installer.go` also spawns `claude` (`plugin list/install/update`), but it is a setup path, not the session: leave it unchanged. Add one comment on that spawn noting it does not set `cmd.Env` and therefore inherits the parent's full environment — a known gap for a separate change, not this one.
- Do NOT set `NoSetGroups: true`.
- Do NOT commit; dark-factory commits for you.
</constraints>

<verification>
Run from the repo root:

```bash
make test
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --config .golangci.yml ./claude/...
grep -n 'RunAsUID' claude/claude-runner-config.go
grep -n 'credentialSysProcAttr' claude/claude-runner.go claude/claude-session.go
grep -n '## Unreleased' CHANGELOG.md
```

Every command must exit 0. `make test` is the repo's suite; the lint run is the CI check that `make test` does not cover (gosec G115 on the `int` → `uint32` conversion is the specific thing it catches). Before finishing, re-run every command and walk each `<acceptance>` line against the change.
</verification>

<acceptance>
- `ClaudeRunnerConfig` carries `RunAsUID`, documented, including the non-root `EPERM` behaviour and the group semantics.
- `credentialSysProcAttr` exists under `//go:build unix`, range-checks its input, sets `Gid` to the same id and `Groups: []uint32{}`, and never sets `NoSetGroups`.
- Both `buildCommand` and `startProcess` apply it when `RunAsUID > 0`, and neither changes behaviour at zero.
- The `claude_test` suite covers the root-gated uid, the non-root `EPERM` on both spawn sites, the zero case, and the helper's range check — all executed, none skipped silently.
- `make test` and the golangci-lint run exit 0.
- `CHANGELOG.md` has a non-empty `## Unreleased` section above `## v0.136.0`.
</acceptance>
