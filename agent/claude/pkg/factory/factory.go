// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/bborbe/agent/agent/claude/pkg"
)

// CreateTaskRunner wires a complete TaskRunner with ClaudeRunner,
// prompt assembly, and result delivery.
func CreateTaskRunner(
	claudeConfigDir pkg.ClaudeConfigDir,
	agentDir pkg.AgentDir,
	allowedTools pkg.AllowedTools,
	model pkg.ClaudeModel,
	env map[string]string,
	envContext map[string]string,
	instructions pkg.Instructions,
	deliverer pkg.ResultDeliverer,
) pkg.TaskRunner {
	return pkg.NewTaskRunner(
		pkg.NewClaudeRunner(pkg.ClaudeRunnerConfig{
			ClaudeConfigDir:  claudeConfigDir,
			AllowedTools:     allowedTools,
			Model:            model,
			WorkingDirectory: agentDir,
			Env:              env,
		}),
		instructions,
		envContext,
		deliverer,
	)
}
