---
status: completed
summary: Wrapped buildFailureSection non-empty message in a fenced code block for readable markdown rendering, added fence assertions to all relevant test blocks, added empty-message test, and updated CHANGELOG.
container: agent-108-wrap-failure-reason-in-fenced-code-block
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T08:51:01Z"
queued: "2026-05-13T08:51:01Z"
started: "2026-05-13T08:51:03Z"
completed: "2026-05-13T08:54:21Z"
---
<summary>
- `lib/delivery/content-generator.go:80-91` `buildFailureSection` currently renders the failure reason as `- **Reason:** <message>\n` — a single bullet line with the full message inline. When the message is short ("agent crashed", "config missing") that reads fine. When it's the new stream-json tail from `lib/v0.61.1` (5 lines of JSON joined with ` | `, ~2KB total), the body section becomes a wall of unwrapped text that renders poorly in Obsidian and other markdown viewers: the `**`, `_`, `[]`, and `{}` characters in the JSON confuse markdown parsers, the text doesn't wrap predictably, and operators can't easily select-and-copy the JSON portion.
- Fix: wrap the message in a fenced code block. The bullet `- **Reason:**` stays as a label line (with no trailing space + content), followed by a blank line, followed by a fenced `\`\`\`` block containing `result.Message` verbatim, followed by `\`\`\`` to close. Result renders as a labeled, monospace, selectable block.
- No public API change. No new fields on `Result`. No message splitting or content extraction. Pure presentation fix in one function.
- Existing tests assert on `## Failure` heading and "Reason" substring — both survive. Add one new assertion that the rendered output contains the fenced block markers when `Message != ""`. The empty-message path (`agent returned status: failed (no message provided)`) keeps its current single-line bullet form — short, no fence needed.
- Live evidence: dev replay of `bborbe/go-skeleton#10` task with broken OAuth (2026-05-13) produced a working but unreadable `## Failure` body. Wrapping the same message content in `\`\`\`` makes it operationally clean.
</summary>

<objective>
Failure body sections become readable in standard markdown viewers (Obsidian, GitHub, generic CommonMark) when the message is long or contains markdown-confusing characters. The contract that the `## Failure` section exists and carries `Result.Message` is unchanged; only the wrapping changes. Operators stop seeing the JSON tail eat its own asterisks and brackets, and gain a one-click select-and-copy block for pasting into incident reports.

This is the last polish on the "operator-readable failure body" story: lib/v0.61.0 added the section, lib/v0.61.1 made its content informative, this change makes that content scan-able at a glance.
</objective>

<context>

Read `CLAUDE.md` at repo root for project conventions (Ginkgo/Gomega, multi-module mono-repo, paired `vX.Y.Z` + `lib/vX.Y.Z` tags on release).

## File to edit

### `lib/delivery/content-generator.go` — function `buildFailureSection`, lines 80-91

**Current:**

```go
func buildFailureSection(result agentlib.AgentResultInfo) string {
	var b strings.Builder
	b.WriteString("## Failure\n\n")
	if result.Message != "" {
		b.WriteString("- **Reason:** ")
		b.WriteString(result.Message)
		b.WriteString("\n")
	} else {
		b.WriteString("- **Reason:** agent returned status: failed (no message provided)\n")
	}
	return b.String()
}
```

**After:**

```go
func buildFailureSection(result agentlib.AgentResultInfo) string {
	var b strings.Builder
	b.WriteString("## Failure\n\n")
	if result.Message != "" {
		b.WriteString("- **Reason:**\n\n")
		b.WriteString("```\n")
		b.WriteString(result.Message)
		b.WriteString("\n```\n")
	} else {
		b.WriteString("- **Reason:** agent returned status: failed (no message provided)\n")
	}
	return b.String()
}
```

Rationale for the structure:
- `- **Reason:**\n\n` — bullet with the label, blank line follows. The blank line is required by CommonMark to terminate the list-item context and allow the next block (the fence) to render at top level.
- `\`\`\`\n<message>\n\`\`\`\n` — fenced code block at column 0 (NOT indented under the bullet). The fence at column 0 renders the content as a plain code block sibling to the list, which Obsidian + GitHub + most CommonMark parsers handle correctly. Indenting the fence under the bullet (`  \`\`\``) is CommonMark-legal but trips up some viewers when the message contains its own backticks or the message is long.
- No language hint on the fence (`\`\`\`` not `\`\`\`json`). Reason: messages may be JSON, plain text, golang error strings, or mixed. A wrong language hint causes syntax-highlight false positives that look worse than no highlighting.
- Trailing newline `\n` after the closing fence so the section appends cleanly to other body content via `ReplaceOrAppendSection`.

### `lib/delivery/content-generator_test.go`

Existing tests that assert `ContainSubstring("## Failure")` and `ContainSubstring("Reason")` continue to pass — neither substring is altered.

Existing tests that assert the message text directly (e.g. `ContainSubstring("claude CLI failed: exit status 1")` at line 56) continue to pass — the message is still rendered verbatim, just inside a fence.

Add ONE new assertion in each Context block that currently exercises the failure path with a non-empty `Message`:

```go
Expect(generated).To(ContainSubstring("```\n"))
```

This guards the contract that the message is fence-wrapped. The triple-backtick + newline is specific enough that no other markdown construct produces it accidentally.

If a test uses `DescribeTable` for failure-section content, add the assertion once at the table level rather than per-entry.

There is currently NO test for the `Status=Failed && Message==""` path of `buildFailureSection`. ADD a new `It("omits fence when Message is empty on failed status")` block inside the existing `FallbackContentGenerator` `Describe` block. The new `It` MUST assert all three:

- `Expect(generated).To(ContainSubstring("## Failure"))`
- `Expect(generated).To(ContainSubstring("no message provided"))` (the fallback string from the empty branch)
- `Expect(generated).NotTo(ContainSubstring("\`\`\`"))` — empty-reason path MUST NOT emit a fence (would render as an empty code block)

Build the test input as `agentlib.AgentResultInfo{Status: agentlib.AgentStatusFailed, Message: "", Output: ""}` with an original-content fixture matching the suite's existing test fixtures.

### `CHANGELOG.md` (repo root)

Add a `## Unreleased` section if not present, or append to it if present:

```
- fix(lib/delivery): wrap the failure-section `Reason:` body in a fenced code block. Previously rendered as a single inline bullet, which produced unreadable output in Obsidian / GitHub / generic CommonMark viewers when `Result.Message` was long or contained markdown-confusing characters (asterisks, brackets, braces — common in JSON tails from `lib/v0.61.1`). The fence preserves monospace formatting, prevents stray markdown interpretation, and gives operators a one-click select-and-copy block. Empty-reason fallback keeps its inline form.
```

</context>

<constraints>

- The change is confined to `lib/delivery/content-generator.go` `buildFailureSection` + `lib/delivery/content-generator_test.go` + `CHANGELOG.md`. No other files touched.
- `ContentGenerator` interface MUST remain unchanged. `Result` shape MUST remain unchanged.
- `## Failure` heading text is FROZEN — do not change it.
- The substring `- **Reason:**` MUST remain present. Although no existing test in this repo directly asserts on `**Reason:**`, preserving the label keeps the rendered body stable for operators reading the task page and avoids a silent visual regression. The change is from `- **Reason:** <inline message>` to `- **Reason:**\n\n\`\`\`\n<message>\n\`\`\``. The label substring is preserved verbatim.
- The empty-message case MUST keep its current single-line bullet form: `- **Reason:** agent returned status: failed (no message provided)\n`. No fence on the empty path.
- Fence language hint: NONE (`\`\`\`` not `\`\`\`json`). Reason in `<context>`.
- Fence position: column 0, NOT indented under the bullet. Reason in `<context>`.
- Errors must be wrapped with `github.com/bborbe/errors` (no new errors introduced here).
- `cd lib && make precommit` MUST exit 0.
- Existing tests asserting on the message content (e.g. `claude CLI failed`) MUST still pass — they use `ContainSubstring`, not `Equal`, so fence-wrapping is transparent.
- Do NOT add fence markers to `buildMinimalResultSection` (lines 93-) — different function, different contract. The `## Result` section is for success-path summaries, doesn't have the long-content problem.
- Do NOT introduce a configuration knob ("should we fence?"). The behavior is unconditional for the non-empty-message path.
- The change is one logical commit: code + tests + CHANGELOG together.
- Tag policy on release: paired `vX.Y.Z` + `lib/vX.Y.Z` per repo `CLAUDE.md`. This prompt does NOT cut the release; that's a sibling manual step.

</constraints>

<failure_modes>

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `Result.Message` contains a literal `\`\`\`` substring (rare — would have to come from agent's own error event) | Fence is broken by the inner backticks; Obsidian renders weirdly | Acceptable for v1; future fix could escape or use `\`\`\`\`` outer fence. Out of scope here. |
| `Result.Message` is empty | Empty-reason path taken; no fence emitted; existing single-line bullet preserved | None — by design |
| Existing test asserts on exact rendered output (e.g. `Expect(generated).To(Equal(...))` with the old format) | Test fails after the change | Update the expected string in the test to match new format. Search for `Equal(.*Reason.*` patterns and rewrite. |
| Obsidian renders the fence at column 0 outside the bullet structure | Acceptable — the `- **Reason:**` label is visually adjacent; readers do not perceive semantic separation | None |
| Multiple agents write multiple `## Failure` sections to the same task page (`ReplaceOrAppendSection` collapse behavior) | Each replacement carries its own fence; no accidental fence-cross-contamination | None — `ReplaceOrAppendSection` operates on heading boundaries, not content |
| Newline handling: message ends with `\n` | Closing fence still on its own line because we explicitly write `\n\`\`\`\n` | None |

</failure_modes>

<acceptance_criteria>

- [ ] `lib/delivery/content-generator.go` `buildFailureSection` writes `- **Reason:**\n\n\`\`\`\n<message>\n\`\`\`\n` when `Result.Message != ""`.
- [ ] `lib/delivery/content-generator.go` `buildFailureSection` writes the unchanged single-line bullet `- **Reason:** agent returned status: failed (no message provided)\n` when `Result.Message == ""` (verify no fence on the empty path).
- [ ] `lib/delivery/content-generator_test.go` has a new assertion `Expect(generated).To(ContainSubstring("\`\`\`\n"))` in at least one Context exercising the non-empty-message failure path.
- [ ] `lib/delivery/content-generator_test.go` has (existing or new) `NotTo(ContainSubstring("\`\`\`"))` in the empty-message Context.
- [ ] Existing tests asserting on the message-content substring (e.g. `ContainSubstring("claude CLI failed")`) still pass.
- [ ] `CHANGELOG.md` has a new `fix(lib/delivery):` bullet under `## Unreleased`.
- [ ] `cd lib && make precommit` exits 0.
- [ ] `git diff --name-only HEAD -- lib/ CHANGELOG.md` shows EXACTLY:
  - `CHANGELOG.md`
  - `lib/delivery/content-generator.go`
  - `lib/delivery/content-generator_test.go`
- [ ] No file outside `lib/delivery/` and `CHANGELOG.md` is modified.

</acceptance_criteria>

<verification>

```bash
cd lib
make precommit
```

Spot-check the rendered output (a one-line eyeball test):

```bash
go test -run TestGinkgo ./delivery/... -v 2>&1 | head -40
```

Then `cat` a sample test output or `Printf` once during a local run to visually confirm the fence wraps the message.

Diff scope check:

```bash
git diff --name-only HEAD -- lib/ CHANGELOG.md
```

Expected end state:
- `buildFailureSection` writes fence-wrapped reason for non-empty messages
- Empty-reason path unchanged
- One new assertion guards the fence
- Existing tests still green
- CHANGELOG documents the change

</verification>

<do_nothing_option>
Leaving `buildFailureSection` as-is means the `lib/v0.61.1` stdout-tail content keeps producing visually noisy task bodies. Operators get the answer (the 401 message IS in the body) but have to skim a wall of JSON to find it. In Obsidian specifically, the asterisks, brackets, and braces in the JSON sometimes hit markdown's emphasis/link parsers and render as broken inline italics or stray strikethrough. The volume of information is right; the formatting is one extra layer of friction on every failure investigation.

This change is the smallest possible move that completes the readability story without re-introducing shape-coupling (no parsing of the message into fields). One function, ~4 lines changed, one new test assertion, one CHANGELOG bullet.
</do_nothing_option>
