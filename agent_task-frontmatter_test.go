// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"time"

	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent"
)

var _ = Describe("TaskFrontmatter", func() {
	Describe("Status", func() {
		It("returns status value", func() {
			fm := lib.TaskFrontmatter{"status": "in_progress"}
			Expect(fm.Status()).To(Equal(domain.TaskStatusInProgress))
		})

		It("returns empty for missing status", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.Status()).To(Equal(domain.TaskStatus("")))
		})
	})

	Describe("Phase", func() {
		It("returns pointer to phase when present", func() {
			fm := lib.TaskFrontmatter{"phase": "planning"}
			Expect(fm.Phase()).NotTo(BeNil())
			Expect(*fm.Phase()).To(Equal(domain.TaskPhasePlanning))
		})

		It("returns nil for missing phase", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.Phase()).To(BeNil())
		})
	})

	Describe("Assignee", func() {
		It("returns TaskAssignee", func() {
			fm := lib.TaskFrontmatter{"assignee": "my-agent"}
			Expect(fm.Assignee()).To(Equal(lib.TaskAssignee("my-agent")))
		})
	})

	Describe("TaskType", func() {
		It("returns TaskType", func() {
			fm := lib.TaskFrontmatter{"task_type": "feature"}
			Expect(fm.TaskType()).To(Equal(lib.TaskType("feature")))
		})

		It("returns empty TaskType when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.TaskType()).To(Equal(lib.TaskType("")))
		})
	})

	Describe("Stage", func() {
		It("returns stage value", func() {
			fm := lib.TaskFrontmatter{"stage": "dev"}
			Expect(fm.Stage()).To(Equal("dev"))
		})

		It("defaults to prod when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.Stage()).To(Equal("prod"))
		})

		It("defaults to prod when empty", func() {
			fm := lib.TaskFrontmatter{"stage": ""}
			Expect(fm.Stage()).To(Equal("prod"))
		})
	})

	Describe("RetryCount", func() {
		It("returns int value", func() {
			fm := lib.TaskFrontmatter{"retry_count": 5}
			Expect(fm.RetryCount()).To(Equal(5))
		})

		It("handles float64 from YAML", func() {
			fm := lib.TaskFrontmatter{"retry_count": float64(3)}
			Expect(fm.RetryCount()).To(Equal(3))
		})

		It("returns 0 when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.RetryCount()).To(Equal(0))
		})
	})

	Describe("MaxRetries", func() {
		It("returns configured value", func() {
			fm := lib.TaskFrontmatter{"max_retries": 10}
			Expect(fm.MaxRetries()).To(Equal(10))
		})

		It("defaults to 3 when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.MaxRetries()).To(Equal(3))
		})
	})

	Describe("TriggerCount", func() {
		It("returns int value", func() {
			fm := lib.TaskFrontmatter{"trigger_count": 2}
			Expect(fm.TriggerCount()).To(Equal(2))
		})

		It("returns 0 when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.TriggerCount()).To(Equal(0))
		})
	})

	Describe("MaxTriggers", func() {
		It("returns configured value", func() {
			fm := lib.TaskFrontmatter{"max_triggers": 7}
			Expect(fm.MaxTriggers()).To(Equal(7))
		})

		It("defaults to 3 when missing", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.MaxTriggers()).To(Equal(3))
		})
	})

	Describe("SpawnNotification", func() {
		It("returns true when set", func() {
			fm := lib.TaskFrontmatter{"spawn_notification": true}
			Expect(fm.SpawnNotification()).To(BeTrue())
		})

		It("returns false when not set", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.SpawnNotification()).To(BeFalse())
		})
	})

	Describe("CurrentJob", func() {
		It("returns job name when set", func() {
			fm := lib.TaskFrontmatter{"current_job": "my-job-abc"}
			Expect(fm.CurrentJob()).To(Equal("my-job-abc"))
		})

		It("returns empty string when not set", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.CurrentJob()).To(Equal(""))
		})
	})

	Describe("JobStartedAt", func() {
		It("parses RFC3339 timestamp", func() {
			fm := lib.TaskFrontmatter{"job_started_at": "2024-01-15T10:30:00Z"}
			t, err := fm.JobStartedAt()
			Expect(err).To(BeNil())
			Expect(t.Year()).To(Equal(2024))
			Expect(t.Month()).To(Equal(time.January))
		})

		It("returns zero time when field absent", func() {
			fm := lib.TaskFrontmatter{}
			t, err := fm.JobStartedAt()
			Expect(err).To(BeNil())
			Expect(t).To(Equal(time.Time{}))
		})

		It("returns error for unparseable timestamp", func() {
			fm := lib.TaskFrontmatter{"job_started_at": "not-a-time"}
			_, err := fm.JobStartedAt()
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("String", func() {
		It("returns string value for existing key", func() {
			fm := lib.TaskFrontmatter{"custom": "value"}
			s, ok := fm.String("custom")
			Expect(ok).To(BeTrue())
			Expect(s).To(Equal("value"))
		})

		It("returns false for missing key", func() {
			fm := lib.TaskFrontmatter{}
			_, ok := fm.String("missing")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Int", func() {
		It("returns int value", func() {
			fm := lib.TaskFrontmatter{"count": 42}
			n, ok := fm.Int("count")
			Expect(ok).To(BeTrue())
			Expect(n).To(Equal(42))
		})

		It("handles float64 from YAML", func() {
			fm := lib.TaskFrontmatter{"count": float64(7)}
			n, ok := fm.Int("count")
			Expect(ok).To(BeTrue())
			Expect(n).To(Equal(7))
		})

		It("returns false for missing key", func() {
			fm := lib.TaskFrontmatter{}
			_, ok := fm.Int("missing")
			Expect(ok).To(BeFalse())
		})
	})
})
