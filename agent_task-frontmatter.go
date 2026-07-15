// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"time"

	"github.com/bborbe/vault-cli/pkg/domain"
)

// TaskFrontmatter is a generic map of frontmatter key-value pairs.
// Serializable as JSON (Kafka) and YAML (vault file).
// Typed accessors provide type-safe access to well-known fields.
type TaskFrontmatter map[string]interface{}

func (f TaskFrontmatter) Status() domain.TaskStatus {
	v, _ := f["status"].(string)
	if canonical, ok := domain.NormalizeTaskStatus(v); ok {
		return canonical
	}
	return domain.TaskStatus(v)
}

func (f TaskFrontmatter) Phase() *domain.TaskPhase {
	v, ok := f["phase"].(string)
	if !ok || v == "" {
		return nil
	}
	if canonical, ok := domain.NormalizeTaskPhase(v); ok {
		return &canonical
	}
	p := domain.TaskPhase(v)
	return &p
}

func (f TaskFrontmatter) Assignee() TaskAssignee {
	v, _ := f["assignee"].(string)
	return TaskAssignee(v)
}

// TaskType returns the task_type frontmatter field as a typed TaskType.
// Returns TaskType("") when the field is absent or holds a non-string value.
func (f TaskFrontmatter) TaskType() TaskType {
	v, _ := f["task_type"].(string)
	return TaskType(v)
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

// TriggerCount returns the number of spawn-trigger events that have fired for this task.
// Returns 0 if the field is absent.
func (f TaskFrontmatter) TriggerCount() int {
	switch v := f["trigger_count"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

// MaxTriggers returns the maximum number of spawn-trigger events allowed for this task.
// Returns 3 if the field is absent, matching the default for max_retries.
func (f TaskFrontmatter) MaxTriggers() int {
	switch v := f["max_triggers"].(type) {
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

// JobStartedAt parses the job_started_at frontmatter field written by PublishSpawnNotification.
// Returns (time.Time{}, nil) when the field is absent — callers treat zero time as "grace elapsed".
// Returns (time.Time{}, err) when the field is present but unparseable.
func (f TaskFrontmatter) JobStartedAt() (time.Time, error) {
	v, _ := f["job_started_at"].(string)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// String reads a string field by key. ok is false when the key is absent
// or holds a non-string value. Generic accessor for ad-hoc fields without
// dedicated typed methods.
func (f TaskFrontmatter) String(key string) (string, bool) {
	v, ok := f[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Int reads an integer field by key, accepting both int (JSON-decoded) and
// float64 (YAML-decoded) underlying types. ok is false when the key is
// absent or holds a non-numeric value. Generic accessor for ad-hoc fields
// without dedicated typed methods.
func (f TaskFrontmatter) Int(key string) (int, bool) {
	v, ok := f[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}
