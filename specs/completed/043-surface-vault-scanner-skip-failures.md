---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-06-01T19:47:57Z"
generating: "2026-06-01T19:48:13Z"
prompted: "2026-06-01T20:23:00Z"
verifying: "2026-06-01T21:25:16Z"
completed: "2026-06-02T06:51:44Z"
branch: dark-factory/surface-vault-scanner-skip-failures
---

## Summary

- Today the controller's vault scanner silently skips task files with broken frontmatter (unresolved git merge markers, YAML parse errors, empty status). The skip emits a `glog.Warningf` log line and nothing else — no Prometheus counter, no alert, no operator-visible signal.
- 2026-05-31 / 2026-06-01 incident: 24 OpenClaw vault files carried unresolved `<<<<<<< HEAD` merge markers (root cause: obsidian-git autocommitting `git-rest` v0.20.1 `MarkerResolver` output). The scanner skipped all 24 across many sweep cycles. Four were `human_review` PR-review tasks waiting on operator decision. Discovery was manual log inspection in pod logs.
- The dedup safety net in the controller relies on the scanner reading every task file. Silently-skipped files break dedup: a watcher (`watcher/github-pr`, `watcher/github-release`) can re-trigger a task whose existing record lives in a skipped file, producing duplicates. Parked `human_review` tasks in skipped files are invisible to operator board filters and to all controller-side tooling.
- This spec makes scanner skips observable and alertable: a labelled Prometheus counter incremented at every skip site, with pre-initialised labels visible before the first skip, plus a log-level bump so the existing log-scrape alert routing surfaces the event.
- Scope is deliberately narrow: counter + log-level fix only. Auto-spawning a `human_review` task for the broken file is explicitly deferred to a follow-up spec to keep this change small enough to merge in one rung.

## Problem

The vault scanner is the controller's single source of truth for "what task files exist on disk". When the scanner skips a file, three downstream invariants break: (1) the dedup safety net misses the file, so watchers can re-create the task; (2) tasks in `human_review` phase that live in skipped files do not surface on the operator board's `assignee == ""` inbox filter; (3) the operator has no signal that anything was skipped — no counter to graph, no alert to fire, only a per-skip log line buried in pod stdout.

The 2026-05-31 / 2026-06-01 incident proved this is not theoretical. 24 task files were silently skipped across many sweep cycles for at least one day. Four held `human_review` PR-review tasks waiting on operator action. Discovery was accidental — an operator noticed the `glog.Warningf` lines in pod logs by hand. The vault doctrine page [[Make Parked Agent Tasks Visible to Operator]] is explicit: a task the operator cannot see is, for all operational purposes, lost. This spec restores that visibility for the skipped-file failure mode.

## Goal

After this change, the operator can answer "is the scanner currently skipping any task files, and if so why?" from Prometheus alone, without reading pod logs. Concretely:

- A counter named `agent_controller_vault_scanner_skipped_files_total` is registered, pre-initialised for every known skip reason, exposed at `/metrics`, and incremented exactly once per skip event with the appropriate `reason` label.
- The counter is non-zero exactly when the scanner has skipped at least one file since process start; its rate over time reflects ongoing skip activity, so a `rate(...) > 0` alert is a sound signal.
- Every skip site that increments the counter ALSO emits a log line at a severity that lands in the controller's existing log-scrape alert routing (i.e. the same severity as other operator-actionable failures in this package, not muted at `INFO`).
- The scanner's pre-skip behaviour is unchanged: skipping a broken file still allows the scan cycle to continue to the next file, no panic, no early return out of the cycle.

**Invariant established:** any new skip site added to `vault_scanner.go` in the future MUST increment the counter with a `reason` label declared in the metrics package, OR a regression test catches the missing increment. The counter is the contract.

## Non-goals

- Do NOT change git-rest's `MarkerResolver` behaviour. The deeper preventive fix (don't emit conflict markers in the first place) lives in a different repo with a different blast radius and is tracked separately by [[Fix unresolved merge conflicts in OpenClaw vault]].
- Do NOT migrate or clean up files already on disk. The 24 files from the 2026-05-31 incident are already resolved (`grep -rl '^<<<<<<<' ~/Documents/Obsidian/OpenClaw/` empty at spec time). No migration script.
- Do NOT install a pre-commit hook in the OpenClaw vault repo. That is an operator-side / vault-side fix, not Go code in this repo.
- Do NOT spawn a `human_review` task per broken file in this spec. That feature is desirable (it closes the operator-visibility loop end-to-end) but adds task-creation plumbing and idempotency state that materially expands scope. Tracked as a follow-up; keep this spec to counter + log level.
- Do NOT add a configuration knob, env var, or feature flag to disable the counter increment or the log-level bump. An escape hatch on the goal is itself a regression; if a future consumer demands variation, that's a separate spec.
- Do NOT change the scanner's constructor public name (`NewVaultScanner` / `NewGitRestVaultScanner`). Adding a `Metrics` dependency may require a constructor signature extension; the public function names stay.
- Do NOT introduce a per-file "skipped" hash entry. The existing `hashes` map is keyed on successfully-parsed files; this spec does not change that. A re-scan that encounters the same broken file SHOULD increment the counter again (rate-based alerts depend on this).
- Do NOT add a new scenario test. The behaviour is fully observable via Ginkgo unit tests (`prometheus.DefaultGatherer.Gather()` reads counter values) and a focused integration test for the log line. No Docker / cluster required.

## Desired Behavior

1. A counter `agent_controller_vault_scanner_skipped_files_total{reason}` is registered in `task/controller/pkg/metrics` via `promauto`, with a Help string naming the package and the operator-visible semantics.

2. The `reason` label has a closed set of values declared in the metrics package: `invalid_frontmatter`, `duplicate_frontmatter_invalid`, `empty_status`, `inject_task_identifier_failed`, `read_failed`. Every label value is pre-initialised to zero in the package `init()` so the counter is visible at `/metrics` before the first skip event.

3. Every existing skip site in `vault_scanner.go` increments the counter exactly once per skipped file per scan cycle. Sites anchored by their log-string prefix (not line number — line numbers drift between spec and impl; the log strings are stable contracts):
   - `processFile`, log `failed to read %s` → `reason: read_failed`.
   - `processFile`, log `skipping %s: invalid frontmatter: %v` at the `extractFrontmatter` failure site → `reason: invalid_frontmatter`.
   - `processFile`, log `skipping %s: invalid frontmatter: %v` at the `DeduplicateFrontmatter` failure site → `reason: duplicate_frontmatter_invalid`.
   - `processFile`, log `skipping %s: invalid frontmatter: %v` at the `yaml.Unmarshal` failure site → `reason: invalid_frontmatter`.
   - `processFile`, log `skipping %s: invalid frontmatter: status is empty` → `reason: empty_status`.
   - `injectAndStore`, log `skipping %s: failed to inject task_identifier: %v` → `reason: inject_task_identifier_failed`.

4. The three `glog.Warningf("skipping %s: invalid frontmatter: …")` call sites and the `glog.Warningf("skipping %s: status is empty")` call site are promoted to `glog.Errorf` (not `Warningf`). Rationale: these are operator-actionable broken-file events that need to surface in the existing controller log-scrape; `Warningf` is also used elsewhere in the package for transient / self-recovering conditions (git-pull failure, list failure) where re-noise on every cycle is acceptable. Promoting only the skip sites preserves the existing noise/signal split. The `read_failed`, `inject_task_identifier_failed`, and `duplicate_frontmatter_invalid` sites stay at their current log levels; the counter alone is sufficient for those (they are rare and already actionable via the counter).

5. The scanner's externally-visible scan behaviour is preserved: a broken file is skipped, the scan cycle continues to the next file, no panic, no early return, the file does NOT appear in `Changed` or `Deleted`, and the file's hash is NOT recorded in the `hashes` map (so the next scan cycle will re-encounter it and re-increment the counter — by design).

6. The metrics package's `Metrics` interface gains a method that returns the counter for a given `reason`. The scanner constructor accepts the `Metrics` interface as a new dependency (signature change to `NewVaultScanner` / `NewGitRestVaultScanner`); `main.go` passes the existing `metrics.New()` singleton. Tests use the existing counterfeiter fake or a real `metrics.New()` instance.

## Constraints

**Must not change:**

- The `VaultScanner` interface (`Run`, `RunCycle`). Public method signatures unchanged.
- The `ScanResult` struct shape. Counter is a side-effect, not a result-channel payload.
- The `hashes` map keying and contents. Broken files stay un-keyed.
- The existing per-cycle log lines for git pull, list, and commit/push failures. Only the four skip sites named in Desired Behavior #4 change log level.
- Existing metrics: `agent_controller_scan_cycles_total`, `tasks_published_total`, `results_written_total`, `git_push_total`, `conflict_resolutions_total`, `frontmatter_commands_total`, `git_rest_calls_total`, `kafka_consume_paused_total`. None renamed, none re-labelled.
- The atomic-write / single-writer git invariant (specs 006, 015).
- The executor / deliverer / result-writer code paths. This spec is scoped to `task/controller/pkg/scanner` and `task/controller/pkg/metrics`.

**Must not regress:**

- Existing `vault_scanner_test.go` Contexts (broken-file skip continues scan; valid files in the same sweep are still published).
- Existing `metrics_test.go` registration and pre-initialisation assertions for the seven existing metrics.
- The 2026-04-24 cap-stickiness path (spec 015) and the 2026-05-25 `human_review` guard (specs 039, 041, 042). These live in `pkg/result`, untouched.
- `make precommit` in `task/controller`.

**Relevant docs:**

- `docs/controller-design.md` — scanner section (if it documents the skip behaviour, append a line naming the counter and the log-level expectation).
- `docs/dod.md` — verification rung for "observable failures" (if it lists the existing controller counters, add the new one).
- [[Make Parked Agent Tasks Visible to Operator]] — vault doctrine that this spec advances.
- [[Fix unresolved merge conflicts in OpenClaw vault]] — sibling Obsidian task; this spec is the systematic-prevention half of its DoD.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility | Concurrency |
|---|---|---|---|---|---|
| Vault file contains unresolved `<<<<<<<` merge marker inside frontmatter | `extractFrontmatter` returns a YAML parse error. `processFile` increments `skipped_files_total{reason="invalid_frontmatter"}`, emits `glog.Errorf("skipping %s: invalid frontmatter: %v", relPath, err)`, returns `nil, "", false`. Scan cycle continues to the next file. | Operator fixes the file in the vault (resolve the merge). Next scan picks it up normally. | Counter rate > 0 on the `invalid_frontmatter` label; error log line in pod stdout. | Fully reversible — fix the file. | Counter is `promauto` thread-safe; multiple scan cycles increment independently. |
| Vault file has duplicate top-level frontmatter keys that `DeduplicateFrontmatter` cannot reconcile | Counter increments `reason="duplicate_frontmatter_invalid"`, existing log line preserved (no level change for this site per Desired Behavior #4), scan continues. | Operator hand-edits the file. | Counter rate > 0 on `duplicate_frontmatter_invalid`. | Reversible. | Unchanged. |
| Vault file parses successfully but has empty `status` field | Counter increments `reason="empty_status"`, log line promoted to `glog.Errorf`, scan continues. The file's hash is NOT stored (current behaviour preserved). | Operator adds a status. | Counter rate > 0 on `empty_status`; error log line. | Reversible. | Unchanged. |
| Vault file unreadable (permission, missing, IO error) | Counter increments `reason="read_failed"`, existing `glog.Warningf` log line preserved (no level change), scan continues. | Underlying filesystem / git-rest fix. | Counter rate > 0 on `read_failed`. | Reversible once IO recovers. | Unchanged. |
| `InjectTaskIdentifier` fails for a file with no existing `task_identifier` | Counter increments `reason="inject_task_identifier_failed"`, existing log line preserved, scan continues. The file stays without an identifier; the next scan will retry. | Investigate the inject failure (frontmatter shape issue). | Counter rate > 0 on `inject_task_identifier_failed`. | Reversible. | Unchanged. |
| The same broken file is present across N consecutive scan cycles | The counter increments N times (once per cycle the file is encountered). This is intentional — `rate(skipped_files_total[5m]) > 0` is the alert signal, and a stuck broken file should keep the alert active. | Operator fix is durable: as soon as the file is repaired, the next cycle does not increment, the rate decays, the alert clears. | Counter delta over time; rate-based alert. | Reversible. | Concurrent scan cycles in a restarted pod each increment independently; counters are per-process and aggregated server-side. |
| Controller pod restarts | Counter resets to zero (process-local Prometheus counters). Pre-initialised label values are restored by `init()`. The next skip in the new process increments from zero. | None — expected Prometheus semantics. | Counter visible at zero immediately after pod ready; non-zero after first skip in the new process. | N/A. | Per-process counters; Prometheus server handles process-restart resets. |
| New skip site added to `vault_scanner.go` without a counter increment | Caught by Acceptance Criterion AC#6 (source-grep over `processFile` and `injectAndStore` matching `glog.Errorf("skipping` or `glog.Warningf("skipping` or `return nil, "", false` paths, against the metrics increment count). | Add the increment, declare the new label in the metrics package, add a pre-init line. | Static check at CI time (the AC#6 grep). | N/A. | N/A. |

## Security / Abuse Cases

The counter exposes coarse-grained vault health (count of broken files by reason) on the controller's `/metrics` endpoint. No file paths, no frontmatter content, no task identifiers are exposed via the metric labels (the `reason` label is a closed enum). An attacker with `/metrics` read access learns "the scanner is skipping N files of type X", which is also visible in the existing pod logs — no incremental disclosure.

No new external inputs are introduced. The `reason` label values are compile-time constants in the metrics package; user-controlled frontmatter content never flows into a label value. (This rules out the prometheus-cardinality-explosion attack where a per-file label would let a malicious vault commit balloon the metric series count.)

The log-level promotion (`Warningf` → `Errorf`) does not change what is logged, only how it is classified. Log content is the same: relative path + parse error message. The relative path is operator-controlled vault content; no agent / model output is logged at the skip sites.

## Acceptance Criteria

- [ ] **Counter registered (AC#1).** The metric `agent_controller_vault_scanner_skipped_files_total` is registered in the default Prometheus registry. Evidence shape: `cd ~/Documents/workspaces/agent/task/controller && go test ./pkg/metrics/... -run 'registers all expected metric names'` passes after extending the existing test's `HaveKey` list to include the new name. The Ginkgo run output shows the new assertion green.

- [ ] **Counter pre-initialised for all reasons (AC#2).** The `reason` label has these five values pre-initialised to zero before the first skip event: `invalid_frontmatter`, `duplicate_frontmatter_invalid`, `empty_status`, `inject_task_identifier_failed`, `read_failed`. Evidence shape: a new Ginkgo `It` in `pkg/metrics/metrics_test.go` calls `prometheus.DefaultGatherer.Gather()` and asserts `gatherLabels(mfs, "agent_controller_vault_scanner_skipped_files_total", "reason")` contains all five values. Test green.

- [ ] **Each skip site increments exactly once (AC#3).** A new Ginkgo Context in `pkg/scanner/vault_scanner_test.go` for each of the five reasons feeds a synthetic broken file through `RunCycle` and asserts the counter delta is exactly 1 after one cycle, exactly 2 after two cycles (re-scan increment by design). Evidence shape: the test calls `prometheus.DefaultGatherer.Gather()` before and after each `RunCycle`, asserts the labelled counter increased by 1 each time. Five Contexts, all green.

- [ ] **Log level promoted on the four skip-site cases (AC#4).** Within the body of `processFile` only, the call sites currently emitting `glog.Warningf("skipping %s: invalid frontmatter` (three sites) and `glog.Warningf("skipping %s: status is empty"` (one site) now emit `glog.Errorf` with equivalent format strings. Evidence shape: `grep -nE 'glog\.(Warning|Error)f\("skipping' task/controller/pkg/scanner/vault_scanner.go` returns four lines all using `Errorf`; the count of `Warningf("skipping` matches in the file is zero. The `read_failed` site (`failed to read`) and `inject_task_identifier_failed` site (`failed to inject task_identifier`) remain `Warningf` (current behaviour preserved).

- [ ] **Scan continues past broken files (AC#5).** A regression test in `pkg/scanner/vault_scanner_test.go` writes a vault with two files: one broken (unresolved merge markers) and one valid. After `RunCycle`, the `Changed` slice contains the valid file's task, the counter has incremented once with `reason="invalid_frontmatter"`, and the broken file's hash is NOT in the scanner's internal `hashes` map (next cycle re-increments). Evidence shape: Ginkgo assertions on the result channel contents, the counter value, and a second-cycle counter delta.

- [ ] **Source-grep ensures every "skipping" site has a counter call adjacent (AC#6).** Within `task/controller/pkg/scanner/vault_scanner.go`, every line matching `glog.(Warningf|Errorf)\("skipping ` is followed within the same function body by a call to the metrics counter for that skip site. Evidence shape — run this exact command, expect the number of `skipping` matches to equal the number of `SkippedFilesTotal(` matches inside each function body:
  ```
  cd ~/Documents/workspaces/agent && \
    awk '/^func \(v \*vaultScanner\) (processFile|injectAndStore)\(/,/^}/' task/controller/pkg/scanner/vault_scanner.go | \
    grep -cE 'glog\.(Warning|Error)f\("skipping|failed to read'
  cd ~/Documents/workspaces/agent && \
    awk '/^func \(v \*vaultScanner\) (processFile|injectAndStore)\(/,/^}/' task/controller/pkg/scanner/vault_scanner.go | \
    grep -cE 'SkippedFilesTotal\('
  ```
  Both counts MUST equal 6 (one per skip site listed in Desired Behavior #3). The script's exit code is 0 when equal; a deliberately removed counter call in a test branch makes the counts diverge and the AC fails.

- [ ] **Existing metrics not regressed (AC#7).** All seven existing metrics in `metrics_test.go` remain registered and pre-initialised. Evidence shape: the full `pkg/metrics` test suite runs green; `gatherLabels` for each existing metric returns the same label set as before.

- [ ] **Constructor signature change reflected in `main.go` (AC#8).** `task/controller/main.go` line ~100 passes the existing `metrics.New()` singleton (or the equivalent constructed instance) into `NewGitRestVaultScanner`. Evidence shape: `grep -n 'NewGitRestVaultScanner\|NewVaultScanner' task/controller/main.go` shows the metrics argument in the call; `make precommit` in `task/controller` exits 0.

- [ ] **make precommit (AC#9).** Evidence shape: `cd ~/Documents/workspaces/agent/task/controller && make precommit` exits 0; final log line contains `ready to commit`.

- [ ] **CHANGELOG entry (AC#10).** `CHANGELOG.md` under `## Unreleased` contains a `feat(controller):` (or `fix(controller):` if the team treats invisible-skip as a bug) entry naming the new counter, referencing the 2026-05-31 / 2026-06-01 incident, and citing [[Make Parked Agent Tasks Visible to Operator]]. Evidence shape: `grep -A 3 'vault_scanner_skipped_files_total' CHANGELOG.md` returns the entry; the entry text mentions `2026-05-31` or `2026-06-01` and `Make Parked Agent Tasks Visible to Operator`.

- [ ] **Doc update (AC#11).** `docs/controller-design.md` (or the equivalent section that describes the scanner's broken-file handling) gains a sentence naming the counter and the alert-signal expectation (`rate(...) > 0` over a sliding window is the operator's signal). Evidence shape: `grep -n 'vault_scanner_skipped_files_total' docs/controller-design.md` returns at least one line.

- [ ] **No new scenario test (AC#12).** All verification is achievable via Ginkgo unit tests against `prometheus.DefaultGatherer.Gather()` and synthetic vault files on the local filesystem. No Docker, no `gh`, no live cluster required. Evidence shape: no new file under `scenarios/`; no Ginkgo `Describe` block added outside `task/controller/pkg/metrics/metrics_test.go` and `task/controller/pkg/scanner/vault_scanner_test.go`.

- [ ] **Post-Deploy (Rung-2) — Live verification on dev cluster (AC#13).** Operator-driven. After the merge commit is deployed to dev via `cd ~/Documents/workspaces/agent-dev && git pull && git merge master && cd task/controller && BRANCH=dev make buca`, the operator (a) confirms the controller pod image SHA in dev matches the `make buca` output for the merge commit; (b) hits `/metrics` on the dev controller pod and confirms `agent_controller_vault_scanner_skipped_files_total{reason="..."}` is present with all five pre-initialised labels at value `0`; (c) plants a single test file with unresolved merge markers in the dev OpenClaw vault, waits one scan cycle, confirms the `invalid_frontmatter` label is now `1` and a `glog.Errorf` line appears in `kubectlquant -n dev logs` for the scanner; (d) removes the test file. Evidence shape: `deploy_check:` field in the verification record names the controller pod image SHA in dev; `deploy_target: dev`; the verification result captures the `/metrics` snippet before-and-after and one `Errorf` log line.

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

Counter registration and pre-init audit:

```
cd ~/Documents/workspaces/agent/task/controller && go test ./pkg/metrics/... -v -run 'vault_scanner_skipped'
```

Per-reason increment audit:

```
cd ~/Documents/workspaces/agent/task/controller && go test ./pkg/scanner/... -v -run 'skipped_files_total'
```

Log-level source audit (expect four `Errorf("skipping` matches, zero `Warningf("skipping` matches):

```
cd ~/Documents/workspaces/agent && grep -cE 'glog\.Errorf\("skipping' task/controller/pkg/scanner/vault_scanner.go
cd ~/Documents/workspaces/agent && grep -cE 'glog\.Warningf\("skipping' task/controller/pkg/scanner/vault_scanner.go
```

Skip-site / counter-call adjacency audit (no `skipping` log without a counter call before the next `return` in the same function body):

```
cd ~/Documents/workspaces/agent && awk '/^func \(v \*vaultScanner\) (processFile|injectAndStore)\(/,/^}/' task/controller/pkg/scanner/vault_scanner.go | grep -nE 'skipping|SkippedFilesTotal'
```

Manual smoke on dev (post-deploy from `agent-dev` worktree per the project workflow):

1. Deploy the merge commit to dev: `cd ~/Documents/workspaces/agent-dev && git pull && git merge master && cd task/controller && BRANCH=dev make buca`.
2. Confirm the controller pod image SHA in dev matches the `make buca` output: `kubectlquant -n dev get pod -l app=task-controller -o jsonpath='{.items[0].spec.containers[0].image}'`.
3. Hit `/metrics` on the dev controller pod (via admin gateway or `kubectl logs`-equivalent) and confirm all five `reason` labels are present at value `0`.
4. Plant `~/Documents/Obsidian/OpenClaw/tasks/spec-NNN-broken-file-smoketest.md` with a frontmatter block containing `<<<<<<< HEAD`. Wait one scan poll-interval.
5. Re-fetch `/metrics`; confirm `reason="invalid_frontmatter"` is at `1`.
6. Confirm `kubectlquant -n dev logs` shows a `glog.Errorf` line referencing the planted file path.
7. Delete the planted file.

## Do-Nothing Option

Cost of leaving this unfixed:

- The 2026-05-31 / 2026-06-01 incident pattern recurs on every future operator action (or autocommit) that lands a broken frontmatter into the vault. Discovery remains accidental log inspection. Mean time to detect: hours-to-days.
- `human_review` tasks in skipped files are invisible to the operator board. Spec 039 / 041 / 042's `assignee == ""` inbox filter relies on the scanner reading every task — that contract is silently broken whenever a broken file is in scope.
- The watcher → controller dedup safety net can produce duplicate tasks when a watcher re-triggers a task whose existing record is in a skipped file. (Probability scales with skipped-file count × watcher event rate.)
- The doctrine page [[Make Parked Agent Tasks Visible to Operator]] is asserted as load-bearing in the vault but contradicted by the code. New contributors reading the doctrine will be misled.

Do-nothing is not viable: the incident is a recurring class (root cause is a third-party tool — obsidian-git autocommits — outside this codebase), and the operator-visibility loop is doctrine. The counter + log-level fix is the smallest change that restores observability; the deferred `human_review`-spawn extension is a follow-up that builds on this counter.

## References

- `task/controller/pkg/scanner/vault_scanner.go` — site of all five skip increments and four log-level promotions.
- `task/controller/pkg/metrics/metrics.go` — counter declaration, `Metrics` interface method, `init()` pre-init block.
- `task/controller/pkg/metrics/metrics_test.go` — registration assertion and pre-init label assertion.
- `task/controller/pkg/scanner/vault_scanner_test.go` — per-reason increment Contexts and the broken-vs-valid scan regression test.
- `task/controller/main.go` line ~100 — constructor call site that passes the `Metrics` dependency.
- `docs/controller-design.md` — scanner section to update with the counter name and alert signal.
- `CHANGELOG.md` — Unreleased entry.
- [[Make Parked Agent Tasks Visible to Operator]] — vault doctrine page advanced by this spec.
- [[Fix unresolved merge conflicts in OpenClaw vault]] — sibling Obsidian task; systematic-prevention half of its DoD.
- `specs/completed/029-per-agent-job-metrics-package.md` — precedent for the `Metrics` interface + counterfeiter fake pattern.
- `specs/completed/039-controller-stop-setting-human-review-on-failure.md`, `041-spawn-notification-early-return-skips-human-review-guard.md`, `042-update-frontmatter-executor-enforces-human-review-doctrine.md` — `assignee == ""` operator-inbox-park signal whose load-bearing dependency on the scanner reading every task this spec restores.
