---
status: approved
spec: [007-agent-config-crd]
created: "2026-04-16T17:30:00Z"
queued: "2026-04-16T18:00:19Z"
---

<summary>
- Executor watches AgentConfig custom resources in its namespace and keeps an in-memory lookup keyed by assignee
- Lookup and store are provided by the shared `github.com/bborbe/k8s` generics — no bespoke store or adapter is written in this service
- Adding, updating, or deleting an AgentConfig is picked up by the executor without a restart
- Task events resolve their agent configuration from the live store instead of the hardcoded list
- Unknown assignees continue to be warned, counted (`skipped_unknown_assignee`), and skipped — same metric name, same behaviour
- The compiled-in hardcoded agent list is removed — the executor binary no longer knows about specific agents
- Informer and Kafka consumer run as concurrent services; cancelling the context stops both cleanly
- Eventual consistency is accepted: a task arriving before the first informer sync is safely skipped and the next matching task succeeds
</summary>

<objective>
Replace the hardcoded `agentConfigs` slice in `task/executor/main.go` with a live in-memory store fed by a Kubernetes informer on `AgentConfig` resources. Reuse the generic `k8s.EventHandler[v1.AgentConfig]` store and `k8s.NewResourceEventHandler[v1.AgentConfig]` adapter from `github.com/bborbe/k8s`, and drive the informer via the generated `SharedInformerFactory` produced by prompt 1. After this prompt the executor discovers agents at runtime — adding an agent is a `kubectl apply` away. The CRD self-install from prompt 1 is invoked on startup.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**Authoritative references (read first):**
- `~/.claude/plugins/marketplaces/coding/docs/go-kubernetes-crd-controller-guide.md` — sections 4 (`Listen`), 5 (event-handler split), 6 (in-memory store — same shape as `k8s.EventHandler`), 7 (antipatterns — **no Lister, no WaitForCacheSync, eventual consistency**), 9 (factory), 10 (`main.go` integration).
- `~/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md`.
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`.
- `~/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md`.
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`.
- `~/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md`.
- `specs/in-progress/007-agent-config-crd.md` — reread §Failure Modes and §Acceptance Criteria.

**Reference implementation (at `~/Documents/workspaces/alert/` on host — read by example, NOT imported):**
- `alert_event-handler.go` — `NewAlertEventHandler` = `k8s.NewEventHandler[v1.Alert]()`. One-liner.
- `alert_resource-event-handler.go` — `NewAlertResourceEventHandler(ctx, eventHandler) = k8s.NewResourceEventHandler[v1.Alert](ctx, eventHandler)`. One-liner.
- `alert_clientset.go` — `CreateClientset` wiring the generated `versioned.Interface`.
- `k8s/client/informers/externalversions/factory.go` — shape of `NewSharedInformerFactory(client, resyncPeriod)` and `.Monitoring().V1().Alerts().Informer()`. The analogue here is `.Agents().V1().AgentConfigs().Informer()` (after codegen).

**Outputs of prompt 1 (preconditions this prompt relies on):**
- `task/executor/k8s/apis/agents.bborbe.dev/v1/` contains `types.go` (implementing `k8s.Type`), `register.go`, `doc.go`, and generated `zz_generated.deepcopy.go`.
- `task/executor/k8s/client/{clientset,informers,listers,applyconfiguration}/` is generated and committed.
- `task/executor/pkg/k8s_connector.go` defines `K8sConnector` with `SetupCustomResourceDefinition` implemented and `Listen` as a placeholder.
- `task/executor/mocks/k8s_connector.go` exists.
- `task/executor/pkg/agent_configuration.go` still defines `AgentConfiguration` — unchanged.

**Key generated symbols (use these — do NOT invent new ones):**
- `versioned "github.com/bborbe/agent/task/executor/k8s/client/clientset/versioned"` — `versioned.Interface`, `versioned.NewForConfig(*rest.Config) (versioned.Interface, error)`.
- `agentinformers "github.com/bborbe/agent/task/executor/k8s/client/informers/externalversions"` — `agentinformers.NewSharedInformerFactory(client, resync)`.
- The factory exposes a path named after the Go package of `k8s/apis/agents.bborbe.dev/` (the `agents` package). Call site will be `factory.Agents().V1().AgentConfigs().Informer()`. If codegen happens to emit a different method chain (for example if a `+groupGoName=` marker renames it), adjust the call site to match — the codegen output is the source of truth.
- `v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"` — `v1.AgentConfig` satisfies `k8s.Type`.

**Key bborbe/k8s generics (already vendored via `github.com/bborbe/k8s v1.13.4`):**
- `k8s.EventHandler[T k8s.Type]` — interface with `OnAdd(ctx, T) error`, `OnUpdate(ctx, old, new T) error`, `OnDelete(ctx, T) error`, plus `Get(ctx) ([]T, error)` (embeds `k8s.Provider[T]`).
- `k8s.NewEventHandler[T k8s.Type]() k8s.EventHandler[T]` — returns the mutex-guarded in-memory store backed by `map[k8s.Identifier]T`.
- `k8s.NewResourceEventHandler[T k8s.Type](ctx, eventHandler) cache.ResourceEventHandler` — the cast adapter.

Confirmed by reading the vendored source of `github.com/bborbe/k8s` on host at `~/Documents/workspaces/k8s/k8s_event-handler-type.go` and `k8s_resource-event-handler.go`. These replace the custom store and custom adapter that the previous draft of this prompt proposed — DO NOT write your own.

**Existing files to read before editing:**
- `task/executor/main.go` — `agentConfigs` package-level slice, `application.Run`, `service.Run` invocation, `factory.CreateConsumer` call.
- `task/executor/pkg/handler/task_event_handler.go` — handler's `agentConfigs pkg.AgentConfigurations` field and `FindByAssignee` lookup. Lookup semantics stay identical; the input type changes.
- `task/executor/pkg/handler/task_event_handler_test.go` — existing Ginkgo specs.
- `task/executor/pkg/factory/factory.go` — `CreateConsumer` wiring.
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct (conversion target) and `TaggedConfigurations`.
- `task/executor/pkg/metrics/metrics.go` — `skipped_unknown_assignee` label is pre-registered.

**Key facts and policies:**
- The informer must live in the executor's own namespace (per spec — cross-namespace is out by construction).
- Per guide section 7: **no `cache.WaitForCacheSync`**. If a task arrives before first sync, the resolver simply finds no match → warn + `skipped_unknown_assignee` + skip.
- `service.Run` from `github.com/bborbe/service` already accepts multiple `run.Func` args. The current call passes two; this prompt adds a third (the informer).
- Store access is via `eventHandler.Get(ctx) ([]v1.AgentConfig, error)` — iterate and filter by `Spec.Assignee`. Do NOT try to index into the internal map — the `k8s.EventHandler` interface does not expose key-based lookup.
- The generic `k8s.EventHandler` holds `v1.AgentConfig` values. Conversion to the internal `pkg.AgentConfiguration` happens at lookup time, not on `OnAdd/OnUpdate`. This is different from a bespoke-store design and keeps the store logic free of executor-specific code.
- Branch tagging (`image + ":" + branch`) happens in the conversion function, with the branch captured once at constructor time.
- **Why `AgentConfigResolver` (not direct `handler.Get(ctx)` in the handler)**: (1) keeps `pkg/handler` free of `v1.AgentConfig` imports — it only sees `pkg.AgentConfiguration`; (2) handler tests mock a narrow `AgentConfigResolver` interface instead of setting up a generic `k8s.EventHandler` fake; (3) centralises the filter + convert + branch-tag logic + `ErrAgentConfigNotFound` wrapping in one place.

**Branch tagging transition:** The current code does `agentConfigs.TaggedConfigurations(string(a.Branch))` once at startup. With CRDs, the image tag is appended per-lookup inside `convert(v1.AgentConfig, branch) pkg.AgentConfiguration`. The `TaggedConfigurations` method on `pkg.AgentConfigurations` becomes dead code once the slice is removed, but leave it in place (unused exported function on a type that still exists) — its removal is a separate cleanup not in scope.
</context>

<requirements>

1. **Delete the bespoke store, adapter, and typed handler — use bborbe/k8s generics directly**

   Do NOT add `pkg/agent_config_store.go`, `pkg/event_handler.go`, or `pkg/event_handler_agent_config.go`. The generic `k8s.EventHandler[v1.AgentConfig]` replaces all three.

2. **Create the thin alias + constructor files (mirroring alert)**

   New file `task/executor/pkg/event_handler_agent_config.go`:
   ```go
   package pkg

   import (
       "github.com/bborbe/k8s"

       v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
   )

   // EventHandlerAgentConfig is the typed in-memory event handler / store
   // for AgentConfig resources. Backed by github.com/bborbe/k8s generics.
   type EventHandlerAgentConfig k8s.EventHandler[v1.AgentConfig]

   // NewEventHandlerAgentConfig returns an empty EventHandlerAgentConfig.
   func NewEventHandlerAgentConfig() EventHandlerAgentConfig {
       return k8s.NewEventHandler[v1.AgentConfig]()
   }
   ```
   Include the BSD license header.

   New file `task/executor/pkg/resource_event_handler_agent_config.go`:
   ```go
   package pkg

   import (
       "context"

       "github.com/bborbe/k8s"
       "k8s.io/client-go/tools/cache"

       v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
   )

   // NewResourceEventHandlerAgentConfig adapts an EventHandlerAgentConfig to the
   // cache.ResourceEventHandler the informer expects.
   func NewResourceEventHandlerAgentConfig(
       ctx context.Context,
       handler EventHandlerAgentConfig,
   ) cache.ResourceEventHandler {
       return k8s.NewResourceEventHandler[v1.AgentConfig](ctx, handler)
   }
   ```
   Both files are one-liners; do NOT add logic. The mirror is `alert_event-handler.go` + `alert_resource-event-handler.go` (see Context).

   NO counterfeiter directives on either file. The mocks needed for tests come from `github.com/bborbe/k8s/mocks` (already vendored) — in particular `k8s.mocks.EventHandler[v1.AgentConfig]` is usable directly if the tests need a fake. If the generic mock shape doesn't fit the test, fall back to a small local fake struct implementing `k8s.EventHandler[v1.AgentConfig]` in the test file (not in `pkg/`). Do NOT create a new counterfeiter target for an aliased generic interface.

3. **Introduce an `AgentConfigResolver` that does the per-lookup conversion**

   New file `task/executor/pkg/agent_config_resolver.go`:
   ```go
   package pkg

   import (
       "context"
       stderrors "errors"

       "github.com/bborbe/errors"
       "github.com/bborbe/k8s"

       v1 "github.com/bborbe/agent/task/executor/k8s/apis/agents.bborbe.dev/v1"
   )

   // ErrAgentConfigNotFound is returned by AgentConfigResolver.Resolve when no
   // AgentConfig in the store has a matching Spec.Assignee.
   var ErrAgentConfigNotFound = stderrors.New("agent config not found")

   //counterfeiter:generate -o ../mocks/agent_config_resolver.go --fake-name FakeAgentConfigResolver . AgentConfigResolver

   // AgentConfigResolver looks up the AgentConfiguration for an assignee by
   // iterating the in-memory AgentConfig store and converting the matching entry.
   type AgentConfigResolver interface {
       Resolve(ctx context.Context, assignee string) (AgentConfiguration, error)
   }

   // NewAgentConfigResolver returns an AgentConfigResolver backed by the given
   // typed store. The branch is captured here and appended as the image tag at
   // resolution time.
   func NewAgentConfigResolver(
       provider k8s.Provider[v1.AgentConfig],
       branch string,
   ) AgentConfigResolver {
       return &agentConfigResolver{provider: provider, branch: branch}
   }

   type agentConfigResolver struct {
       provider k8s.Provider[v1.AgentConfig]
       branch   string
   }

   func (r *agentConfigResolver) Resolve(ctx context.Context, assignee string) (AgentConfiguration, error) {
       items, err := r.provider.Get(ctx)
       if err != nil {
           return AgentConfiguration{}, errors.Wrapf(ctx, err, "list agent configs")
       }
       for _, it := range items {
           if it.Spec.Assignee == assignee {
               return convert(it, r.branch), nil
           }
       }
       return AgentConfiguration{}, errors.Wrapf(ctx, ErrAgentConfigNotFound, "find assignee %q", assignee)
   }

   func convert(obj v1.AgentConfig, branch string) AgentConfiguration {
       return AgentConfiguration{
           Assignee:        obj.Spec.Assignee,
           Image:           obj.Spec.Image + ":" + branch,
           Env:             copyEnv(obj.Spec.Env),
           SecretName:      obj.Spec.SecretName,
           VolumeClaim:     obj.Spec.VolumeClaim,
           VolumeMountPath: obj.Spec.VolumeMountPath,
       }
   }

   func copyEnv(in map[string]string) map[string]string {
       out := make(map[string]string, len(in))
       for k, v := range in {
           out[k] = v
       }
       return out
   }
   ```
   `k8s.Provider[T]` is the read-only slice of `k8s.EventHandler[T]` (see `~/Documents/workspaces/k8s/k8s_event-handler-type.go`). The resolver accepts the narrower interface so tests can pass a simple fake provider.

   BSD license header required.

4. **Implement `K8sConnector.Listen` (replacing the placeholder from prompt 1)**

   Modify `task/executor/pkg/k8s_connector.go`. Replace the placeholder `Listen` body with an implementation that uses the **generated** `SharedInformerFactory`:

   ```go
   func (c *k8sConnector) Listen(
       ctx context.Context,
       namespace string,
       handler cache.ResourceEventHandler,
   ) error {
       clientset, err := versioned.NewForConfig(c.config)
       if err != nil {
           return errors.Wrapf(ctx, err, "build agentconfig clientset")
       }
       factory := agentinformers.NewSharedInformerFactoryWithOptions(
           clientset,
           defaultResync,
           agentinformers.WithNamespace(namespace),
       )
       informer := factory.Agents().V1().AgentConfigs().Informer()
       if _, err := informer.AddEventHandler(handler); err != nil {
           return errors.Wrapf(ctx, err, "add event handler")
       }
       stopCh := make(chan struct{})
       factory.Start(stopCh)
       defer close(stopCh)
       select {
       case <-ctx.Done():
       case <-stopCh:
       }
       return nil
   }

   const defaultResync = 5 * time.Minute
   ```

   Imports to add to the existing file:
   - `"time"`
   - `versioned "github.com/bborbe/agent/task/executor/k8s/client/clientset/versioned"`
   - `agentinformers "github.com/bborbe/agent/task/executor/k8s/client/informers/externalversions"`

   If codegen emitted a different Go path for the "Agents" navigation method (e.g. `AgentsBborbeDev()` instead of `Agents()`), match the generated output exactly. Confirm by opening `task/executor/k8s/client/informers/externalversions/factory.go` after codegen and grepping for the group navigation method.

   Per guide section 7: **do NOT add `cache.WaitForCacheSync`**. Eventual consistency is the contract.

5. **Update the task event handler to use the resolver**

   Modify `task/executor/pkg/handler/task_event_handler.go`:

   a. Replace the `agentConfigs pkg.AgentConfigurations` field with `resolver pkg.AgentConfigResolver`.

   b. Change `NewTaskEventHandler` signature:
      ```go
      func NewTaskEventHandler(
          jobSpawner spawner.JobSpawner,
          branch base.Branch,
          resolver pkg.AgentConfigResolver,
      ) TaskEventHandler
      ```

   c. Replace the `FindByAssignee` block in `ConsumeMessage`:

      Old:
      ```go
      config, ok := h.agentConfigs.FindByAssignee(string(task.Frontmatter.Assignee()))
      if !ok {
          glog.Warningf("skip task %s: unknown assignee %s", task.TaskIdentifier, task.Frontmatter.Assignee())
          metrics.TaskEventsTotal.WithLabelValues("skipped_unknown_assignee").Inc()
          return nil
      }
      ```

      New:
      ```go
      config, err := h.resolver.Resolve(ctx, string(task.Frontmatter.Assignee()))
      if err != nil {
          if stderrors.Is(err, pkg.ErrAgentConfigNotFound) {
              glog.Warningf(
                  "skip task %s: unknown assignee %s",
                  task.TaskIdentifier, task.Frontmatter.Assignee(),
              )
              metrics.TaskEventsTotal.WithLabelValues("skipped_unknown_assignee").Inc()
              return nil
          }
          metrics.TaskEventsTotal.WithLabelValues("error").Inc()
          return errors.Wrapf(ctx, err, "resolve agent config for task %s", task.TaskIdentifier)
      }
      ```
      Add import `stderrors "errors"` (alongside the existing `github.com/bborbe/errors`).

6. **Update the factory**

   Modify `task/executor/pkg/factory/factory.go`:

   a. Change `CreateConsumer` signature — replace `agentConfigs pkg.AgentConfigurations` with `resolver pkg.AgentConfigResolver`. Pass `resolver` to `handler.NewTaskEventHandler`.

   b. Add factory functions (each single-`return`, per `go-factory-pattern.md`):
      ```go
      func CreateEventHandlerAgentConfig() pkg.EventHandlerAgentConfig {
          return pkg.NewEventHandlerAgentConfig()
      }

      func CreateResourceEventHandlerAgentConfig(
          ctx context.Context,
          handler pkg.EventHandlerAgentConfig,
      ) cache.ResourceEventHandler {
          return pkg.NewResourceEventHandlerAgentConfig(ctx, handler)
      }

      func CreateAgentConfigResolver(
          handler pkg.EventHandlerAgentConfig,
          branch base.Branch,
      ) pkg.AgentConfigResolver {
          return pkg.NewAgentConfigResolver(handler, string(branch))
      }
      ```
      Add imports `"context"` and `"k8s.io/client-go/tools/cache"`.

      Note `CreateAgentConfigResolver` accepts `pkg.EventHandlerAgentConfig` (which satisfies `k8s.Provider[v1.AgentConfig]` via the embedded interface) — no additional conversion needed.

7. **Wire everything in `main.go`**

   Modify `task/executor/main.go`:

   a. **Delete the package-level `agentConfigs` slice entirely.** The executor must have no compiled-in agent catalog (spec AC "Executor binary has no compiled-in agent catalog").

   b. Remove the `taggedConfigs := agentConfigs.TaggedConfigurations(...)` line.

   c. In `Run`, after `kubeClient, err := kubernetes.NewForConfig(kubeConfig)` and before the `saramaClient` block, add:
      ```go
      connector := factory.CreateK8sConnector(kubeConfig)
      if err := connector.SetupCustomResourceDefinition(ctx); err != nil {
          return errors.Wrapf(ctx, err, "setup AgentConfig CRD")
      }
      eventHandlerAgentConfig := factory.CreateEventHandlerAgentConfig()
      resourceEventHandler := factory.CreateResourceEventHandlerAgentConfig(ctx, eventHandlerAgentConfig)
      resolver := factory.CreateAgentConfigResolver(eventHandlerAgentConfig, a.Branch)
      ```

   d. Replace the `taggedConfigs` argument to `CreateConsumer` with `resolver`:
      ```go
      consumer := factory.CreateConsumer(
          saramaClient,
          a.Branch,
          kubeClient,
          a.Namespace,
          a.KafkaBrokers,
          resolver,
          log.DefaultSamplerFactory,
          currentDateTimeGetter,
      )
      ```

   e. Expand the `service.Run` call to also run the informer:
      ```go
      return service.Run(
          ctx,
          func(ctx context.Context) error {
              return connector.Listen(ctx, a.Namespace, resourceEventHandler)
          },
          func(ctx context.Context) error {
              return consumer.Consume(ctx)
          },
          a.createHTTPServer(),
      )
      ```

   f. Remove the now-unused import of `pkg` from main.go if nothing else references it (`pkg.AgentConfigurations` was the only user). Keep `factory` and any other imports.

8. **Update tests**

   a. `task/executor/pkg/handler/task_event_handler_test.go`:
      - Replace the `agentConfigs pkg.AgentConfigurations` setup with a `fakeResolver *mocks.FakeAgentConfigResolver`.
      - In `BeforeEach`:
        ```go
        fakeResolver = &mocks.FakeAgentConfigResolver{}
        fakeResolver.ResolveReturns(pkg.AgentConfiguration{Assignee: "claude", Image: "my-image:latest"}, nil)
        ```
      - Replace the `handler.NewTaskEventHandler(fakeSpawner, base.Branch("prod"), agentConfigs)` call with `handler.NewTaskEventHandler(fakeSpawner, base.Branch("prod"), fakeResolver)`. Update every similar construction in the file (see the `base.Branch("dev")` cases).
      - Update "skips unknown assignee without error": before the call do `fakeResolver.ResolveReturns(pkg.AgentConfiguration{}, errors.Wrapf(ctx, pkg.ErrAgentConfigNotFound, "find assignee"))` and expect `err == nil`, `fakeSpawner.SpawnJobCallCount() == 0`, metric branch taken (asserting via the return value is enough — we already assert `err == nil`).
      - Add a new spec: "returns wrapped error when resolver fails with non-NotFound" — `fakeResolver.ResolveReturns(pkg.AgentConfiguration{}, errors.Errorf(ctx, "boom"))` → expect `err != nil`, `SpawnJobCallCount == 0`, `metrics.TaskEventsTotal.WithLabelValues("error")` path (simple assertion: `err != nil`).

   b. New file `task/executor/pkg/agent_config_resolver_test.go` — Ginkgo specs using a small local `fakeProvider` struct that implements `k8s.Provider[v1.AgentConfig]` by returning a canned slice:
      - `Resolve` returns the converted `AgentConfiguration` with `Image == spec.Image + ":" + branch`.
      - `Resolve` returns `ErrAgentConfigNotFound` (wrapped) when no item matches.
      - `Resolve` defensively copies the env map — mutating the input spec after the call must not affect the returned `AgentConfiguration.Env`.
      - `Resolve` returns a wrapped error when `provider.Get` returns an error.
      - Branch tagging: given `branch="dev"` and `Spec.Image="foo/bar"`, result has `Image == "foo/bar:dev"`.

9. **Regenerate mocks**

   Run `cd task/executor && make generate`. This creates:
   - `task/executor/mocks/agent_config_resolver.go` (from the new counterfeiter directive).
   - Refreshes existing mocks.

   If the codegen output for `k8s/client/` is missed (e.g., a fresh clone where prompt 1 was committed but mocks were cleared), run `make generatek8s` first — otherwise skip it. The test suite references `task/executor/k8s/apis/agents.bborbe.dev/v1/*.go` and the generated `k8s/client/` packages; both must be present.

10. **Run precommit**

    Final: `cd task/executor && make precommit` must pass (exit 0).

</requirements>

<constraints>
- Do NOT add `cache.WaitForCacheSync` or any startup-sync gate — per guide section 7, eventual consistency is the contract.
- Do NOT use `Lister` or `Indexer` APIs from `k8s/client/listers/` — the resolver iterates `k8s.Provider[T].Get(ctx)` only.
- Do NOT write a bespoke mutex-guarded store, custom event handler struct, or custom cache adapter. Use `k8s.NewEventHandler[v1.AgentConfig]()` and `k8s.NewResourceEventHandler[v1.AgentConfig](ctx, handler)` from `github.com/bborbe/k8s`.
- Do NOT ship a separate CRD YAML manifest.
- Do NOT change the Kafka topic, `lib.Task` schema, or Job naming.
- Do NOT change the `skipped_unknown_assignee` metric name or any other metric.
- Do NOT commit — dark-factory handles git.
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` — never `fmt.Errorf`.
- Use `github.com/golang/glog` for logging.
- The `agentConfigs` package-level slice in `main.go` must be DELETED — executor binary has no compiled-in agent catalog (spec AC).
- Branch tagging happens exactly once, in `convert()`, using the branch captured at resolver construction time.
- No counterfeiter directive on `EventHandlerAgentConfig` — it is a type alias of a generic interface and mocks come from `github.com/bborbe/k8s/mocks` or local test fakes.

Failure-mode coverage mapped to requirements (from spec §Failure Modes):
- "No matching AgentConfig for assignee" → requirement 5 (returns `ErrAgentConfigNotFound` → handler warns + increments `skipped_unknown_assignee` + returns nil).
- "Informer not yet synced when first task arrives" → same path as above: `Get(ctx)` returns empty slice → `ErrAgentConfigNotFound` → warn + skip. Next task after sync succeeds.
- "AgentConfig CR deleted while executor running" → informer calls `OnDelete` on `k8s.EventHandler[v1.AgentConfig]` → entry removed → next `Resolve` misses → warn + skip.
- "AgentConfig CR updated" → informer calls `OnUpdate` → entry replaced → next `Resolve` returns new values.
- "API server unavailable at startup" → `SetupCustomResourceDefinition` returns wrapped error → `Run` returns → executor exits non-zero.
- "RBAC missing" → `connector.Listen` receives a forbidden from the informer → `factory.Start` logs repeatedly via the informer and the error surfaces on reflector retries; the executor stays up but the informer never populates. The spec accepts this (eventual consistency).
- All existing tests must still pass.
</constraints>

<verification>
```bash
cd task/executor && make generate
```
Must create `task/executor/mocks/agent_config_resolver.go` and refresh existing mocks.

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify package-level `agentConfigs` is removed:
```bash
grep -n "^var agentConfigs\|agentConfigs\s*=\s*pkg.AgentConfigurations" task/executor/main.go
```
Must return zero matches.

Verify resolver usage in handler:
```bash
grep -n "resolver.Resolve\|AgentConfigResolver" task/executor/pkg/handler/task_event_handler.go
```
Must show the new wiring.

Verify informer is wired in Run:
```bash
grep -n "connector.Listen\|CreateK8sConnector" task/executor/main.go
```
Must show `Listen` is called inside `service.Run`.

Verify generated SharedInformerFactory is used (not a hand-rolled `cache.NewSharedIndexInformer` + `cache.ListWatch`):
```bash
grep -n "NewSharedInformerFactory\|AgentConfigs().Informer" task/executor/pkg/k8s_connector.go
```
Must show the factory call. And:
```bash
grep -n "cache.NewSharedIndexInformer\|cache.ListWatch" task/executor/pkg/k8s_connector.go
```
Must return zero matches.

Verify bborbe/k8s generics are used (no hand-written store/adapter):
```bash
grep -rn "sync.RWMutex\|sync.Mutex" task/executor/pkg/ | grep -v _test.go
```
Must return zero matches in the new files (`event_handler_agent_config.go`, `resource_event_handler_agent_config.go`, `agent_config_resolver.go`).

```bash
grep -n "k8s.NewEventHandler\|k8s.NewResourceEventHandler" task/executor/pkg/event_handler_agent_config.go task/executor/pkg/resource_event_handler_agent_config.go
```
Must show both generic constructors are used.

Verify no `WaitForCacheSync`:
```bash
grep -rn "WaitForCacheSync" task/executor/ | grep -v vendor | grep -v k8s/client
```
Must return zero matches.

Verify branch tagging happens in conversion:
```bash
grep -n 'Spec.Image\s*+\s*":"\s*+\s*branch\|Spec\.Image +' task/executor/pkg/agent_config_resolver.go
```
Must show the image+branch concatenation.

Verify no bespoke `AgentConfigStore` interface was added:
```bash
grep -rn "type AgentConfigStore\b" task/executor/pkg/
```
Must return zero matches.
</verification>
