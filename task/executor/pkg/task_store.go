// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"sync"

	lib "github.com/bborbe/agent/lib"
)

// TaskStore is a thread-safe map from TaskIdentifier to Task.
// It is populated when the executor spawns a Job and consumed by the job
// informer when publishing synthetic failure results.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[lib.TaskIdentifier]lib.Task
}

// NewTaskStore creates an empty TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{tasks: make(map[lib.TaskIdentifier]lib.Task)}
}

// Store saves the task for the given identifier (called on job spawn).
func (s *TaskStore) Store(id lib.TaskIdentifier, task lib.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[id] = task
}

// Load retrieves the task for the given identifier.
func (s *TaskStore) Load(id lib.TaskIdentifier) (lib.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// Delete removes the task for the given identifier (called on job termination).
func (s *TaskStore) Delete(id lib.TaskIdentifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
}
