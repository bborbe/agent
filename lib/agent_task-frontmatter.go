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
