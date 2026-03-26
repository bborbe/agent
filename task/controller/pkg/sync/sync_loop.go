// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sync

import (
	"context"

	"github.com/golang/glog"

	"github.com/bborbe/agent/task/controller/pkg/publisher"
	"github.com/bborbe/agent/task/controller/pkg/scanner"
)

//counterfeiter:generate -o ../../mocks/sync_loop.go --fake-name FakeSyncLoop . SyncLoop

// SyncLoop orchestrates scanning and publishing of task events.
type SyncLoop interface {
	Run(ctx context.Context) error
}

// NewSyncLoop creates a SyncLoop that connects scanner results to publisher calls.
func NewSyncLoop(
	scanner scanner.VaultScanner,
	publisher publisher.TaskPublisher,
) SyncLoop {
	return &syncLoop{
		scanner:   scanner,
		publisher: publisher,
	}
}

type syncLoop struct {
	scanner   scanner.VaultScanner
	publisher publisher.TaskPublisher
}

func (s *syncLoop) Run(ctx context.Context) error {
	go func() {
		if err := s.scanner.Run(ctx); err != nil {
			glog.Warningf("scanner run failed: %v", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-s.scanner.Results():
			for _, task := range result.Changed {
				if err := s.publisher.PublishChanged(ctx, task); err != nil {
					glog.Warningf("publish changed task failed: %v", err)
				}
			}
			for _, id := range result.Deleted {
				if err := s.publisher.PublishDeleted(ctx, id); err != nil {
					glog.Warningf("publish deleted task failed: %v", err)
				}
			}
		}
	}
}
