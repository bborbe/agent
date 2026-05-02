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
	libmetrics "github.com/bborbe/metrics"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	bolt "go.etcd.io/bbolt"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/conflict"
	"github.com/bborbe/agent/task/controller/pkg/factory"
	"github.com/bborbe/agent/task/controller/pkg/gitclient"
	"github.com/bborbe/agent/task/controller/pkg/gitrestclient"
	"github.com/bborbe/agent/task/controller/pkg/publisher"
	"github.com/bborbe/agent/task/controller/pkg/result"
	"github.com/bborbe/agent/task/controller/pkg/scanner"
	pkgsync "github.com/bborbe/agent/task/controller/pkg/sync"
)

const vaultLocalPath = "/data/vault"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN       string            `required:"true"  arg:"sentry-dsn"        env:"SENTRY_DSN"        usage:"SentryDSN"                                                    display:"length"`
	SentryProxy     string            `required:"false" arg:"sentry-proxy"      env:"SENTRY_PROXY"      usage:"Sentry Proxy"`
	Listen          string            `required:"true"  arg:"listen"            env:"LISTEN"            usage:"address to listen to"`
	GitURL          string            `required:"true"  arg:"git-url"           env:"GIT_URL"           usage:"vault git repository URL (SSH format)"`
	KafkaBrokers    string            `required:"true"  arg:"kafka-brokers"     env:"KAFKA_BROKERS"     usage:"comma-separated Kafka broker addresses"`
	Branch          base.Branch       `required:"true"  arg:"branch"            env:"BRANCH"            usage:"Kafka topic prefix branch (develop/live)"`
	GitBranch       string            `required:"false" arg:"git-branch"        env:"GIT_BRANCH"        usage:"git branch to track"                                                           default:"main"`
	PollInterval    time.Duration     `required:"false" arg:"poll-interval"     env:"POLL_INTERVAL"     usage:"vault polling interval"                                                        default:"60s"`
	TaskDir         string            `required:"false" arg:"task-dir"          env:"TASK_DIR"          usage:"task directory within vault"                                                   default:"24 Tasks"`
	DataDir         string            `required:"true"  arg:"data-dir"          env:"DATA_DIR"          usage:"directory for BoltDB offset storage"`
	NoSync          bool              `required:"false" arg:"no-sync"           env:"NO_SYNC"           usage:"disable BoltDB fsync (for testing only)"`
	GeminiAPIKey    string            `required:"true"  arg:"gemini-api-key"    env:"GEMINI_API_KEY"    usage:"Gemini API key for LLM conflict resolution"                   display:"length"`
	GitRestURL      string            `required:"false" arg:"git-rest-url"      env:"GIT_REST_URL"      usage:"git-rest HTTP API base URL (required when USE_GIT_REST=true)"                  default:"http://vault-obsidian-openclaw:9090"`
	UseGitRest      bool              `required:"false" arg:"use-git-rest"      env:"USE_GIT_REST"      usage:"use git-rest HTTP API instead of local git clone"                              default:"false"`
	BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version (git describe --tags --always --dirty)"                      default:"dev"`
	BuildGitCommit  string            `required:"false" arg:"build-git-commit"  env:"BUILD_GIT_COMMIT"  usage:"Build Git commit hash"                                                         default:"none"`
	BuildDate       *libtime.DateTime `required:"false" arg:"build-date"        env:"BUILD_DATE"        usage:"Build timestamp (RFC3339)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildGitVersion, a.BuildGitCommit, a.BuildDate)
	glog.V(1).
		Infof("agent-task-controller started version=%s commit=%s", a.BuildGitVersion, a.BuildGitCommit)

	gc, gitRestClient, err := a.createGitClient(ctx)
	if err != nil {
		return err
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

	currentDateTime := libtime.NewCurrentDateTime()
	syncLoop := a.createSyncLoop(gc, eventObjectSender, currentDateTime)

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

	resultWriter := result.NewResultWriter(gc, a.TaskDir, currentDateTime)
	commandConsumer := factory.CreateCommandConsumer(
		saramaClientProvider,
		syncProducer,
		db,
		a.Branch,
		resultWriter,
		gc,
		a.TaskDir,
	)

	return service.Run(
		ctx,
		syncLoop.Run,
		commandConsumer,
		a.createHTTPServer(syncLoop, gitRestClient),
	)
}

// createGitClient constructs the vault GitClient based on the feature flag.
// Returns the client and the gitRestClient (nil when USE_GIT_REST=false).
func (a *application) createGitClient(
	ctx context.Context,
) (gitclient.GitClient, gitrestclient.GitRestClient, error) {
	if a.UseGitRest {
		restClient := gitrestclient.NewGitRestClient(a.GitRestURL)
		gc := gitrestclient.NewGitClient(restClient, vaultLocalPath)
		if err := gc.EnsureCloned(ctx); err != nil {
			return nil, nil, errors.Wrapf(ctx, err, "probe git-rest readiness")
		}
		glog.V(1).Infof("using git-rest HTTP API at %s", a.GitRestURL)
		return gc, restClient, nil
	}
	conflictResolver := conflict.NewGeminiConflictResolver(a.GeminiAPIKey)
	gc := gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch, conflictResolver)
	if err := gc.EnsureCloned(ctx); err != nil {
		return nil, nil, errors.Wrapf(ctx, err, "ensure git clone")
	}
	return gc, nil, nil
}

// createSyncLoop wires the vault scanner and task publisher into a SyncLoop.
func (a *application) createSyncLoop(
	gc gitclient.GitClient,
	eventObjectSender cdb.EventObjectSender,
	currentDateTime libtime.CurrentDateTimeGetter,
) pkgsync.SyncLoop {
	if a.UseGitRest {
		trigger := make(chan struct{}, 1)
		return pkgsync.NewSyncLoop(
			scanner.NewGitRestVaultScanner(gc, a.TaskDir, a.PollInterval, trigger),
			publisher.NewTaskPublisher(eventObjectSender, lib.TaskV1SchemaID, currentDateTime),
			trigger,
		)
	}
	return factory.CreateSyncLoop(
		gc, a.TaskDir, a.PollInterval, eventObjectSender, currentDateTime,
	)
}

func (a *application) createHTTPServer(
	syncLoop pkgsync.SyncLoop,
	gitRestClient gitrestclient.GitRestClient,
) run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			if gitRestClient != nil {
				ready, err := gitRestClient.IsReady(req.Context())
				if err != nil || !ready {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("git-rest not ready"))
					return
				}
			}
			_, _ = w.Write([]byte("OK"))
		})
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
