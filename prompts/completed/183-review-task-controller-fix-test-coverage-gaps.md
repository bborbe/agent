---
status: completed
summary: Added test coverage for NewGitRestVaultScanner, exponentialBackoff, extractFrontmatter CRLF, and processFile YAML unmarshal failure
container: agent-exec-183-review-task-controller-fix-test-coverage-gaps
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T06:06:37Z"
completed: "2026-05-26T06:25:17Z"
---

<summary>
- Adds test for NewGitRestVaultScanner using fake git client (fileOps path)
- Adds test for exponentialBackoff function directly
- Adds test for ExtractFrontmatter with CRLF line endings
- Adds test for processFile YAML unmarshal failure after deduplication
</summary>

<objective>
Four untested code paths identified by test coverage analysis: (1) `NewGitRestVaultScanner` constructor never exercised — only `NewVaultScanner` (local file ops) is tested; (2) `exponentialBackoff` function never called — only `zeroBackoff` and `shortBackoff` are used in tests; (3) `ExtractFrontmatter` CRLF trim path untested; (4) `processFile` YAML unmarshal failure after `deduplicateFrontmatter` is untested. After this fix, all four gaps are covered.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner_test.go` — existing test patterns
- `task/controller/pkg/gitrestclient/git_rest_client_test.go` — existing backoff tests
- `task/controller/pkg/result/result_writer_test.go` — ExtractFrontmatter tests
- `task/controller/pkg/scanner/vault_scanner.go` — `NewGitRestVaultScanner` at line 113, `processFile` at line 229
</context>

<requirements>

### 1. Add test for NewGitRestVaultScanner

In `pkg/scanner/vault_scanner_test.go`, add an `It(...)` block inside the existing `Describe("NewGitRestVaultScanner")` or `Describe("VaultScanner")` block:

```go
It("uses fileOps (ListFiles/ReadFile/WriteFile) through the fileOps interface", func() {
    // Use testGitClient which implements fileOps via ListFiles/ReadFile/WriteFile
    // Exercise the path through NewGitRestVaultScanner
    // Assert that the scanner uses the fileOps methods correctly
})
```

### 2. Add test for exponentialBackoff

In `pkg/gitrestclient/git_rest_client_test.go`, add a `Describe("exponentialBackoff")` block:

```go
Describe("exponentialBackoff", func() {
    It("returns correct doubling sequence", func() {
        Expect(exponentialBackoff(1)).To(Equal(1 * time.Second))
        Expect(exponentialBackoff(2)).To(Equal(2 * time.Second))
        Expect(exponentialBackoff(3)).To(Equal(4 * time.Second))
        Expect(exponentialBackoff(4)).To(Equal(8 * time.Second))
        Expect(exponentialBackoff(5)).To(Equal(16 * time.Second))
    })
})
```

Note: `exponentialBackoff` is a package-level unexported function. To test it, either export it for tests or use the package-level test approach (`package gitrestclient_test` would need to export it — prefer exporting via a test helper).

### 3. Add test for ExtractFrontmatter with CRLF

In `pkg/result/result_writer_test.go`, add:

```go
It("handles CRLF line endings", func() {
    content := []byte("---\r\ntask_identifier: test\r\n---\r\nbody")
    fm, err := ExtractFrontmatter(ctx, content)
    Expect(err).NotTo(HaveOccurred())
    Expect(fm).To(Equal("task_identifier: test"))
})
```

### 4. Add test for processFile YAML unmarshal failure after deduplication

This requires a file whose content survives deduplication but has invalid YAML. Create a test that provides such input to `processFile`.

### 5. Run tests:
```bash
cd task/controller && make test
```

### 6. Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

</requirements>

<constraints>
- Only add test code — no production code changes
- Follow Ginkgo v2 + Gomega patterns
- External test package (`_test`)
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
