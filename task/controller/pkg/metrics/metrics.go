// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ScanCyclesTotal counts scan cycle completions by result.
var ScanCyclesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_controller_scan_cycles_total",
		Help: "Total number of scan cycles completed.",
	},
	[]string{"result"},
)

// TasksPublishedTotal counts task events published by type.
var TasksPublishedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_controller_tasks_published_total",
		Help: "Total number of task events published.",
	},
	[]string{"type"},
)

// ResultsWrittenTotal counts result write attempts by outcome.
var ResultsWrittenTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_controller_results_written_total",
		Help: "Total number of task result write attempts.",
	},
	[]string{"result"},
)

// GitPushTotal counts git push attempts by outcome.
var GitPushTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_controller_git_push_total",
		Help: "Total number of git push attempts.",
	},
	[]string{"result"},
)

// ConflictResolutionsTotal counts per-file conflict resolution attempts by outcome.
var ConflictResolutionsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_controller_conflict_resolutions_total",
		Help: "Total number of per-file conflict resolution attempts.",
	},
	[]string{"result"},
)

// FrontmatterCommandsTotal counts atomic frontmatter command executions
// by operation ("increment_frontmatter" | "update_frontmatter") and
// outcome ("success" | "error" | "not_found").
var FrontmatterCommandsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "agent_task_controller_frontmatter_commands_total",
		Help: "Total number of atomic frontmatter commands processed, by operation and outcome.",
	},
	[]string{"operation", "outcome"},
)

func init() {
	ScanCyclesTotal.WithLabelValues("changes").Add(0)
	ScanCyclesTotal.WithLabelValues("no_changes").Add(0)
	ScanCyclesTotal.WithLabelValues("error").Add(0)

	TasksPublishedTotal.WithLabelValues("changed").Add(0)
	TasksPublishedTotal.WithLabelValues("deleted").Add(0)

	ResultsWrittenTotal.WithLabelValues("success").Add(0)
	ResultsWrittenTotal.WithLabelValues("not_found").Add(0)
	ResultsWrittenTotal.WithLabelValues("error").Add(0)

	GitPushTotal.WithLabelValues("success").Add(0)
	GitPushTotal.WithLabelValues("retry_success").Add(0)
	GitPushTotal.WithLabelValues("conflict_resolved").Add(0)
	GitPushTotal.WithLabelValues("error").Add(0)

	ConflictResolutionsTotal.WithLabelValues("success").Add(0)
	ConflictResolutionsTotal.WithLabelValues("error").Add(0)

	for _, op := range []string{"increment_frontmatter", "update_frontmatter"} {
		for _, outcome := range []string{"success", "error", "not_found"} {
			FrontmatterCommandsTotal.WithLabelValues(op, outcome).Add(0)
		}
	}
}
