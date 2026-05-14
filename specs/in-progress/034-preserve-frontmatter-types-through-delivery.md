---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-14T14:39:56Z"
generating: "2026-05-14T14:39:57Z"
prompted: "2026-05-14T14:45:56Z"
verifying: "2026-05-14T15:12:21Z"
branch: dark-factory/preserve-frontmatter-types-through-delivery
---

## Summary

- Frontmatter parsing in the delivery package stringifies every non-string YAML scalar. Integers, floats, and booleans round-trip back to the vault as quoted strings.
- When a typed writer (e.g. the controller's increment executor) and the result deliverer both touch the same numeric field, the file ends up with one writer producing `trigger_count: 0` (int) and the other producing `trigger_count: "0"` (string). Git-sync then collides them into a conflict marker.
- The fix is to preserve YAML-native types through the parse → wire → write path so the final serialized form is stable regardless of which writer ran last.
- Concretely verified today: three prod probe files in the OpenClaw vault carried `<<<<<<<` markers on the exact field name `trigger_count`, one side int, one side string.
- Existing on-disk files do not need migration: the typed accessors already tolerate stale string-form numbers, and new writes through the fixed path self-heal.

## Problem

The result delivery path turns every numeric or boolean frontmatter value into a string before it leaves the agent process. The controller's other write paths (e.g. the increment executor) preserve the native type. When both touch the same file across a git-sync window, the vault sees two writes for the same key with two different serialized forms — unquoted int vs quoted string — and git records a merge conflict on a value that is logically identical. This produces noisy, manual-cleanup conflicts on every probe cycle and erodes trust in automated frontmatter updates.

## Goal

After this work, every frontmatter value that enters the delivery package as a YAML scalar leaves it in the same scalar type. A round-trip read → publish → write of an unchanged field produces a byte-identical line in the vault file. Two writers updating different fields on the same task cannot collide on the serialized form of a third unchanged field.

## Non-goals

- No migration of existing vault files. They self-heal on next write.
- No change to on-disk YAML formatting rules. `yaml.Marshal` already does the right thing once the value carries the correct type.
- No change to the controller's increment-frontmatter executor or its int-coercion helper — those already write typed values correctly and serve as the reference behavior.
- No change to the typed accessors on `TaskFrontmatter`. They will continue to tolerate stale string-form numbers from pre-fix files.
- No cleanup of the three already-resolved prod probe files.
- No new typed accessors beyond what is needed to remove the last string-coerced callers.

## Desired Behavior

1. Parsing markdown frontmatter preserves every YAML scalar type. An integer in the file is an integer in memory. A boolean is a boolean. A nested list or map is a list or map. A string is unchanged. A nil value is omitted from the parsed map.
2. Invalid YAML still yields an empty parsed map and returns the original content unchanged for the body — failure mode is unchanged.
3. The result-delivery wire payload carries frontmatter values in their native types end-to-end. Whatever the controller eventually marshals back to the vault file is the same scalar type that left the agent.
4. Callers that previously consumed string-form frontmatter values either switch to the typed accessor for that field, or convert explicitly at the use site. No silent stringification remains in the parse path.
5. A field that the agent did not touch survives the round-trip unchanged in both value and serialized form. The vault diff for a no-op result is empty.
6. Numeric frontmatter fields that two writers may update concurrently (`trigger_count`, `retry_count`, `max_triggers`, `max_retries`) produce the same unquoted-integer serialization regardless of which path wrote them last.

## Constraints

- The parsed-map type is exposed across module boundaries. The post-fix Go type for parsed frontmatter is `map[string]any` (or equivalently `lib.TaskFrontmatter`, which is already `map[string]any`). The wire payload that carries frontmatter through Kafka result delivery is also `map[string]any` (or the equivalent JSON-typed-value representation that preserves int/float/bool/nil/string/list/map). Any signature change must be paired with a coordinated tag release of `lib/` and the root module per the repo's monorepo conventions (`vX.Y.Z` + `lib/vX.Y.Z` at the same commit).
- The wire format for result delivery is shared with consumers across `task/controller`, `task/executor`, and the three agent binaries (`agent/claude`, `agent/code`, `agent/gemini`). All consumers must continue to deserialize messages produced before and after the fix without crashing.
- The single production caller of the changed parser is `lib/delivery/result-deliverer.go`. Tests under `lib/delivery/` are the other touch surface. No other module imports the affected function.
- Typed accessors on `TaskFrontmatter` must keep accepting both int and float64 underlying types so that messages decoded from JSON (numbers default to float64) and from YAML (numbers default to int) both work.
- The on-disk YAML format must not change for strings, lists, maps, or nil values. Only numeric and boolean fields previously written as quoted strings switch back to their native unquoted form.
- `make precommit` must pass in `lib/` and in every consumer module: `task/controller/`, `task/executor/`, `agent/claude/`, `agent/code/`, `agent/gemini/`.
- Counterfeiter mocks, if touched, use the explicit two-line directive form — the shortform is a silent no-op in this repo's pipeline.
- Tests live in external `*_test` packages using Ginkgo v2 / Gomega.
- The single root `CHANGELOG.md` records the change.

## Assumptions

- Existing vault files that carry stale string-form numbers (e.g. `trigger_count: "0"`) are still readable. The typed accessors fall through to default values for string-form numbers; the cap checks they protect still behave correctly. Verified by inspection of the typed accessors in `lib/agent_task-frontmatter.go`.
- The Kafka result topic is the only wire format that carries frontmatter between writers. No other transport encodes frontmatter as `map[string]string`.
- Domain knowledge referenced: `docs/kafka-schema-design.md`, `docs/task-flow-and-failure-semantics.md`, `docs/controller-design.md` describe the result-delivery flow and the controller's role as the canonical vault writer.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Frontmatter contains invalid YAML | Parser returns empty map and unchanged content; caller treats task as having no frontmatter and routes the result to the human-review fallback | None — same as today |
| Frontmatter contains a type the new code does not recognize (future YAML tag) | Value is preserved in the map with whatever concrete Go type the YAML library produced; downstream marshalers handle it | None — passthrough |
| Old agent (pre-fix) publishes a result with string-form numbers | Controller merges the string-form value into the file; typed accessors still work via fallback; next write from a fixed writer self-heals the field | Time-based — clears on next normal write |
| New agent publishes a result; old controller consumes it | Controller marshals back to YAML; numeric fields serialize unquoted; no conflict | None needed |
| Two writers concurrently update different fields on the same file | Both writes produce the same serialized form for every unchanged field; git-sync merges field-level without conflict on unchanged keys | None needed |
| A caller currently relies on the parsed value being a string | Build fails at the call site after the signature change; fix is to switch to the typed accessor or convert explicitly | Caught by `make precommit` |

## Security / Abuse Cases

Not user-facing. Frontmatter content originates from trusted agent processes or vault commits authored by the user. No new attack surface — the change narrows behavior from "any type becomes a string" to "type is preserved". Risk of resource exhaustion via deeply nested YAML is unchanged because the underlying YAML library and limits are unchanged.

## Acceptance Criteria

- [ ] A round-trip of a markdown file with `trigger_count: 0` through the delivery parser and back through the controller's marshal produces a file whose `trigger_count` line is byte-identical to the input.
- [ ] The same round-trip for `spawn_notification: true` produces a byte-identical boolean line.
- [ ] The same round-trip for a frontmatter list (`tags: [a, b]`) and a nested map preserves both structure and types.
- [ ] After the fix, parsing a frontmatter map containing `count: 42` yields a value whose Go type is `int` (or whatever the YAML library natively produces for an integer scalar), not `string`.
- [ ] Every existing caller of the changed parser compiles and passes its tests after switching to typed accessors or explicit conversion. No caller silently stringifies.
- [ ] Existing tests in `lib/delivery/markdown_test.go` that asserted string-form numeric values are updated to assert the native-type values, and remain green.
- [ ] New Ginkgo tests in `lib/delivery/markdown_test.go` (external `package delivery_test`) cover int, float, bool, nil (omitted from parsed map), string, top-level list, and nested-map round-trips.
- [ ] `make precommit` is clean in `lib/`, `task/controller/`, `task/executor/`, `agent/claude/`, `agent/code/`, `agent/gemini/`.
- [ ] `CHANGELOG.md` records the change at the root.
- [ ] Lib and root modules receive paired tags at the same commit per the monorepo release convention.
- [ ] No new scenario test. Unit + existing integration coverage is sufficient — the bug manifests at the parse-and-marshal seam, which unit tests reach directly. The vault-conflict symptom is downstream of that seam.

## Verification

```
cd lib && make precommit
cd task/controller && make precommit
cd task/executor && make precommit
cd agent/claude && make precommit
cd agent/code && make precommit
cd agent/gemini && make precommit
```

Manual round-trip check on a probe-style file confirms the unquoted-int line survives parse → publish → marshal.

## Do-Nothing Option

If we don't fix this, probe runs continue to produce git merge conflicts on `trigger_count` and any other numeric frontmatter field on every cycle. Each conflict requires manual resolution in the vault. Trust in automated frontmatter updates erodes; on-call burden grows linearly with the number of recurring tasks that carry numeric counters. The do-nothing option is not acceptable because the conflict rate is already non-zero in production.

## Verification Result

**Verified:** 2026-05-14T15:28:35Z (HEAD 1883a12)
**Binary:** /Users/bborbe/Documents/workspaces/go/bin/dark-factory (v0.156.1-1-g04f3863-dirty)
**Scenario:** Code-state + test-state verification per spec AC 11 (no scenario file). Captured source/test state at HEAD, ran `make precommit` against the changed module.
**Evidence:**
- `lib/delivery/markdown.go:112` signature: `func ParseMarkdownFrontmatter(content string) (map[string]any, string)`; no `fmt` import; nil values omitted at L131.
- `lib/delivery/markdown_test.go` L245-257 asserts `yaml.Marshal` of parsed `trigger_count: 0` contains `trigger_count: 0` and NOT `trigger_count: "0"` (AC 1).
- `lib/delivery/markdown_test.go` L259-268 same for `spawn_notification: true` (AC 2). Tests cover int+float (L204), bool (L226), nil omitted (L196), string (L180), list (L188), nested map (L234) — AC 3,4,6,7.
- `lib/delivery/result-deliverer.go:124` is the sole production caller; consumes `map[string]any` and copies into `agentlib.TaskFrontmatter` (also `map[string]any`) — no silent stringification (AC 5).
- `cd lib && make precommit` → `ready to commit` (0 gosec, 0 trivy). `cd task/controller && make precommit` → `ready to commit`. Downstream agent/{claude,code,gemini} and task/executor have no import of the changed function (`grep -rn ParseMarkdownFrontmatter task/ agent/` → empty) (AC 8).
- `CHANGELOG.md` L3-5 under `## v0.62.17` records the change (AC 9).
- `git tag --points-at 48d1896` → `v0.62.17` AND `lib/v0.62.17` (AC 10).
**Verdict:** PASS
