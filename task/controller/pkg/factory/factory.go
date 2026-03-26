// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"time"

	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"

	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/publisher"
	"github.com/bborbe/agent/task/controller/pkg/scanner"
	pkgsync "github.com/bborbe/agent/task/controller/pkg/sync"
)

// CreateSyncLoop wires together a VaultScanner and TaskPublisher into a SyncLoop.
func CreateSyncLoop(
	gitClient gitclient.GitClient,
	taskDir string,
	pollInterval time.Duration,
	syncProducer libkafka.SyncProducer,
	schemaID cdb.SchemaID,
	branch string,
) pkgsync.SyncLoop {
	vaultScanner := scanner.NewVaultScanner(gitClient, taskDir, pollInterval)
	taskPublisher := publisher.NewTaskPublisher(syncProducer, schemaID, branch)
	return pkgsync.NewSyncLoop(vaultScanner, taskPublisher)
}
