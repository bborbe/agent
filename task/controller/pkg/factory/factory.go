// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/run"
	libtime "github.com/bborbe/time"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/command"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/publisher"
	"github.com/bborbe/agent/task/controller/pkg/result"
	"github.com/bborbe/agent/task/controller/pkg/scanner"
	pkgsync "github.com/bborbe/agent/task/controller/pkg/sync"
)

// CreateSyncLoop wires together a VaultScanner and TaskPublisher into a SyncLoop.
func CreateSyncLoop(
	gitClient gitclient.GitClient,
	taskDir string,
	pollInterval time.Duration,
	eventObjectSender cdb.EventObjectSender,
	currentDateTimeGetter libtime.CurrentDateTimeGetter,
) pkgsync.SyncLoop {
	trigger := make(chan struct{}, 1)
	return pkgsync.NewSyncLoop(
		scanner.NewVaultScanner(
			gitClient,
			taskDir,
			pollInterval,
			trigger,
		),
		publisher.NewTaskPublisher(
			eventObjectSender,
			lib.TaskV1SchemaID,
			currentDateTimeGetter,
		),
		trigger,
	)
}

// CreateCommandConsumer wires a CQRS command consumer for agent-task-v1-request.
func CreateCommandConsumer(
	saramaClientProvider libkafka.SaramaClientProvider,
	syncProducer libkafka.SyncProducer,
	db libkv.DB,
	branch base.Branch,
	resultWriter result.ResultWriter,
) run.Func {
	executor := command.NewTaskResultExecutor(resultWriter)
	return cdb.RunCommandConsumerTxDefault(
		saramaClientProvider,
		syncProducer,
		db,
		lib.TaskV1SchemaID,
		branch,
		true, // ignoreUnsupported: skip commands with unknown operations
		cdb.CommandObjectExecutorTxs{executor},
	)
}
