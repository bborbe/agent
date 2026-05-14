// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics_test

import (
	"time"

	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	agentlib "github.com/bborbe/agent/lib"
	libmetrics "github.com/bborbe/agent/lib/metrics"
)

var _ = Describe("NewJobMetrics", func() {
	var (
		registry        *prometheus.Registry
		currentDateTime libtime.CurrentDateTime
		m               libmetrics.JobMetrics
	)

	BeforeEach(func() {
		registry = prometheus.NewRegistry()
		currentDateTime = libtime.NewCurrentDateTime()
		m = libmetrics.NewJobMetrics(registry, currentDateTime)
	})

	Context("collector registration", func() {
		It("registers the expected metric families on the registry", func() {
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(mfs))
			for _, mf := range mfs {
				names = append(names, mf.GetName())
			}
			Expect(names).To(ContainElements(
				"agent_job_run_total",
				"agent_job_duration_seconds",
			))
		})
	})

	Context("counter pre-initialization", func() {
		It("pre-initializes done at zero", func() {
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			var counterMF *dto.MetricFamily
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_run_total" {
					counterMF = mf
				}
			}
			Expect(counterMF).NotTo(BeNil(), "agent_job_run_total metric family not found")
			Expect(counterMF.Metric).To(HaveLen(3), "expected 3 pre-initialized label combinations")
		})

		It("pre-initialized counter values are zero before any RecordRun call", func() {
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_run_total" {
					for _, metric := range mf.Metric {
						Expect(metric.Counter.GetValue()).To(Equal(0.0))
					}
				}
			}
		})
	})

	Context("RecordRun", func() {
		var fixedTime time.Time

		BeforeEach(func() {
			fixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
			currentDateTime.SetNow(libtime.DateTime(fixedTime))
		})

		It("increments the run counter for the given status", func() {
			m.RecordRun(agentlib.AgentStatusDone)
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_run_total" {
					for _, metric := range mf.Metric {
						for _, lp := range metric.Label {
							if lp.GetName() == "status" && lp.GetValue() == "done" {
								Expect(metric.Counter.GetValue()).To(Equal(1.0))
							}
						}
					}
				}
			}
		})

		It("sets the gauge to the injected timestamp (Unix seconds)", func() {
			m.RecordRun(agentlib.AgentStatusDone)
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_last_run_timestamp_seconds" {
					for _, metric := range mf.Metric {
						for _, lp := range metric.Label {
							if lp.GetName() == "status" && lp.GetValue() == "done" {
								Expect(metric.Gauge.GetValue()).To(Equal(float64(fixedTime.Unix())))
							}
						}
					}
				}
			}
		})
	})

	Context("RecordDuration", func() {
		It("observes the histogram without error", func() {
			m.RecordDuration(5 * time.Second)
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			var histMF *dto.MetricFamily
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_duration_seconds" {
					histMF = mf
				}
			}
			Expect(histMF).NotTo(BeNil())
			Expect(histMF.Metric).To(HaveLen(1))
			Expect(histMF.Metric[0].Histogram.GetSampleCount()).To(Equal(uint64(1)))
		})

		It("observes the correct bucket (5s lands in the ≤5 bucket)", func() {
			m.RecordDuration(5 * time.Second)
			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			for _, mf := range mfs {
				if mf.GetName() == "agent_job_duration_seconds" {
					found := false
					for _, bucket := range mf.Metric[0].Histogram.Bucket {
						if bucket.GetUpperBound() == 5.0 {
							Expect(bucket.GetCumulativeCount()).To(Equal(uint64(1)))
							found = true
						}
					}
					Expect(found).To(BeTrue(), "bucket with upper bound 5.0 not found")
				}
			}
		})
	})

	Context("BuildJobMetricsName", func() {
		It("returns a stable job name string for claude-agent", func() {
			Expect(
				libmetrics.BuildJobMetricsName("claude-agent"),
			).To(Equal("agent_job_claude_agent"))
		})

		It("returns a stable job name string for code-agent", func() {
			Expect(libmetrics.BuildJobMetricsName("code-agent")).To(Equal("agent_job_code_agent"))
		})
	})
})
