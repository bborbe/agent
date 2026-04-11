// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	agentlib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/factory"
)

// agentConfigs maps assignee names to agent configurations (image + env vars).
// The BRANCH env var is appended as image tag at runtime.
// Add new agents here when new agent types are onboarded.
var agentConfigs = pkg.AgentConfigurations{
	{
		Assignee: "claude",
		Image:    "docker.quant.benjamin-borbe.de:443/agent-claude",
		Env:      map[string]string{},
	},
	{
		Assignee:   "backtest-agent",
		Image:      "docker.quant.benjamin-borbe.de:443/agent-backtest",
		Env:        map[string]string{},
		SecretName: "agent-backtest",
	},
	{
		Assignee:        "trade-analysis-agent",
		Image:           "docker.quant.benjamin-borbe.de:443/agent-trade-analysis",
		Env:             map[string]string{},
		SecretName:      "agent-trade-analysis",
		VolumeClaim:     "agent-trade-analysis",
		VolumeMountPath: "/home/claude/.claude",
	},
}

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN      string            `required:"true"  arg:"sentry-dsn"       env:"SENTRY_DSN"       usage:"SentryDSN"                                display:"length"`
	SentryProxy    string            `required:"false" arg:"sentry-proxy"     env:"SENTRY_PROXY"     usage:"Sentry Proxy"`
	Listen         string            `required:"true"  arg:"listen"           env:"LISTEN"           usage:"address to listen to"`
	KafkaBrokers   string            `required:"true"  arg:"kafka-brokers"    env:"KAFKA_BROKERS"    usage:"comma-separated Kafka broker addresses"`
	Branch         base.Branch       `required:"true"  arg:"branch"           env:"BRANCH"           usage:"Kafka topic prefix branch (develop/live)"`
	Namespace      string            `required:"true"  arg:"namespace"        env:"NAMESPACE"        usage:"K8s namespace to spawn Jobs in"`
	BuildGitCommit string            `required:"false" arg:"build-git-commit" env:"BUILD_GIT_COMMIT" usage:"Build Git commit hash"                                     default:"none"`
	BuildDate      *libtime.DateTime `required:"false" arg:"build-date"       env:"BUILD_DATE"       usage:"Build timestamp (RFC3339)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	agentlib.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)
	glog.V(1).Infof("agent-task-executor started commit=%s", a.BuildGitCommit)

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return errors.Wrapf(ctx, err, "get in-cluster k8s config")
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return errors.Wrapf(ctx, err, "create k8s client")
	}

	saramaClient, err := libkafka.CreateSaramaClient(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create sarama client")
	}
	defer saramaClient.Close()

	taggedConfigs := agentConfigs.TaggedConfigurations(string(a.Branch))

	currentDateTimeGetter := libtime.NewCurrentDateTime()
	consumer := factory.CreateConsumer(
		saramaClient,
		a.Branch,
		kubeClient,
		a.Namespace,
		a.KafkaBrokers,
		taggedConfigs,
		log.DefaultSamplerFactory,
		currentDateTimeGetter,
	)

	return service.Run(
		ctx,
		func(ctx context.Context) error {
			return consumer.Consume(ctx)
		},
		a.createHTTPServer(),
	)
}

func (a *application) createHTTPServer() run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
