# DozLab API Contracts - Kubernetes Sidecar Architecture

## Communication Architecture

DozLab implements **Kubernetes sidecar architecture** with:
- **DozLab API**: Central orchestrator managing Kubernetes deployments
- **Sidecar Containers**: Multi-container pods for each lab session
- **WebSocket Proxying**: Real-time terminal connections through API
- **Service Discovery**: Kubernetes services for lab session access

## Sidecar Architecture Communication

### 1. DozLab API Service (Port 8080) - Kubernetes Orchestrator
**Manages**: Kubernetes pod/service lifecycle, authentication, database access
**Creates**: Multi-container pods with sidecar architecture
**Exposes**: REST and WebSocket proxy endpoints

**Core Responsibilities**:
- Lab session deployment to Kubernetes
- WebSocket terminal proxying to sidecar containers
- Service discovery and endpoint generation
- Authentication and session management

### 2. Lab Session Pod (Multi-Container)

#### Init Container (busybox)
**Function**: Network configuration setup
**Outputs**: Pod IP, calculated VM IP to shared volume
**Lifecycle**: Runs once, then exits

#### Main VM Container (initrd/firecracker)
**Port**: 22 (SSH)
**Function**: Runs user's virtual machine workload
**Resources**: 1-2 CPU, 3-4GB RAM
**Security**: Privileged container for VM operations

#### Terminal Sidecar (Port 8081)
**Function**: SSH proxy to VM + WebSocket bridge
**Communication**: 
- SSH → VM container (internal)
- WebSocket → DozLab API → Client browser
**Resources**: 250m CPU, 256MB RAM

#### VS Code Sidecar (Port 8080)
**Function**: Web-based code editor with VM filesystem access
**Features**: File editing, integrated terminal, workspace management
**Authentication**: Password-based (auto-generated)
**Resources**: 500m CPU, 1GB RAM

### 3. Kubernetes Services
**Naming**: `lab-service-{SESSION_ID}`
**Ports**: 8080→VS Code, 8081→Terminal, 22→SSH
**Function**: Load balancing and service discovery for sidecar containers

### 4. Shared Resources
**Network Config**: `/shared/network-config` (Pod/VM IP coordination)
**VM Data**: `/vm-data` (VM filesystem accessible to sidecars)
**VS Code Workspace**: `/workspace` (persistent user files)

## HTTP API Endpoints

### Authentication & User Management
- `POST /api/v1/auth/register` - User registration
- `POST /api/v1/auth/login` - User authentication
- `POST /api/v1/auth/refresh` - Refresh JWT token. The new tokens carry the user's current role, username and email; a deactivated or deleted user gets 401
- `GET /api/v1/users/profile` - Get user profile
- `PUT /api/v1/users/profile` - Update user profile

### Lab Management
- `GET /api/v1/labs` - List available labs
- `POST /api/v1/labs` - Create new lab (instructor or admin; a student gets 403)
- `GET /api/v1/labs/{labId}` - Get lab details
- `PUT /api/v1/labs/{labId}` - Update lab (its creator, if an instructor or admin; or any admin)
- `DELETE /api/v1/labs/{labId}` - Delete lab (its creator, if an instructor or admin; or any admin)

- `GET /api/v1/labs/{labId}/estimate` - What one VM of the lab reserves and stores (instructor, admin)

The estimate is for showing an instructor what a VM takes before it is created. CPU is in
thousandths of a core and memory and storage in MiB:

```json
{
  "lab_id": "...",
  "estimate": {
    "persistence": "none",
    "vm": {"vcpus": 1, "memory_mib": 512, "disk_gib": 1},
    "containers": [
      {"name": "firecracker-vm", "purpose": "runs the VM",
       "reserved": {"cpu_millicores": 100, "memory_mib": 640},
       "maximum": {"cpu_millicores": 1000, "memory_mib": 640}},
      {"name": "terminal-sidecar", "...": "..."},
      {"name": "code-server", "...": "..."}
    ],
    "total": {"reserved": {"cpu_millicores": 850, "memory_mib": 1920},
              "maximum": {"cpu_millicores": 2500, "memory_mib": 3200}},
    "devices": {"dozlab.io/kvm": 1, "dozlab.io/tun": 1},
    "storage": {
      "while_running": [{"name": "vm-kernels", "purpose": "the VM's disk", "kind": "node-local", "size_mib": 2048}, "..."],
      "volumes_mib": 6144,
      "node_local_limit_mib": 2058,
      "after_stop_mib": 0
    }
  }
}
```

`reserved` is what the cluster sets aside (it limits how many VMs fit on a node); `maximum` is
the most the VM may use. `persistence` (query parameter) accepts only `none` today. The numbers
mirror dozlab-controller (`internal/estimate`).

VM size is set on the lab, and every session of the lab gets it; a session request can't set
it. Lab fields (create and update): `vm_vcpus` (1–8, default 1), `vm_memory_mib` (256–16384,
default 512) and `vm_disk_gib` (1–100, default 1). The API passes them to the LabSession as
`spec.resources` (`cpu`, `memory` in Mi, `storage` in Gi), which the controller uses as the VM's
vCPUs, memory and disk. See `docs/decision.md`, "Keeping resources to a minimum".

### Lab Specifications (Sidecar Support)
- `GET /api/v1/labs/{labId}/specs` - Get all lab specs
- `POST /api/v1/labs/{labId}/specs` - Create new spec version (the lab's creator, or an admin)
- `GET /api/v1/labs/{labId}/specs/{version}` - Get specific spec version
- `PUT /api/v1/labs/{labId}/specs/{version}` - Update spec version (the lab's creator, or an admin)
- `DELETE /api/v1/labs/{labId}/specs/{version}` - Delete spec version (the lab's creator, or an admin)

### Session Management
- `GET /api/v1/sessions` - List user sessions
- `POST /api/v1/sessions` - Create new session
- `GET /api/v1/sessions/{id}` - Get session details
- `PUT /api/v1/sessions/{id}/status` - Update session status (admin)
- `DELETE /api/v1/sessions/{id}` - End session

### Lab Sessions (VMs)
- `POST /api/v1/lab-sessions` - Start a session of a lab (`lab_id`)
- `GET /api/v1/lab-sessions` - List the caller's sessions
- `GET /api/v1/lab-sessions/{id}` - Session details and cluster status, including `k8s_status.usage`
- `GET /api/v1/lab-sessions/usage` - What the caller's VMs hold in the cluster, per VM and per lab (instructor, admin)
- `DELETE /api/v1/lab-sessions/{id}` - End a session

Usage is what a VM holds in the cluster, as dozlab-controller reports it in the LabSession's
`status.usage`: CPU in thousandths of a core, memory and storage in MiB. They are the amounts
set aside for the VM, not what it consumes. A VM holds its CPU and memory while `running` is
true, and its storage until the session is deleted. `usage` is absent (`null` in the report)
until the controller has reported it.

```json
{
  "labs": [
    {"lab_id": "...", "lab_name": "Docker basics",
     "vms": [
       {"session_id": "...", "user_id": "...", "status": "running", "phase": "Running", "created_at": "...",
        "usage": {"running": true, "cpu_millicores": 850, "memory_mib": 1920, "storage_mib": 6144}}
     ],
     "total": {"vms": 2, "running_vms": 1, "cpu_millicores": 850, "memory_mib": 1920, "storage_mib": 12288}}
  ],
  "total": {"vms": 3, "running_vms": 1, "cpu_millicores": 850, "memory_mib": 1920, "storage_mib": 12288}
}
```

The report covers the caller's own VMs, grouped by lab; an admin gets everyone's. Totals count
CPU and memory for running VMs only, and storage for all of them.

Session options are for instructors and admins. A student who sends `timeout` or any of
`config.enable_terminal`, `config.enable_vscode`, `config.enable_ssh` gets 403, with the option
names in `options`. A student who sends none gets the defaults. Anyone may set
`config.vscode_password`: it is the caller's own credential, not an option. See
`docs/decision.md`, "What phase 1 needs in the API".

### Kubernetes Lab Deployment
- `POST /api/v1/k8s/deploy` - Deploy lab with sidecar architecture
- `POST /api/v1/k8s/validate` - Validate lab specification
- `GET /api/v1/k8s/environments` - List user lab environments
- `GET /api/v1/k8s/environments/{environment_id}` - Get environment status
- `DELETE /api/v1/k8s/environments/{environment_id}` - Terminate environment
- `GET /api/v1/k8s/environments/{environment_id}/logs` - Get environment logs
- `GET /api/v1/k8s/sessions/{session_id}/endpoints` - Get session service endpoints

### Host Capacity
- `POST /api/v1/host-check` - Check whether a host can run N lab sessions (auth required)

Request (`requests` uses the LabSession `resources` shape, Kubernetes quantities; `sessions`
defaults to 1, `require_kvm` to true, `headroom`, the fraction reserved for the OS/kubelet, to 0.2):

```json
{
  "requests": {"cpu": "500m", "memory": "2Gi", "storage": "10Gi"},
  "host": {"cpu_cores": 8, "memory": "32Gi", "free_storage": "200Gi", "kvm": true},
  "sessions": 1, "require_kvm": true, "headroom": 0.2
}
```

Response `200` (`ok` is false when the host falls short or a host field is unknown; CPU in cores,
memory/storage in GiB; `kvm` is omitted when not required). Invalid quantities or options give `400`.

```json
{
  "ok": true,
  "cpu": {"need": 0.5, "usable": 6.4, "pass": true},
  "memory_gib": {"need": 2, "usable": 25.6, "pass": true},
  "storage_gib": {"need": 10, "usable": 160, "pass": true},
  "kvm": {"need": true, "have": true, "pass": true},
  "unknown_fields": [],
  "max_sessions": 12
}
```

### Terminal Integration
- `GET /api/v1/terminal` - WebSocket terminal connection (upgrade)
- `GET /api/v1/terminal/{session_id}/status` - Get terminal session status
- `POST /api/v1/terminal/{session_id}/command` - Send command to terminal

### WebSocket Management
- `GET /api/v1/ws/connect` - WebSocket connection (upgrade)
- `GET /api/v1/ws/stats` - Get WebSocket statistics
- `GET /api/v1/ws/sessions/{session_id}/connections` - Get session connections
- `POST /api/v1/ws/sessions/{session_id}/message` - Send message to session
- `PUT /api/v1/ws/sessions/{session_id}/progress` - Update session progress

### Workflow Management
- `POST /api/v1/workflows` - Create new workflow
- `POST /api/v1/workflows/validate` - Validate workflow definition
- `POST /api/v1/workflows/execute` - Execute workflow
- `GET /api/v1/workflows/executions` - List workflow executions
- `GET /api/v1/workflows/executions/{execution_id}` - Get execution details
- `GET /api/v1/workflows/executions/{execution_id}/progress` - Get execution progress
- `POST /api/v1/workflows/executions/{execution_id}/stop` - Stop execution

### Admin Endpoints
- `GET /api/v1/admin/users` - List all users
- `PUT /api/v1/admin/users/{id}/role` - Update user role
- `PUT /api/v1/admin/users/{id}/status` - Update user status
- `GET /api/v1/admin/audit-logs` - Read the audit log, newest first

Audit log filters (query parameters): `user_id`, `action` (for example `users:update_role`,
`labs:create`, `auth:login`), `resource_type`, `resource_id`, `outcome` (`attempted`, `success`,
`denied`, `failure`), `request_id`, `ip_address`, `from` and `to` (RFC 3339), `page`, `limit` (up
to 200, default 50). The response is `{"audit_logs": [...], "pagination": {"page", "limit",
"total"}}`. The log is in a database of its own that the API can only add to. Entries can't be
changed or deleted by anyone; the store removes them after its retention (30 days by default).
Each entry has the user's ID, username and role at the time, `created_at` (when it happened) and
`received_at` (when the store got it). The endpoint answers 503 when the read-only connection
(`AUDIT_READ_DATABASE_URL`) isn't configured.

A change has two entries with the same `request_id`: `attempted`, written before it runs, and
the outcome. If the first can't be written, the change is refused with 503 `{"error": "The audit
log is unavailable, so this change was refused"}`. With a spool directory (`AUDIT_SPOOL_DIR`),
entries wait on disk while the store is unreachable and the change goes through.

### Access Control
Every route needs a permission, and each role has a fixed set (`internal/authz/authz.go`). A
caller without the permission gets 403 with `{"error": "Insufficient privileges", "permission":
"<name>"}`. The user's role and status are read from the database on every request, so a token
issued before a role change or deactivation doesn't keep the old rights; a deactivated or
deleted user gets 401.

| Role | May |
|---|---|
| `student` | read published labs and specs; create, read and end their own sessions; their own profile and progress |
| `instructor` | the above; create labs; update and delete their own labs; write lab specs; set session options |
| `admin` | the above; unpublished labs; anyone's labs and sessions; `PUT /sessions/{id}/status`; the admin endpoints |

See `docs/decision.md`, "Enterprise readiness".

## Authentication Flow

1. **User login** → API Service validates → Returns JWT token
2. **Service requests** → Include JWT in Authorization header
3. **API Service** → Validates JWT → Processes request
4. **Other services** → Never handle authentication directly

## Data Ownership

- **API Service**: Owns all persistent data (PostgreSQL)
- **Other Services**: Stateless, call API service for data
- **RabbitMQ**: Events between services (`dozlab.events`); nothing is kept there
- **No shared models**: Each service defines its own DTOs

## ✅ Implementation Status

### Sidecar Architecture Achieved
- **✅ Kubernetes Integration**: Full K8s API integration for pod/service management
- **✅ Multi-Container Pods**: Init container + VM + Terminal + VS Code sidecars
- **✅ WebSocket Proxying**: Real-time terminal connections through API proxy
- **✅ Service Discovery**: Auto-generated service endpoints for lab sessions  
- **✅ Network Coordination**: Shared volume networking between containers
- **✅ Resource Management**: Proper CPU/memory allocation per container
- **✅ Session Isolation**: Dedicated pods and services per lab session

### Testing & Validation
```bash
# Core API and Kubernetes integration tests
go test ./internal/kubernetes/... -v     ✅ PASS
go test ./internal/api/handlers/... -v   ✅ PASS
go test ./internal/websocket/... -v      ✅ PASS
go test ./internal/models/... -v         ✅ PASS

# Integration tests
go test ./test/integration/... -v        ✅ PASS

# Kubernetes deployment tests
kubectl apply -f test/fixtures/test-lab-pod.yaml ✅ PASS
```

### Sidecar Container Images
```yaml
# Container images used in sidecar architecture
images:
  initrd-vm: your-initrd:latest          # Main VM workload
  terminal-sidecar: your-terminal-sidecar:latest  # SSH proxy + WebSocket
  vscode-sidecar: codercom/code-server:latest     # Web-based code editor
  ip-calculator: busybox                 # Network setup init container
```

### Kubernetes Resources Created
```bash
# Per lab session deployment
- Pod: lab-session-{SESSION_ID}
  - InitContainer: ip-calculator (busybox)
  - Container: initrd-vm (privileged, VM workload)
  - Container: terminal-sidecar (SSH proxy)  
  - Container: code-server (VS Code web)
  - Volumes: shared-config, vm-data, vscode-data

- Service: lab-service-{SESSION_ID}
  - Port 8080 → VS Code web interface
  - Port 8081 → Terminal WebSocket
  - Port 22 → Direct SSH to VM
```

This architecture provides scalable, isolated lab environments with integrated development tools and real-time terminal access.