---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Replace fmt.Errorf with errors.Wrapf in PrintResult to follow banned-error-pattern rule
- Wrap bare return err in create-command title validation closures
- Wrap bare return err in claude-plugin-installer EnsureInstalled flow
</summary>

<objective>
Fix error handling violations in lib/: fmt.Errorf usage in agent_print-result.go, bare return err in create-command.go, and bare return err in claude-plugin-installer.go. After this change, all error returns use github.com/bborbe/errors with context wrapping and no bare return err remains in these files.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- lib/agent_print-result.go
- lib/command/task/create-command.go
- lib/claude/claude-plugin-installer.go
- lib/agent_markdown.go (for comparison of correct errors.Wrapf usage at line 114)
</context>

<requirements>
<!-- 1. Fix lib/agent_print-result.go:22 - fmt.Errorf instead of errors.Wrapf -->
<!--    - PrintResult currently takes no context; add context.Context parameter -->
<!--    - Replace fmt.Errorf("marshal result: %w", err) with errors.Wrapf(ctx, err, "marshal result") -->
<!--    - Remove unused "fmt" import if no longer needed -->
<!-- 2. Fix lib/command/task/create-command.go:54, 57 - bare return err -->
<!--    - validateTitleEdges call: return errors.Wrap(ctx, err, "validate title edges") -->
<!--    - validateTitleForbiddenChars call: return errors.Wrap(ctx, err, "validate title forbidden chars") -->
<!--    - These are inside the closure returned by validateCreateTitle -->
<!-- 3. Fix lib/claude/claude-plugin-installer.go:86, 111, 114 - bare return err -->
<!--    - Line 86 (ensureOne → EnsureInstalled): return errors.Wrap(ctx, err, "ensure plugin installed: "+spec.Name) -->
<!--    - Line 111 (runHard marketplace add): return errors.Wrap(ctx, err, "run marketplace add: "+spec.Marketplace) -->
<!--    - Line 114 (runHard plugin install): return errors.Wrap(ctx, err, "run plugin install: "+spec.Name) -->
<!-- 4. Verify all callers of PrintResult are updated to pass context -->
<!--    - Search for PrintResult calls in lib/ and agent/ directories -->
</requirements>

<constraints>
- Only change files in `lib/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), context propagation, factory pattern, time injection
</constraints>

<verification>
cd lib && make precommit
</verification>
