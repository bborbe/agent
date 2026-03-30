// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
	"context"
	"errors"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/mocks"
	"github.com/bborbe/agent/task/controller/pkg/command"
)

var _ = Describe("NewTaskResultExecutor", func() {
	var (
		ctx        context.Context
		fakeWriter *mocks.FakeResultWriter
		executor   cdb.CommandObjectExecutorTx
		schemaID   cdb.SchemaID
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeWriter = &mocks.FakeResultWriter{}
		executor = command.NewTaskResultExecutor(fakeWriter)
		schemaID = cdb.SchemaID{
			Group:   "agent",
			Kind:    "task",
			Version: "v1",
		}
	})

	Describe("CommandOperation", func() {
		It("returns UpdateResult", func() {
			Expect(executor.CommandOperation()).To(Equal(base.CommandOperation("UpdateResult")))
		})
	})

	Describe("SendResultEnabled", func() {
		It("returns false", func() {
			Expect(executor.SendResultEnabled()).To(BeFalse())
		})
	})

	Describe("HandleCommand", func() {
		buildCommandObject := func(event base.Event) cdb.CommandObject {
			return cdb.CommandObject{
				Command: base.Command{
					RequestID: base.NewRequestID(),
					Operation: command.TaskResultCommandOperation,
					Initiator: "test-user",
					Data:      event,
				},
				SchemaID: schemaID,
			}
		}

		Context("valid command", func() {
			It("calls WriteResult once with correct TaskFile", func() {
				now := libtime.DateTime(time.Now())
				taskFile := lib.TaskFile{
					Object: base.Object[base.Identifier]{
						Identifier: base.Identifier("event-uuid-test"),
						Created:    now,
						Modified:   now,
					},
					TaskIdentifier: lib.TaskIdentifier("24 Tasks/test-task.md"),
					Frontmatter: lib.TaskFrontmatter{
						"status": "done",
					},
					Content: "## Result\n\nTask completed successfully.",
				}
				event, err := base.ParseEvent(ctx, taskFile)
				Expect(err).To(BeNil())

				cmdObj := buildCommandObject(event)
				fakeWriter.WriteResultReturns(nil)

				eventID, resultEvent, handleErr := executor.HandleCommand(ctx, nil, cmdObj)
				Expect(handleErr).To(BeNil())
				Expect(eventID).To(BeNil())
				Expect(resultEvent).To(BeNil())
				Expect(fakeWriter.WriteResultCallCount()).To(Equal(1))

				_, gotTaskFile := fakeWriter.WriteResultArgsForCall(0)
				Expect(gotTaskFile.TaskIdentifier).To(Equal(taskFile.TaskIdentifier))
				Expect(gotTaskFile.Content).To(Equal(taskFile.Content))
			})
		})

		Context("malformed JSON payload", func() {
			It("returns ErrCommandObjectSkipped and WriteResult is never called", func() {
				// A map containing a channel cannot be JSON-marshaled, triggering MarshalInto failure.
				event := base.Event{
					"channel": make(chan int),
				}
				cmdObj := buildCommandObject(event)

				eventID, resultEvent, handleErr := executor.HandleCommand(ctx, nil, cmdObj)
				Expect(errors.Is(handleErr, cdb.ErrCommandObjectSkipped)).To(BeTrue())
				Expect(eventID).To(BeNil())
				Expect(resultEvent).To(BeNil())
				Expect(fakeWriter.WriteResultCallCount()).To(Equal(0))
			})
		})

		Context("invalid request — empty task ID", func() {
			It("returns ErrCommandObjectSkipped and WriteResult is never called", func() {
				taskFile := lib.TaskFile{
					TaskIdentifier: lib.TaskIdentifier(""),
					Frontmatter:    lib.TaskFrontmatter{},
					Content:        "some content",
				}
				event, err := base.ParseEvent(ctx, taskFile)
				Expect(err).To(BeNil())

				cmdObj := buildCommandObject(event)

				eventID, resultEvent, handleErr := executor.HandleCommand(ctx, nil, cmdObj)
				Expect(errors.Is(handleErr, cdb.ErrCommandObjectSkipped)).To(BeTrue())
				Expect(eventID).To(BeNil())
				Expect(resultEvent).To(BeNil())
				Expect(fakeWriter.WriteResultCallCount()).To(Equal(0))
			})
		})

		Context("WriteResult returns error", func() {
			It("returns the error wrapped", func() {
				now := libtime.DateTime(time.Now())
				taskFile := lib.TaskFile{
					Object: base.Object[base.Identifier]{
						Identifier: base.Identifier("event-uuid-error"),
						Created:    now,
						Modified:   now,
					},
					TaskIdentifier: lib.TaskIdentifier("24 Tasks/error-task.md"),
					Frontmatter:    lib.TaskFrontmatter{},
					Content:        "content",
				}
				event, err := base.ParseEvent(ctx, taskFile)
				Expect(err).To(BeNil())

				cmdObj := buildCommandObject(event)
				fakeWriter.WriteResultReturns(errors.New("disk full"))

				_, _, handleErr := executor.HandleCommand(ctx, nil, cmdObj)
				Expect(handleErr).NotTo(BeNil())
				Expect(handleErr.Error()).To(ContainSubstring("write result for task"))
			})
		})
	})
})
