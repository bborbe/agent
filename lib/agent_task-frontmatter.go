// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"github.com/bborbe/vault-cli/pkg/domain"
)

// TaskFrontmatter is a generic map of frontmatter key-value pairs.
// Serializable as JSON (Kafka) and YAML (vault file).
// Typed accessors provide type-safe access to well-known fields.
type TaskFrontmatter map[string]interface{}

func (f TaskFrontmatter) Status() domain.TaskStatus {
	v, _ := f["status"].(string)
	return domain.TaskStatus(v)
}

func (f TaskFrontmatter) Phase() *domain.TaskPhase {
	v, ok := f["phase"].(string)
	if !ok || v == "" {
		return nil
	}
	p := domain.TaskPhase(v)
	return &p
}

func (f TaskFrontmatter) Assignee() TaskAssignee {
	v, _ := f["assignee"].(string)
	return TaskAssignee(v)
}

// Stage returns the execution stage from the "stage" key.
// Returns "prod" if the key is absent or empty.
func (f TaskFrontmatter) Stage() string {
	v, _ := f["stage"].(string)
	if v == "" {
		return "prod"
	}
	return v
}

// RetryCount returns the number of failed attempts recorded in frontmatter.
// Returns 0 when the field is absent.
func (f TaskFrontmatter) RetryCount() int {
	switch v := f["retry_count"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

// MaxRetries returns the maximum number of failures allowed before escalation.
// Returns 3 when the field is absent (spec default).
func (f TaskFrontmatter) MaxRetries() int {
	switch v := f["max_retries"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 3
	}
}

// SpawnNotification returns true when this result is a job-spawn tracking update
// rather than an agent outcome. The controller skips the retry counter for these.
func (f TaskFrontmatter) SpawnNotification() bool {
	v, _ := f["spawn_notification"].(bool)
	return v
}

// CurrentJob returns the K8s Job name recorded when the executor spawned a Job for this task.
// Returns an empty string when not set.
func (f TaskFrontmatter) CurrentJob() string {
	v, _ := f["current_job"].(string)
	return v
}
