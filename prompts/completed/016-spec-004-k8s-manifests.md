---
status: completed
spec: [004-task-executor-service]
summary: 'Created all K8s manifests for task/executor: Deployment, Service, Secret, ServiceAccount, Role, RoleBinding, and k8s/Makefile; updated CHANGELOG.md'
container: agent-016-spec-004-k8s-manifests
dark-factory-version: v0.69.0
created: "2026-03-29T13:00:00Z"
queued: "2026-03-29T14:35:59Z"
started: "2026-03-29T14:51:40Z"
completed: "2026-03-29T14:52:49Z"
branch: dark-factory/task-executor-service
---

<summary>
- task/executor gains a k8s/ directory with all manifests needed to deploy the service
- A Deployment runs the service with LISTEN, SENTRY_DSN, SENTRY_PROXY, BRANCH, KAFKA_BROKERS, and NAMESPACE env vars
- A Service exposes port 9090 with Prometheus scrape annotations
- A Secret holds the sentry-dsn key using the same teamvault template pattern as other services
- A ServiceAccount named agent-task-executor is created for the service Pod
- A Role grants the service account permission to create/get/list/watch/delete batch/v1 Jobs within the namespace
- A RoleBinding binds the Role to the ServiceAccount
- The Deployment references the ServiceAccount so spawned Pods inherit Job-creation RBAC
- A k8s/Makefile follows the same three-include pattern as prompt/controller/k8s/Makefile
</summary>

<objective>
Create all K8s manifests required to deploy `task/executor` in a namespace: Deployment, Service, Secret, ServiceAccount, Role, and RoleBinding. The RBAC resources grant the service permission to create and manage batch/v1 Jobs in its own namespace. This is the third and final prompt for spec-004.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `prompt/controller/k8s/agent-prompt-controller-deploy.yaml` — reference Deployment to copy structure from
- `prompt/controller/k8s/agent-prompt-controller-svc.yaml` — reference Service to copy from
- `prompt/controller/k8s/agent-prompt-controller-secret.yaml` — reference Secret to copy from
- `prompt/controller/k8s/Makefile` — reference Makefile (three includes, adjust path depth)
- `task/executor/main.go` — confirms CLI flags: listen, sentry-dsn, sentry-proxy, branch, kafka-brokers, namespace
</context>

<requirements>
### 1. Create `task/executor/k8s/Makefile`

Follow the same pattern as `prompt/controller/k8s/Makefile` (same directory depth, same `../../../` include paths):

```makefile
include ../../../Makefile.variables
include ../../../Makefile.env
include ../../../Makefile.k8s
```

### 2. Create `task/executor/k8s/agent-task-executor-secret.yaml`

```yaml
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
data:
  sentry-dsn: '{{ "SENTRY_DSN_KEY" | env | teamvaultUrl | base64 }}'
```

### 3. Create `task/executor/k8s/agent-task-executor-svc.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
  annotations:
    admin/port: '9090'
    admin/path: ''
spec:
  ports:
  - name: http
    port: 9090
  selector:
    app: agent-task-executor
```

### 4. Create `task/executor/k8s/agent-task-executor-rbac.yaml`

This file contains the ServiceAccount, Role, and RoleBinding as a multi-document YAML file (three `---` separated documents):

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
rules:
  - apiGroups:
      - batch
    resources:
      - jobs
    verbs:
      - create
      - get
      - list
      - watch
      - delete
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: agent-task-executor
subjects:
  - kind: ServiceAccount
    name: agent-task-executor
    namespace: '{{ "NAMESPACE" | env }}'
```

### 5. Create `task/executor/k8s/agent-task-executor-deploy.yaml`

The Deployment must:
- Set `serviceAccountName: agent-task-executor` so the Pod gets K8s API credentials
- Pass `NAMESPACE` as an env var using the K8s downward API (fieldRef) so the service knows which namespace to spawn Jobs in
- Include the same keel.sh annotations, nodeAffinity, and Prometheus scrape annotations as `prompt/controller`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-task-executor
  namespace: '{{ "NAMESPACE" | env }}'
  annotations:
    keel.sh/policy: force
    keel.sh/trigger: poll
    keel.sh/match-tag: "true"
    keel.sh/pollSchedule: "@every 1m"
    random: '{{ "RANDOM" | env }}'
spec:
  replicas: 1
  selector:
    matchLabels:
      app: agent-task-executor
  template:
    metadata:
      annotations:
        prometheus.io/path: /metrics
        prometheus.io/port: "9090"
        prometheus.io/scheme: http
        prometheus.io/scrape: "true"
        random: '{{ "RANDOM" | env }}'
      labels:
        app: agent-task-executor
    spec:
      serviceAccountName: agent-task-executor
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node_type
                    operator: In
                    values:
                      - '{{ "NAMESPACE" | env }}'
      containers:
        - name: service
          args:
            - -v={{"LOGLEVEL" | env}}
          env:
            - name: LISTEN
              value: ':9090'
            - name: SENTRY_DSN
              valueFrom:
                secretKeyRef:
                  key: sentry-dsn
                  name: agent-task-executor
            - name: SENTRY_PROXY
              value: '{{"SENTRY_PROXY_URL" | env}}'
            - name: BRANCH
              value: '{{ "BRANCH" | env }}'
            - name: KAFKA_BROKERS
              value: '{{ "KAFKA_BROKERS" | env }}'
            - name: NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          image: '{{"DOCKER_REGISTRY" | env}}/agent-task-executor:{{"BRANCH" | env}}'
          imagePullPolicy: Always
          livenessProbe:
            failureThreshold: 5
            httpGet:
              path: /healthz
              port: 9090
              scheme: HTTP
            initialDelaySeconds: 10
            successThreshold: 1
            timeoutSeconds: 5
          ports:
            - containerPort: 9090
              name: http
          readinessProbe:
            httpGet:
              path: /readiness
              port: 9090
              scheme: HTTP
            initialDelaySeconds: 5
            timeoutSeconds: 5
          resources:
            limits:
              cpu: 500m
              memory: 50Mi
            requests:
              cpu: 20m
              memory: 20Mi
      imagePullSecrets:
        - name: docker
```

**Important**: The `NAMESPACE` env var is injected via the K8s downward API (`fieldRef: fieldPath: metadata.namespace`) rather than from a static template variable. This ensures the service always spawns Jobs in its own namespace even if the manifest is deployed to multiple namespaces.

### 6. Update `CHANGELOG.md`

Add to `## Unreleased` in the root `CHANGELOG.md`. If `## Unreleased` does not exist, create it before the first `## v` entry:

```
- feat: Add K8s manifests for task/executor (Deployment, Service, Secret, ServiceAccount, Role, RoleBinding)
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify any Go source files — this prompt is YAML/config only
- Do NOT run `make precommit` — no Go code changed; YAML linting is not part of the precommit pipeline
- ServiceAccount name must be `agent-task-executor` — matches the service name
- Role must be a Role (namespace-scoped), NOT a ClusterRole — the service must not affect other namespaces
- Role must include exactly: create, get, list, watch, delete on batch/jobs
- The `NAMESPACE` env var in the Deployment must use the downward API fieldRef (not the template `{{ "NAMESPACE" | env }}`), so the Pod always knows its own namespace at runtime
- Image name must be `agent-task-executor` — matches the Makefile SERVICE variable
- All template placeholders use the same `{{ "VAR" | env }}` syntax as other manifests in this repo
</constraints>

<verification>
Verify all manifest files exist:
```bash
ls task/executor/k8s/
```
Expected files: `Makefile`, `agent-task-executor-deploy.yaml`, `agent-task-executor-svc.yaml`, `agent-task-executor-secret.yaml`, `agent-task-executor-rbac.yaml`

Verify RBAC resources are present:
```bash
grep -c 'kind: Role\b' task/executor/k8s/agent-task-executor-rbac.yaml
```
Expected: 1

```bash
grep -c 'kind: RoleBinding' task/executor/k8s/agent-task-executor-rbac.yaml
```
Expected: 1

```bash
grep -c 'kind: ServiceAccount' task/executor/k8s/agent-task-executor-rbac.yaml
```
Expected: 1

Verify serviceAccountName is set in the Deployment:
```bash
grep 'serviceAccountName' task/executor/k8s/agent-task-executor-deploy.yaml
```
Expected: `serviceAccountName: agent-task-executor`

Verify NAMESPACE uses downward API:
```bash
grep -A3 'name: NAMESPACE' task/executor/k8s/agent-task-executor-deploy.yaml
```
Expected: contains `fieldRef` and `metadata.namespace`
</verification>
