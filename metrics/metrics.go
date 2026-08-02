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

// TokenType is the value of the type label on agent_job_tokens_total. The set is
// closed: no caller-supplied or session-supplied value ever becomes a label, so
// the family's cardinality is fixed at len(AvailableTokenTypes) series.
type TokenType string

// String returns the label value as a plain string.
func (t TokenType) String() string {
	return string(t)
}

const (
	// TokenTypeInput counts fresh (non-cached) input tokens.
	TokenTypeInput TokenType = "input"
	// TokenTypeOutput counts generated output tokens.
	TokenTypeOutput TokenType = "output"
	// TokenTypeCacheRead counts input tokens served from the prompt cache.
	TokenTypeCacheRead TokenType = "cache_read"
	// TokenTypeCacheCreation counts input tokens written into the prompt cache.
	TokenTypeCacheCreation TokenType = "cache_creation"
)

// AvailableTokenTypes is the closed set of token types. Iterating it is what
// guarantees every label combination is pre-initialized, so rate() evaluates to
// zero rather than no-data before the first job runs.
var AvailableTokenTypes = []TokenType{
	TokenTypeInput,
	TokenTypeOutput,
	TokenTypeCacheRead,
	TokenTypeCacheCreation,
}

// JobUsage is the LLM token and turn summary of one finished agent job.
// The zero value is valid and records nothing but zeros.
type JobUsage struct {
	// InputTokens is the count of fresh (non-cached) input tokens the job consumed.
	InputTokens int64
	// OutputTokens is the count of output tokens the job produced.
	OutputTokens int64
	// CacheReadTokens is the count of input tokens served from the prompt cache.
	CacheReadTokens int64
	// CacheCreationTokens is the count of input tokens written into the prompt cache.
	CacheCreationTokens int64
	// Turns is the number of conversation turns the job took.
	Turns int64
}

// JobMetrics records per-job Prometheus metrics at the result-publish boundary.
type JobMetrics interface {
	// RecordRun atomically increments the run counter and sets the last-run
	// gauge for the given status label. Both operations use the same label
	// value; they cannot drift.
	RecordRun(status agentlib.AgentStatus)
	// RecordDuration observes the run duration histogram.
	RecordDuration(d time.Duration)
	// RecordUsage records the token and turn summary of a finished job: each
	// token count advances its own type-labelled series and the turn count
	// advances the turn counter. A negative value is skipped for that counter
	// only; the other counters in the same call still record.
	RecordUsage(usage JobUsage)
}

// NewJobMetrics creates a JobMetrics that registers five collectors onto the
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
	tokenCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_job_tokens_total",
			Help: "Total LLM tokens consumed by agent jobs, by token type.",
		},
		[]string{"type"},
	)
	turnCounter := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "agent_job_turns_total",
			Help: "Total number of conversation turns taken by agent jobs.",
		},
	)
	registry.MustRegister(counter, gauge, histogram, tokenCounter, turnCounter)
	// Pre-initialize counter for all terminal statuses so absent() alerts work
	// even before any Job has run.
	counter.WithLabelValues(string(agentlib.AgentStatusDone)).Add(0)
	counter.WithLabelValues(string(agentlib.AgentStatusFailed)).Add(0)
	counter.WithLabelValues(string(agentlib.AgentStatusNeedsInput)).Add(0)
	// Pre-initialize the token series and the turn counter so rate() evaluates to
	// zero (not no-data) for a process that has not yet run a job.
	for _, tokenType := range AvailableTokenTypes {
		tokenCounter.WithLabelValues(tokenType.String()).Add(0)
	}
	turnCounter.Add(0)
	return &jobMetrics{
		counter:         counter,
		gauge:           gauge,
		histogram:       histogram,
		tokenCounter:    tokenCounter,
		turnCounter:     turnCounter,
		currentDateTime: currentDateTime,
	}
}

type jobMetrics struct {
	counter         *prometheus.CounterVec
	gauge           *prometheus.GaugeVec
	histogram       prometheus.Histogram
	tokenCounter    *prometheus.CounterVec
	turnCounter     prometheus.Counter
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

func (m *jobMetrics) RecordUsage(usage JobUsage) {
	m.addTokens(TokenTypeInput, usage.InputTokens)
	m.addTokens(TokenTypeOutput, usage.OutputTokens)
	m.addTokens(TokenTypeCacheRead, usage.CacheReadTokens)
	m.addTokens(TokenTypeCacheCreation, usage.CacheCreationTokens)
	if usage.Turns >= 0 {
		m.turnCounter.Add(float64(usage.Turns))
	}
}

// addTokens advances the token counter for one token type. A negative count is
// skipped: prometheus.Counter.Add panics on a negative delta, and the counts
// originate from a subprocess's stdout, so a hostile or buggy value must not be
// able to take the job down.
func (m *jobMetrics) addTokens(tokenType TokenType, count int64) {
	if count < 0 {
		return
	}
	m.tokenCounter.WithLabelValues(tokenType.String()).Add(float64(count))
}

// BuildJobMetricsName returns the standardized PushGateway job name for an
// agent job binary. All agent binaries must use this function to ensure the
// job name is consistent across deployments.
//
// Example: BuildJobMetricsName("claude-agent") → "agent_job_claude_agent"
func BuildJobMetricsName(agentName string) string {
	return bborbemetrics.BuildName("agent-job", agentName).String()
}
