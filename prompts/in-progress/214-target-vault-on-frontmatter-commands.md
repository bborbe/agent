---
status: failed
execution_id: agent-target-vault-echo-exec-214-target-vault-on-frontmatter-commands
dark-factory-version: dev
created: "2026-09-03T15:26:04Z"
queued: "2026-09-03T15:26:04Z"
started: "2026-09-03T15:26:21Z"
completed: "2026-09-03T15:31:34Z"
lastFailReason: 'validate completion report: completion report status: partial'
---
# Route frontmatter commands by target vault

---
status: draft
---

<summary>
- Frontmatter update and increment commands can name the vault they belong to
- A consumer that serves a different vault can skip such commands before doing any vault work
- Senders can fill in a default vault automatically, the way the complete command already does
- Legacy commands without the field keep working exactly as before
- The new field survives the Kafka round-trip and validates like the existing vault fields
</summary>

<objective>
Add an optional TargetVault routing field to UpdateFrontmatterCommand and IncrementFrontmatterCommand, mirroring CompleteCommand, so cross-vault consumers can skip commands that are not theirs. This ends the "scan-and-drop on the wrong controller" noise where a controller spends its full lookup-retry budget on a task file that lives in another vault.
</objective>

<context>
Read CLAUDE.md for project conventions.

Pattern references (read before writing):
- command/task/complete-command.go — TargetVault field shape + slug validation via validateCreateTargetVault
- command/task/complete-command-sender.go — constructor defaultVault substitution at SendCommand time (substitute only when cmd value empty; invalid defaultVault surfaces as validation error on first send)
- command/task/update-frontmatter-command.go + command/task/update-frontmatter-command-sender.go — change these
- command/task/increment-frontmatter-command.go + command/task/increment-frontmatter-command-sender.go — change these

Consumer-side context (out of scope here): agent-task-controller's pkg/routing routes results by a target_vault frontmatter key; frontmatter commands currently have no routing key at all, which is why both vault controllers consume every cross-vault command.

No consumer of the new behavior is added in this prompt — no producer passes defaultVault yet; the consuming prompts land in agent-task-executor and agent-task-controller after this library release.
</context>

<requirements>
1. In UpdateFrontmatterCommand add a `TargetVault string` field with json tag `targetVault,omitempty`; validate it with the same slug rule create-command uses (validateCreateTargetVault), keeping it optional (empty is valid).
2. Same field + same validation on IncrementFrontmatterCommand.
3. Give UpdateFrontmatterCommandSender and IncrementFrontmatterCommandSender constructors a `defaultVault string` parameter; SendCommand substitutes it into cmd.TargetVault when cmd.TargetVault is empty and defaultVault is non-empty — byte-for-byte the complete-command-sender pattern, including the doc comment about first-send validation. Update the existing one-argument constructor calls in command/task/update-frontmatter-command-sender_test.go and command/task/increment-frontmatter-command-sender_test.go (BeforeEach) to pass "" as the second argument.
4. JSON round-trip test for both commands: marshal + unmarshal with targetVault set (field survives) and without (omitempty keeps the key absent, so old consumers see an unchanged wire shape).
5. Validate table test for both commands: valid slug ("personal", "openclaw") accepted, empty accepted, invalid slugs ("Open Claw", "UPPER", "a_b", "-lead") rejected — same table shape as create-command's target-vault tests.
6. Sender substitution tests: empty cmd.TargetVault + defaultVault set → filled; cmd.TargetVault already set → left untouched; both empty → field absent from the published payload.
7. Add a CHANGELOG.md entry under `## Unreleased` for the new optional targetVault field and the new sender constructor parameter.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Wire compatibility: absent field must not change the bytes of existing messages (omitempty); do not touch the operation strings ("update-frontmatter", "increment-frontmatter")
- The field is a routing key, NOT frontmatter data — do not fold it into the Updates map
- Never run `go mod vendor`; use `-mod=mod` for any go test command that needs it
</constraints>

<verification>
Run `make precommit`; must pass.
</verification>
