// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spawner

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/job_spawner.go --fake-name FakeJobSpawner . JobSpawner

// JobSpawner creates a K8s Job for a task.
type JobSpawner interface {
	SpawnJob(ctx context.Context, task lib.Task, image string) error
}

// NewJobSpawner creates a new JobSpawner backed by the K8s batch/v1 API.
func NewJobSpawner(
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	branch string,
) JobSpawner {
	return &jobSpawner{
		kubeClient:   kubeClient,
		namespace:    namespace,
		kafkaBrokers: kafkaBrokers,
		branch:       branch,
	}
}

type jobSpawner struct {
	kubeClient   kubernetes.Interface
	namespace    string
	kafkaBrokers string
	branch       string
}

func (s *jobSpawner) SpawnJob(ctx context.Context, task lib.Task, image string) error {
	jobName := jobNameFromTask(task.TaskIdentifier)
	backoffLimit := int32(0)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: s.namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: "docker"}},
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: image,
							Env: []corev1.EnvVar{
								{Name: "TASK_CONTENT", Value: string(task.Content)},
								{Name: "TASK_ID", Value: string(task.TaskIdentifier)},
								{Name: "KAFKA_BROKERS", Value: s.kafkaBrokers},
								{Name: "BRANCH", Value: s.branch},
							},
						},
					},
				},
			},
		},
	}

	_, err := s.kubeClient.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
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
		Infof("created job %s for task %s with image %s", jobName, task.TaskIdentifier, image)
	return nil
}

// jobNameFromTask returns the K8s Job name for a task: "agent-{first-8-chars-of-taskID}".
func jobNameFromTask(taskID lib.TaskIdentifier) string {
	id := string(taskID)
	if len(id) > 8 {
		id = id[:8]
	}
	return "agent-" + id
}
