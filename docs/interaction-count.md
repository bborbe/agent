# Interaction Count

Two frontmatter keys on a published agent result record what a run cost — `metrics_agent_turns` in turns taken, `metrics_interaction_count` in human attention consumed. They count different things, are written by different writers under different rules, and must never be summed, averaged, or compared.

## metrics_agent_turns — the agent-side turn count

Every conversation turn the Claude session took, taken from the CLI's own end-of-run summary — the same summary that already supplies the token counts.

The agent writes this key into the frontmatter of every result it publishes. One payload carries one run's number: it is never summed across steps, phases, or runs.

A summary that reported no turn total, or a zero or negative total, publishes no key at all. Absent is not zero — an absent key is no measurement, not a measured zero.

Claude only. A provider without a Claude session publishes neither this key nor `metrics_interaction_count`.

## metrics_interaction_count — the human interaction count

The same key name, written by two sides under two different rules.

**Human side.** vault-cli's `work-on` / `complete` lifecycle writes the number of user-role turns in the task's recorded session logs, deduplicated per session id, tool results included. That rule and that lifecycle are unchanged.

**Agent side.** The run writes the key into its published result, counting the user-role entries in its own session transcript that the CLI records as human-authored. It is the same key by name only — the counting rules differ, and the divergence is recorded here rather than reconciled.

**Evidence, not assertion.** A recorded `0` means the transcript was read and held no human-authored entry; that zero *is* the unattended-delivery claim. The key is left absent — indeterminate, never zero — when the evidence is unavailable: no session id, or a transcript that cannot be located, read, or parsed. A run in that state still publishes its turn count.

**Never lowered.** When the task content the run received already records a count and the run's own evidence is smaller, nothing is published for that key and the recorded value stands.

**Snapshot.** A recorded count is a snapshot at write time. A zero means "nothing human at delivery", never "nothing human ever".

## The three quantities are not comparable

The agent-side turn count (`metrics_agent_turns`), the human-side interaction count (`metrics_interaction_count`), and the Prometheus turn counter (`agent_job_turns_total`) are three distinct quantities.

The two rules for `metrics_interaction_count` count different things: the human side counts all user-role turns with tool results included, the agent side counts only the human-authored user-role entries. They are **not comparable**, must not be summed, averaged, or reconciled, and neither may be inferred from the other's absence. Absence is a first-class outcome on both sides; substituting a zero for it manufactures a claim nobody observed.

## agent_job_turns_total — a third, unrelated quantity

`agent_job_turns_total` is a process-lifetime cumulative Prometheus counter, pushed to the PushGateway. It is not per task and it does not ride any payload. See [job-metrics.md](job-metrics.md) for the full metric reference.

It is never derived from either frontmatter key, and neither frontmatter key is ever derived from it.
