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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	lib "github.com/bborbe/agent/lib"
	agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
	pkg "github.com/bborbe/agent/task/executor/pkg"
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
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{"GEMINI_API_KEY": "test-gemini-key"},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			Expect(job.Name).To(Equal("claude-abc12345-20260403173500"))
			Expect(job.Namespace).To(Equal("test-ns"))
			Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
			Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))

			Expect(job.Spec.TTLSecondsAfterFinished).NotTo(BeNil())
			Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(600)))

			Expect(job.Spec.Template.Labels).To(HaveKeyWithValue("app", "agent"))
			Expect(job.Spec.Template.Labels).To(HaveKey("component"))

			Expect(job.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal("docker"))

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

		It("sets agent.benjamin-borbe.de/task-id label on spawned job", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("task-uuid-label-test"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{},
			}
			jobName, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(jobName).NotTo(BeEmpty())

			jobs, listErr := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(listErr).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))
			Expect(
				jobs.Items[0].Labels,
			).To(HaveKeyWithValue("agent.benjamin-borbe.de/task-id", string(task.TaskIdentifier)))
			Expect(
				jobs.Items[0].Spec.Template.Labels,
			).To(HaveKeyWithValue("agent.benjamin-borbe.de/task-id", string(task.TaskIdentifier)))
		})

		It("includes all per-agent env vars from config", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-multi-env"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "trade-analysis-agent",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "trade-analysis-agent",
				Image:    "registry/agent-trade-analysis:dev",
				Env: map[string]string{
					"ANTHROPIC_API_KEY": "test-anthropic-key",
					"EXTRA_VAR":         "extra-value",
				},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			envMap := make(map[string]string)
			for _, e := range container.Env {
				envMap[e.Name] = e.Value
			}
			Expect(envMap["ANTHROPIC_API_KEY"]).To(Equal("test-anthropic-key"))
			Expect(envMap["EXTRA_VAR"]).To(Equal("extra-value"))
		})

		It("uses assignee from frontmatter in job name", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abcdefghijklmnop"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "backtest-agent",
				},
			}
			_, err := jobSpawner.SpawnJob(
				ctx,
				task,
				pkg.AgentConfiguration{Image: "img:latest", Env: map[string]string{}},
			)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(Equal("backtest-agent-abcdefgh-20260403173500"))
		})

		It("falls back to 'agent' prefix when assignee is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc"),
				Frontmatter:    lib.TaskFrontmatter{},
			}
			_, err := jobSpawner.SpawnJob(
				ctx,
				task,
				pkg.AgentConfiguration{Image: "img:latest", Env: map[string]string{}},
			)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(HavePrefix("agent-"))
			Expect(jobs.Items[0].Name).To(Equal("agent-abc-20260403173500"))
		})

		It("returns job name when job already exists (AlreadyExists)", func() {
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-abc12345-20260403173500",
					Namespace: "test-ns",
				},
			}
			fakeClient = fake.NewClientset(existingJob)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				currentDateTime,
			)

			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			jobName, err := jobSpawner.SpawnJob(
				ctx,
				task,
				pkg.AgentConfiguration{Image: "img:latest", Env: map[string]string{}},
			)
			Expect(err).To(BeNil())
			Expect(jobName).To(Equal("claude-abc12345-20260403173500"))
		})

		It("mounts PVC when VolumeClaim is set", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-pvc"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee:        "claude",
				Image:           "my-image:latest",
				Env:             map[string]string{},
				VolumeClaim:     "agent-claude-pvc",
				VolumeMountPath: "/data",
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			Expect(job.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Volumes[0].Name).To(Equal("agent-data"))
			Expect(job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim).NotTo(BeNil())
			Expect(
				job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName,
			).To(Equal("agent-claude-pvc"))

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			Expect(container.VolumeMounts[0].Name).To(Equal("agent-data"))
			Expect(container.VolumeMounts[0].MountPath).To(Equal("/data"))
		})

		It("has no volumes when VolumeClaim is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-no-pvc"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			Expect(job.Spec.Template.Spec.Volumes).To(BeEmpty())
			Expect(job.Spec.Template.Spec.Containers[0].VolumeMounts).To(BeEmpty())
		})

		It("returns error when VolumeClaim is set but VolumeMountPath is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-bad-pvc"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee:    "claude",
				Image:       "my-image:latest",
				Env:         map[string]string{},
				VolumeClaim: "agent-claude-pvc",
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).NotTo(BeNil())
			Expect(
				err.Error(),
			).To(ContainSubstring("VolumeMountPath required when VolumeClaim is set"))
		})

		It("mounts secret as envFrom when SecretName is set", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-secret"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "backtest-agent",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee:   "backtest-agent",
				Image:      "my-image:latest",
				Env:        map[string]string{},
				SecretName: "agent-backtest",
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			Expect(container.EnvFrom).To(HaveLen(1))
			Expect(container.EnvFrom[0].SecretRef).NotTo(BeNil())
			Expect(container.EnvFrom[0].SecretRef.Name).To(Equal("agent-backtest"))
		})

		It("has no envFrom when SecretName is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-no-secret"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			Expect(container.EnvFrom).To(BeEmpty())
		})

		It("applies resource requests and limits from config.Resources", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-resources"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{},
				Resources: &agentv1.AgentResources{
					Requests: agentv1.AgentResourceList{
						CPU:              "500m",
						Memory:           "1Gi",
						EphemeralStorage: "2Gi",
					},
					Limits: agentv1.AgentResourceList{
						CPU:              "1",
						Memory:           "2Gi",
						EphemeralStorage: "4Gi",
					},
				},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			Expect(container.Resources.Requests.Cpu().String()).To(Equal("500m"))
			Expect(container.Resources.Limits.Cpu().String()).To(Equal("1"))
			Expect(container.Resources.Requests.Memory().String()).To(Equal("1Gi"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("2Gi"))
			Expect(container.Resources.Requests[corev1.ResourceEphemeralStorage]).
				To(Equal(resource.MustParse("2Gi")))
			Expect(container.Resources.Limits[corev1.ResourceEphemeralStorage]).
				To(Equal(resource.MustParse("4Gi")))
		})

		It("uses k8s builder defaults when Resources is nil", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-nil-resources"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee:  "claude",
				Image:     "my-image:latest",
				Env:       map[string]string{},
				Resources: nil,
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			Expect(container.Resources.Limits.Cpu().String()).To(Equal("50m"))
			Expect(container.Resources.Requests.Cpu().String()).To(Equal("20m"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("50Mi"))
			Expect(container.Resources.Requests.Memory().String()).To(Equal("20Mi"))
			_, hasEphReq := container.Resources.Requests[corev1.ResourceEphemeralStorage]
			Expect(hasEphReq).To(BeFalse())
			_, hasEphLim := container.Resources.Limits[corev1.ResourceEphemeralStorage]
			Expect(hasEphLim).To(BeFalse())
		})

		It("leaves CPU limit at builder default when only Requests.CPU is set", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc-one-sided"),
				Frontmatter: lib.TaskFrontmatter{
					"assignee": "claude",
				},
			}
			config := pkg.AgentConfiguration{
				Assignee: "claude",
				Image:    "my-image:latest",
				Env:      map[string]string{},
				Resources: &agentv1.AgentResources{
					Requests: agentv1.AgentResourceList{CPU: "500m"},
				},
			}
			_, err := jobSpawner.SpawnJob(ctx, task, config)
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			container := jobs.Items[0].Spec.Template.Spec.Containers[0]
			Expect(container.Resources.Requests.Cpu().String()).To(Equal("500m"))
			Expect(container.Resources.Limits.Cpu().String()).To(Equal("50m"))
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
			_, err := jobSpawner.SpawnJob(
				ctx,
				task,
				pkg.AgentConfiguration{Image: "img:latest", Env: map[string]string{}},
			)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("IsJobActive", func() {
		It("returns false when no jobs exist", func() {
			active, err := jobSpawner.IsJobActive(ctx, lib.TaskIdentifier("tid-1"))
			Expect(err).To(BeNil())
			Expect(active).To(BeFalse())
		})

		It("returns true when active job exists (status.active > 0)", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-20260403173500",
					Namespace: "test-ns",
					Labels:    map[string]string{"component": "tid-2"},
				},
				Status: batchv1.JobStatus{
					Active: 1,
				},
			}
			fakeClient = fake.NewClientset(job)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				currentDateTime,
			)

			active, err := jobSpawner.IsJobActive(ctx, lib.TaskIdentifier("tid-2"))
			Expect(err).To(BeNil())
			Expect(active).To(BeTrue())
		})

		It("returns false when completed job exists (status.succeeded > 0)", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-20260403173500",
					Namespace: "test-ns",
					Labels:    map[string]string{"component": "tid-3"},
				},
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			}
			fakeClient = fake.NewClientset(job)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				currentDateTime,
			)

			active, err := jobSpawner.IsJobActive(ctx, lib.TaskIdentifier("tid-3"))
			Expect(err).To(BeNil())
			Expect(active).To(BeFalse())
		})

		It("returns false when failed job exists (status.failed > 0, active == 0)", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-20260403173500",
					Namespace: "test-ns",
					Labels:    map[string]string{"component": "tid-4"},
				},
				Status: batchv1.JobStatus{
					Failed: 1,
					Active: 0,
				},
			}
			fakeClient = fake.NewClientset(job)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				currentDateTime,
			)

			active, err := jobSpawner.IsJobActive(ctx, lib.TaskIdentifier("tid-4"))
			Expect(err).To(BeNil())
			Expect(active).To(BeFalse())
		})

		It("returns true for newly created job (no status set yet)", func() {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claude-20260403173500",
					Namespace: "test-ns",
					Labels:    map[string]string{"component": "tid-5"},
				},
				Status: batchv1.JobStatus{},
			}
			fakeClient = fake.NewClientset(job)
			jobSpawner = spawner.NewJobSpawner(
				fakeClient,
				"test-ns",
				"kafka:9092",
				"develop",
				currentDateTime,
			)

			active, err := jobSpawner.IsJobActive(ctx, lib.TaskIdentifier("tid-5"))
			Expect(err).To(BeNil())
			Expect(active).To(BeTrue())
		})
	})
})
