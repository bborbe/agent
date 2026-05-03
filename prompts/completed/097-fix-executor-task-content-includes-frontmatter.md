---
status: completed
summary: Fixed task executor to include YAML frontmatter in TASK_CONTENT env var for spawned Jobs by adding renderTaskContent helper and buildJobEnvBuilder method, with 3 new regression tests and a round-trip integration test.
container: agent-097-fix-executor-task-content-includes-frontmatter
dark-factory-version: dev
created: "2026-05-03T00:00:00Z"
queued: "2026-05-03T13:04:56Z"
started: "2026-05-03T13:04:57Z"
completed: "2026-05-03T13:11:16Z"
---

<summary>
- The task executor today strips YAML frontmatter from the markdown it sends to spawned agent pods via the `TASK_CONTENT` env var. The agent receives the body only.
- Downstream agents (e.g. agent-pr-reviewer) parse `TASK_CONTENT` back into a Markdown and read fields like `clone_url`, `ref`, `base_ref` from the frontmatter map. Today that map is always empty for spawned tasks, so the agent fails immediately with `clone_url is missing from task frontmatter` and the task escalates to `phase: human_review`.
- After this prompt, the executor renders the full markdown (frontmatter + body) before assigning to `TASK_CONTENT`, so the spawned agent sees exactly what is in the vault file.
- A regression test asserts the spawned Job's `TASK_CONTENT` env contains both `---` delimiters and the frontmatter keys.
- Scope is the executor only — the controller's result writer and the lib markdown parser are already correct and untouched.
</summary>

<objective>
Stop the executor from stripping frontmatter when emitting `TASK_CONTENT`. After this prompt, every spawned Job receives the full markdown (frontmatter delimiters + frontmatter YAML + body) in its `TASK_CONTENT` env var, unblocking pr-reviewer and any future agent that reads frontmatter fields.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `errors.Wrapf` from `github.com/bborbe/errors`, never `fmt.Errorf`.
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2 + Gomega, external `_test` package convention.
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `## Unreleased` section conventions.

**Bug observed live in dev (2026-05-03):**
Pod `pr-reviewer-agent-a5b7903a-20260503125556-2splt` failed with:
`{"Status":"failed","Message":"execution step: clone_url is missing from task frontmatter"}`
The owning vault file contained `clone_url`, `ref`, `base_ref` correctly. The executor stripped them before launch, so the agent's `lib.ParseMarkdown` produced an empty `Frontmatter` map.

**Root cause** — `task/executor/pkg/spawner/job_spawner.go` `SpawnJob` method, around line 77:
```go
envBuilder.Add("TASK_CONTENT", string(task.Content))
```
`lib.Task.Content` is the body only (see `lib/agent_task.go:23` — `Content TaskContent`). The frontmatter lives separately in `task.Frontmatter` (line 22) and never reaches the spawned Job.

**Reference pattern (DO NOT modify these files — read only):**
- `task/controller/pkg/result/result_writer.go:122-132` — controller's `WriteResult` shows the canonical `"---\n" + yaml + "---\n" + body` rendering. The yaml import is `gopkg.in/yaml.v3` (see `result_writer.go:17`).
- `lib/agent_markdown.go:108-119` — `Markdown.Marshal` shows the same render shape; informative for the rendering contract but DO NOT call it from the spawner (it operates on a `Markdown` struct, not a `Task` — keep the spawner's render local and inline).

**Files to read in full before editing:**
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob` is the only function that changes. The `envBuilder.Add("TASK_CONTENT", ...)` call at ~line 77 is the bug site. Imports already include `github.com/bborbe/errors`.
- `task/executor/pkg/spawner/job_spawner_test.go` — `Describe("SpawnJob")` block. The test at line 61 (`"creates a job with correct name and env vars"`) currently asserts `envMap["TASK_CONTENT"]).To(Equal("do the work"))` — that assertion changes shape after this fix. Several other test cases (lines 113, 141, 168, 199, 218, 251, 267, 302, 326) construct `lib.Task{...}` with various `Frontmatter`/`Content` combinations. Each that asserts `TASK_CONTENT` value (or its absence of frontmatter) must be updated; those that don't reference `TASK_CONTENT` need no change.
- `lib/agent_task.go` — confirms `Task.Frontmatter` is `lib.TaskFrontmatter` (a `map[string]any`-shaped type) and `Task.Content` is `lib.TaskContent` (a string-shaped type).

**Key facts (verified):**
- `TaskFrontmatter` is convertible to `map[string]any` (controller's result writer does `yaml.Marshal(map[string]any(merged))` at `result_writer.go:125`).
- `task.Content` is `lib.TaskContent`; `string(task.Content)` is the body string.
- `gopkg.in/yaml.v3` is already a transitive dep of this module (used by controller). Verify with `go list -m gopkg.in/yaml.v3` from `task/executor/`. If `task/executor/go.mod` does not yet list it, `goimports` / `go mod tidy` (run by `make precommit`) will add it.
- When `task.Frontmatter` is empty (`len == 0`), the rendered output should still be a valid markdown — i.e. emit `"---\n---\n" + body` (matches `Markdown.Marshal`'s shape when frontmatter is non-empty; when frontmatter is truly empty, see decision below).

**Empty-frontmatter decision:**
Mirror the controller's behaviour: it always emits the `---\n...\n---\n` wrapper because `merged` always has at least the controller-set fields. For the executor, `task.Frontmatter` should also always be non-empty in practice (it is parsed from a vault file that has `assignee`, `phase`, etc.). If `task.Frontmatter` IS empty, `yaml.Marshal` of an empty map returns `"{}\n"` — undesirable. Therefore: when `len(task.Frontmatter) == 0`, skip the wrapper entirely and emit `string(task.Content)` unchanged. This preserves the existing behaviour for any caller that legitimately has no frontmatter and avoids `{}` poisoning.
</context>

<requirements>

**Execute steps in this order. Run `make precommit` only in the final step.**

1. **Add a yaml import to `task/executor/pkg/spawner/job_spawner.go`.**

   Add `"gopkg.in/yaml.v3"` to the import block. Keep imports grouped per project convention (stdlib, blank line, third-party, blank line, intra-module — match the existing layout in this file).

2. **Render the full markdown before assigning to `TASK_CONTENT`.**

   Replace the existing single line at ~line 77:
   ```go
   envBuilder.Add("TASK_CONTENT", string(task.Content))
   ```

   With a render block that emits the frontmatter wrapper when `task.Frontmatter` is non-empty, and the raw body otherwise:
   ```go
   taskContent, err := renderTaskContent(ctx, task)
   if err != nil {
       return "", errors.Wrapf(ctx, err, "render task content for task %s", task.TaskIdentifier)
   }
   envBuilder.Add("TASK_CONTENT", taskContent)
   ```

   Note that `SpawnJob` returns `(string, error)`; the new `return "", err` matches that signature. Place this block in the same position as the original `envBuilder.Add("TASK_CONTENT", ...)` call — BEFORE the other `envBuilder.Add` calls so no env-ordering side effects appear.

3. **Add `renderTaskContent` as an unexported helper in the same file (`job_spawner.go`).**

   Place it alongside the other unexported helpers near the bottom of the file (after `taskPhaseString`, before or after `jobNameFromTask` — pick whichever keeps related helpers together). Use this exact shape:

   ```go
   // renderTaskContent serializes task into the markdown form an agent expects:
   // "---\n<yaml-frontmatter>---\n<body>". When task.Frontmatter is empty, the
   // body is returned unchanged (no empty "{}" wrapper).
   //
   // The agent side (lib.ParseMarkdown) reads frontmatter fields like
   // clone_url / ref / base_ref directly from this string — keep the wrapper
   // shape byte-compatible with controller/pkg/result.WriteResult.
   func renderTaskContent(ctx context.Context, task lib.Task) (string, error) {
       if len(task.Frontmatter) == 0 {
           return string(task.Content), nil
       }
       fmBytes, err := yaml.Marshal(map[string]any(task.Frontmatter))
       if err != nil {
           return "", errors.Wrapf(ctx, err, "marshal frontmatter for task %s", task.TaskIdentifier)
       }
       return "---\n" + string(fmBytes) + "---\n" + string(task.Content), nil
   }
   ```

   Do NOT export the helper. It is an implementation detail of `SpawnJob`.

4. **Update `task/executor/pkg/spawner/job_spawner_test.go`.**

   For the test at line ~61 (`"creates a job with correct name and env vars"`) — the `task.Frontmatter` already has `assignee` and `phase`. After the fix, `TASK_CONTENT` will be `"---\nassignee: claude\nphase: planning\n---\ndo the work"` (or with map-key order possibly swapped — yaml.v3 sorts map keys alphabetically, so `assignee` then `phase` is stable). Replace:
   ```go
   Expect(envMap["TASK_CONTENT"]).To(Equal("do the work"))
   ```
   With assertions that are ordering-tolerant and verify the round-trip contract:
   ```go
   Expect(envMap["TASK_CONTENT"]).To(HavePrefix("---\n"))
   Expect(envMap["TASK_CONTENT"]).To(ContainSubstring("\n---\n"))
   Expect(envMap["TASK_CONTENT"]).To(ContainSubstring("assignee: claude"))
   Expect(envMap["TASK_CONTENT"]).To(ContainSubstring("phase: planning"))
   Expect(envMap["TASK_CONTENT"]).To(HaveSuffix("\n---\ndo the work"))
   ```

   Walk every other `It(...)` in the file and update any `TASK_CONTENT` assertion to match the new shape (use `ContainSubstring` for body assertions; use the wrapper assertions above for frontmatter presence). Tests that do not assert `TASK_CONTENT` need no change.

5. **Add a new dedicated test case for the bug.**

   Add a new `It(...)` inside the existing `Describe("SpawnJob")` block. Place it immediately after the first happy-path test for locality. Required shape:
   ```go
   It("includes frontmatter delimiters and keys in TASK_CONTENT so spawned agents can parse fields like clone_url", func() {
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("pr-task-uuid"),
           Frontmatter: lib.TaskFrontmatter{
               "assignee":  "pr-reviewer",
               "phase":     "in_progress",
               "clone_url": "https://github.com/bborbe/code-reviewer.git",
               "ref":       "f82244d6abcdef",
               "base_ref":  "master",
           },
           Content: lib.TaskContent("# PR Review:\n\nbody here"),
       }
       config := pkg.AgentConfiguration{
           Assignee: "pr-reviewer",
           Image:    "pr-reviewer-agent:latest",
           Env:      map[string]string{},
       }
       _, err := jobSpawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())

       jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       Expect(jobs.Items).To(HaveLen(1))
       container := jobs.Items[0].Spec.Template.Spec.Containers[0]
       envMap := make(map[string]string)
       for _, e := range container.Env {
           envMap[e.Name] = e.Value
       }
       got := envMap["TASK_CONTENT"]
       // wrapper present
       Expect(got).To(HavePrefix("---\n"))
       Expect(got).To(ContainSubstring("\n---\n# PR Review:"))
       // frontmatter keys propagated — these are exactly the fields the
       // pr-reviewer execution step reads
       Expect(got).To(ContainSubstring("clone_url: https://github.com/bborbe/code-reviewer.git"))
       Expect(got).To(ContainSubstring("ref: f82244d6abcdef"))
       Expect(got).To(ContainSubstring("base_ref: master"))
       Expect(got).To(ContainSubstring("assignee: pr-reviewer"))
       // body preserved
       Expect(got).To(ContainSubstring("# PR Review:\n\nbody here"))
   })
   ```

6. **Add a level-1 round-trip integration test in the SAME file.**

   The bug class here is "the agent parses what the executor emits — but no test exercises that boundary". Add a second new `It(...)` immediately after the test in step 5 that calls `lib.ParseMarkdown` on the emitted `TASK_CONTENT` and asserts the parsed frontmatter equals the input frontmatter. This catches future regressions where the wrapper shape drifts away from what `lib.ParseMarkdown` accepts:
   ```go
   It("emits TASK_CONTENT that lib.ParseMarkdown round-trips back to the original frontmatter", func() {
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("roundtrip-task"),
           Frontmatter: lib.TaskFrontmatter{
               "assignee":  "pr-reviewer",
               "clone_url": "https://github.com/bborbe/code-reviewer.git",
               "ref":       "abc123",
               "base_ref":  "master",
           },
           Content: lib.TaskContent("# Body\n"),
       }
       config := pkg.AgentConfiguration{Assignee: "pr-reviewer", Image: "x:y"}
       _, err := jobSpawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())

       jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       container := jobs.Items[0].Spec.Template.Spec.Containers[0]
       var taskContent string
       for _, e := range container.Env {
           if e.Name == "TASK_CONTENT" {
               taskContent = e.Value
           }
       }
       Expect(taskContent).NotTo(BeEmpty())

       parsed, parseErr := lib.ParseMarkdown(ctx, taskContent)
       Expect(parseErr).NotTo(HaveOccurred())
       Expect(parsed.Frontmatter).To(HaveKeyWithValue("clone_url", "https://github.com/bborbe/code-reviewer.git"))
       Expect(parsed.Frontmatter).To(HaveKeyWithValue("ref", "abc123"))
       Expect(parsed.Frontmatter).To(HaveKeyWithValue("base_ref", "master"))
       Expect(parsed.Frontmatter).To(HaveKeyWithValue("assignee", "pr-reviewer"))
   })
   ```

   This covers the integration seam end-to-end: executor render → env var → agent parse. Without this test, a future change to either side could silently break the contract again.

7. **Add an empty-frontmatter test case.**

   Add a third new `It(...)` to confirm the fallback path:
   ```go
   It("emits raw body without wrapper when frontmatter is empty", func() {
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("no-fm-task"),
           Frontmatter:    lib.TaskFrontmatter{}, // explicitly empty
           Content:        lib.TaskContent("just a body"),
       }
       config := pkg.AgentConfiguration{Assignee: "claude", Image: "x:y"}
       _, err := jobSpawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())

       jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       container := jobs.Items[0].Spec.Template.Spec.Containers[0]
       envMap := make(map[string]string)
       for _, e := range container.Env {
           envMap[e.Name] = e.Value
       }
       Expect(envMap["TASK_CONTENT"]).To(Equal("just a body"))
       Expect(envMap["TASK_CONTENT"]).NotTo(ContainSubstring("---"))
       Expect(envMap["TASK_CONTENT"]).NotTo(ContainSubstring("{}"))
   })
   ```

8. **Append a CHANGELOG entry.**

   Edit `CHANGELOG.md` at the repo root. Append a bullet under the existing `## Unreleased` section (do not create a new section — `## Unreleased` already exists per recent commits):
   ```markdown
   - fix(task/executor): include YAML frontmatter when rendering `TASK_CONTENT` for spawned Jobs. Previously only the body was emitted, causing pr-reviewer (and any agent that reads frontmatter fields like `clone_url`, `ref`, `base_ref`) to fail with `clone_url is missing from task frontmatter`. The executor now emits `---\n<yaml>\n---\n<body>` matching the controller's result writer; round-trips through `lib.ParseMarkdown` cleanly.
   ```

9. **Run final verification:**
   ```bash
   cd task/executor && make precommit
   ```
   Must exit 0.

   If `make precommit` reports a missing `gopkg.in/yaml.v3` in `task/executor/go.mod`, run `go mod tidy` from `task/executor/` and re-run `make precommit`.

</requirements>

<constraints>
- Only edit files under `task/executor/` and `CHANGELOG.md` at repo root. Do NOT touch `task/controller/`, `lib/`, `agent/`, or any K8s manifest.
- Do NOT change agent or lib parsing — `lib.ParseMarkdown` is correct; the executor was the bug.
- Do NOT change the `JobSpawner` interface — only the body of `SpawnJob` and a new unexported `renderTaskContent` helper file-local.
- Use `github.com/bborbe/errors` (`errors.Wrapf`); never `fmt.Errorf`.
- Wrapper shape MUST be byte-compatible with `task/controller/pkg/result/result_writer.go:130-132` so both writers produce identical bytes for identical inputs.
- Empty `task.Frontmatter` (len == 0) MUST emit raw body — no `{}\n` wrapper, no `---\n---\n` wrapper. This preserves backwards-compatibility for any caller that today passes an empty frontmatter.
- Tests follow Ginkgo v2 + Gomega; external `_test` package (`spawner_test`) per existing convention.
- `make precommit` MUST exit 0 in `task/executor/`.
- Existing tests must keep passing (after the assertion shape updates in step 4).
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# Verify yaml import added
grep -n "gopkg.in/yaml.v3" task/executor/pkg/spawner/job_spawner.go
# Must show one line in the import block

# Verify the helper exists
grep -n "renderTaskContent" task/executor/pkg/spawner/job_spawner.go
# Must show definition + at least one call site (in SpawnJob)

# Verify the bug-site call no longer emits string(task.Content) directly for TASK_CONTENT
grep -n 'envBuilder.Add("TASK_CONTENT"' task/executor/pkg/spawner/job_spawner.go
# Must show: envBuilder.Add("TASK_CONTENT", taskContent) — NOT string(task.Content)

# Verify the wrapper render shape matches the controller's
grep -n '"---\\n"' task/executor/pkg/spawner/job_spawner.go
# Must show the same prefix/suffix literals as task/controller/pkg/result/result_writer.go:130-132

# Verify the round-trip test exists
grep -n "lib.ParseMarkdown" task/executor/pkg/spawner/job_spawner_test.go
# Must show at least one occurrence inside the new round-trip It(...)

# Verify the empty-frontmatter fallback test exists
grep -n "emits raw body without wrapper" task/executor/pkg/spawner/job_spawner_test.go
# Must show one match

# Verify CHANGELOG bullet present
grep -n "fix(task/executor): include YAML frontmatter" CHANGELOG.md
# Must show one match under ## Unreleased

# Run all checks
cd task/executor && make precommit
# Must exit 0
```

**Manual post-deploy check (informational, NOT executable here):**
After deploy, trigger a fresh pr-reviewer task with `phase: in_progress, status: in_progress` and frontmatter including `clone_url`, `ref`, `base_ref`. The agent pod log's `TaskContent` argument should show the full markdown including frontmatter delimiters, and the agent must NOT fail with `clone_url is missing from task frontmatter`.
</verification>
