// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"

	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	libtimetest "github.com/bborbe/time/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg"
)

var _ = Describe("ResultPublisher", func() {
	var (
		ctx             context.Context
		publisher       pkg.ResultPublisher
		currentDateTime libtime.CurrentDateTime
	)

	BeforeEach(func() {
		ctx = context.Background()
		currentDateTime = libtime.NewCurrentDateTime()
		currentDateTime.SetNow(libtimetest.ParseDateTime("2026-04-18T12:00:00Z"))
		publisher = pkg.NewResultPublisher(
			libkafka.NewSyncProducerNop(),
			base.Branch("prod"),
			currentDateTime,
		)
	})

	Describe("PublishSpawnNotification", func() {
		It("succeeds with valid task", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-1"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    "ai_review",
					"assignee": "claude",
				},
				Content: lib.TaskContent("do the work"),
			}
			err := publisher.PublishSpawnNotification(ctx, task, "claude-20260418120000")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("PublishFailure", func() {
		It("succeeds with valid task", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-2"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    "ai_review",
					"assignee": "claude",
				},
				Content: lib.TaskContent("do the work"),
			}
			err := publisher.PublishFailure(ctx, task, "claude-20260418120000", "pod OOM killed")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
