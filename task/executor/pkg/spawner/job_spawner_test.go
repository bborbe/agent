// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spawner_test

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"context"
	"fmt"
	"testing"

	libtime "github.com/bborbe/time"
	libtimetest "github.com/bborbe/time/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

func TestSpawner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spawner Suite")
}

var _ = Describe("JobSpawner", func() {
	var (
		ctx             context.Context
		fakeClient      *fake.Clientset
		jobSpawner      spawner.JobSpawner
		currentDateTime libtime.CurrentDateTime
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = fake.NewClientset()
		currentDateTime = libtime.NewCurrentDateTime()
		currentDateTime.SetNow(libtimetest.ParseDateTime("2026-04-03T17:35:00Z"))
		jobSpawner = spawner.NewJobSpawner(
			fakeClient,
			"test-ns",
			"kafka:9092",
			"develop",
			"test-gemini-key",
			currentDateTime,
		)
	})

	Describe("SpawnJob", func() {
		It("creates a job with correct name and env vars", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
				Content: lib.TaskContent("do the work"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "my-image:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			Expect(job.Name).To(Equal("claude-20260403173500"))
			Expect(job.Namespace).To(Equal("test-ns"))
			Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
			Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("my-image:latest"))

			envMap := make(map[string]string)
			for _, e := range container.Env {
				envMap[e.Name] = e.Value
			}
			Expect(envMap["TASK_CONTENT"]).To(Equal("do the work"))
			Expect(envMap["TASK_ID"]).To(Equal("abc12345-rest-ignored"))
			Expect(envMap["KAFKA_BROKERS"]).To(Equal("kafka:9092"))
			Expect(envMap["BRANCH"]).To(Equal("develop"))
			Expect(envMap["GEMINI_API_KEY"]).To(Equal("test-gemini-key"))
		})

		It("uses assignee from frontmatter in job name", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abcdefghijklmnop"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "backtest-agent",
				},
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(Equal("backtest-agent-20260403173500"))
		})

		It("falls back to 'agent' prefix when assignee is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc"),
				Frontmatter:    lib.TaskFrontmatter{},
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(HavePrefix("agent-"))
			Expect(jobs.Items[0].Name).To(Equal("agent-20260403173500"))
		})

		It("returns nil when job already exists (AlreadyExists)", func() {
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-20260403173500",
					Namespace: "test-ns",
				},
			}
			fakeClient = fake.NewClientset(existingJob)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				"test-gemini-key",
				currentDateTime,
			)

			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())
		})

		It("returns error on unexpected K8s error", func() {
			fakeClient.PrependReactor(
				"create",
				"jobs",
				func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, k8serrors.NewInternalError(fmt.Errorf("server error"))
				},
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).NotTo(BeNil())
		})
	})
})
