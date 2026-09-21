---
status: completed
summary: 'Pinned k8s.io/kube-openapi in go.mod''s require block (be32def86098) instead of a replace, dropped the structured-merge-diff/v7 exclude, and added an ## Unreleased changelog entry so consumers no longer resolve two structured-merge-diff majors.'
execution_id: agent-exec-219-fix-structured-merge-diff-major-split
dark-factory-version: v0.196.0
created: "2026-09-21T19:58:50Z"
queued: "2026-09-21T19:58:50Z"
started: "2026-09-21T19:59:11Z"
completed: "2026-09-21T20:02:30Z"
---

# Fix the structured-merge-diff major split that makes this library unconsumable

<summary>
- Every consumer that requires this library alongside `github.com/bborbe/cqrs` currently fails to build
- The failure is a version-major split: two incompatible `structured-merge-diff` majors resolve at once
- The split is invisible in this repo's own build, because the current workarounds only apply here
- Two module directives cause it, and both are ignored when this module is a dependency rather than the main module
- Replacing them with an ordinary dependency pin fixes every consumer
- Consumers need no change of their own
- No source code changes are required
</summary>

<objective>
Make this library consumable again by pinning `k8s.io/kube-openapi` in the `require` block instead of the `replace` block, so the constraint survives into consumers. Today, every release from v0.87.5 onward is unusable by any consumer that also pulls `github.com/bborbe/cqrs` — which is the whole trading fleet — and the split cannot be seen from inside this repository, because `replace` and `exclude` are honoured only when the module is the main module.
</objective>

<context>
Read `CLAUDE.md` for project conventions, and `docs/dod.md` for the project Definition of Done — its Install section already forbids `exclude`/`replace` directives in `go.mod` ("break remote install"), which is the rule this change implements. Note it bans them categorically; this change deliberately keeps the unrelated `cloud.google.com/go v0.26.0` exclude, because `exclude` is honoured only in the main module and therefore cannot break a consumer.

Read `go.mod`. Two directives matter:

- A `replace` block near the top pinning `k8s.io/kube-openapi` to `v0.0.0-20260821135717-be32def86098`.
- An `exclude` block near the bottom carrying `sigs.k8s.io/structured-merge-diff/v7 v7.0.0`.

Read `CHANGELOG.md` for the entry format. There is currently no `## Unreleased` section — the topmost heading is a released version.

Background, so the fix is not mis-derived: the `require` block already asks for `k8s.io/kube-openapi v0.0.0-20260904170622-9ab3195f2a72`, which depends on `structured-merge-diff` **v7**. That pseudo-version is the only source of v7 in the graph. Everything else — `k8s.io/apimachinery`, `k8s.io/api`, `k8s.io/client-go`, `k8s.io/apiextensions-apiserver` — depends on **v6**. So v6 and v7 both resolve, and `k8s.io/apimachinery@v0.37.0/pkg/util/managedfields/internal/typeconverter.go:51` fails with a `v6` vs `v7` `TypeDef` mismatch. Note that apimachinery itself wants **v6**; the goal is to converge on v6, not to migrate anything to v7.
</context>

<requirements>
1. In `go.mod`, delete the entire `replace` block. It contains exactly one entry, pinning `k8s.io/kube-openapi`. Nothing else references that block.

2. In `go.mod`, delete the `sigs.k8s.io/structured-merge-diff/v7 v7.0.0` line from the `exclude` block. Leave the other entry (`cloud.google.com/go v0.26.0`) untouched — that exclude entry stays. After `go mod tidy` it may render as the single-line form `exclude cloud.google.com/go v0.26.0`; that is correct, and `make precommit` expands it back into a block.

3. In `go.mod`, in the `require` block, change the `k8s.io/kube-openapi` version from the pseudo-version ending `9ab3195f2a72` to the one ending `be32def86098`. The resulting line is:

   ```
   k8s.io/kube-openapi v0.0.0-20260821135717-be32def86098 // indirect
   ```

   This is the same version the deleted `replace` was pinning — the change is that it now lives where consumers can see it. Keep the `// indirect` marker; `go mod tidy` will restore it if it is dropped.

4. Run `go mod tidy`. It must leave the `k8s.io/kube-openapi` line present at the `be32def86098` version. If `tidy` moves it or removes it, stop and report — that means the module no longer resolves the pin, and the fix is not complete.

5. Add an `## Unreleased` section at the top of `CHANGELOG.md`, above the topmost released heading, containing one bullet:

   ```
   - chore: pin `k8s.io/kube-openapi` in the `require` block instead of a `replace`, and drop the `structured-merge-diff/v7` exclude — both directives are main-module-only, so consumers were resolving two `structured-merge-diff` majors at once and failing to build
   ```

6. Before finishing, walk the final `go.mod` once against this checklist: no `replace` block; no `v7` line in the `exclude` block; `kube-openapi` pinned at `be32def86098` inside `require`. Then run the self-check described in the verification section.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Change only `go.mod`, `go.sum` (only if `go mod tidy` rewrites it — it does not in this case) and `CHANGELOG.md`. No Go source file changes are needed or wanted; if the build fails for a reason that appears to need a source change, stop and report instead of writing code
- Do NOT run `go mod vendor` — `vendor/` is a build-time artifact in this repo, gitignored and regenerated by the image build
- Do NOT use `-mod=vendor` in any command
- Do NOT add any new `replace` or `exclude` directive, and do not touch the `cloud.google.com/go v0.26.0` exclude
- Do NOT change the Go directive, the module path, or any other dependency version
- Do NOT bump any version string in `helm/` or elsewhere — releases are handled separately
</constraints>

<verification>
Run `make precommit` — must pass.

Note what this does and does not prove. `make precommit` passes on the unfixed tree too, because the `replace` does hold this module together while it is the main module. It is a regression guard, not evidence the split is fixed. The directives below are the evidence, because they are what the consumer reads.

The `replace` is gone — `grep -c 'kube-openapi =>' go.mod || true` must print `0`. On the unfixed tree this prints `1`. Read the printed count, not the exit status: `grep -c` exits `1` when the count is zero, so the *correct* tree makes this command exit non-zero.

The `v7` exclude is gone — `! grep -q 'structured-merge-diff/v7' go.mod` must succeed. On the unfixed tree this fails, because the `exclude` carries the `v7` line.

The unrelated exclude survived — `grep -q 'cloud.google.com/go v0.26.0' go.mod` must succeed. Match on the module path, not on the block syntax: a plain `go mod tidy` collapses a one-entry `exclude` block to the single-line form `exclude cloud.google.com/go v0.26.0`, and `make precommit` expands it back into a block. Both forms are correct.

The pin is now where a consumer can see it — this must print `1`:

```bash
awk '/^\tk8s\.io\/kube-openapi v0\.0\.0-20260821135717-be32def86098 \/\/ indirect$/ {n++} END{print n+0}' go.mod
```

It matches the tab-indented `require` line specifically, so it does not count the deleted `replace` line — a plain `grep -c` on the version string prints `1` on the unfixed tree too, because the `replace` carried the same version, and would report a fix that is not there.

Do NOT verify this with `go mod graph` in this repository. It reports one major here either way, because both directives mask `v7` from this module's own graph — the `exclude` drops the excluded version, and the `replace` substitutes a `kube-openapi` whose own requirement is `v6`. It passes on the unfixed tree and would report a fix that is not there.

Before you finish: re-run every command above and confirm each stated result, then walk the requirement checklist once against the final `go.mod`. The point of this change is what a *consumer* resolves, and no command runnable inside this repository can observe that — so the checks above assert the exact bytes a consumer's resolver reads. Do not substitute a green `make precommit` for them.
</verification>
