   <picture>
      <source media="(prefers-color-scheme: light)" srcset="https://cdn.jsdelivr.net/gh/SomeBlackMagic/stackman@master/docs/9cb7e16c-f057-4660-a96f-258fd980389d.png" width="350" alt="Stackman Logo">
      <img src="https://cdn.jsdelivr.net/gh/SomeBlackMagic/stackman@master/docs/2bcf0cd7-f652-4fd9-9c67-ea1dc4515ca8.png" width="350" alt="Cilium Logo">
   </picture>



![Build App](https://github.com/SomeBlackMagic/stackman/actions/workflows/build.yaml/badge.svg)
[![codecov](https://codecov.io/gh/SomeBlackMagic/stackman/graph/badge.svg?token=Y64PUNF9KD)](https://codecov.io/gh/SomeBlackMagic/stackman)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPL%203.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](go.mod)
[![Docker](https://img.shields.io/badge/Docker-Swarm-2496ED?logo=docker)](https://docs.docker.com/engine/swarm/)
[![Github Repo Size](https://img.shields.io/github/repo-size/SomeBlackMagic/stackman.svg)](https://github.com/SomeBlackMagic/stackman)
![GitHub Release](https://img.shields.io/github/v/release/SomeBlackMagic/stackman)

**stackman** - Docker Swarm stack orchestrator with health-aware deployment, intelligent rollback, and
Helm-like workflow for Docker Swarm.

A CLI tool written in Go that brings **zero-downtime deployments**, **real-time health monitoring**, and **automatic
rollback** to Docker Swarm, filling the gap between basic `docker stack deploy` and enterprise-grade deployment
automation.

---

## The Problem It Solves

Docker Swarm's `docker stack deploy` has a critical limitation: it returns **immediately** after submitting the
deployment request, without waiting for services to actually start, become healthy, or validate successful deployment.
This creates production risks:

### Problems with `docker stack deploy`

| Problem                    | Impact                                            | Example                                     |
|----------------------------|---------------------------------------------------|---------------------------------------------|
| **No validation**          | Deployments appear successful even when they fail | CI/CD marks green, but service crashes      |
| **No health awareness**    | Broken services go unnoticed until user reports   | Database migration fails, app starts anyway |
| **Manual rollback**        | No automatic recovery from bad deployments        | 3 AM page, manual investigation required    |
| **Silent failures**        | Task failures, health check failures ignored      | Service dies repeatedly, no alerts          |
| **No deployment tracking** | Can't tell when deployment actually completes     | Is it done? Is it healthy? Unknown.         |

### How stackman Solves This

`stackman` wraps Docker Swarm API with **deployment intelligence**:

✅ **Waits for deployment** - Monitors service updates until all tasks are running
✅ **Health validation** - Ensures all containers pass health checks before success
✅ **Automatic rollback** - Reverts to previous state on failure or timeout
✅ **Real-time visibility** - Streams logs, events, and health status during deployment
✅ **Production-ready** - Signal handling, proper exit codes, CI/CD integration
✅ **Task tracking** - Monitors old task shutdown and new task startup with version control

---

## How It Works

stackman follows a **deployment lifecycle** pattern with automatic safety mechanisms:

```
┌──────────────────────────────────────────────────────────────────┐
│ 1. PARSE & VALIDATE                                              │
│    • Parse docker-compose.yml                                    │
│    • Validate image tags (no :latest without --allow-latest)     │
│    • Convert compose spec to Swarm ServiceSpec                   │
└──────────────────────────────────────────────────────────────────┘
                              ↓
┌──────────────────────────────────────────────────────────────────┐
│ 2. SNAPSHOT (Rollback Preparation)                               │
│    • Capture current service specs (ServiceInspect)              │
│    • Store service versions and task states                      │
│    • Record resources (networks, volumes, secrets, configs)      │
└──────────────────────────────────────────────────────────────────┘
                              ↓
┌──────────────────────────────────────────────────────────────────┐
│ 3. DEPLOYMENT                                                    │
│    • Remove obsolete services (--prune)                          │
│    • Pull images with progress tracking                          │
│    • Create/update networks (overlay)                            │
│    • Create/update volumes (local)                               │
│    • Deploy services with unique DeployID label                  │
│    • Track service version changes                               │
└──────────────────────────────────────────────────────────────────┘
                              ↓
┌──────────────────────────────────────────────────────────────────┐
│ 4. HEALTH MONITORING (unless --no-wait)                          │
│    • Subscribe to Docker events (task lifecycle)                 │
│    • Start per-task monitors with log streaming                  │
│    • Track UpdateStatus.State → "completed"                      │
│    • Poll container health status (State.Health)                 │
│    • Wait for all tasks: Running + Healthy                       │
└──────────────────────────────────────────────────────────────────┘
                              ↓
                    ┌─────────────────┐
                    │ All healthy?    │
                    └─────────────────┘
                       ↙           ↘
                  YES               NO/TIMEOUT/SIGINT
                   ↓                 ↓
          ┌──────────────┐    ┌──────────────────┐
          │ ✅ SUCCESS   │    │ ⚠️ ROLLBACK      │
          │ Exit 0       │    │ • Restore specs  │
          └──────────────┘    │ • Revert versions│
                              │ • Wait healthy   │
                              │ Exit 1/2/130     │
                              └──────────────────┘
```

### Deployment Process Details

#### Phase 1: Pre-Deployment

- **Compose parsing** - YAML → internal model (no external compose libraries)
- **Path resolution** - Converts relative paths to absolute using `STACKMAN_WORKDIR`
- **Validation** - Checks `:latest` tag protection, required fields
- **Templating** - Applies `${VAR}` environment variable substitution

#### Phase 2: Snapshotting

- **ServiceInspect** - Captures current `Spec` and `Version.Index` for each service
- **Resource inventory** - Records existing networks, volumes, secrets, configs
- **Rollback readiness** - Ensures we can restore previous state on failure

#### Phase 3: Deployment Execution

1. **Image Pull** - Pre-pulls all images (respects `DOCKER_CONFIG_PATH` for auth)
2. **Resource Creation** - Networks → Volumes → Secrets → Configs (dependency order)
3. **Service Update** - Uses `ServiceUpdate` API with current `Version.Index`
4. **DeployID Injection** - Adds `com.stackman.deploy.id` label to all tasks for tracking

#### Phase 4: Health Monitoring

- **Event Subscription** - Listens to `type=task` events filtered by stack namespace
- **Task Watchers** - Spawns goroutine per task for log streaming and container inspection
- **UpdateStatus Tracking** - Waits for `UpdateStatus.State == "completed"`
- **Health Polling** - Periodic `ContainerInspect` checks `State.Health.Status == "healthy"`
- **DeployID Filtering** - Only monitors tasks with matching `com.stackman.deploy.id` label

#### Phase 5: Rollback (on failure)

- **Trigger Conditions**: Health timeout, task failures, SIGINT/SIGTERM
- **Restoration**: Applies previous `ServiceSpec` using saved `Version.Index`
- **Health Re-check**: Waits for rolled-back services to become healthy (with `--rollback-timeout`)

---

## Key Features

### 🚀 Deployment Intelligence

- ✅ **Health-aware deployment** - Waits for all services to become healthy before declaring success
- ✅ **Automatic rollback** - Reverts to previous state on failure, timeout, or interrupt (SIGINT/SIGTERM)
- ✅ **Version-aware updates** - Tracks service `Version.Index` to prevent update conflicts
- ✅ **DeployID tracking** - Injects unique deployment ID into all tasks for precise monitoring
- ✅ **Image pre-pull** - Pulls images before deployment with progress tracking
- ✅ **Dependency-ordered deployment** - Creates resources in correct order (networks → volumes → secrets → services)

### 🔍 Real-Time Monitoring

- ✅ **Event-driven architecture** - Subscribes to Docker events (`type=task`) for instant task lifecycle updates
- ✅ **Per-task monitoring** - Spawns dedicated watcher goroutine for each task with log streaming
- ✅ **UpdateStatus tracking** - Monitors `ServiceInspectWithRaw` → `UpdateStatus.State == "completed"`
- ✅ **Health polling** - Periodic `ContainerInspect` checks `State.Health.Status`
- ✅ **Failed task detection** - Reports task failures with exit codes and error messages
- ✅ **No healthcheck tolerance** - Services without healthchecks are considered healthy if running

### 📦 Compose File Support

- ✅ **Custom YAML parser** - No external compose libraries (only `gopkg.in/yaml.v3`)
- ✅ **Path resolution** - Converts relative paths (`./data`) to absolute using `STACKMAN_WORKDIR` or CWD
- ✅ **Environment substitution** - Supports `${VAR}` syntax for environment variables
- ✅ **Full Swarm spec mapping** - Converts `deploy.replicas`, `deploy.update_config`, `deploy.placement`, etc.
- ✅ **Resource support** - Networks (overlay), Volumes (local), Secrets, Configs (parsing implemented)
- ✅ **Healthcheck conversion** - Maps `healthcheck` to `ContainerSpec.Healthcheck`

### 🛡️ Safety & Reliability

- ✅ **Snapshot-based rollback** - Captures `ServiceInspect` before deployment for safe revert
- ✅ **Signal handling** - Intercepts SIGINT/SIGTERM → triggers rollback → exits with code 130
- ✅ **Timeout protection** - `--timeout` for deployment, `--rollback-timeout` for rollback
- ✅ **Image tag validation** - Blocks `:latest` tag unless `--allow-latest` is set
- ✅ **Idempotency** - Repeated applies without changes result in no-op
- ✅ **Concurrent-safe** - Handles multiple goroutines for task monitoring with mutexes

### 🔧 Operational Features

- ✅ **Multiple subcommands** - `apply`, `rollback`, `diff`, `status`, `logs`, `events`
- ✅ **CI/CD friendly** - Proper exit codes (0=success, 1=failure, 2=timeout, 130=interrupted)
- ✅ **TLS support** - Respects `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH`
- ✅ **Registry authentication** - Uses `DOCKER_CONFIG_PATH` for private registry auth (`config.json`)
- ✅ **Parallel updates** - `--parallel` flag for concurrent service updates (not yet fully implemented)
- ✅ **No external dependencies** - Only uses: `github.com/docker/docker`, `github.com/docker/go-units`,
  `golang.org/x/net`, `gopkg.in/yaml.v3`

---

## Installation

### Option 1: Download Pre-built Binary

```bash
# Linux (amd64)
wget https://github.com/SomeBlackMagic/stackman/releases/latest/download/stackman-linux-amd64
chmod +x stackman-linux-amd64
sudo mv stackman-linux-amd64 /usr/local/bin/stackman

# macOS (amd64)
curl -L https://github.com/SomeBlackMagic/stackman/releases/latest/download/stackman-darwin-amd64 -o stackman
chmod +x stackman
sudo mv stackman /usr/local/bin/stackman
```

### Option 2: Build from Source

```bash
git clone https://github.com/SomeBlackMagic/stackman.git
cd stackman
go build -o stackman .
sudo mv stackman /usr/local/bin/stackman
```

### Option 3: Using Make

```bash
make build          # Build binary
make install        # Install to /usr/local/bin
```

### Verify Installation

```bash
stackman version
```

---

## Usage

### Commands Overview

```bash
stackman <command> [flags]
```

#### Available Commands

| Command    | Description                           | Status        |
|------------|---------------------------------------|---------------|
| `apply`    | Deploy or update a stack              | ✅ Implemented |
| `rollback` | Rollback stack to previous state      | 🚧 Stub       |
| `diff`     | Show deployment plan without applying | 🚧 Stub       |
| `status`   | Show current stack status             | 🚧 Stub       |
| `logs`     | Show logs for stack services          | 🚧 Stub       |
| `events`   | Show events for stack services        | 🚧 Stub       |
| `version`  | Show version information              | ✅ Implemented |

### `apply` Command (Primary Usage)

Deploy or update a Docker Swarm stack with health monitoring and automatic rollback.

#### Basic Syntax

```bash
stackman apply -n <stack-name> -f <compose-file> [flags]
```

#### Flags

| Flag                 | Type     | Default        | Description                                       |
|----------------------|----------|----------------|---------------------------------------------------|
| `-n, --name`         | string   | **(required)** | Stack name                                        |
| `-f, --file`         | string   | **(required)** | Path to docker-compose.yml                        |
| `--values`           | string   | -              | Values file for templating (not yet implemented)  |
| `--set`              | string   | -              | Set values (key=value pairs, not yet implemented) |
| `--timeout`          | duration | `15m`          | Deployment health check timeout                   |
| `--rollback-timeout` | duration | `10m`          | Rollback timeout                                  |
| `--no-wait`          | bool     | `false`        | Don't wait for health checks                      |
| `--prune`            | bool     | `false`        | Remove orphaned services                          |
| `--allow-latest`     | bool     | `false`        | Allow :latest image tags                          |
| `--parallel`         | int      | `1`            | Parallel service updates (not yet implemented)    |
| `--logs`             | bool     | `true`         | Stream container logs during deployment           |

### Examples

#### Basic Deployment

```bash
stackman apply -n mystack -f docker-compose.yml
```

#### With Custom Timeouts

```bash
stackman apply -n mystack -f docker-compose.yml --timeout 20m --rollback-timeout 5m
```

#### Deploy Without Waiting (Fire and Forget)

```bash
stackman apply -n mystack -f docker-compose.yml --no-wait
```

#### Remove Obsolete Services

```bash
stackman apply -n mystack -f docker-compose.yml --prune
```

#### Allow :latest Tag (Not Recommended for Production)

```bash
stackman apply -n mystack -f docker-compose.yml --allow-latest
```

#### Disable Log Streaming

```bash
stackman apply -n mystack -f docker-compose.yml --logs=false
```

#### Using Environment Variables for Docker Connection

```bash
export DOCKER_HOST=tcp://192.168.1.100:2376
export DOCKER_TLS_VERIFY=1
export DOCKER_CERT_PATH=/path/to/certs
stackman apply -n mystack -f docker-compose.yml
```

#### Using Private Registry Authentication

```bash
# Ensure $HOME/.docker/config.json contains auth credentials
# Or set custom path:
export DOCKER_CONFIG_PATH=/etc/docker
stackman apply -n mystack -f docker-compose.yml
```

---

## Configuration

### Environment Variables

stackman reads configuration from environment variables:

#### Docker Connection

| Variable             | Description                                         | Default                       | Example                    |
|----------------------|-----------------------------------------------------|-------------------------------|----------------------------|
| `DOCKER_HOST`        | Docker daemon socket                                | `unix:///var/run/docker.sock` | `tcp://192.168.1.100:2376` |
| `DOCKER_TLS_VERIFY`  | Enable TLS verification                             | `0`                           | `1`                        |
| `DOCKER_CERT_PATH`   | Path to TLS certificates                            | -                             | `/etc/docker/certs`        |
| `DOCKER_CONFIG_PATH` | Path to Docker config directory (for registry auth) | `$HOME/.docker`               | `/etc/docker`              |

#### Deployment Behavior

| Variable                    | Description                                                | Default                   | Example                      |
|-----------------------------|------------------------------------------------------------|---------------------------|------------------------------|
| `STACKMAN_WORKDIR`          | Base path for relative volume mounts                       | Current working directory | `/var/app/stacks/production` |
| `STACKMAN_DEPLOY_TIMEOUT`   | Deployment timeout (overridden by `--timeout` flag)        | `15m`                     | `20m`                        |
| `STACKMAN_ROLLBACK_TIMEOUT` | Rollback timeout (overridden by `--rollback-timeout` flag) | `10m`                     | `5m`                         |

#### Logging & Output

| Variable    | Description                                  | Default | Example |
|-------------|----------------------------------------------|---------|---------|
| `LOG_LEVEL` | Log verbosity (not yet implemented)          | `info`  | `debug` |
| `NO_COLOR`  | Disable colored output (not yet implemented) | `false` | `true`  |

### Configuration Precedence

Priority (highest to lowest):

1. **Command-line flags** (e.g., `--timeout 20m`)
2. **Environment variables** (e.g., `STACKMAN_DEPLOY_TIMEOUT=20m`)
3. **Default values** (e.g., `15m` for timeout)

---

## Exit Codes

stackman follows standard Unix exit code conventions for CI/CD integration:

| Code    | Meaning          | Trigger Condition                                             | Rollback Performed?           |
|---------|------------------|---------------------------------------------------------------|-------------------------------|
| **0**   | Success          | All services deployed and healthy                             | N/A                           |
| **1**   | Failure          | Deployment failed (parse error, API error, validation failed) | ✅ Yes (if deployment started) |
| **2**   | Timeout          | Health check timeout reached                                  | ✅ Yes                         |
| **3**   | Rollback Failed  | Deployment failed AND rollback also failed (as per spec)      | ⚠️ Attempted but failed       |
| **4**   | Connection Error | Docker API/Registry connection failed (as per spec)           | N/A                           |
| **130** | Interrupted      | User pressed Ctrl+C (SIGINT) or SIGTERM received              | ✅ Yes                         |

### Exit Code Usage in CI/CD

```bash
# GitLab CI / GitHub Actions example
stackman apply -n production -f docker-compose.yml
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
  echo "✅ Deployment successful"
elif [ $EXIT_CODE -eq 1 ]; then
  echo "❌ Deployment failed, rollback succeeded"
  exit 1
elif [ $EXIT_CODE -eq 2 ]; then
  echo "⏱️ Deployment timeout, rollback succeeded"
  exit 1
elif [ $EXIT_CODE -eq 130 ]; then
  echo "🛑 Deployment interrupted, rollback succeeded"
  exit 1
else
  echo "💥 Critical error (code $EXIT_CODE)"
  exit $EXIT_CODE
fi
```

---

## Requirements

### Runtime Requirements

| Requirement          | Version                 | Purpose             |
|----------------------|-------------------------|---------------------|
| **Docker Engine**    | 19.03+                  | Swarm API access    |
| **Docker Swarm**     | Initialized             | `docker swarm init` |
| **Operating System** | Linux / macOS / Windows | Cross-platform      |
| **Architecture**     | amd64 / arm64           | Binary architecture |

### Build Requirements (Only for Building from Source)

| Requirement | Version    | Purpose            |
|-------------|------------|--------------------|
| **Go**      | 1.24+      | Compiler toolchain |
| **Make**    | (optional) | Build automation   |

### Docker Compose File Requirements

✅ **Supported**: Compose file format version 3.x (Swarm mode)
❌ **Not Supported**: Compose file version 2.x (standalone Docker)

#### Health Check Recommendations

- ✅ **Recommended**: Define `healthcheck` for all services for accurate deployment validation
- ⚠️ **Optional**: Services without healthcheck are considered healthy if task is in `running` state
- 🔍 **Best Practice**: Use fast healthchecks (`interval: 5-10s`) with reasonable `start_period`

### Minimal Working Example

```yaml
version: '3.8'

services:
  web:
    image: nginx:1.25-alpine
    healthcheck:
      test: [ "CMD", "wget", "-q", "--spider", "http://localhost" ]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 5s
    deploy:
      replicas: 2
      update_config:
        parallelism: 1
        delay: 10s

networks:
  default:
    driver: overlay
```

### Pre-Deployment Checklist

Before using stackman, ensure:

```bash
# 1. Docker is running
docker info

# 2. Swarm is initialized
docker swarm init

# 3. You are a swarm manager node
docker node ls

# 4. Your compose file is valid
docker-compose -f docker-compose.yml config

# 5. Required images are pullable (for private registries)
docker login registry.example.com
```

---

## Output Examples

### Successful Deployment

```
2025/11/06 14:33:05 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:33:05 Parsing compose file: docker-compose.yml
2025/11/06 14:33:05 Creating snapshot of current stack state...
2025/11/06 14:33:06 Snapshotted service: mystack_web (version 42)
2025/11/06 14:33:06 Snapshotted service: mystack_api (version 38)
2025/11/06 14:33:06 Snapshot created with 2 services
2025/11/06 14:33:06 Starting deployment of stack: mystack
2025/11/06 14:33:06 No obsolete services to remove
2025/11/06 14:33:06 Pulling image: nginx:latest
2025/11/06 14:33:08 Image nginx:latest pulled successfully
2025/11/06 14:33:08 Network mystack_default already exists
2025/11/06 14:33:08 Updating service: mystack_web
2025/11/06 14:33:09 Service mystack_web updated, waiting for tasks to be recreated...
[event:service:mystack_web] update
[event:container:mystack_web.1.xyz] start
Stack deployed successfully. Starting health checks...
2025/11/06 14:33:12 Starting log streaming for 2 services...
Waiting for service tasks to start...
2025/11/06 14:33:15 Waiting for services to become healthy...
2025/11/06 14:33:20 Container statuses: mystack_web.1: running/starting, mystack_api.1: running/healthy
[event:container:mystack_web.1.xyz] healthcheck passed (exit 0): curl -f http://localhost
2025/11/06 14:33:25 Container statuses: mystack_web.1: running/healthy, mystack_api.1: running/healthy
2025/11/06 14:33:25 All containers are healthy (checked 2 containers)
All containers healthy.
```

### Failed Deployment with Rollback

```
2025/11/06 14:45:10 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:45:10 Parsing compose file: docker-compose.yml
2025/11/06 14:45:10 Creating snapshot of current stack state...
2025/11/06 14:45:11 Starting deployment of stack: mystack
2025/11/06 14:45:11 Pulling image: myapp:broken-version
2025/11/06 14:45:15 Updating service: mystack_api
[event:service:mystack_api] update
[event:container:mystack_api.1.abc] start
[event:container:mystack_api.1.abc] healthcheck failed (exit 1): curl -f http://localhost/health
2025/11/06 14:45:30 ERROR: Service mystack_api task abc123def456 failed with state shutdown (desired: shutdown)
2025/11/06 14:45:30 ERROR: New task abc123def456 failed with state complete (desired: shutdown): task: non-zero exit (1)
  Container exit code: 1
  Task was shutdown and replaced (likely healthcheck failure)
ERROR: Services failed healthcheck or didn't start in time.
Starting rollback to previous state...
2025/11/06 14:45:31 Rolling back stack: mystack
2025/11/06 14:45:31 Rolling back service: mystack_api to version 38
2025/11/06 14:45:32 Service mystack_api rolled back successfully
2025/11/06 14:45:32 Rollback completed for stack: mystack
Rollback completed successfully
```

### Interrupted Deployment

```
2025/11/06 14:50:15 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:50:15 Deploying stack: mystack
[event:service:mystack_web] update
^C
2025/11/06 14:50:20 Received signal: interrupt
2025/11/06 14:50:20 Deployment interrupted, initiating rollback...
Starting rollback to previous state...
2025/11/06 14:50:21 Rolling back stack: mystack
2025/11/06 14:50:22 Service mystack_web rolled back successfully
Rollback completed successfully
```

---

## Project Structure

stackman follows a clean, modular architecture aligned with the technical specification:

```
stackman/
├── main.go                      # Entry point and CLI orchestration
├── cmd/                         # CLI commands (cobra-like structure)
│   ├── root.go                  # Command router and usage
│   ├── apply.go                 # apply command (✅ IMPLEMENTED)
│   ├── rollback.go              # rollback command (🚧 stub)
│   ├── logs.go                  # logs command (🚧 stub)
│   ├── events.go                # events command (🚧 stub)
│   ├── stubs.go                 # Stub implementations for incomplete commands
│   └── version.go               # version command (✅ IMPLEMENTED)
├── internal/                    # Internal packages (not importable externally)
│   ├── compose/                 # ✅ Compose file parsing (no external libs)
│   │   ├── types.go             # Compose spec types (services, networks, volumes, etc.)
│   │   ├── parser.go            # YAML → ComposeSpec parser (gopkg.in/yaml.v3)
│   │   └── converter.go         # ComposeSpec → Swarm ServiceSpec converter
│   ├── swarm/                   # ✅ Docker Swarm API client wrapper
│   │   ├── interface.go         # StackDeployer interface
│   │   ├── stack.go             # Stack deployment orchestration
│   │   ├── services.go          # Service create/update logic
│   │   ├── images.go            # Image pull with progress tracking
│   │   ├── networks.go          # Network create/inspect logic
│   │   ├── volumes.go           # Volume create/inspect logic
│   │   ├── cleanup.go           # Obsolete service removal
│   │   ├── rollback.go          # Rollback execution
│   │   └── state.go             # Current swarm state reading
│   ├── health/                  # ✅ Health monitoring and event handling
│   │   ├── watcher.go           # Event-driven task watcher (Docker events API)
│   │   ├── monitor.go           # Per-task monitor with log streaming
│   │   ├── events.go            # Event types and subscription
│   │   └── service_update_monitor.go  # ServiceInspect UpdateStatus tracker
│   ├── snapshot/                # ✅ Snapshot creation and restoration
│   │   └── snapshot.go          # ServiceInspect capture and rollback
│   ├── deployment/              # ✅ Deployment ID generation
│   │   └── id.go                # Unique deployment ID (timestamp-based)
│   ├── paths/                   # ✅ Path resolution logic
│   │   └── resolver.go          # STACKMAN_WORKDIR + relative → absolute conversion
│   ├── plan/                    # 🚧 Diff and deployment plan (partially implemented)
│   │   ├── types.go             # Plan types (Create/Update/Delete)
│   │   ├── planner.go           # Diff logic (current vs desired state)
│   │   └── formatter.go         # Plan output formatting
│   ├── apply/                   # 🔜 Apply orchestration (currently in cmd/apply.go)
│   ├── rollback/                # 🔜 Rollback orchestration (currently in snapshot/)
│   ├── signals/                 # 🔜 SIGINT/SIGTERM handling (currently in cmd/apply.go)
│   └── output/                  # 🔜 Structured output and formatting
├── tests/                       # ✅ Integration tests
│   ├── apply_test.go            # Full deployment cycle tests
│   ├── health_test.go           # Health check monitoring tests
│   ├── resources_test.go        # Networks, volumes, secrets, configs tests
│   ├── operations_test.go       # Prune, signals tests
│   ├── negative_test.go         # Error handling tests
│   ├── helpers_test.go          # Test utilities
│   └── testdata/                # Test compose files
└── docs/                        # Documentation
    ├── stackman-diff-techspec-full.md  # Full technical specification
    ├── TEST_RESULTS.md          # Test validation results
    └── TESTING.md               # Testing guide
```

### Package Responsibilities

| Package                | Responsibility                                                   | Status                                 |
|------------------------|------------------------------------------------------------------|----------------------------------------|
| `cmd/`                 | CLI interface, argument parsing, command routing                 | ✅ Core commands implemented            |
| `internal/compose/`    | Parse `docker-compose.yml` → internal model                      | ✅ Fully implemented                    |
| `internal/swarm/`      | Docker Swarm API operations (services, tasks, networks, volumes) | ✅ Core operations implemented          |
| `internal/health/`     | Event-driven task monitoring, health checks, log streaming       | ✅ Fully implemented                    |
| `internal/snapshot/`   | Capture and restore service state for rollback                   | ✅ Implemented                          |
| `internal/deployment/` | Generate unique deployment IDs for task tracking                 | ✅ Implemented                          |
| `internal/paths/`      | Resolve relative paths to absolute using `STACKMAN_WORKDIR`      | ✅ Implemented                          |
| `internal/plan/`       | Diff current vs desired state, generate deployment plan          | 🚧 Partially implemented               |
| `internal/apply/`      | High-level apply orchestration                                   | 🔜 To be extracted from `cmd/apply.go` |
| `internal/rollback/`   | High-level rollback orchestration                                | 🔜 To be extracted from `snapshot/`    |
| `internal/signals/`    | SIGINT/SIGTERM handling with context propagation                 | 🔜 To be extracted from `cmd/apply.go` |
| `internal/output/`     | Structured logging, JSON output, progress formatting             | 🔜 Planned                             |

---

## Docker Compose Support

`stackman` includes a comprehensive Docker Compose parser that converts `docker-compose.yml` files to Docker Swarm
service specifications.

### Supported Docker Compose Features

#### Service Configuration

- **Images & Build**: `image`, `build` (context, dockerfile, args, target, cache_from)
- **Commands**: `command`, `entrypoint`
- **Environment**: `environment` (array and map formats), `env_file`
- **Container Settings**: `hostname`, `domainname`, `user`, `working_dir`, `stdin_open`, `tty`, `read_only`, `init`
- **Lifecycle**: `stop_signal`, `stop_grace_period`, `restart`

#### Networking

- **Ports**: Short syntax (`"8080:80"`) and long syntax (with mode and protocol)
- **Networks**: Network attachment with aliases
- **DNS**: `dns`, `dns_search`, `dns_opt`
- **Hosts**: `extra_hosts`, `mac_address`

#### Storage

- **Volumes**: Bind mounts with automatic relative → absolute path conversion
- **Named Volumes**: Volume references from top-level `volumes:` section
- **Tmpfs**: Temporary filesystem mounts

#### Health Checks

- **Test Commands**: CMD-SHELL and exec array formats
- **Timing**: `interval`, `timeout`, `retries`, `start_period`
- **Control**: `disable` flag

#### Deployment (Swarm-specific)

- **Mode**: `replicated` (with replica count) or `global`
- **Updates**: Parallelism, delay, order, failure action, monitor period, max failure ratio
- **Rollback**: Same configuration as updates
- **Resources**: CPU and memory limits/reservations
- **Restart Policy**: Condition, delay, max attempts, window
- **Placement**: Node constraints, spread preferences, max replicas per node

#### Security & Capabilities

- **Capabilities**: `cap_add`, `cap_drop`
- **Devices**: Device mappings
- **Isolation**: Container isolation technology

#### Top-Level Sections

- **Services**: Complete service definitions
- **Networks**: Custom networks with driver options, IPAM config
- **Volumes**: Named volumes with driver options
- **Secrets**: File or external secrets (parsed, creation not implemented)
- **Configs**: File or external configs (parsed, creation not implemented)

### Known Limitations

Some Docker Compose fields are **parsed but not applied** due to Docker Swarm API restrictions:

| Field                     | Reason                               |
|---------------------------|--------------------------------------|
| `privileged`              | Not supported in Swarm mode          |
| `security_opt`            | Not available in Swarm ContainerSpec |
| `sysctls`                 | Not available in Swarm ContainerSpec |
| `ulimits`                 | Not available in Swarm ContainerSpec |
| `links`, `external_links` | Deprecated in favor of networks      |
| `depends_on`              | No start order control in Swarm      |

These fields remain in the type definitions for completeness and potential future use.

---

## Use Cases

### CI/CD Pipelines

```yaml
# GitLab CI example
deploy:
  stage: deploy
  script:
    - stackman production docker-compose.yml 10 5
  only:
    - main
```

### Blue-Green Deployments

```bash
# Deploy to green environment
stackman green-stack docker-compose.yml

# If successful, switch traffic and deploy to blue
stackman blue-stack docker-compose.yml
```

### Canary Deployments

```yaml
# docker-compose.yml
services:
  web:
    image: myapp:${VERSION}
    deploy:
      replicas: 1  # Start with 1 replica
      update_config:
        parallelism: 1
        delay: 30s
```

```bash
# Deploy canary
VERSION=v2.0 stackman mystack docker-compose.yml

# If healthy, scale up
docker service scale mystack_web=10
```

### Multi-Environment Deployment

```bash
#!/bin/bash
# deploy.sh

ENVIRONMENTS=("dev" "staging" "production")
COMPOSE_FILE="docker-compose.yml"

for ENV in "${ENVIRONMENTS[@]}"; do
  echo "Deploying to $ENV..."
  if stackman "${ENV}-stack" "$COMPOSE_FILE" 15 3; then
    echo "✓ $ENV deployment successful"
  else
    echo "✗ $ENV deployment failed, stopping"
    exit 1
  fi
done
```

---

## Troubleshooting

### Common Issues and Solutions

#### 🔴 Issue: Deployment times out waiting for health checks

**Symptoms**:

```
[HealthCheck] ⏳ Task abc123 (mystack_web) is starting
ERROR: timeout after 15m waiting for services to become healthy
```

**Root Causes**:

- Health check `start_period` too short for slow-starting apps
- Health check command timing out or failing
- Insufficient resources (CPU/memory) causing slow startup

**Solutions**:

1. **Increase deployment timeout**:
   ```bash
   stackman apply -n mystack -f docker-compose.yml --timeout 30m
   ```

2. **Adjust healthcheck in compose file**:
   ```yaml
   healthcheck:
     test: ["CMD", "curl", "-f", "http://localhost/health"]
     interval: 10s
     timeout: 5s
     retries: 3
     start_period: 60s  # Increase if app needs more startup time
   ```

3. **Test healthcheck command manually**:
   ```bash
   docker exec <container-id> curl -f http://localhost/health
   ```

---

#### 🔴 Issue: Service keeps failing with "task: non-zero exit"

**Symptoms**:

```
[ServiceMonitor] ❌ Service mystack_api: Task xyz789 failed - task: non-zero exit (1)
ERROR: New task failed with state complete (desired: shutdown): task: non-zero exit (1)
```

**Root Causes**:

- Application crash on startup
- Health check failing repeatedly
- Missing environment variables or secrets
- Configuration errors

**Solutions**:

1. **Check service logs**:
   ```bash
   docker service logs mystack_servicename --tail 100
   ```

2. **Inspect task details**:
   ```bash
   docker service ps mystack_servicename --no-trunc
   ```

3. **Test container locally** (before swarm deployment):
   ```bash
   docker run --rm myimage:tag
   ```

4. **Check for missing config/secrets**:
   ```bash
   docker config ls
   docker secret ls
   ```

---

#### 🔴 Issue: Rollback restores old version but it's also unhealthy

**Symptoms**:

```
Starting rollback to previous state...
[ServiceMonitor] ❌ Service mystack_web: Task def456 failed
ERROR: Rollback failed: services did not become healthy after rollback
```

**Root Cause**: Previous version has underlying health issues (database schema mismatch, missing dependencies, etc.)

**Solutions**:

1. **Deploy a known-good version** (not rollback):
   ```bash
   # Use older compose file with working version
   stackman apply -n mystack -f docker-compose.v1.2.3.yml
   ```

2. **Fix health check in previous version**:
   ```yaml
   # Temporarily disable strict health checks
   healthcheck:
     test: ["CMD", "true"]  # Always passes
   ```

3. **Manual intervention required**:
   ```bash
   # Remove stack completely
   docker stack rm mystack

   # Wait for cleanup
   sleep 30

   # Deploy known-good version
   stackman apply -n mystack -f docker-compose.good.yml
   ```

---

#### 🔴 Issue: "No services found" or "Stack not found"

**Symptoms**:

```
ERROR: No services found for stack: mystack
```

**Root Causes**:

- Stack name mismatch
- Swarm mode not initialized
- Wrong Docker daemon context

**Solutions**:

1. **Verify swarm is initialized**:
   ```bash
   docker info | grep Swarm
   # Should show: "Swarm: active"

   # If not active:
   docker swarm init
   ```

2. **List existing stacks**:
   ```bash
   docker stack ls
   ```

3. **Check Docker context**:
   ```bash
   docker context ls
   docker context use default
   ```

---

#### 🔴 Issue: Image pull fails with authentication error

**Symptoms**:

```
ERROR: failed to pull image registry.example.com/myapp:latest: unauthorized
```

**Solutions**:

1. **Login to registry**:
   ```bash
   docker login registry.example.com
   ```

2. **Set Docker config path**:
   ```bash
   export DOCKER_CONFIG_PATH=$HOME/.docker
   stackman apply -n mystack -f docker-compose.yml
   ```

3. **Verify auth config**:
   ```bash
   cat $HOME/.docker/config.json
   # Should contain "auths": { "registry.example.com": { "auth": "..." } }
   ```

---

#### 🔴 Issue: Tasks stuck in "Preparing" state

**Symptoms**:

```
[HealthCheck] ⏳ Task ghi012 (mystack_db) is preparing
(repeats indefinitely)
```

**Root Causes**:

- Image pull in progress (large images)
- Node resource constraints
- Network issues

**Solutions**:

1. **Check task status**:
   ```bash
   docker service ps mystack_servicename --no-trunc
   ```

2. **Check node resources**:
   ```bash
   docker node ls
   docker node inspect <node-id> --format '{{.Status}}'
   ```

3. **Manually pull image on node**:
   ```bash
   # SSH to swarm node
   docker pull myregistry/myimage:tag
   ```

---

#### 🟡 Issue: stackman hangs after "Stack deployed successfully"

**Symptom**: Command doesn't exit after deployment

**Root Cause**: Waiting for health checks (expected behavior)

**Solutions**:

1. **Use `--no-wait`** if you don't want to wait:
   ```bash
   stackman apply -n mystack -f docker-compose.yml --no-wait
   ```

2. **Check health check status** in another terminal:
   ```bash
   docker service ps mystack_servicename
   ```

---

### Debugging Commands

```bash
# View real-time events
docker events --filter type=container --filter type=service

# Inspect service configuration
docker service inspect mystack_servicename --pretty

# Check service update status
docker service inspect mystack_servicename --format '{{.UpdateStatus}}'

# View task history (including failed tasks)
docker service ps mystack_servicename --no-trunc

# Get container logs for specific task
docker service logs mystack_servicename --tail 100 --follow
```

---

## TODOLIST

This is a comprehensive roadmap aligned with the [technical specification](docs/stackman-techspec.md). Items are
prioritized by importance and complexity.

### 🔥 Priority 1: Core Functionality (MVP Requirements)

#### Deployment & Health Monitoring

- [x] **Snapshot-based rollback** - Capture service state before deployment
- [x] **Event-driven task monitoring** - Subscribe to Docker events for task lifecycle
- [x] **Health status polling** - Check `ContainerInspect.State.Health`
- [x] **UpdateStatus tracking** - Wait for `ServiceInspect.UpdateStatus.State == completed`
- [x] **DeployID injection** - Add `com.stackman.deploy.id` label to tasks
- [x] **Signal handling** - SIGINT/SIGTERM → rollback
- [ ] **Exit code alignment** - Implement codes 2 (timeout), 3 (rollback failed), 4 (connection error)
- [ ] **Reconciliation loop** - Periodic task list refresh to catch missed events

#### Compose File Support

- [x] **YAML parser** - Parse docker-compose.yml with `gopkg.in/yaml.v3`
- [x] **Path resolution** - Convert relative paths using `STACKMAN_WORKDIR`
- [x] **Environment substitution** - Support `${VAR}` syntax
- [x] **Service spec conversion** - Map `deploy.*` to Swarm `ServiceSpec`
- [ ] **Secrets creation** - Implement `docker secret create` from `secrets:` section
- [ ] **Configs creation** - Implement `docker config create` from `configs:` section
- [ ] **Templating engine** - Implement `--values` and `--set` (basic key-value replacement)

#### Resource Management

- [x] **Network creation** - Create overlay networks from `networks:` section
- [x] **Volume creation** - Create local volumes from `volumes:` section
- [ ] **Secrets handling** - Full lifecycle (create, update, attach to services)
- [ ] **Configs handling** - Full lifecycle (create, update, attach to services)
- [ ] **External resources** - Respect `external: true` flag (skip create/delete)
- [ ] **Resource pruning** - Implement `--prune` for orphaned networks/volumes/secrets/configs

### 🚀 Priority 2: Advanced Features

#### Commands

- [x] **apply command** - Main deployment workflow (✅ implemented)
- [ ] **diff command** - Show deployment plan without applying
- [ ] **status command** - Show current stack status with health info
- [ ] **logs command** - Stream logs from stack services with filters
- [ ] **events command** - Stream Docker events filtered by stack
- [ ] **rollback command** - Standalone rollback from saved snapshot

#### Deployment Intelligence

- [ ] **Diff-based planning** - Only update services that actually changed (use `internal/plan`)
- [ ] **Parallel service updates** - Implement `--parallel` flag for concurrent updates
- [ ] **Dependency ordering** - Respect `depends_on` for deployment order (best-effort)
- [ ] **Smart rollback decision** - Only rollback changed services, not entire stack
- [ ] **Update progress tracking** - Real-time progress bar with task counts

#### Safety & Validation

- [x] **:latest tag blocking** - Require `--allow-latest` flag
- [ ] **Conflict detection** - Check for name conflicts in resources
- [ ] **Version conflict handling** - Retry `ServiceUpdate` on `Version.Index` race condition
- [ ] **Secret content masking** - Never log secret data
- [ ] **Dry-run mode** - `--dry-run` flag to show plan without applying

### 🔧 Priority 3: Production Readiness

#### Observability

- [ ] **Structured logging** - Implement `internal/output` with log levels
- [ ] **JSON output mode** - `--json` flag for machine-readable output
- [ ] **Progress formatting** - Pretty progress bars and status tables
- [ ] **Deployment metrics** - Report deployment duration, task counts, health check times
- [ ] **Audit log** - Log all actions (create, update, delete) with timestamps

#### Configuration

- [ ] **Config file support** - `.stackman.yaml` for default settings
- [ ] **Multiple Docker hosts** - `--docker-host` flag for remote deployments
- [ ] **TLS configuration** - Full support for `--tls`, `--cert-path`, `--tlsverify`
- [ ] **Context switching** - Respect Docker contexts

#### Reliability

- [ ] **Retry logic** - Exponential backoff for API failures
- [ ] **Timeout configurability** - Per-service timeout overrides
- [ ] **Graceful degradation** - Continue deployment if non-critical services fail
- [ ] **Connection pooling** - Optimize Docker API client usage

### 🧪 Priority 4: Testing & Documentation

#### Testing

- [x] **Unit tests** - Core logic (compose parser, path resolver, health monitor)
- [x] **Integration tests** - Full deployment cycle with real Swarm
- [ ] **Negative tests** - Error handling (API failures, timeout, invalid compose)
- [ ] **Rollback tests** - Verify rollback correctness
- [ ] **Signal tests** - SIGINT/SIGTERM handling
- [ ] **Secrets/configs tests** - Lifecycle testing
- [ ] **Performance tests** - Large stacks (20+ services)

#### Documentation

- [x] **README** - Comprehensive usage guide
- [x] **Technical spec** - Full architecture document
- [ ] **API documentation** - GoDoc for all packages
- [ ] **Examples** - Real-world compose files (with secrets, configs, multiple networks)
- [ ] **Troubleshooting guide** - Common errors and solutions (✅ added to README)
- [ ] **Migration guide** - From `docker stack deploy` to `stackman`

### 📦 Priority 5: Nice-to-Have

- [ ] **Auto-completion** - Bash/Zsh/Fish completion scripts
- [ ] **Plugin system** - Custom hooks (pre-deploy, post-deploy, on-failure)
- [ ] **Remote snapshots** - Store snapshots in S3/registry for team collaboration
- [ ] **Canary deployments** - Built-in support for gradual rollouts
- [ ] **Blue-green deployments** - Automated traffic switching
- [ ] **Notification integrations** - Slack/Discord/PagerDuty webhooks
- [ ] **Web UI** - Optional web dashboard for stack status
- [ ] **Multi-stack orchestration** - Deploy multiple stacks with dependencies

### 🐛 Known Issues

- [ ] **Race condition in event handling** - Rare: events may be missed if subscription starts after task creation (
  workaround: reconciliation loop)
- [ ] **Large image pull timeout** - No streaming progress for image pull in logs
- [ ] **No task restart limit** - Swarm may restart failed tasks indefinitely
- [ ] **Health check log truncation** - Long health check output is truncated
- [ ] **Parallel updates not implemented** - `--parallel` flag parsed but not used

### 📊 Implementation Status Summary

| Category            | Implemented | Planned | Total  | % Complete |
|---------------------|-------------|---------|--------|------------|
| Core Deployment     | 9           | 3       | 12     | 75%        |
| Compose Support     | 4           | 3       | 7      | 57%        |
| Resources           | 2           | 5       | 7      | 29%        |
| Commands            | 2           | 5       | 7      | 29%        |
| Safety & Validation | 1           | 4       | 5      | 20%        |
| Testing             | 2           | 6       | 8      | 25%        |
| **TOTAL**           | **20**      | **26**  | **46** | **43%**    |

### Contributing Priority

If you want to contribute, focus on these high-impact items:

1. **Exit code alignment** - Easy win, improves CI/CD integration
2. **Secrets/Configs creation** - Required for full compose parity
3. **diff command** - Highly requested, relatively simple
4. **JSON output mode** - Enables advanced tooling integration
5. **Reconciliation loop** - Improves reliability

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

---

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

### Development Setup

```bash
# Clone repository
git clone https://github.com/SomeBlackMagic/stackman.git
cd stackman

# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build -o stackman .
```

---

## License

Licensed under the **GPL-3.0** license.
See [LICENSE](LICENSE) for details.

---

## Credits

Developed by [SomeBlackMagic](https://github.com/SomeBlackMagic)

Built with:

- [Docker Engine API](https://docs.docker.com/engine/api/)
- [Go](https://golang.org/)
- [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) for YAML parsing
