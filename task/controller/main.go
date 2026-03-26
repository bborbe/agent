// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bborbe/agent/task/controller/pkg/factory"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
)

const vaultLocalPath = "/data/vault"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN    string        `required:"true"  arg:"sentry-dsn"    env:"SENTRY_DSN"    usage:"SentryDSN"                              display:"length"`
	SentryProxy  string        `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"  usage:"Sentry Proxy"`
	Listen       string        `required:"true"  arg:"listen"        env:"LISTEN"        usage:"address to listen to"`
	GitURL       string        `required:"true"  arg:"git-url"       env:"GIT_URL"       usage:"vault git repository URL (SSH format)"`
	KafkaBrokers string        `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"comma-separated Kafka broker addresses"`
	GitBranch    string        `required:"false" arg:"git-branch"    env:"GIT_BRANCH"    usage:"git branch to track"                                     default:"main"`
	PollInterval time.Duration `required:"false" arg:"poll-interval" env:"POLL_INTERVAL" usage:"vault polling interval"                                  default:"60s"`
	TaskDir      string        `required:"false" arg:"task-dir"      env:"TASK_DIR"      usage:"task directory within vault"                             default:"24 Tasks"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-task-controller started")

	gitClient := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch)
	if err := gitClient.EnsureCloned(ctx); err != nil {
		return err
	}

	syncProducer, err := libkafka.NewSyncProducer(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return err
	}
	defer syncProducer.Close()

	eventObjectSender := cdb.NewEventObjectSender(
		libkafka.NewJSONSender(syncProducer, log.DefaultSamplerFactory),
		base.Branch(a.GitBranch),
		log.DefaultSamplerFactory,
	)

	syncLoop := factory.CreateSyncLoop(
		gitClient,
		a.TaskDir,
		a.PollInterval,
		eventObjectSender,
	)

	return service.Run(
		ctx,
		a.createHTTPServer(),
		syncLoop.Run,
	)
}

func (a *application) createHTTPServer() run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
