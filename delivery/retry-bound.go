// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"strconv"

	agentlib "github.com/bborbe/agent"
)

// retryCountKey is the frontmatter field holding the number of consecutive
// failed executions of this task.
//
// Deliberately NOT trigger_count. The executor writes trigger_count AFTER it
// spawns the Job, while the pod publishes frontmatter derived from its
// start-of-run snapshot — so the increment is reset by the very loop it counts,
// and it never accumulates. retry_count is written by this package, by the
// publisher, from the value the run itself read, and nothing else touches it,
// so the increment survives the next Job's snapshot.
const retryCountKey = "retry_count"

// nextRetryCount returns the retry count to record for a failed execution, and
// whether that count has reached the task's max_retries bound.
//
// The count is a consecutive-failure streak: incremented on every failed
// result and cleared by any forward-progress result (see resetRetryCount), so a
// task that fails twice and then succeeds starts over rather than accumulating
// toward the bound. exhausted reports count >= MaxRetries(), which is 3 when the
// task does not set max_retries.
func nextRetryCount(fm agentlib.TaskFrontmatter) (count int, exhausted bool) {
	count = fm.RetryCount() + 1
	return count, count >= fm.MaxRetries()
}

// escalateFailure takes the record out of the controller's claim predicate
// without making it terminal.
//
// The predicate selects on a trigger phase AND a non-empty assignee, so clearing
// the assignee stops the re-dispatch while status stays in_progress and the phase
// stays as the resume cursor: the record remains visible and non-terminal, and a
// manual re-assign resumes it from the same phase. previous_assignee records the
// pre-clear owner (spec 027). Caller has already written the retry count.
func escalateFailure(fm agentlib.TaskFrontmatter) {
	if prev := string(fm.Assignee()); prev != "" {
		fm["previous_assignee"] = prev
	}
	fm["assignee"] = ""
}

// resetRetryCount clears the failure streak after a forward-progress result.
// Conditional so a task that never failed is not stamped with retry_count: 0.
func resetRetryCount(fm agentlib.TaskFrontmatter) {
	if fm.RetryCount() != 0 {
		fm[retryCountKey] = 0
	}
}

// frontmatterOf parses content's frontmatter into a TaskFrontmatter map.
// Returns an empty map when content carries no frontmatter.
func frontmatterOf(content string) agentlib.TaskFrontmatter {
	fm, _ := ParseMarkdownFrontmatter(content)
	frontmatter := agentlib.TaskFrontmatter{}
	for k, v := range fm {
		frontmatter[k] = v
	}
	return frontmatter
}

// writeRetryCount records count in content's frontmatter as an integer.
func writeRetryCount(content string, count int) string {
	return SetFrontmatterField(content, retryCountKey, strconv.Itoa(count))
}

// applyFailureBound is the content-path twin of the failed branch in
// applyResultFrontmatter: it records the incremented retry count and, once the
// retry bound (or the pre-existing trigger cap) is reached, escalates the record
// out of the claim predicate. Status and phase are left to the caller.
func applyFailureBound(content string) string {
	fm := frontmatterOf(content)
	count, exhausted := nextRetryCount(fm)
	content = writeRetryCount(content, count)
	if exhausted || triggerCountAtCap(fm) {
		if prev := getFrontmatterField(content, "assignee"); prev != "" {
			content = SetFrontmatterField(content, "previous_assignee", prev)
		}
		content = SetFrontmatterField(content, "assignee", "")
	}
	return content
}

// clearFailureBound is the content-path twin of resetRetryCount: it clears the
// failure streak after a forward-progress result, and is a no-op when the field
// is absent or already zero.
func clearFailureBound(content string) string {
	if frontmatterOf(content).RetryCount() == 0 {
		return content
	}
	return writeRetryCount(content, 0)
}
