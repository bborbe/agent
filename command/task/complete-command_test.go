// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"encoding/json"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/command/task"
)

var _ = Describe("CompleteCommandOperation", func() {
	It("has expected string value", func() {
		Expect(task.CompleteCommandOperation).To(Equal(base.CommandOperation("complete-task")))
	})
})

var _ = Describe("CompleteCommand", func() {
	It("round-trips through JSON with recovery sha", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
			RecoverySHA:    "0123456789abcdef0123456789abcdef01234567",
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got task.CompleteCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.RecoverySHA).To(Equal(cmd.RecoverySHA))
	})

	It("omits recovery sha field when empty (omitempty)", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).NotTo(ContainSubstring(`"recoverySha"`))
	})

	It("validates a well-formed command", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
			RecoverySHA:    "0123456789abcdef0123456789abcdef01234567",
		}
		Expect(cmd.Validate(context.Background())).To(Succeed())
	})

	It("rejects an empty task identifier", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier(""),
			RecoverySHA:    "0123456789abcdef0123456789abcdef01234567",
		}
		Expect(cmd.Validate(context.Background())).NotTo(Succeed())
	})

	It("rejects a malformed recovery sha", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
			RecoverySHA:    "not-a-valid-sha",
		}
		Expect(cmd.Validate(context.Background())).NotTo(Succeed())
	})

	It("rejects a recovery sha with a non-hex character", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
			RecoverySHA:    "zzzz56789abcdef0123456789abcdef01234567",
		}
		Expect(cmd.Validate(context.Background())).NotTo(Succeed())
	})

	It("accepts an empty recovery sha", func() {
		cmd := task.CompleteCommand{
			TaskIdentifier: lib.TaskIdentifier("task-existing"),
		}
		Expect(cmd.Validate(context.Background())).To(Succeed())
	})
})
