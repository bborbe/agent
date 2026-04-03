---
status: draft
created: "2026-04-03T15:30:00Z"
---

<summary>
- Scanner tolerates duplicate YAML frontmatter keys instead of skipping the file
- Duplicate keys are resolved by keeping the last value (standard YAML merge behavior)
- Files with duplicate frontmatter keys are automatically cleaned up before parsing
- Output frontmatter is guaranteed to never contain duplicate keys
- Existing tests pass, new tests cover duplicate key handling
</summary>

<objective>
Make VaultScanner resilient to duplicate YAML frontmatter keys. Currently yaml.v3 Unmarshal rejects files with duplicate keys (e.g. two `task_identifier` lines), causing the scanner to skip valid tasks. After this change, duplicate keys are silently resolved (last wins) and the file is processed normally.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Problem:** VaultScanner skips files with duplicate frontmatter keys:
```
skipping tasks/analyse-trades-2026-04-02.md: invalid frontmatter: yaml: unmarshal errors:
  line 10: mapping key "task_identifier" already defined at line 1
```

Duplicate keys can appear when:
- ResultWriter writes frontmatter that includes a key already present in the incoming map
- Manual edits accidentally duplicate a line
- obsidian-git merges create duplicates

**Current behavior:** `yaml.Unmarshal` in `processFile` (vault_scanner.go:161) returns an error on duplicate keys, causing the file to be skipped entirely.

**Desired behavior:** Deduplicate keys before Unmarshal (last value wins), log a warning, and process the file normally.

Files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — processFile and extractFrontmatter
- `task/controller/pkg/result/result_writer.go` — WriteResult frontmatter generation
- `task/controller/pkg/scanner/vault_scanner_test.go` — existing tests
</context>

<requirements>
1. **Add `deduplicateFrontmatter` helper in `task/controller/pkg/scanner/vault_scanner.go`:**
   - Takes raw frontmatter YAML string
   - Uses `yaml.Decoder` to parse into `yaml.Node` tree
   - Walks mapping nodes, keeps only the last occurrence of each key
   - Re-marshals to clean YAML string
   - Returns deduplicated YAML + bool indicating if duplicates were found

2. **Update `processFile` to use deduplication:**
   - After `extractFrontmatter`, call `deduplicateFrontmatter`
   - If duplicates found, log `glog.Warningf("file %s has duplicate frontmatter keys, deduplicating", relPath)`
   - Pass deduplicated YAML to `yaml.Unmarshal`

3. **Add tests:**
   - Test `deduplicateFrontmatter` with: no duplicates, one duplicate key, multiple duplicate keys
   - Test `processFile` with a file containing duplicate `task_identifier` — should parse successfully
   - Use Ginkgo/Gomega patterns per project conventions

4. **Run tests and precommit:**
   ```bash
   cd task/controller && make test
   cd task/controller && make precommit
   ```
</requirements>

<constraints>
- Do NOT change how frontmatter is written (ResultWriter) — only how it is read (VaultScanner)
- Use `gopkg.in/yaml.v3` Node API for deduplication, not string manipulation
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT update CHANGELOG.md
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run tests:

```bash
cd task/controller && make test
```
Must pass with exit code 0.

Verify deduplication function exists:

```bash
grep -n "deduplicateFrontmatter" task/controller/pkg/scanner/vault_scanner.go
```
Must show the function definition.

Run precommit:

```bash
cd task/controller && make precommit
```
Must pass with exit code 0.
</verification>
