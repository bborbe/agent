# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
