# paperclip-go

[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org)
[![Tests](https://img.shields.io/badge/Tests-170+-success?style=for-the-badge)](https://github.com/JSLEEKR/paperclip-go)
[![Zero Deps](https://img.shields.io/badge/Dependencies-0-brightgreen?style=for-the-badge)](https://github.com/JSLEEKR/paperclip-go/blob/master/go.mod)
[![License](https://img.shields.io/badge/License-MIT-blue?style=for-the-badge)](LICENSE)

Re-implementation of [paperclip](https://github.com/paperclipai/paperclip) (40K+ stars) core orchestration engine in Go.

**paperclip** is an open-source platform for running zero-human AI companies. This project reimplements the server-side orchestration engine — the control plane that manages agents, tasks, budgets, heartbeats, routines, and approvals — in Go with zero dependencies and lease-based locking.

## Why This Exists

The original paperclip is a TypeScript monolith with 24+ production dependencies, an embedded PostgreSQL requirement, and in-memory locking that causes the most-reported bug (#1971, 148 comments): stale execution locks that prevent agents from picking up tasks.

paperclip-go solves these problems:

| Problem | Original (TypeScript) | paperclip-go |
|---------|----------------------|--------------|
| **Stale locks** (#1971) | In-memory Promise lock, manual recovery | Lease-based auto-expiry, automatic reaping |
| **Heavy runtime** | Node.js + 24 deps + embedded PG | Single binary, zero deps |
| **No horizontal scaling** | In-memory `runningProcesses` Set | No in-memory coordination state |
| **Crash recovery** | "Reports problems, doesn't fix them" | Expired leases auto-release |
| **Concurrency** | Single-threaded Promise-based | Goroutines + sync.RWMutex |

## Quick Start

### Install

```bash
go install github.com/JSLEEKR/paperclip-go/cmd/paperclip@latest
```

### Build from source

```bash
git clone https://github.com/JSLEEKR/paperclip-go.git
cd paperclip-go
go build -o paperclip ./cmd/paperclip/
```

### Basic Usage

```bash
# Create a company
paperclip company create --name "Acme AI" --slug acme-ai

# Hire an agent
paperclip agent hire --company <company-id> --name "Claude" --role executor

# Create a task
paperclip task create --company <company-id> --title "Build login page"

# Assign task to agent
paperclip task assign --id <task-id> --agent <agent-id>

# Check out task (atomic, lease-based)
paperclip task checkout --id <task-id> --agent <agent-id> --run <run-id>

# Complete task
paperclip task complete --id <task-id> --run <run-id>

# Set a budget policy
paperclip budget set --company <company-id> --scope agent --scope-id <agent-id> --amount 5000

# Create a routine
paperclip routine create --company <company-id> --title "Daily standup" --agent <agent-id> --cron "0 9 * * *"

# View company status
paperclip status

# Check version
paperclip version
```

## Architecture

```
paperclip-go/
├── cmd/paperclip/main.go       # CLI entry point
├── pkg/
│   ├── company/company.go      # Company CRUD, multi-tenant isolation
│   ├── agent/agent.go          # Agent registry, org hierarchy, hire/fire
│   ├── task/task.go            # Task lifecycle, atomic checkout, delegation
│   ├── budget/budget.go        # 3-tier budget enforcement
│   ├── heartbeat/heartbeat.go  # Run lifecycle, queue, lease-based locking
│   ├── routine/routine.go      # Cron scheduling, catch-up policies
│   ├── approval/approval.go    # Governance gates, approval workflows
│   ├── cost/cost.go            # Cost tracking, multi-dimensional aggregation
│   ├── goal/goal.go            # Goal management, hierarchical objectives
│   └── store/store.go          # In-memory store with JSON persistence
└── go.mod                      # Zero external dependencies
```

### 10 Core Packages

| Package | Purpose | Key Features |
|---------|---------|-------------|
| `company` | Tenant isolation | CRUD, slug uniqueness, configurable limits |
| `agent` | AI employee management | Hire/fire, pause/resume, org hierarchy, cycle detection |
| `task` | Work item lifecycle | Atomic checkout, lease-based locking, delegation chains |
| `budget` | Cost control | 3-tier enforcement (visibility/soft/hard), auto-pause |
| `heartbeat` | Execution engine | Queue → claim → execute → complete, orphan reaping |
| `routine` | Scheduled tasks | Cron parser, timezone support, catch-up policies |
| `approval` | Governance | Approval workflows, revision requests, comment threads |
| `cost` | Cost attribution | Per-agent, per-provider, billing code aggregation |
| `goal` | Objectives | Hierarchical goals, priority-based default goal |
| `store` | Storage | Thread-safe generic collections, JSON persistence |

## Key Features

### 1. Lease-Based Locking (solves #1971)

The original paperclip's biggest bug is stale execution locks. When an agent process crashes, its in-memory lock persists until manual intervention. Our implementation uses time-limited leases:

```go
// Checkout atomically claims a task with a lease
err := taskSvc.Checkout(taskID, agentID, runID)
// Lease auto-expires after 30 minutes (configurable)

// Background reaper finds and releases expired leases
reaped := taskSvc.ReapExpiredLeases()
// Returns IDs of tasks that were automatically released
```

No manual intervention needed. Crashed agents' tasks automatically become available.

### 2. 3-Tier Budget Enforcement

```
Tier 1: Visibility — dashboards at company/agent/project level
Tier 2: Soft alert  — configurable warning threshold (default: 80%)
Tier 3: Hard ceiling — auto-pause agent, create incident
```

```go
result, _ := budgetSvc.RecordCost(costEvent)
if result.Blocked {
    // Agent is auto-paused, incident created
    // Resolve with: budgetSvc.ResolveIncident(id, "raise_budget_and_resume")
}
```

Hierarchical evaluation: company -> agent -> project. Any hard stop blocks invocation.

### 3. Heartbeat Engine

Complete run lifecycle with concurrency control:

```
queued -> claim (with lease) -> running -> completed/failed/timed_out
```

- Per-agent concurrency limits (1-10)
- Block checks (budget, pause status)
- Background queue processor with configurable tick interval
- Automatic orphan reaping for expired leases

### 4. Task Delegation

Tasks support hierarchical delegation with billing attribution:

```go
child, _ := taskSvc.Delegate(parentID, childID, "Sub-task", fromAgent, toAgent)
// child.RequestDepth = parent.RequestDepth + 1
// child.BillingCode inherited from parent
// child.Priority inherited from parent
```

### 5. Cron Scheduling

Full 5-field cron expression support:

```go
cron, _ := routine.ParseCron("*/5 9-17 * * 1-5")
next := cron.NextTick(time.Now()) // Next weekday 9-5 at 5-min mark
```

Features:
- Wildcard (`*`), ranges (`1-5`), steps (`*/5`), lists (`0,30`)
- Timezone-aware scheduling
- Catch-up policies: `skip_missed` or `enqueue_missed_with_cap`
- Concurrency policies: `always_enqueue`, `skip_if_active`, `coalesced`

### 6. Approval Workflows

```
pending -> approved | rejected | revision_requested
revision_requested -> pending (resubmit)
```

- Approval kinds: hire_agent, budget_change, budget_incident, fire_agent, role_change
- Comment threads for iterative review
- Callbacks on approve/reject for automated actions

### 7. Org Hierarchy

```go
// Build reporting chains
agentSvc.Hire(Agent{ID: "ceo", Name: "CEO"})
agentSvc.Hire(Agent{ID: "mgr", Name: "Manager", ReportsTo: "ceo"})
agentSvc.Hire(Agent{ID: "dev", Name: "Dev", ReportsTo: "mgr"})

chain := agentSvc.ChainOfCommand("dev") // [dev, mgr, ceo]
reports := agentSvc.DirectReports("ceo") // [mgr]
```

Automatic cycle detection prevents circular reporting.

### 8. Cost Attribution

Multi-dimensional cost aggregation:

```go
byAgent := costSvc.ByAgent(companyID, from, to)
byProvider := costSvc.ByProvider(companyID, from, to)
byTask := costSvc.TotalByTask(taskID)
byBilling := costSvc.ListByBillingCode("BC-001")
monthly := costSvc.MonthlyTotal(companyID)
```

### 9. In-Memory Store with JSON Persistence

Zero infrastructure required for development:

```go
store := store.New(".paperclip") // JSON files in .paperclip/
store := store.New("")           // Pure in-memory, no persistence
```

Thread-safe generic collections with type parameters:

```go
collection := store.NewCollection[MyRecord]()
collection.Put(record)
collection.Get(id)
collection.Filter(func(r MyRecord) bool { return r.Active })
```

### 10. Full Task Audit Log

Every task lifecycle event is recorded:

```go
entries := taskSvc.GetAuditLog(taskID)
// [created, assigned, checkout, completed, ...]
// Each entry: action, agent, run, old_status, new_status, timestamp
```

## CLI Commands

| Command | Subcommands | Description |
|---------|------------|-------------|
| `company` | create, list, get, delete | Manage AI companies |
| `agent` | hire, fire, pause, resume, list, get | Manage AI agents |
| `task` | create, list, assign, checkout, complete | Manage tasks |
| `budget` | set, list, incidents | Budget policies |
| `routine` | create, list, enable, disable | Scheduled routines |
| `goal` | create, list, complete, abandon | Company goals |
| `status` | — | Company status overview |
| `version` | — | Print version |

## Go API Usage

```go
package main

import (
    "github.com/JSLEEKR/paperclip-go/pkg/store"
    "github.com/JSLEEKR/paperclip-go/pkg/company"
    "github.com/JSLEEKR/paperclip-go/pkg/agent"
    "github.com/JSLEEKR/paperclip-go/pkg/task"
    "github.com/JSLEEKR/paperclip-go/pkg/budget"
    "github.com/JSLEEKR/paperclip-go/pkg/heartbeat"
)

func main() {
    s := store.New(".paperclip")

    companySvc := company.NewService(s)
    agentSvc := agent.NewService(s)
    taskSvc := task.NewService(s)
    budgetSvc := budget.NewService(s)
    hbEngine := heartbeat.NewEngine(s)

    // Create a company
    co, _ := companySvc.Create(company.Company{
        ID: "co-1", Name: "Acme AI", Slug: "acme-ai",
    })

    // Hire an agent
    ag, _ := agentSvc.Hire(agent.Agent{
        ID: "ag-1", CompanyID: co.ID, Name: "Claude",
        Role: agent.RoleExecutor,
    })

    // Create and assign a task
    tk, _ := taskSvc.Create(task.Task{
        ID: "task-1", CompanyID: co.ID, Title: "Build feature",
    })
    taskSvc.Assign(tk.ID, ag.ID)

    // Set budget
    budgetSvc.CreatePolicy(budget.Policy{
        ID: "bp-1", CompanyID: co.ID,
        Scope: budget.ScopeAgent, ScopeID: ag.ID,
        AmountCents: 10000, WarnPercent: 0.8,
    })

    // Enqueue and claim heartbeat run
    run, _ := hbEngine.Enqueue(heartbeat.Run{
        ID: "run-1", CompanyID: co.ID, AgentID: ag.ID,
    })
    hbEngine.Claim(run.ID)

    // Atomic task checkout with lease
    taskSvc.Checkout(tk.ID, ag.ID, run.ID)

    // ... agent does work ...

    // Complete
    taskSvc.Complete(tk.ID, run.ID)
    hbEngine.Complete(run.ID, `{"result":"success"}`, 1000, 500, 25)
}
```

## Testing

```bash
go test ./...
```

170+ test functions covering:

| Package | Tests | Coverage |
|---------|-------|---------|
| store | 12 | CRUD, persistence, ordering |
| company | 13 | CRUD, slug uniqueness, config defaults |
| agent | 17 | Hire/fire, pause, org hierarchy, cycles |
| task | 24 | Checkout, leases, delegation, transitions |
| budget | 17 | 3-tier enforcement, incidents, resolution |
| heartbeat | 22 | Queue, claim, complete, leases, reaping |
| routine | 21 | Cron parsing, scheduling, catch-up |
| approval | 15 | Submit, approve, reject, revision |
| cost | 13 | Recording, aggregation, billing codes |
| goal | 16 | Hierarchy, completion, default goal |

## Improvements Over Original

| Feature | paperclip (TS) | paperclip-go |
|---------|---------------|--------------|
| Lock recovery | Manual (#1971 bug) | Automatic lease expiry |
| Dependencies | 24 production | 0 (stdlib only) |
| Binary size | Node.js + deps | Single Go binary |
| Concurrency | Promise-based, single-threaded | Goroutines + RWMutex |
| Data store | Requires PostgreSQL | In-memory + JSON persistence |
| Horizontal scaling | In-memory state blocks it | No in-memory coordination |
| Crash recovery | Stale locks accumulate | Expired leases auto-release |
| Type safety | Runtime (Zod + Ajv) | Compile-time |

## Scope

### Implemented (core orchestration)
- Company management (multi-tenant)
- Agent registry (hire/fire/pause, org hierarchy)
- Task system (atomic checkout, delegation, audit)
- Budget enforcement (3-tier, auto-pause)
- Heartbeat engine (queue, lease, reaping)
- Routine scheduling (cron, catch-up, concurrency)
- Approval governance (workflows, comments)
- Cost tracking (multi-dimensional)
- Goal management (hierarchical)
- CLI for all operations

### Out of Scope (not reimplemented)
- React UI (frontend concern)
- Individual agent adapters (claude-local, codex-local, etc.)
- Plugin system (20+ services, integration concern)
- Better Auth integration
- S3 storage, image processing

## Environment

```
PAPERCLIP_DATA_DIR  # Data directory for JSON persistence (default: .paperclip)
```

## License

MIT

---

Inspired by [paperclip](https://github.com/paperclipai/paperclip) — Open-source orchestration for zero-human companies.
