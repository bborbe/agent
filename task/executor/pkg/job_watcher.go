// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"time"

	"github.com/bborbe/errors"
	libk8s "github.com/bborbe/k8s"
	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../mocks/job_watcher.go --fake-name FakeJobWatcher . JobWatcher

// JobWatcher watches batch/v1 Jobs in the executor's namespace and publishes
// synthetic failure results for terminal-state Jobs that belong to spawned tasks.
type JobWatcher interface {
	// Run starts the Job informer and blocks until ctx is cancelled.
	Run(ctx context.Context) error
	// HandleJob processes a single Job (invoked by the informer event handlers
	// and by unit tests directly, avoiding the need for a fake informer).
	HandleJob(ctx context.Context, job *batchv1.Job)
}

// NewJobWatcher creates a JobWatcher.
func NewJobWatcher(
	kubeClient kubernetes.Interface,
	namespace libk8s.Namespace,
	taskStore *TaskStore,
	publisher ResultPublisher,
) JobWatcher {
	return &jobWatcher{
		kubeClient: kubeClient,
		namespace:  namespace,
		taskStore:  taskStore,
		publisher:  publisher,
	}
}

type jobWatcher struct {
	kubeClient kubernetes.Interface
	namespace  libk8s.Namespace
	taskStore  *TaskStore
	publisher  ResultPublisher
}

func (w *jobWatcher) Run(ctx context.Context) error {
	factory := k8sinformers.NewSharedInformerFactoryWithOptions(
		w.kubeClient,
		5*time.Minute,
		k8sinformers.WithNamespace(string(w.namespace)),
		k8sinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = "agent.benjamin-borbe.de/task-id"
		}),
	)
	informer := factory.Batch().V1().Jobs().Informer()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			job, ok := obj.(*batchv1.Job)
			if !ok {
				return
			}
			w.HandleJob(ctx, job)
		},
		UpdateFunc: func(_, newObj interface{}) {
			job, ok := newObj.(*batchv1.Job)
			if !ok {
				return
			}
			w.HandleJob(ctx, job)
		},
	})
	if err != nil {
		return errors.Wrapf(ctx, err, "add job informer event handler")
	}

	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return errors.Errorf(ctx, "timed out waiting for job informer cache sync")
	}
	glog.V(2).Infof("job informer started in namespace %s", w.namespace)
	<-ctx.Done()
	return nil
}

func (w *jobWatcher) HandleJob(ctx context.Context, job *batchv1.Job) {
	taskIDStr, ok := job.Labels["agent.benjamin-borbe.de/task-id"]
	if !ok || taskIDStr == "" {
		return
	}
	taskID := lib.TaskIdentifier(taskIDStr)

	if isJobFailed(job) {
		reason := jobFailureReason(job)
		glog.V(2).Infof("job %s/%s failed (task %s): %s", job.Namespace, job.Name, taskID, reason)
		w.handleTerminal(ctx, taskID, job, reason, true)
		return
	}
	if isJobSucceeded(job) {
		glog.V(2).Infof("job %s/%s succeeded (task %s)", job.Namespace, job.Name, taskID)
		w.handleTerminal(ctx, taskID, job, "job completed without publishing result", false)
	}
}

// handleTerminal publishes a synthetic failure (when appropriate) and deletes the Job.
// alwaysPublish is true for Failed jobs; for Succeeded jobs it only publishes if the
// task is still in the TaskStore (agent has not yet published a result).
func (w *jobWatcher) handleTerminal(
	ctx context.Context,
	taskID lib.TaskIdentifier,
	job *batchv1.Job,
	reason string,
	alwaysPublish bool,
) {
	task, ok := w.taskStore.Load(taskID)
	if ok {
		w.publishSyntheticFailure(ctx, taskID, task, job, reason)
	} else {
		w.logMissingTask(taskID, job, alwaysPublish)
	}
	w.deleteJob(ctx, job)
}

func (w *jobWatcher) publishSyntheticFailure(
	ctx context.Context,
	taskID lib.TaskIdentifier,
	task lib.Task,
	job *batchv1.Job,
	reason string,
) {
	if err := w.publisher.PublishFailure(ctx, task, job.Name, reason); err != nil {
		glog.Errorf("publish synthetic failure for task %s (job %s): %v", taskID, job.Name, err)
	} else {
		glog.V(2).Infof("published synthetic failure for task %s (job %s)", taskID, job.Name)
	}
	w.taskStore.Delete(taskID)
}

func (w *jobWatcher) logMissingTask(
	taskID lib.TaskIdentifier,
	job *batchv1.Job,
	alwaysPublish bool,
) {
	if alwaysPublish {
		glog.Warningf(
			"task %s not in task store; job %s/%s failed but cannot publish synthetic failure (no original task content)",
			taskID,
			job.Namespace,
			job.Name,
		)
		return
	}
	glog.V(3).Infof(
		"task %s not in task store; job %s/%s succeeded — agent likely published result already",
		taskID, job.Namespace, job.Name,
	)
}

func (w *jobWatcher) deleteJob(ctx context.Context, job *batchv1.Job) {
	propagation := metav1.DeletePropagationBackground
	if err := w.kubeClient.BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil {
		glog.Warningf("delete job %s/%s failed: %v", job.Namespace, job.Name, err)
	} else {
		glog.V(2).Infof("deleted terminal job %s/%s", job.Namespace, job.Name)
	}
}

func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobSucceeded(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func jobFailureReason(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue && c.Message != "" {
			return c.Message
		}
	}
	return "unknown failure reason"
}
