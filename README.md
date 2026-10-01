# DozLab API - Kubernetes Sidecar Lab Platform

A cloud-native educational platform with **Kubernetes sidecar architecture** for hands-on technical labs featuring real VM environments, integrated terminals, and VS Code access.

## 🏗️ Architecture Overview

DozLab uses a **Kubernetes sidecar pattern** with microservice API managing multi-container lab environments:

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  Web Browser    │────│  Load Balancer   │────│  DozLab API     │
└─────────────────┘    └──────────────────┘    │  (Port 8080)    │
                                               │                 │
┌─────────────────┐                           │ • Authentication│
│   VS Code Web   │────┐                      │ • Lab Management│
└─────────────────┘    │                      │ • K8s Deployment│
                       │                      │ • WebSocket Proxy│
┌─────────────────┐    │                      └─────────────────┘
│Web Terminal (WS)│────┘                               │
└─────────────────┘                                   │
                                                       ▼
                                          ┌─────────────────────┐
                                          │  Kubernetes Cluster │
                                          │                     │
    ┌─────────────────────────────────────│  Lab Session Pod    │─────────────────────────┐
    │                                     │                     │                         │
    │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
    │  │  InitContainer  │  │   Main VM       │  │Terminal Sidecar │  │  VS Code        │ │
    │  │  (IP Calculator)│  │(initrd/firecracker)│  │   (Port 8081)   │  │   Sidecar       │ │
    │  │                 │  │   (Port 22)     │  │                 │  │  (Port 8080)    │ │
    │  │ • Pod IP calc   │  │ • VM runner     │  │ • SSH to VM     │  │ • Code editing  │ │
    │  │ • VM IP setup   │  │ • Firecracker   │  │ • WebSocket     │  │ • File browser  │ │
    │  │ • Network config│  │ • User workload │  │ • Terminal I/O  │  │ • Integrated    │ │
    │  └─────────────────┘  └─────────────────┘  └─────────────────┘  │   terminal      │ │
    │                                                                 └─────────────────┘ │
    │                                                                                     │
    │  ┌─────────────────────────────────────────────────────────────────────────────┐   │
    │  │                           Shared Volumes                                    │   │
    │  │  • /shared/network-config (Pod/VM IP coordination)                         │   │
    │  │  • /vm-data (VM filesystem access)                                         │   │
    │  │  • /workspace (VS Code workspace)                                          │   │
    │  └─────────────────────────────────────────────────────────────────────────────┘   │
    └─────────────────────────────────────────────────────────────────────────────────────┘
                                          │                     │
                                          ▼                     ▼
                                 ┌─────────────────┐   ┌──────────────────┐
                                 │ Kubernetes      │   │ PostgreSQL +     │
                                 │ Service         │   │ Redis            │
                                 │ (Load Balancer) │   │                  │
                                 │ • Port 8080     │   │ • Sessions       │
                                 │ • Port 8081     │   │ • Lab specs      │
                                 │ • Port 22       │   │ • User data      │
                                 └─────────────────┘   └──────────────────┘
```

## 🚀 Sidecar Architecture Components

### **DozLab API Service** (Kubernetes Orchestrator)
- **Lab Deployment**: Creates multi-container pods with sidecar architecture
- **Kubernetes Integration**: Direct K8s API for pod/service management
- **Authentication**: JWT-based security and session management
- **WebSocket Proxy**: Routes terminal connections to sidecar containers
- **Service Discovery**: Generates endpoints for all lab services

### **Lab Session Pod** (Multi-Container Architecture)

#### **Init Container** (Network Setup)
- **IP Calculation**: Determines VM IP from pod IP
- **Network Configuration**: Sets up shared network config for all containers
- **Resource**: Lightweight busybox container

#### **Main VM Container** (User Workload)
- **Firecracker/initrd**: Runs actual user virtual machine
- **Privileged Access**: Required for VM operations
- **SSH Server**: Port 22 for direct VM access
- **Resources**: 1-2 CPU, 3-4GB RAM

#### **Terminal Sidecar** (Port 8081)
- **SSH Proxy**: Connects to VM via internal SSH
- **WebSocket Bridge**: Real-time terminal I/O over WebSocket
- **Session Management**: Per-session terminal connections
- **VM Discovery**: Auto-detects VM IP from shared config

#### **VS Code Sidecar** (Port 8080)
- **Code Server**: Web-based VS Code interface
- **VM Integration**: Direct access to VM filesystem
- **File Synchronization**: Real-time file editing
- **Password Protected**: Auto-generated session passwords

### **Kubernetes Services**
- **Load Balancing**: Routes traffic to correct sidecar containers
- **Service Discovery**: Consistent naming `lab-service-{SESSION_ID}`
- **Port Mapping**: 8080→VS Code, 8081→Terminal, 22→SSH
- **Session Isolation**: Each lab session gets dedicated service

### **Shared Resources**
- **Network Config**: Pod/VM IP coordination via shared volume
- **VM Data**: VM filesystem accessible to VS Code
- **Workspace**: Persistent user workspace data

## 📋 Quick Start

### **Development Setup**
The server entrypoint is `cmd/api`. It needs PostgreSQL (with the schema applied), Redis and RabbitMQ.

```bash
cd dozlab-api

# Start PostgreSQL + Redis + RabbitMQ (management UI on http://localhost:15672, guest/guest)
docker run -d --name dozlab-pg -e POSTGRES_PASSWORD=password -e POSTGRES_DB=dozlab -p 5432:5432 postgres:16-alpine
docker run -d --name dozlab-redis -p 6379:6379 redis:7-alpine
docker run -d --name dozlab-rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Apply the schema (the server does not run migrations)
for f in internal/database/migrations/*.up.sql; do docker exec -i dozlab-pg psql -U postgres -d dozlab < "$f"; done

# Configure and run
export JWT_SECRET=change-me-to-a-long-random-secret
export DB_HOST=localhost DB_NAME=dozlab DB_USER=postgres DB_PASSWORD=password
export REDIS_HOST=localhost
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
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
| `REDIS_URL` | — | Full Redis URL; otherwise `REDIS_ADDR`, or `REDIS_HOST:REDIS_PORT` (port `6379`) |
| `REDIS_PASSWORD`, `REDIS_DB` | db `0` | |
| `RABBITMQ_URL` | — | Required; the server exits without it. AMQP URL of the event bus broker |
| `RABBITMQ_PREFETCH` | `10` | Unacked deliveries per consumer |
| `RABBITMQ_MAX_RETRIES` | `5` | Retries of a failed event before it goes to `dozlab.events.dlq` |
| `RABBITMQ_RETRY_DELAY` | `10s` | Wait in the retry queue before redelivery. Changing it for an existing deployment requires deleting `dozlab.events.dozlab-api.retry` first (RabbitMQ rejects redeclaring a queue with different arguments) |
| `KUBECONFIG` | `~/.kube/config` | In-cluster config is tried first. Without either, the server still starts but lab session routes are disabled |

The server shuts down gracefully on SIGINT/SIGTERM (15 s drain).

#### Event bus (RabbitMQ)

`EventBusService` publishes and consumes through RabbitMQ (`internal/messaging`). Redis still
stores events that have a TTL (`GetEvent`) and the per-session event streams. The topology is
declared on startup and is idempotent:

- `dozlab.events`: durable topic exchange; the routing key is the event type (e.g. `session.created`).
- `dozlab.events.dozlab-api`: durable quorum queue for this service, bound to the event types it subscribes to.
- `dozlab.events.dozlab-api.retry`: failed messages wait here for `RABBITMQ_RETRY_DELAY`, then
  dead-letter back to `dozlab.events` with key `retry.dozlab-api`, so they return only to this service.
- `dozlab.events.dozlab-api.instance.<id>`: per-process queue for events every replica must see
  (`SubscribeInstance`); see the notifications section below.
- `dozlab.events.dlq`: messages that failed `RABBITMQ_MAX_RETRIES` times (headers
  `x-dozlab-last-error`, `x-dozlab-consumer-group`, `x-dozlab-original-routing-key`).

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

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379

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

# Audit log: also record every successful read (default: changes, refused and failed
# requests, and sensitive reads only)
AUDIT_READS=false
```

### Access control and audit log

- **Access control:** `internal/authz/authz.go` is the one table of which role has which
  permission, and `internal/api/routes.go` names the permission each route needs.
- **Audit log:** `internal/audit` records who did what, to which record, from where and when in
  the `audit_logs` table. Migration `004_audit_log.up.sql` must be applied: it adds the columns
  and makes the table append-only. Admins read the log at `GET /api/v1/admin/audit-logs`.
- The append-only rule is a PostgreSQL trigger, so its test needs a real database:
  `AUDIT_TEST_DATABASE_URL=postgres://... go test ./internal/audit/` (migrations applied).

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

### **Redis Pub/Sub** (Event-Driven)
```go
// Examiner publishes score update
redis.PublishScoreUpdate(ctx, userID, labID, scoreData)

// WebSocket subscribes to score updates
redis.Subscribe(ctx, func(event RedisEvent) {
    // Send real-time update to user
    websocket.Send(event.Data)
})
```

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
│   ├── redis_client.go
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
- ✅ **No Shared Code**: Services communicate over HTTP/Redis only
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

- **[API Contracts](./api-contracts.md)**: HTTP endpoints and Redis events
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
scripts/e2e-timing.sh up          # Postgres + Redis in Docker, migrations, RabbitMQ port-forward, API
scripts/e2e-timing.sh run all     # vm and k8s labs (or: run vm / run k8s)
scripts/e2e-timing.sh down
```

Each lab is seeded as a published row with its own init image (`E2E_IMAGE_VM`,
`E2E_IMAGE_K8S`). The run fails fast if the pod doesn't run that image. Each stage is
appended to `$TIMINGS` as one JSON line (`~/.dozlab-local/timings.jsonl` when it exists, the
same format as dozlab.sh's lab timings).

