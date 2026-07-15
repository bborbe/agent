// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"time"

	bborbemetrics "github.com/bborbe/metrics"
	libtime "github.com/bborbe/time"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
)

//counterfeiter:generate -o mocks/job-metrics.go --fake-name JobMetrics . JobMetrics

// JobMetrics records per-job Prometheus metrics at the result-publish boundary.
type JobMetrics interface {
	// RecordRun atomically increments the run counter and sets the last-run
	// gauge for the given status label. Both operations use the same label
	// value; they cannot drift.
	RecordRun(status agentlib.AgentStatus)
	// RecordDuration observes the run duration histogram.
	RecordDuration(d time.Duration)
}

// NewJobMetrics creates a JobMetrics that registers three collectors onto the
// caller-owned registry. The caller must NOT pass nil for registry.
// Registration failures (e.g. duplicate registration) panic — they are
// programmer errors caught at startup.
func NewJobMetrics(
	registry *prometheus.Registry,
	currentDateTime libtime.CurrentDateTime,
) JobMetrics {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_job_run_total",
			Help: "Total number of agent job runs by terminal status.",
		},
		[]string{"status"},
	)
	gauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "agent_job_last_run_timestamp_seconds",
			Help: "Unix timestamp (seconds) of the last agent job run, by terminal status.",
		},
		[]string{"status"},
	)
	histogram := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "agent_job_duration_seconds",
			Help:    "Duration of agent job runs in seconds.",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800},
		},
	)
	registry.MustRegister(counter, gauge, histogram)
	// Pre-initialize counter for all terminal statuses so absent() alerts work
	// even before any Job has run.
	counter.WithLabelValues(string(agentlib.AgentStatusDone)).Add(0)
	counter.WithLabelValues(string(agentlib.AgentStatusFailed)).Add(0)
	counter.WithLabelValues(string(agentlib.AgentStatusNeedsInput)).Add(0)
	return &jobMetrics{
		counter:         counter,
		gauge:           gauge,
		histogram:       histogram,
		currentDateTime: currentDateTime,
	}
}

type jobMetrics struct {
	counter         *prometheus.CounterVec
	gauge           *prometheus.GaugeVec
	histogram       prometheus.Histogram
	currentDateTime libtime.CurrentDateTime
}

func (m *jobMetrics) RecordRun(status agentlib.AgentStatus) {
	s := string(status)
	m.counter.WithLabelValues(s).Inc()
	m.gauge.WithLabelValues(s).Set(float64(m.currentDateTime.Now().Unix()))
}

func (m *jobMetrics) RecordDuration(d time.Duration) {
	m.histogram.Observe(d.Seconds())
}

// BuildJobMetricsName returns the standardized PushGateway job name for an
// agent job binary. All agent binaries must use this function to ensure the
// job name is consistent across deployments.
//
// Example: BuildJobMetricsName("claude-agent") → "agent_job_claude_agent"
func BuildJobMetricsName(agentName string) string {
	return bborbemetrics.BuildName("agent-job", agentName).String()
}
