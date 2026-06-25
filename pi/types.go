// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

import (
	agentlib "github.com/bborbe/agent"
)

// AgentStatus mirrors the shared agent status type.
type AgentStatus = agentlib.AgentStatus

const (
	AgentStatusDone       = agentlib.AgentStatusDone
	AgentStatusFailed     = agentlib.AgentStatusFailed
	AgentStatusNeedsInput = agentlib.AgentStatusNeedsInput
)

// Result is the structured output from a Pi CLI run.
type Result struct {
	Result string `json:"result"`
}

func (r Result) GetResult() string { return r.Result }
