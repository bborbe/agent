// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spawner

import (
	"context"

	"github.com/bborbe/errors"
	k8s "github.com/bborbe/k8s"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	lib "github.com/bborbe/agent/lib"
	pkg "github.com/bborbe/agent/task/executor/pkg"
)

const componentLabelKey = "component"

//counterfeiter:generate -o ../../mocks/job_spawner.go --fake-name FakeJobSpawner . JobSpawner

// JobSpawner creates a K8s Job for a task.
type JobSpawner interface {
	SpawnJob(ctx context.Context, task lib.Task, config pkg.AgentConfiguration) error
	// IsJobActive returns true if an active (not completed/failed) K8s Job exists
	// for the given task identifier. Uses the `component` label set by SpawnJob.
	IsJobActive(ctx context.Context, taskIdentifier lib.TaskIdentifier) (bool, error)
}

// NewJobSpawner creates a new JobSpawner backed by the K8s batch/v1 API.
func NewJobSpawner(
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	branch string,
	currentDateTimeGetter libtime.CurrentDateTimeGetter,
) JobSpawner {
	return &jobSpawner{
		kubeClient:            kubeClient,
		namespace:             namespace,
		kafkaBrokers:          kafkaBrokers,
		branch:                branch,
		currentDateTimeGetter: currentDateTimeGetter,
	}
}

type jobSpawner struct {
	kubeClient            kubernetes.Interface
	namespace             string
	kafkaBrokers          string
	branch                string
	currentDateTimeGetter libtime.CurrentDateTimeGetter
}

func (s *jobSpawner) SpawnJob(
	ctx context.Context,
	task lib.Task,
	config pkg.AgentConfiguration,
) error {
	assignee := task.Frontmatter.Assignee().String()
	now := s.currentDateTimeGetter.Now()
	jobName := jobNameFromTask(assignee, now)

	envBuilder := k8s.NewEnvBuilder()
	envBuilder.Add("TASK_CONTENT", string(task.Content))
	envBuilder.Add("TASK_ID", string(task.TaskIdentifier))
	envBuilder.Add("KAFKA_BROKERS", s.kafkaBrokers)
	envBuilder.Add("BRANCH", s.branch)
	for key, value := range config.Env {
		envBuilder.Add(key, value)
	}

	containerBuilder := k8s.NewContainerBuilder()
	containerBuilder.SetName(k8s.Name("agent"))
	containerBuilder.SetImage(config.Image)
	containerBuilder.SetEnvBuilder(envBuilder)

	containersBuilder := k8s.NewContainersBuilder()
	containersBuilder.SetContainerBuilders([]k8s.HasBuildContainer{containerBuilder})

	podSpecBuilder := k8s.NewPodSpecBuilder()
	podSpecBuilder.SetContainersBuilder(containersBuilder)
	podSpecBuilder.SetRestartPolicy(corev1.RestartPolicyNever)
	podSpecBuilder.SetImagePullSecrets([]string{"docker"})

	objectMetaBuilder := k8s.NewObjectMetaBuilder()
	objectMetaBuilder.SetName(k8s.Name(jobName))
	objectMetaBuilder.SetNamespace(k8s.Namespace(s.namespace))

	jobBuilder := k8s.NewJobBuilder()
	jobBuilder.SetObjectMetaBuild(objectMetaBuilder)
	jobBuilder.SetPodSpecBuilder(podSpecBuilder)
	jobBuilder.SetBackoffLimit(0)
	jobBuilder.SetApp("agent")
	jobBuilder.SetComponent(string(task.TaskIdentifier))

	job, err := jobBuilder.Build(ctx)
	if err != nil {
		return errors.Wrapf(ctx, err, "build job for task %s", task.TaskIdentifier)
	}

	_, err = s.kubeClient.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			glog.V(2).
				Infof("job %s already exists for task %s, treating as success", jobName, task.TaskIdentifier)
			return nil
		}
		return errors.Wrapf(
			ctx,
			err,
			"create job %s for task %s failed",
			jobName,
			task.TaskIdentifier,
		)
	}
	glog.V(2).
		Infof("created job %s for task %s with image %s", jobName, task.TaskIdentifier, config.Image)
	return nil
}

func (s *jobSpawner) IsJobActive(
	ctx context.Context,
	taskIdentifier lib.TaskIdentifier,
) (bool, error) {
	labelSelector := componentLabelKey + "=" + string(taskIdentifier)
	jobs, err := s.kubeClient.BatchV1().Jobs(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return false, errors.Wrapf(ctx, err, "list jobs for task %s", taskIdentifier)
	}
	for _, job := range jobs.Items {
		if job.Status.Succeeded > 0 {
			continue
		}
		if job.Status.Failed > 0 && job.Status.Active == 0 {
			continue
		}
		return true, nil
	}
	return false, nil
}

// jobNameFromTask returns the K8s Job name for a task: "{assignee}-{YYYYMMDDHHMMSS}".
// If assignee is empty, "agent" is used as the default prefix.
// Job names are DNS-compliant (<=63 chars, [a-z0-9]([-a-z0-9]*[a-z0-9])?).
// Assignees should be short lowercase strings (e.g. "claude", "backtest-agent").
func jobNameFromTask(assignee string, now libtime.DateTime) string {
	if assignee == "" {
		assignee = "agent"
	}
	return assignee + "-" + now.UTC().Format("20060102150405")
}
