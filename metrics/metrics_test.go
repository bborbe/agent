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

	agentlib "github.com/bborbe/agent"
	libmetrics "github.com/bborbe/agent/metrics"
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

	findFamily := func(name string) *dto.MetricFamily {
		mfs, err := registry.Gather()
		Expect(err).NotTo(HaveOccurred())
		for _, mf := range mfs {
			if mf.GetName() == name {
				return mf
			}
		}
		return nil
	}

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

	Context("RecordUsage", func() {
		Context("collector registration", func() {
			It("registers agent_job_tokens_total and agent_job_turns_total families", func() {
				m.RecordRun(agentlib.AgentStatusDone) // prime the gauge family
				mfs, err := registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				names := make([]string, 0, len(mfs))
				for _, mf := range mfs {
					names = append(names, mf.GetName())
				}
				Expect(names).To(ContainElements(
					"agent_job_tokens_total",
					"agent_job_turns_total",
				))
			})
		})

		Context("counter pre-initialization", func() {
			It("token counter has four pre-initialized series", func() {
				tokenMF := findFamily("agent_job_tokens_total")
				Expect(tokenMF).NotTo(BeNil(), "agent_job_tokens_total metric family not found")
				Expect(
					tokenMF.Metric,
				).To(HaveLen(4), "expected 4 pre-initialized label combinations")

				labelValues := make([]string, 0, 4)
				for _, metric := range tokenMF.Metric {
					for _, lp := range metric.Label {
						if lp.GetName() == "type" {
							labelValues = append(labelValues, lp.GetValue())
						}
					}
					Expect(metric.Counter.GetValue()).To(Equal(0.0))
				}
				Expect(labelValues).To(ConsistOf("input", "output", "cache_read", "cache_creation"))
			})

			It("turn counter is pre-initialized at zero", func() {
				turnMF := findFamily("agent_job_turns_total")
				Expect(turnMF).NotTo(BeNil(), "agent_job_turns_total metric family not found")
				Expect(turnMF.Metric).To(HaveLen(1))
				Expect(turnMF.Metric[0].Counter.GetValue()).To(Equal(0.0))
			})
		})

		Context("usage recording", func() {
			It("records distinct token values per kind and turn count", func() {
				m.RecordUsage(libmetrics.JobUsage{
					InputTokens:         11,
					OutputTokens:        22,
					CacheReadTokens:     33,
					CacheCreationTokens: 44,
					Turns:               5,
				})

				tokenMF := findFamily("agent_job_tokens_total")
				Expect(tokenMF).NotTo(BeNil())
				for _, metric := range tokenMF.Metric {
					for _, lp := range metric.Label {
						if lp.GetName() == "type" {
							switch lp.GetValue() {
							case "input":
								Expect(metric.Counter.GetValue()).To(Equal(11.0))
							case "output":
								Expect(metric.Counter.GetValue()).To(Equal(22.0))
							case "cache_read":
								Expect(metric.Counter.GetValue()).To(Equal(33.0))
							case "cache_creation":
								Expect(metric.Counter.GetValue()).To(Equal(44.0))
							}
						}
					}
				}

				turnMF := findFamily("agent_job_turns_total")
				Expect(turnMF).NotTo(BeNil())
				Expect(turnMF.Metric[0].Counter.GetValue()).To(Equal(5.0))
			})

			It("accumulates across multiple RecordUsage calls", func() {
				m.RecordUsage(libmetrics.JobUsage{
					InputTokens:         11,
					OutputTokens:        22,
					CacheReadTokens:     33,
					CacheCreationTokens: 44,
					Turns:               5,
				})
				m.RecordUsage(libmetrics.JobUsage{
					InputTokens:         11,
					OutputTokens:        22,
					CacheReadTokens:     33,
					CacheCreationTokens: 44,
					Turns:               5,
				})

				tokenMF := findFamily("agent_job_tokens_total")
				Expect(tokenMF).NotTo(BeNil())
				for _, metric := range tokenMF.Metric {
					for _, lp := range metric.Label {
						if lp.GetName() == "type" {
							switch lp.GetValue() {
							case "input":
								Expect(metric.Counter.GetValue()).To(Equal(22.0))
							case "output":
								Expect(metric.Counter.GetValue()).To(Equal(44.0))
							case "cache_read":
								Expect(metric.Counter.GetValue()).To(Equal(66.0))
							case "cache_creation":
								Expect(metric.Counter.GetValue()).To(Equal(88.0))
							}
						}
					}
				}

				turnMF := findFamily("agent_job_turns_total")
				Expect(turnMF).NotTo(BeNil())
				Expect(turnMF.Metric[0].Counter.GetValue()).To(Equal(10.0))
			})

			It("skips negative input token count without panic", func() {
				Expect(func() {
					m.RecordUsage(libmetrics.JobUsage{
						InputTokens:         -5,
						OutputTokens:        7,
						CacheReadTokens:     8,
						CacheCreationTokens: 9,
						Turns:               3,
					})
				}).NotTo(Panic())

				tokenMF := findFamily("agent_job_tokens_total")
				Expect(tokenMF).NotTo(BeNil())
				for _, metric := range tokenMF.Metric {
					for _, lp := range metric.Label {
						if lp.GetName() == "type" {
							switch lp.GetValue() {
							case "input":
								Expect(metric.Counter.GetValue()).To(Equal(0.0))
							case "output":
								Expect(metric.Counter.GetValue()).To(Equal(7.0))
							case "cache_read":
								Expect(metric.Counter.GetValue()).To(Equal(8.0))
							case "cache_creation":
								Expect(metric.Counter.GetValue()).To(Equal(9.0))
							}
						}
					}
				}

				turnMF := findFamily("agent_job_turns_total")
				Expect(turnMF).NotTo(BeNil())
				Expect(turnMF.Metric[0].Counter.GetValue()).To(Equal(3.0))
			})

			It("skips negative turns without panic while input records", func() {
				Expect(func() {
					m.RecordUsage(libmetrics.JobUsage{
						InputTokens: 4,
						Turns:       -1,
					})
				}).NotTo(Panic())

				tokenMF := findFamily("agent_job_tokens_total")
				Expect(tokenMF).NotTo(BeNil())
				for _, metric := range tokenMF.Metric {
					for _, lp := range metric.Label {
						if lp.GetName() == "type" && lp.GetValue() == "input" {
							Expect(metric.Counter.GetValue()).To(Equal(4.0))
						}
					}
				}

				turnMF := findFamily("agent_job_turns_total")
				Expect(turnMF).NotTo(BeNil())
				Expect(turnMF.Metric[0].Counter.GetValue()).To(Equal(0.0))
			})
		})

		Context("help string quality", func() {
			It("every registered family has a non-empty help string", func() {
				m.RecordRun(agentlib.AgentStatusDone) // prime gauge
				mfs, err := registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				for _, mf := range mfs {
					Expect(mf.GetHelp()).NotTo(BeEmpty())
				}
			})

			It("all five help strings are pairwise distinct", func() {
				m.RecordRun(agentlib.AgentStatusDone) // prime gauge
				mfs, err := registry.Gather()
				Expect(err).NotTo(HaveOccurred())
				helps := make([]string, 0, len(mfs))
				for _, mf := range mfs {
					helps = append(helps, mf.GetHelp())
				}
				// Deduplicate
				seen := make(map[string]bool)
				unique := make([]string, 0, len(helps))
				for _, h := range helps {
					if !seen[h] {
						seen[h] = true
						unique = append(unique, h)
					}
				}
				Expect(unique).To(HaveLen(len(helps)), "duplicate help strings found")
			})
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
