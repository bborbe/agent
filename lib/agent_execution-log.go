// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	libtime "github.com/bborbe/time"
)

// ExecutionLogEntry records a single run of an agent job.
type ExecutionLogEntry struct {
	Run       int              `json:"run"`
	Timestamp libtime.DateTime `json:"timestamp"`
	Message   string           `json:"message"`
}

// ExecutionLog is an ordered list of execution log entries.
type ExecutionLog []ExecutionLogEntry
