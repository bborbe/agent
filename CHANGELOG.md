# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

- refactor: move trigger channel ownership into SyncLoop; expose Trigger() method on SyncLoop interface; remove raw channel from factory and main.go

## v0.7.0

- feat: add /trigger HTTP endpoint for on-demand vault scan cycles
- feat: add trigger channel to VaultScanner for external scan triggering
- docs: add dark-factory prompts for trigger endpoint and UUID task identifier spec

## v0.6.2

- fix: add separate BRANCH env var for Kafka topic prefix (was using GIT_BRANCH 'main' instead of 'dev'→'develop')

## v0.6.1

- fix: change TASK_DIR from '24 Tasks' to 'tasks' matching OpenClaw vault structure
- fix: return publish errors instead of logging warnings (fail fast via CancelOnFirstErrorWait)
- docs: add deployment guide with buca workflow and useful links

## v0.6.0

- refactor: replace go func() with run.CancelOnFirstErrorWait in sync_loop
- refactor: change VaultScanner interface to caller-owned channel (Run(ctx, chan<- ScanResult))
- fix: reduce cognitive complexity by extracting processResult method
- feat: add /setloglevel endpoint with 5-minute auto-reset
- fix: align glog V-levels (V2=heartbeat, V3=per-item, V4=trace)
- docs: add README with service description and dev/prod setloglevel links

## v0.5.0

- feat: switch git auth from token to SSH key mounted as K8s secret volume
- feat: migrate to per-service go.mod with replace directives for shared lib (matching trading monorepo pattern)
- feat: decouple GIT_BRANCH from BRANCH env var for independent vault repo branch control
- fix: update .gitignore to match trading pattern (vendor without prefix for per-service dirs)
- fix: osv-scanner scans current dir instead of ROOTDIR to avoid vendor false positives

## v0.4.0

- feat: refactor TaskPublisher to use CQRS EventObjectSender stack (SyncProducer → JSONSender → EventObjectSender) matching trading best practices
- feat: add K8s deployment manifests for task/controller (StatefulSet with PVC, Service, Secret with teamvault)
- feat: add shared K8s infra (Makefile.k8s, Makefile.env, env files) for make apply workflow
- chore: align test suites with GinkgoConfiguration + 60s timeout, add gexec compile test to main_test.go

## v0.3.1

- chore: verify all tests pass, linting succeeds, and precommit checks are green

## v0.3.0

- feat: wire VaultScanner to TaskPublisher via SyncLoop in task/controller, publishing changed and deleted task events to Kafka; integrate sync loop with HTTP server in main.go for concurrent operation with graceful shutdown

## v0.2.0

- feat: add VaultScanner service in task/controller that polls git, detects file changes via sha256 content hashing, parses YAML frontmatter, and emits ScanResult events with changed and deleted task identifiers

## v0.1.0

### Added
- Initial project structure
- [Module] github.com/bborbe/agent
- feat: add GitClient interface and implementation in task/controller for git clone/validate via os/exec subprocess
- feat: add CLI flags (git-url, git-token, kafka-brokers, git-branch, poll-interval, task-dir) to task/controller application struct
- fix: update osv-scanner in Makefile.precommit to use ROOTDIR so subdirectory make precommit can find go.mod
- chore: suppress pre-existing moby/buildkit vulnerability in .osv-scanner.toml
