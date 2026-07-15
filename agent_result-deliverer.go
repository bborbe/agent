// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "context"

//counterfeiter:generate -o mocks/agent-result-deliverer.go --fake-name AgentResultDeliverer . ResultDeliverer

// ResultDeliverer publishes an agent step result back to the task controller.
//
// Implementations live in lib/delivery: NoopResultDeliverer (tests),
// FileResultDeliverer (local CLI), KafkaResultDeliverer (production K8s).
type ResultDeliverer interface {
	DeliverResult(ctx context.Context, result AgentResultInfo) error
}
