// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"net/http"
	"os"
	"time"

	boltkv "github.com/bborbe/boltkv"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	bolt "go.etcd.io/bbolt"

	"github.com/bborbe/agent/task/controller/pkg/factory"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/result"
	pkgsync "github.com/bborbe/agent/task/controller/pkg/sync"
)

const vaultLocalPath = "/data/vault"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN    string        `required:"true"  arg:"sentry-dsn"    env:"SENTRY_DSN"    usage:"SentryDSN"                                display:"length"`
	SentryProxy  string        `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"  usage:"Sentry Proxy"`
	Listen       string        `required:"true"  arg:"listen"        env:"LISTEN"        usage:"address to listen to"`
	GitURL       string        `required:"true"  arg:"git-url"       env:"GIT_URL"       usage:"vault git repository URL (SSH format)"`
	KafkaBrokers string        `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"comma-separated Kafka broker addresses"`
	Branch       base.Branch   `required:"true"  arg:"branch"        env:"BRANCH"        usage:"Kafka topic prefix branch (develop/live)"`
	GitBranch    string        `required:"false" arg:"git-branch"    env:"GIT_BRANCH"    usage:"git branch to track"                                       default:"main"`
	PollInterval time.Duration `required:"false" arg:"poll-interval" env:"POLL_INTERVAL" usage:"vault polling interval"                                    default:"60s"`
	TaskDir      string        `required:"false" arg:"task-dir"      env:"TASK_DIR"      usage:"task directory within vault"                               default:"24 Tasks"`
	DataDir      string        `required:"true"  arg:"data-dir"      env:"DATA_DIR"      usage:"directory for BoltDB offset storage"`
	NoSync       bool          `required:"false" arg:"no-sync"       env:"NO_SYNC"       usage:"disable BoltDB fsync (for testing only)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-task-controller started")

	gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch)
	if err := gitClient.EnsureCloned(ctx); err != nil {
		return errors.Wrapf(ctx, err, "ensure git clone")
	}

	syncProducer, err := libkafka.NewSyncProducer(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create kafka sync producer")
	}
	defer syncProducer.Close()

	eventObjectSender := cdb.NewEventObjectSender(
		libkafka.NewJSONSender(syncProducer, log.DefaultSamplerFactory),
		a.Branch,
		log.DefaultSamplerFactory,
	)

	syncLoop := factory.CreateSyncLoop(
		gitClient,
		a.TaskDir,
		a.PollInterval,
		eventObjectSender,
	)

	var boltOptions []boltkv.ChangeOptions
	if a.NoSync {
		boltOptions = append(boltOptions, func(opts *bolt.Options) {
			opts.NoSync = true
		})
	}
	db, err := boltkv.OpenDir(ctx, a.DataDir, boltOptions...)
	if err != nil {
		return errors.Wrapf(ctx, err, "open boltkv dir %s", a.DataDir)
	}
	defer db.Close()

	saramaClientProvider, err := libkafka.NewSaramaClientProviderByType(
		ctx,
		libkafka.SaramaClientProviderTypeReused,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create sarama client provider")
	}
	defer saramaClientProvider.Close()

	resultWriter := result.NewResultWriter(gitClient, a.TaskDir)
	commandConsumer := factory.CreateCommandConsumer(
		saramaClientProvider,
		syncProducer,
		db,
		a.Branch,
		resultWriter,
	)

	return service.Run(
		ctx,
		syncLoop.Run,
		commandConsumer,
		a.createHTTPServer(syncLoop),
	)
}

func (a *application) createHTTPServer(syncLoop pkgsync.SyncLoop) run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))
		router.Path("/trigger").HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			syncLoop.Trigger()
			glog.V(2).Infof("trigger fired via HTTP")
			_, _ = resp.Write([]byte("trigger fired"))
		})

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
