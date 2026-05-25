---
status: completed
summary: Pass context.Context to parser.New instead of using context.Background(); propagate ctx to genai.Client and error wrapping
container: agent-exec-152-review-agent-gemini-add-context-to-parser
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T21:02:17Z"
completed: "2026-05-25T21:05:42Z"
---

<summary>
- Parser constructor uses hardcoded `context.Background()` internally, making it impossible to test with deadline or cancellation
- After this change, `parser.New` accepts a `context.Context` parameter and propagates it to the Gemini client and error wrapping
- The factory `CreateGeminiParser` is updated to pass its `ctx` parameter through to `parser.New`
</summary>

<objective>
Stop using `context.Background()` in the parser constructor. After this change, the parser can be instantiated with a deadline or cancelled context, enabling proper testability and graceful shutdown behavior.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- agent/gemini/pkg/parser/parser.go (~line 36-54, functions New and NewWithClient)
- agent/gemini/pkg/factory/factory.go (~line 43-53, CreateGeminiParser)
- agent/gemini/main.go (~line 94-98, call site)
- agent/gemini/cmd/run-task/main.go (~line 53-56, call site)
</context>

<requirements>
1. **Update `parser.New` signature** in `agent/gemini/pkg/parser/parser.go`

   Change the function signature from:
   ```go
   func New(apiKey string, model string) (*Parser, error)
   ```
   To:
   ```go
   func New(ctx context.Context, apiKey string, model string) (*Parser, error)
   ```

   Update the body to pass `ctx` to `genai.NewClient`:
   ```go
   client, err := genai.NewClient(ctx, &genai.ClientConfig{
       APIKey:  apiKey,
       Backend: genai.BackendGeminiAPI,
   })
   ```
   Remove the `//nolint:contextcheck` comment since the context is now properly propagated.

   Update the error wrapping to use `ctx`:
   ```go
   if err != nil {
       return nil, errors.Wrap(ctx, err, "create genai client failed")
   }
   ```
   Remove the second `//nolint:contextcheck` comment.

2. **Update `CreateGeminiParser`** in `agent/gemini/pkg/factory/factory.go`

   Update the call to `parser.New` to pass `ctx`:
   ```go
   p, err := parser.New(ctx, apiKey, model)
   ```

3. **Update call sites** in `agent/gemini/main.go` and `agent/gemini/cmd/run-task/main.go`

   In both files, update the call to `CreateGeminiParser` (which already receives `ctx`) — no change needed to the factory call itself since `CreateGeminiParser` now passes `ctx` through.

   However, verify the call site in `main.go:94` passes `ctx`:
   ```go
   geminiParser, err := factory.CreateGeminiParser(ctx, apiKey, model)
   ```

4. **Add tests for parser constructor with context** in `agent/gemini/pkg/parser/parser_test.go`

   Add test cases:
   - `New` with cancelled context → client creation should respect cancellation
   - `New` with deadline exceeded → should return context error

   Use `context.WithCancel` and `context.WithTimeout` to create test contexts.

5. **Run tests**
   ```bash
   cd agent/gemini && make test
   ```

6. **Run precommit**
   ```bash
   cd agent/gemini && make precommit
   ```
</requirements>

<constraints>
- Only change files in `agent/gemini/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf`), context propagation
- Remove `//nolint:contextcheck` comments from the updated code since context is now properly passed
</constraints>

<verification>
cd agent/gemini && make precommit
</verification>
