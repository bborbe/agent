---
status: approved
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
---

<summary>
- Adds context cancellation check to scanFiles loop in vault_scanner.go
- Adds context cancellation check to collectDeleted iteration over v.hashes map
- Ensures graceful shutdown when context is cancelled during long file scans
</summary>

<objective>
The `scanFiles` function at line 184 iterates over all vault `.md` files without checking `ctx.Done()`. In a vault with thousands of files, a cancelled context would cause the full scan to complete before honoring cancellation. Similarly, `collectDeleted` at line 364 iterates over the `v.hashes` map with no context check. After this fix, both loops check for context cancellation per iteration.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — `scanFiles` at line 170, `collectDeleted` at line 364
</context>

<requirements>

### 1. Add ctx check to scanFiles loop

In the `scanFiles` function, add a `select` with `ctx.Done()` at the top of the loop body:

```go
for _, relPath := range paths {
    select {
    case <-ctx.Done():
        return nil, errors.Wrap(ctx, ctx.Err(), "scanFiles cancelled")
    default:
    }
    // ... existing processFile call
}
```

### 2. Add ctx check to collectDeleted

The `collectDeleted` function currently takes no context parameter. Either:
- Add `ctx context.Context` parameter and call it from the caller with the available ctx, OR
- Add the check inside the loop if ctx is not available at the call site

If adding ctx parameter, update the call site in `runCycle` to pass `ctx`.

```go
func (v *vaultScanner) collectDeleted(ctx context.Context, seen map[string]struct{}) []lib.TaskIdentifier {
    var deleted []lib.TaskIdentifier
    for relPath, entry := range v.hashes {
        select {
        case <-ctx.Done():
            return deleted, errors.Wrap(ctx, ctx.Err(), "collectDeleted cancelled")
        default:
        }
        if _, ok := seen[relPath]; !ok {
            deleted = append(deleted, entry.taskIdentifier)
            delete(v.hashes, relPath)
        }
    }
    return deleted
}
```

### 3. Run tests:
```bash
cd task/controller && make test
```

### 4. Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

</requirements>

<constraints>
- Only change `task/controller/pkg/scanner/vault_scanner.go`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`
</constraints>

<verification>
cd task/controller && make precommit
</verification>
