# DozLab API - Kubernetes Sidecar Lab Platform

A cloud-native educational platform with **Kubernetes sidecar architecture** for hands-on technical labs featuring real VM environments, integrated terminals, and VS Code access.

## Architecture

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/architecture-dark.png">
  <img alt="DozLab architecture: the browser reaches Traefik on a single-node k3s cluster; Traefik sends /api to dozlab-api and each session's terminal and editor paths to its lab pod; dozlab-api writes to PostgreSQL and creates LabSessions; dozlab-controller creates the lab pods and publishes phase changes to RabbitMQ, which the API consumes" src="docs/diagrams/architecture.png">
</picture>

The browser loads the Nuxt frontend from GitHub Pages and reaches the cluster through Tailscale
Funnel and Traefik. Traefik sends `/api` to this service, and each session's
`/sessions/<id>/terminal` and `/sessions/<id>/vscode` straight to that session's pod.

- **dozlab-api** (this repo): JWT auth, access control, the audit log, and lab and session
  records in PostgreSQL. To start a lab it creates a `LabSession` resource; it never builds pods.
- **dozlab-controller** watches `LabSession`s. For each one it creates two PVCs, the SSH-key
  Secret, the pod, a Service (`lab-service-<id>`) and an Ingress, then publishes each phase
  change to RabbitMQ.
- **The lab session pod** runs two init containers (`init-rootfs` writes the VM's root
  filesystem, `network-setup` writes the VM's addresses), then three containers:
  `firecracker-vm` runs the Firecracker microVM, unprivileged but with `NET_ADMIN`, `SYS_ADMIN`
  and `SYS_RESOURCE` and with `/dev/kvm` and `/dev/net/tun` from the `dozlab.io/kvm` and
  `dozlab.io/tun` resources; `terminal-sidecar` bridges a WebSocket to SSH on port 8081; and
  `code-server` serves VS Code on port 8080.
- **RabbitMQ** brings phase changes and notifications back to this service, which pushes them to
  the browser on `/api/v1/ws` (see "Event bus" below).

### Starting a lab session

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/session-start-dark.png">
  <img alt="Sequence of starting a lab session: the browser posts to dozlab-api, which creates a LabSession and answers 202; the controller creates the pod and marks the session Running with its URLs; the browser asks every 3 seconds until it gets the endpoints, then opens a terminal WebSocket to the pod" src="docs/diagrams/session-start.png">
</picture>

`POST /api/v1/lab-sessions` checks that the caller may start the lab, refuses a second open
session for the same lab (409), saves the session as pending, creates the `LabSession` and
answers `202`. The controller does the rest. The frontend's workspace page then calls
`GET /api/v1/lab-sessions/:id` every 3 seconds; each call reads the `LabSession`'s status from
Kubernetes. Once the session is `Running` with its endpoints, the page opens the terminal
WebSocket straight to the pod.

The diagrams in this README are PNGs in `docs/diagrams/`, in light and dark versions.

## 📋 Quick Start

### **Development Setup**
The server entrypoint is `cmd/api`. It needs PostgreSQL (with the schema applied) and RabbitMQ.

```bash
cd dozlab-api

# Start PostgreSQL + RabbitMQ (management UI on http://localhost:15672, guest/guest)
docker run -d --name dozlab-pg -e POSTGRES_PASSWORD=password -e POSTGRES_DB=dozlab -p 5432:5432 postgres:16-alpine
docker run -d --name dozlab-rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Apply the schema (the server does not run migrations)
for f in internal/database/migrations/*.up.sql; do docker exec -i dozlab-pg psql -U postgres -d dozlab < "$f"; done

# Configure and run
export JWT_SECRET=change-me-to-a-long-random-secret
export DB_HOST=localhost DB_NAME=dozlab DB_USER=postgres DB_PASSWORD=password
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
# The audit log needs a database of its own ("Access control and audit log" below). To try the
# API without one, entries can go to the server log instead:
export AUDIT_REQUIRED=false
go run ./cmd/api

# Or build a binary
go build -o bin/api ./cmd/api && ./bin/api

# API will be available at:
# Main API: http://localhost:8080
# Health Check: http://localhost:8080/health
# Swagger Docs: http://localhost:8080/swagger/index.html
```

Settings read by `cmd/api` (see `internal/config`):

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `JWT_SECRET` | — | Required; the server exits without it |
| `DATABASE_URL` | — | Full Postgres URL; if unset it is built from `DB_*` |
| `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD` | port `5432` | Used when `DATABASE_URL` is unset |
| `DB_SSLMODE` | `disable` | Used when `DATABASE_URL` is unset |
| `RABBITMQ_URL` | — | Required; the server exits without it. AMQP URL of the event bus broker |
| `RABBITMQ_PREFETCH` | `10` | Unacked deliveries per consumer |
| `RABBITMQ_MAX_RETRIES` | `5` | Retries of a failed event on the shared queue before it goes to `dozlab.events.dlq`. Nothing consumes the shared queue yet |
| `RABBITMQ_RETRY_DELAY` | `10s` | Wait in the retry queue before redelivery. Changing it for an existing deployment requires deleting `dozlab.events.dozlab-api.retry` first (RabbitMQ rejects redeclaring a queue with different arguments) |
| `KUBECONFIG` | `~/.kube/config` | In-cluster config is tried first. Without either, the server still starts but lab session routes are disabled |

The server shuts down gracefully on SIGINT/SIGTERM (15 s drain).

#### Event bus (RabbitMQ)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/event-flow-dark.png">
  <img alt="Event flow: the controller publishes phase changes and the API publishes notifications to the dozlab.events exchange, which copies each event into a queue per API process; that process's handler acks it and pushes it to the browser, or sends it to the dead-letter queue if handling fails" src="docs/diagrams/event-flow.png">
</picture>

`EventBusService` publishes and consumes through RabbitMQ (`internal/messaging`). The exchange and
the DLQ are declared on startup, and each queue when something subscribes to it. Declaring is
idempotent.

- `dozlab.events`: durable topic exchange; the routing key is the event type (e.g. `session.created`).
- `dozlab.events.dozlab-api.instance.<id>`: per-process queue for events every replica must see
  (`SubscribeInstance`). This is the queue the API consumes today, for `notification` and
  `labsession.phase_changed`; see the notifications section below.
- `dozlab.events.dozlab-api`: durable quorum queue shared by this service's replicas
  (`Subscribe`), with `dozlab.events.dozlab-api.retry`, where failed messages wait for
  `RABBITMQ_RETRY_DELAY` and then dead-letter back to `dozlab.events` with key
  `retry.dozlab-api`. Nothing calls `Subscribe` yet, so these two queues aren't declared and no
  event is retried today.
- `dozlab.events.dlq`: messages that failed (after `RABBITMQ_MAX_RETRIES` retries on the shared
  queue, or at once on an instance queue), with headers `x-dozlab-last-error`,
  `x-dozlab-consumer-group` and `x-dozlab-original-routing-key`.

Messages are persistent and published with confirms; a delivery is acked only after its
handlers succeed. The bus reconnects with backoff if the connection drops. Handlers may run
more than once for one event, so they should be idempotent.

`POST /api/v1/proxy/notifications` publishes a `notification` event (routing key
`notification`) with `user_id`/`session_id` from the request, and `type`, `message`, `data` and
the caller's `sender_id` in `data`. It returns 200 with the `event_id` once the broker confirms
the message, and 503 if publishing fails.

The API consumes them itself and pushes each event to the target user's open connections on
`GET /api/v1/ws`. Each API process has its own queue, `dozlab.events.dozlab-api.instance.<id>`
(`<id>` is `<hostname>-<random>`), bound to `notification`, so every replica receives every
notification and delivers it to the connections it holds. The queue is a non-durable classic
queue: it survives a reconnect, and the broker deletes it 2 minutes after its process stops
consuming. Delivery is best-effort. If the user isn't connected, the event is acked and dropped,
and a failed handler sends the event to `dozlab.events.dlq` without retries.

LabSession phase changes published by dozlab-controller (`labsession.phase_changed`, see its
README) go to the same instance queue. Each one becomes a `session_status` message on the
owner's (`user_id`) connections:

```json
{"type": "session_status", "session_id": "...", "timestamp": 1790546400,
 "data": {"status": "running", "phase": "Running", "session_id": "...", "user_id": "...",
          "event_id": "<labsession uid>.Running", "message": "Lab session is running",
          "endpoints": {"terminal": "..."}, "reason": "(if set)"}}
```

`status` is the lowercased phase (`pending`, `creating`, `running`, `failed`, `terminating`).
The controller can publish a phase twice with the same `event_id`, so clients should ignore repeats.

Connecting to `/api/v1/ws`: browsers can't set `Authorization` on a WebSocket, so they pass the
JWT as a subprotocol after the `dozlab.bearer` sentinel, and the server echoes only
`dozlab.bearer`. Other clients can use `Authorization: Bearer <JWT>`. The options and
trade-offs are in `docs/decision.md`.

```js
const ws = new WebSocket("ws://localhost:8080/api/v1/ws", ["dozlab.bearer", accessToken]);
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data); // {type: "notification", session_id, data: {id, type, message, data, sender_id}, timestamp}
};
```

Run the RabbitMQ integration tests with `RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test ./internal/messaging/`;
without `RABBITMQ_URL` they are skipped.

### **Kubernetes Setup**
```bash
# Ensure Kubernetes cluster is running (minikube, kind, etc.)
kubectl cluster-info

# Create namespace for labs
kubectl create namespace dozlab-labs

# Configure API to use Kubernetes
export KUBECONFIG=$HOME/.kube/config

# Deploy a lab session
curl -X POST http://localhost:8080/api/v1/k8s/deploy \
  -H "Authorization: Bearer <jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "lab_id": "550e8400-e29b-41d4-a716-446655440000",
    "session_id": "123e4567-e89b-12d3-a456-426614174000"
  }'
```

### **Local Development (Without Kubernetes)**
```bash
# Run without a Kubernetes config: the API starts, lab session routes are disabled
KUBECONFIG=/dev/null go run ./cmd/api

# Test API endpoints
go test ./internal/... -v
```


## 🔧 Configuration

DozLab API configuration for sidecar architecture:

```bash
# Database Configuration
DB_HOST=localhost
DB_PORT=5432
DB_NAME=dozlab
DB_USER=postgres
DB_PASSWORD=password

# JWT Authentication
JWT_SECRET=your-jwt-secret-key
JWT_EXPIRY=24h

# Kubernetes Configuration
KUBECONFIG=/path/to/kubeconfig
K8S_NAMESPACE=dozlab-labs
K8S_ENABLED=true

# Sidecar Images
INITRD_IMAGE=your-initrd:latest
TERMINAL_SIDECAR_IMAGE=your-terminal-sidecar:latest
VSCODE_IMAGE=codercom/code-server:latest

# Network Configuration
ALLOWED_ORIGINS=http://localhost:3000,https://yourdomain.com

# Audit store: a database of its own. The API writes with a login that may only add entries
AUDIT_DATABASE_URL=postgres://dozlab_api_audit:...@audit-db:5432/dozlab_audit
# ... and the admin endpoint reads with one that may only read. Unset: the endpoint is off
AUDIT_READ_DATABASE_URL=postgres://dozlab_audit_admin:...@audit-db:5432/dozlab_audit
# Where entries wait while the store is unreachable; must survive a restart. Unset: a change
# is refused while the store is down
AUDIT_SPOOL_DIR=/var/lib/dozlab/audit-spool
# Refuse a change (503) when it can't be recorded. false: only log the failed write, and run
# without AUDIT_DATABASE_URL (entries go to the server log)
AUDIT_REQUIRED=true
# Also record every successful read (default: changes, refused and failed requests, and
# sensitive reads only)
AUDIT_READS=false
```

### Access control and audit log

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/access-checks-dark.png">
  <img alt="Flowchart of the access checks: a valid token, an active user whose role is read from the database, a role with the route's permission in the authz table, and for one record ownership or manage_any; each failed check ends in 401, 403 or 404" src="docs/diagrams/access-checks.png">
</picture>

A protected request passes four checks in order: a valid token (401), a user who still exists and
is active, with the role read from the database on every request (401), a role that has the
route's permission (403), and, in the handler, for one record, that it is the caller's own or the
caller has the `manage_any` permission (403 for labs, 404 for sessions).

- **Access control:** `internal/authz/authz.go` is the one table of which role has which
  permission, and `internal/api/routes.go` names the permission each route needs.
- **Audit log:** `internal/audit` records who did what, to which record, from where and when,
  in a database apart from the app's. The app's database has no audit table (migration 004).

Setting up the audit store, as a PostgreSQL superuser and then as the store's owner:

```bash
# 1. A database with its own owner (ideally on its own server)
psql -c "CREATE ROLE dozlab_audit_owner LOGIN CREATEROLE PASSWORD '...'" \
     -c "CREATE DATABASE dozlab_audit OWNER dozlab_audit_owner"
# 2. The table, the append-only triggers, the retention and the two roles, as the owner
psql -U dozlab_audit_owner -d dozlab_audit -f internal/database/audit_migrations/001_audit_store.up.sql
# 3. One login that may only add entries (the API) and one that may only read them
psql -c "CREATE ROLE dozlab_api_audit LOGIN PASSWORD '...' IN ROLE dozlab_audit_writer" \
     -c "CREATE ROLE dozlab_audit_admin LOGIN PASSWORD '...' IN ROLE dozlab_audit_reader"
```

Entries are kept 30 days. The store's owner changes that, the API can't:
`UPDATE audit_settings SET retention_days = 365;` (at least 30, or 0 to keep them for ever).

The store's rules are grants, triggers and a function, so their test needs a real PostgreSQL
with the three logins: `AUDIT_TEST_WRITER_URL=... AUDIT_TEST_READER_URL=... AUDIT_TEST_OWNER_URL=...
go test ./internal/audit/`. `scripts/e2e-timing.sh up` sets the store up for local runs.

See `docs/decision.md`, "Enterprise readiness", for what is and isn't covered.

## 🧪 Testing

```bash
# Run all tests
go test ./... -v

# Test specific components
go test ./internal/kubernetes/... -v
go test ./internal/api/handlers/... -v
go test ./internal/websocket/... -v

# Integration tests with Kubernetes
go test ./test/integration/... -v

# Test sidecar deployment
kubectl apply -f test/fixtures/test-lab-pod.yaml
```

## 📦 Deployment

### **Development Environment**

```bash
# Local development with minikube
minikube start
kubectl create namespace dozlab-labs

# Deploy API
docker build -t dozlab-api:dev .
kubectl apply -f k8s/development/

# Or run locally with external K8s
export KUBECONFIG=$HOME/.kube/config
go run ./cmd/api
```

### **Production Kubernetes**

```bash
# Production deployment to EKS/GKE/AKS
kubectl apply -f k8s/production/

# Scale API deployment
kubectl scale deployment dozlab-api --replicas=3

# Monitor sidecar pods
kubectl get pods -n dozlab-labs -l app=lab-environment
```

## 🔌 Inter-Service Communication

### **HTTP API Calls** (Data Operations)
```go
// Worker calls API service
client := NewAPIClient("http://dozlab-api:8080")
job, err := client.GetPendingJobs()

// Examiner calls API service  
rules, err := client.GetValidationRules(labID)
client.SubmitValidationResult(result)
```

### **RabbitMQ** (Event-Driven)
Events go through the `dozlab.events` topic exchange; see "Event bus (RabbitMQ)" above.

## 📁 Project Structure

```
cmd/
├── api/           # Central API service
│   ├── config.go
│   ├── api_client_test.go
│   └── ...
├── websocket/     # Real-time communication
│   ├── config.go
│   ├── dto.go
│   ├── api_client.go
│   └── config_test.go
├── worker/        # Background job processing
├── examiner/      # Auto-validation engine
├── filesystem/    # File management + terminal
└── workflow/      # Task orchestration

docker/
├── Dockerfile.api
├── Dockerfile.websocket
├── Dockerfile.worker
├── Dockerfile.examiner
├── Dockerfile.filesystem
└── Dockerfile.workflow

migrations/
├── 001_initial_schema.up.sql
└── 002_lab_init_image.up.sql   # labs.init_image: the lab's rootfs init image

docker-compose.dev.yml    # Development setup
api-contracts.md         # Service communication specs
COMMUNICATION_FLOWS.md   # Detailed architecture flows
```

## 🔍 Key Features

### **True Microservice Benefits**
- ✅ **Independent Deployment**: Each service scales separately
- ✅ **No Shared Code**: Services communicate over HTTP/RabbitMQ only
- ✅ **Technology Freedom**: Each service can use different languages
- ✅ **Fault Isolation**: One service failure doesn't crash others
- ✅ **Team Independence**: Different teams can own different services

### **Educational Platform Features**
- 🎓 **Interactive Labs**: Hands-on technical exercises
- 🤖 **Auto-validation**: 15+ validation rule types
- 📊 **Real-time Feedback**: Progressive scoring and hints  
- 💻 **Web Terminal**: Browser-based coding environment
- 🔄 **Workflow Engine**: Complex lab task orchestration
- 👥 **Collaboration**: File sharing and real-time updates

## 📚 Documentation

- **[API Contracts](./api-contracts.md)**: HTTP endpoints and events
- **[Communication Flows](./COMMUNICATION_FLOWS.md)**: Detailed service interactions  
- **[Progress Today](./progress.today)**: Development progress and roadmap

## 🤝 Contributing

1. **Service-Specific Changes**: Work in `cmd/[service-name]/`
2. **Add Tests**: Every service change needs tests
3. **Update Docs**: Keep API contracts and flows updated
4. **Docker**: Test with `docker-compose up --build`

## 📈 Monitoring & Observability

Each service exposes:
- **Health Checks**: `/health` endpoints
- **Metrics**: Prometheus-compatible metrics
- **Logging**: Structured JSON logs
- **Tracing**: Distributed tracing ready

---

**DozLab**: Cloud-native education platform built for scale, reliability, and developer experience. 🚀

## End-to-end timing

`scripts/e2e-timing.sh` starts a lab session through the API the way a user does and times
every stage, from `POST /api/v1/lab-sessions` to the VM answering SSH and the API reporting
`Running`, then from `DELETE` to the pod, LabSession and SSH key Secret being gone.

```bash
export KUBECONFIG=~/.kube/dozlab-local.yaml
scripts/e2e-timing.sh up          # migrations, Postgres and RabbitMQ port-forwards, API
scripts/e2e-timing.sh run all     # vm and k8s labs (or: run vm / run k8s)
scripts/e2e-timing.sh down
```

The database is the cluster's Postgres (`dozlab-infra/postgres.yaml`). The script connects to
it and applies new migrations; it never creates or removes it, so users and labs are kept.

Each lab is seeded as a published row with its own init image (`E2E_IMAGE_VM`,
`E2E_IMAGE_K8S`). The run fails fast if the pod doesn't run that image. Each stage is
appended to `$TIMINGS` as one JSON line (`~/.dozlab-local/timings.jsonl` when it exists, the
same format as dozlab.sh's lab timings).

