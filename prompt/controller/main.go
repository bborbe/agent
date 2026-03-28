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

	"github.com/bborbe/agent/prompt/controller/pkg/factory"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN    string      `required:"true"  arg:"sentry-dsn"    env:"SENTRY_DSN"    usage:"SentryDSN"                                display:"length"`
	SentryProxy  string      `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"  usage:"Sentry Proxy"`
	Listen       string      `required:"true"  arg:"listen"        env:"LISTEN"        usage:"address to listen to"`
	KafkaBrokers string      `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"comma-separated Kafka broker addresses"`
	Branch       base.Branch `required:"true"  arg:"branch"        env:"BRANCH"        usage:"Kafka topic prefix branch (develop/live)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-prompt-controller started")

	saramaClient, err := libkafka.CreateSaramaClient(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create sarama client")
	}
	defer saramaClient.Close()

	syncProducer, err := libkafka.NewSyncProducerFromSaramaClient(
		ctx,
		saramaClient,
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

	consumer := factory.CreateConsumer(
		saramaClient,
		a.Branch,
		eventObjectSender,
		log.DefaultSamplerFactory,
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
