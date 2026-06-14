---
status: completed
summary: Added optional TargetVault field (JSON targetVault,omitempty) to task.CreateCommand with slug regex validator (^[a-z][a-z0-9-]*$) wired into CreateCommand.Validate; added 2 JSON round-trip tests and 2 DescribeTable slug-validity tables (4 valid + 5 invalid entries); validateCreateTargetVault at 100% coverage; make precommit in lib exits 0.
container: agent-multi-vault-routing-exec-200-multi-vault-routing-targetvault-field
dark-factory-version: v0.177.1
created: "2026-06-14T21:22:29Z"
queued: "2026-06-14T21:22:29Z"
started: "2026-06-14T21:22:31Z"
completed: "2026-06-14T21:27:00Z"
---

<summary>
- The CreateCommand Kafka payload gains a new top-level string field that lets producers declare which Obsidian vault a task belongs in
- When the field is empty, it is omitted from the JSON wire form entirely — old producers and old controllers keep working byte-for-byte
- When the field is non-empty, it must match a slug pattern (lowercase letter, then lowercase letters / digits / hyphens); any other value is rejected before publish
- Two new tests prove the wire form is backward compatible: a command with an empty field round-trips without ever emitting the JSON key
- A third new test proves explicit values are preserved across marshal/unmarshal and that the JSON contains the new key
- A new table test enumerates the slug-valid set (openclaw, personal, vault-2) and the slug-invalid set (Personal, leading space, internal space, leading digit, leading hyphen) so a future change to the regex cannot silently regress
- The CreateCommand struct, its Validate method, and its existing tests are the only files touched; the sender and the controller are out of scope for this prompt
- The validation function is exposed as a small helper in the same file so the sender (prompt 2) and the controller (prompt 3) can reuse it without duplicating the regex literal

</summary>

<objective>
Add an optional `TargetVault` field to `task.CreateCommand` (JSON `targetVault,omitempty`, Go type `string`) and extend `CreateCommand.Validate` with a slug check (`^[a-z][a-z0-9-]*$`) that only fires when the field is non-empty. Add the unit tests required by spec 044 AC 2, 3, 4: a JSON round-trip test for the empty case, a round-trip test for the explicit-value case, and a `DescribeTable` enumerating the slug matrix. The sender and the controller are out of scope; this prompt is the smallest self-contained unit that unblocks them.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md` (Interface → Constructor → Struct + error wrapping rules).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` (use `github.com/bborbe/errors`, never `fmt.Errorf`).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-validation-framework-guide.md` (`validation.All` / `validation.Name` / `validation.HasValidationFunc`).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` (Ginkgo v2 / Gomega / no stdlib table tests / no direct `*testing.T` outside the suite entry-point).

Key files to read in full before editing:
- `/workspace/lib/command/task/create-command.go` — the struct lives at lines 24-29 and `Validate` at lines 32-37; the existing `validateCreateTitle` helper (lines 39-61) is the in-file precedent for a field validator that returns `validation.HasValidation` and wraps with `errors.Wrap`
- `/workspace/lib/command/task/create-command_test.go` — the existing Ginkgo test file; new tests slot in alongside the existing `Describe("CreateCommand", ...)` and `Describe("CreateCommand.Validate", ...)` blocks

In-repo precedent for the slug regex (READ-ONLY reference, do not modify):
- `/workspace/lib/agent_task-type.go` lines 15-46 — same shape: a package-level `regexp.MustCompile` and a `Validate` method that wraps `!regex.MatchString` with `errors.Wrap(ctx, validation.Error, "...")`. Mirror that pattern but for the slightly stricter `^[a-z][a-z0-9-]*$` (must start with a lowercase letter; agent_task-type allows leading digit/hyphen — vault names are stricter).

Spec being implemented: `specs/in-progress/044-multi-vault-routing.md`. The exact field shape, JSON tag form, validation rules, and the four Acceptance Criteria this prompt covers (AC 2, 3, 4) are spelled out in the spec.

Inlined load-bearing snippets (copy verbatim into the new code, do not paraphrase from memory):

Current `CreateCommand` struct (lines 24-29 of `create-command.go`):
```go
type CreateCommand struct {
    TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
    Title          string              `json:"title"`
    Frontmatter    lib.TaskFrontmatter `json:"frontmatter"`
    Body           string              `json:"body,omitempty"`
}
```

Current `CreateCommand.Validate` method (lines 32-37):
```go
func (cmd CreateCommand) Validate(ctx context.Context) error {
    return validation.All{
        validation.Name("Title", validateCreateTitle(cmd.Title)),
        validation.Name("Body", validateCreateBody(cmd.Body)),
    }.Validate(ctx)
}
```

Current `validateCreateTitle` helper (lines 39-61) — the shape to mirror for `validateCreateTargetVault`:
```go
func validateCreateTitle(title string) validation.HasValidation {
    return validation.HasValidationFunc(func(ctx context.Context) error {
        runes := []rune(title)
        if len(runes) == 0 {
            return errors.Wrap(ctx, validation.Error, "title must not be empty")
        }
        ...
    })
}
```
</context>

<requirements>

1. **Add the `TargetVault` field to the struct**

   In `/workspace/lib/command/task/create-command.go`, extend the `CreateCommand` struct with a new last field. The struct (currently four fields) gains a fifth field. Place the new field AFTER `Body` so the existing four fields keep their relative order — diff reviewers expect field-additions to be appended, not inserted. The new field:

   ```go
   // TargetVault is the slug of the Obsidian vault this task belongs in.
   // Empty value means "use the controller's legacy default (openclaw)".
   // Wire format uses omitempty so legacy producers that never set it stay byte-compatible.
   TargetVault string `json:"targetVault,omitempty"`
   ```

   The field type is `string` (not a custom domain type) — the spec keeps the wire type simple and validates via the slug regex. The JSON tag is `targetVault,omitempty` — the lowercase camelCase form matches the existing `taskIdentifier` / `frontmatter` / `body` tags in this same struct.

2. **Add the slug regex constant and validator**

   In the same file, add a package-level regex variable and a validator helper, modeled on the `validateCreateTitle` shape. The regex literal is `^[a-z][a-z0-9-]*$` — copied verbatim from spec 044 Desired Behavior #2. Place both the regex and the helper in `create-command.go` directly below the existing `validateCreateTitle` / `validateCreateBody` block (or above the `Validate` method if more natural — your call, but keep the file flat, do NOT create a new file). The new code:

   ```go
   var targetVaultSlugRegexp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

   func validateCreateTargetVault(targetVault string) validation.HasValidation {
       return validation.HasValidationFunc(func(ctx context.Context) error {
           if targetVault == "" {
               // Empty is valid: legacy producers and the controller's
               // "use default vault" semantics both rely on it.
               return nil
           }
           if !targetVaultSlugRegexp.MatchString(targetVault) {
               return errors.Wrapf(
                   ctx,
                   validation.Error,
                   "targetVault %q must match ^[a-z][a-z0-9-]*$",
                   targetVault,
               )
           }
           return nil
       })
   }
   ```

   Add `"regexp"` to the stdlib import group of `create-command.go` (it is not currently imported in this file — verify with the existing import block before adding). The `errors` and `validation` imports are already present.

3. **Wire the new validator into `CreateCommand.Validate`**

   Modify the `Validate` method body to add the new check. The result must compile and must keep the existing `Title` and `Body` checks in the same relative order:

   ```go
   func (cmd CreateCommand) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("Title", validateCreateTitle(cmd.Title)),
           validation.Name("Body", validateCreateBody(cmd.Body)),
           validation.Name("TargetVault", validateCreateTargetVault(cmd.TargetVault)),
       }.Validate(ctx)
   }
   ```

   The new entry uses the same `validation.Name(...)` wrapper as the existing two — this produces a structured error mentioning `TargetVault` when the regex check fails, which downstream consumers (the sender in prompt 2) will see via `errors.Wrapf(ctx, err, "validate CreateCommand")`.

4. **Add a JSON round-trip test for the empty-TargetVault case (AC 3)**

   In `/workspace/lib/command/task/create-command_test.go`, add a new `It` block inside the existing `Describe("CreateCommand", ...)` block (the one starting around line 26 that already contains the existing round-trip tests). The new test:

   ```go
   It("round-trips with empty TargetVault: marshaled JSON has no targetVault key", func() {
       cmd := task.CreateCommand{
           TaskIdentifier: lib.TaskIdentifier("task-novault"),
           Title:          "My Task",
           Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
       }
       data, err := json.Marshal(cmd)
       Expect(err).To(BeNil())
       Expect(string(data)).NotTo(ContainSubstring("targetVault"))

       var got task.CreateCommand
       Expect(json.Unmarshal(data, &got)).To(Succeed())
       Expect(got.TargetVault).To(BeEmpty())
   })
   ```

   The two assertions are the spec's AC 3 evidence: the unmarshaled struct's `TargetVault` is empty AND the marshaled JSON does not contain the substring `targetVault` (proving `omitempty` is wired correctly).

5. **Add a JSON round-trip test for the explicit-TargetVault case (AC 3)**

   In the same `Describe("CreateCommand", ...)` block, add a second `It`:

   ```go
   It("round-trips with explicit TargetVault: JSON contains targetVault value", func() {
       cmd := task.CreateCommand{
           TaskIdentifier: lib.TaskIdentifier("task-personal"),
           Title:          "My Task",
           Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
           TargetVault:    "personal",
       }
       data, err := json.Marshal(cmd)
       Expect(err).To(BeNil())
       Expect(string(data)).To(ContainSubstring(`"targetVault":"personal"`))

       var got task.CreateCommand
       Expect(json.Unmarshal(data, &got)).To(Succeed())
       Expect(got.TargetVault).To(Equal("personal"))
   })
   ```

   This is the spec's AC 3 evidence for the non-empty case: the JSON contains the explicit key/value AND the value survives marshal/unmarshal.

6. **Add a slug-validity table test to the Validate suite (AC 4)**

   In `/workspace/lib/command/task/create-command_test.go`, add TWO new test blocks inside the existing `Describe("CreateCommand.Validate", ...)` block (the one starting around line 82, which already has the title-validation DescribeTable). The two blocks are:

   ```go
   DescribeTable("TargetVault empty value is valid",
       func(targetVault string) {
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "T",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
               TargetVault:    targetVault,
           }
           Expect(cmd.Validate(ctx)).To(Succeed())
       },
       Entry("empty string", ""),
       Entry("openclaw", "openclaw"),
       Entry("personal", "personal"),
       Entry("vault-2", "vault-2"),
   )

   DescribeTable("TargetVault invalid value is rejected",
       func(targetVault string) {
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "T",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
               TargetVault:    targetVault,
           }
           err := cmd.Validate(ctx)
           Expect(err).To(HaveOccurred())
           Expect(err.Error()).To(ContainSubstring("TargetVault"))
       },
       Entry("capitalized", "Personal"),
       Entry("leading space", " personal"),
       Entry("internal space", "per sonal"),
       Entry("leading digit", "1personal"),
       Entry("leading hyphen", "-personal"),
   )
   ```

   The 5 invalid entries are taken verbatim from spec 044 AC 4. The 4 valid entries cover the empty case (regression guard for `omitempty` consumers that leave it empty) plus three slugs of varying shape. The error-message assertion contains `TargetVault` so the field-named error from `validation.Name(...)` is exercised end-to-end.

7. **Verify wire compatibility is preserved (AC 1)**

   After implementing requirements 1-6, confirm two things by inspection of the new code:
   - A `task.CreateCommand{}` zero value marshals to JSON that does NOT contain the substring `targetVault` (covered by the new test in requirement 4).
   - The new field's JSON tag ends in `,omitempty` — verify by `grep "json:\"targetVault" /workspace/lib/command/task/create-command.go` returning one match with `,omitempty` after the value.

   Do NOT add an additional test for this — requirement 4 already covers it.

8. **Run `make precommit` in the lib service directory**

   From `/workspace/lib`:

   ```bash
   cd /workspace/lib && make precommit
   ```

   Must exit 0. All new tests turn green; all existing tests in the package remain green.

</requirements>

<constraints>
- Do NOT modify the `NewCreateCommandSender` signature or its body. The sender is the subject of prompt 2; touching it here would couple the two prompts and break the decomposition.
- Do NOT add a custom Go type for the vault name (no `type VaultName string`). The spec keeps it as `string` on the wire and validates via the slug regex — adding a type here would force a JSON-marshaling decision that the spec defers.
- Do NOT add a CHANGELOG entry in this prompt. Changelog updates are the controller-side prompt 3's responsibility (and they describe the full end-to-end feature, not just the field). Adding a duplicate or partial entry here would fragment the changelog.
- Do NOT add a new scenario under `scenarios/`. Spec 044 explicitly says NO new scenario — unit tests are sufficient for the field + validation behavior.
- Do NOT change the JSON tags on the existing four fields. The wire form for legacy fields stays byte-identical.
- Do NOT add a per-character check (forbidding specific characters) — the slug regex is the only rule. A value like `vault_name` fails the regex (underscore is not in `[a-z0-9-]`) and that single check is enough.
- Do NOT add the `validateCreateTargetVault` function to a new file. Keep it in `create-command.go` alongside the other two validators — the file is small and the package is flat.
- Do NOT commit — dark-factory handles git.
- All existing tests in `lib/command/task/...` must continue to pass after the change.
- Follow the project's `bborbe/errors` and `github.com/bborbe/validation` patterns (no `fmt.Errorf`, no `context.Background()` in business logic).
- Coverage for the new `validateCreateTargetVault` helper and the modified `Validate` method must be ≥80% per the project's DoD — the new DescribeTable entries cover both branches (empty, non-empty valid, non-empty invalid).
- Use the slug regex literal `^[a-z][a-z0-9-]*$` verbatim — do NOT substitute `regexp.QuoteMeta` or any other escape. The spec pins this exact pattern.
- Use the JSON tag `targetVault,omitempty` verbatim — lowercase first letter, no acronym capitalization, `omitempty` enabled.
- Use the validator function name `validateCreateTargetVault` (with the `validateCreate` prefix, matching the sibling `validateCreateTitle` / `validateCreateBody`).
</constraints>

<verification>
```bash
cd /workspace/lib && go test ./command/task/... -v -run 'TargetVault'
```
Must show all new TargetVault tests (the two `Describe` blocks and the two `It` round-trip blocks) turning green. The 9 new `Entry` lines (4 valid + 5 invalid) must all pass.

```bash
cd /workspace/lib && go test ./command/task/... -v
```
Must pass with all existing tests in the package still green.

```bash
grep -n 'targetVault' /workspace/lib/command/task/create-command.go
```
Must return at least two matching lines: one for the struct field JSON tag, one for the regex pattern literal (or error message), and one for the `validation.Name("TargetVault", ...)` call in `Validate`.

```bash
grep -cE 'glog\.|fmt\.' /workspace/lib/command/task/create-command.go
```
Should be zero (this file does not log; it only validates).

```bash
cd /workspace/lib && make precommit
```
Must exit 0.
</verification>

## DARK-FACTORY-REPORT
```yaml
status: success # or: failed, partial
summary: <one-paragraph description of what changed>
verification:
  command: "cd /workspace/lib && make precommit"
  exitCode: 0
improvements:
  - <category: PROMPT|GUIDE|GLOBAL>: <one-line suggestion>  # or omit if none
```
</content>
</invoke>