// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import "context"

//counterfeiter:generate -o ../mocks/pkg-result-deliverer.go --fake-name PkgResultDeliverer . ResultDeliverer

// ResultDeliverer publishes an AgentResult back after task execution completes.
type ResultDeliverer interface {
	DeliverResult(ctx context.Context, result AgentResult) error
}

// NewNoopResultDeliverer creates a ResultDeliverer that does nothing.
func NewNoopResultDeliverer() ResultDeliverer {
	return &noopResultDeliverer{}
}

type noopResultDeliverer struct{}

func (n *noopResultDeliverer) DeliverResult(_ context.Context, _ AgentResult) error {
	return nil
}
