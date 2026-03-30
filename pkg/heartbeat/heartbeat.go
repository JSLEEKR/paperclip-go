// Package heartbeat manages the run lifecycle — queue, claim with lease,
// execute, timeout recovery, and orphan reaping.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// RunStatus represents a heartbeat run's lifecycle state.
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	RunTimedOut  RunStatus = "timed_out"
)

// InvocationSource indicates what triggered a run.
type InvocationSource string

const (
	SourceOnDemand   InvocationSource = "on_demand"
	SourceTimer      InvocationSource = "timer"
	SourceAssignment InvocationSource = "assignment"
	SourceAutomation InvocationSource = "automation"
)

// Run represents a single heartbeat execution.
type Run struct {
	ID               string           `json:"id"`
	CompanyID        string           `json:"company_id"`
	AgentID          string           `json:"agent_id"`
	TaskID           string           `json:"task_id,omitempty"`
	Status           RunStatus        `json:"status"`
	Source           InvocationSource `json:"source"`
	LeaseExpiresAt   *time.Time       `json:"lease_expires_at,omitempty"`
	ProcessPID       int              `json:"process_pid,omitempty"`
	ExitCode         *int             `json:"exit_code,omitempty"`
	ErrorMessage     string           `json:"error_message,omitempty"`
	InputTokens      int64            `json:"input_tokens"`
	OutputTokens     int64            `json:"output_tokens"`
	CostCents        int64            `json:"cost_cents"`
	ResultJSON       string           `json:"result_json,omitempty"`
	SessionIDBefore  string           `json:"session_id_before,omitempty"`
	SessionIDAfter   string           `json:"session_id_after,omitempty"`
	QueuedAt         time.Time        `json:"queued_at"`
	StartedAt        *time.Time       `json:"started_at,omitempty"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// GetID implements store.Record.
func (r Run) GetID() string { return r.ID }

// GetCompanyID implements store.Record.
func (r Run) GetCompanyID() string { return r.CompanyID }

// Duration returns how long the run took.
func (r *Run) Duration() time.Duration {
	if r.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if r.CompletedAt != nil {
		end = *r.CompletedAt
	}
	return end.Sub(*r.StartedAt)
}

// IsTerminal returns true if the run is in a final state.
func (r *Run) IsTerminal() bool {
	return r.Status == RunCompleted || r.Status == RunFailed ||
		r.Status == RunCancelled || r.Status == RunTimedOut
}

// DefaultLeaseDuration for heartbeat runs.
const DefaultLeaseDuration = 30 * time.Minute

// DefaultTickInterval for the queue processor.
const DefaultTickInterval = 5 * time.Second

// ExecuteFunc is the function called to actually execute a run.
type ExecuteFunc func(ctx context.Context, run Run) (result string, inputTokens, outputTokens, costCents int64, err error)

// BlockCheckFunc checks if an agent is blocked (budget, paused, etc.).
type BlockCheckFunc func(companyID, agentID string) (blocked bool, reason string)

// Engine manages the heartbeat run lifecycle.
type Engine struct {
	mu             sync.RWMutex
	runs           *store.Collection[Run]
	st             *store.Store
	leaseDuration  time.Duration
	tickInterval   time.Duration
	executeFunc    ExecuteFunc
	blockCheck     BlockCheckFunc
	maxConcurrent  map[string]int // agentID → max concurrent
	cancel         context.CancelFunc
	stopped        chan struct{}
}

// NewEngine creates a new heartbeat engine.
func NewEngine(s *store.Store) *Engine {
	eng := &Engine{
		runs:          store.NewCollection[Run](),
		st:            s,
		leaseDuration: DefaultLeaseDuration,
		tickInterval:  DefaultTickInterval,
		maxConcurrent: make(map[string]int),
		stopped:       make(chan struct{}),
	}
	_ = store.LoadCollection(s, "heartbeat_runs", eng.runs)
	return eng
}

// SetExecuteFunc sets the execution callback.
func (e *Engine) SetExecuteFunc(fn ExecuteFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executeFunc = fn
}

// SetBlockCheck sets the block check callback.
func (e *Engine) SetBlockCheck(fn BlockCheckFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blockCheck = fn
}

// SetLeaseDuration configures lease duration.
func (e *Engine) SetLeaseDuration(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.leaseDuration = d
}

// SetMaxConcurrent sets max concurrent runs for an agent.
func (e *Engine) SetMaxConcurrent(agentID string, max int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if max <= 0 {
		max = 1
	}
	if max > 10 {
		max = 10
	}
	e.maxConcurrent[agentID] = max
}

// Enqueue creates a new queued run.
func (e *Engine) Enqueue(run Run) (Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if run.ID == "" {
		return Run{}, errors.New("run id is required")
	}
	if run.AgentID == "" {
		return Run{}, errors.New("run agent_id is required")
	}
	if run.CompanyID == "" {
		return Run{}, errors.New("run company_id is required")
	}

	now := time.Now().UTC()
	run.Status = RunQueued
	run.QueuedAt = now
	run.CreatedAt = now
	if run.Source == "" {
		run.Source = SourceOnDemand
	}

	e.runs.Put(run)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return run, nil
}

// Claim atomically transitions a queued run to running with a lease.
func (e *Engine) Claim(runID string) (Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs.Get(runID)
	if !ok {
		return Run{}, fmt.Errorf("run %q not found", runID)
	}

	if run.Status != RunQueued {
		return Run{}, fmt.Errorf("run %q is not queued (status: %s)", runID, run.Status)
	}

	// Check agent block
	if e.blockCheck != nil {
		if blocked, reason := e.blockCheck(run.CompanyID, run.AgentID); blocked {
			return Run{}, fmt.Errorf("agent %q blocked: %s", run.AgentID, reason)
		}
	}

	// Check max concurrent
	if e.isAtMaxConcurrent(run.AgentID) {
		return Run{}, fmt.Errorf("agent %q at max concurrent runs", run.AgentID)
	}

	now := time.Now().UTC()
	exp := now.Add(e.leaseDuration)
	run.Status = RunRunning
	run.StartedAt = &now
	run.LeaseExpiresAt = &exp

	e.runs.Put(run)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return run, nil
}

// ClaimNext claims the next queued run for an agent.
func (e *Engine) ClaimNext(agentID string) (Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check block
	queued := e.runs.Filter(func(r Run) bool {
		return r.AgentID == agentID && r.Status == RunQueued
	})

	if len(queued) == 0 {
		return Run{}, fmt.Errorf("no queued runs for agent %q", agentID)
	}

	// Find oldest queued
	oldest := queued[0]
	for _, r := range queued[1:] {
		if r.QueuedAt.Before(oldest.QueuedAt) {
			oldest = r
		}
	}

	if e.blockCheck != nil {
		if blocked, reason := e.blockCheck(oldest.CompanyID, agentID); blocked {
			return Run{}, fmt.Errorf("agent %q blocked: %s", agentID, reason)
		}
	}

	if e.isAtMaxConcurrent(agentID) {
		return Run{}, fmt.Errorf("agent %q at max concurrent runs", agentID)
	}

	now := time.Now().UTC()
	exp := now.Add(e.leaseDuration)
	oldest.Status = RunRunning
	oldest.StartedAt = &now
	oldest.LeaseExpiresAt = &exp

	e.runs.Put(oldest)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return oldest, nil
}

// Complete marks a run as completed.
func (e *Engine) Complete(runID string, result string, inputTokens, outputTokens, costCents int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs.Get(runID)
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	now := time.Now().UTC()
	run.Status = RunCompleted
	run.CompletedAt = &now
	run.ResultJSON = result
	run.InputTokens = inputTokens
	run.OutputTokens = outputTokens
	run.CostCents = costCents
	run.LeaseExpiresAt = nil

	e.runs.Put(run)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return nil
}

// Fail marks a run as failed.
func (e *Engine) Fail(runID string, errMsg string, exitCode int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs.Get(runID)
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	now := time.Now().UTC()
	run.Status = RunFailed
	run.CompletedAt = &now
	run.ErrorMessage = errMsg
	run.ExitCode = &exitCode
	run.LeaseExpiresAt = nil

	e.runs.Put(run)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return nil
}

// Cancel cancels a run.
func (e *Engine) Cancel(runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs.Get(runID)
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	if run.IsTerminal() {
		return fmt.Errorf("run %q is already terminal (status: %s)", runID, run.Status)
	}

	now := time.Now().UTC()
	run.Status = RunCancelled
	run.CompletedAt = &now
	run.LeaseExpiresAt = nil

	e.runs.Put(run)
	_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	return nil
}

// RenewLease extends a running run's lease.
func (e *Engine) RenewLease(runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	run, ok := e.runs.Get(runID)
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	if run.Status != RunRunning {
		return fmt.Errorf("run %q is not running", runID)
	}

	exp := time.Now().UTC().Add(e.leaseDuration)
	run.LeaseExpiresAt = &exp
	e.runs.Put(run)
	return nil
}

// ReapExpiredLeases finds runs with expired leases and marks them timed out.
func (e *Engine) ReapExpiredLeases() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	var reaped []string

	for _, r := range e.runs.List() {
		if r.Status == RunRunning && r.LeaseExpiresAt != nil && now.After(*r.LeaseExpiresAt) {
			nowUTC := time.Now().UTC()
			r.Status = RunTimedOut
			r.CompletedAt = &nowUTC
			r.ErrorMessage = "lease expired — automatic timeout recovery"
			r.LeaseExpiresAt = nil
			e.runs.Put(r)
			reaped = append(reaped, r.ID)
		}
	}

	if len(reaped) > 0 {
		_ = store.SaveCollection(e.st, "heartbeat_runs", e.runs)
	}
	return reaped
}

// Get retrieves a run by ID.
func (e *Engine) Get(runID string) (Run, error) {
	r, ok := e.runs.Get(runID)
	if !ok {
		return Run{}, fmt.Errorf("run %q not found", runID)
	}
	return r, nil
}

// List returns all runs.
func (e *Engine) List() []Run {
	return e.runs.List()
}

// ListByAgent returns runs for an agent.
func (e *Engine) ListByAgent(agentID string) []Run {
	return e.runs.Filter(func(r Run) bool {
		return r.AgentID == agentID
	})
}

// ListQueued returns queued runs for an agent.
func (e *Engine) ListQueued(agentID string) []Run {
	return e.runs.Filter(func(r Run) bool {
		return r.AgentID == agentID && r.Status == RunQueued
	})
}

// ListRunning returns running runs for an agent.
func (e *Engine) ListRunning(agentID string) []Run {
	return e.runs.Filter(func(r Run) bool {
		return r.AgentID == agentID && r.Status == RunRunning
	})
}

// ActiveRunCount returns the number of running (non-terminal) runs for an agent.
func (e *Engine) ActiveRunCount(agentID string) int {
	count := 0
	for _, r := range e.runs.List() {
		if r.AgentID == agentID && r.Status == RunRunning {
			count++
		}
	}
	return count
}

// Count returns total run count.
func (e *Engine) Count() int {
	return e.runs.Count()
}

// Start begins the background queue processor.
func (e *Engine) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	go func() {
		defer close(e.stopped)
		ticker := time.NewTicker(e.tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.ReapExpiredLeases()
				e.processQueue()
			}
		}
	}()
}

// Stop halts the background queue processor.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
		<-e.stopped
	}
}

// processQueue attempts to execute queued runs.
func (e *Engine) processQueue() {
	e.mu.RLock()
	executeFunc := e.executeFunc
	e.mu.RUnlock()

	if executeFunc == nil {
		return
	}

	queued := e.runs.Filter(func(r Run) bool {
		return r.Status == RunQueued
	})

	for _, run := range queued {
		claimed, err := e.Claim(run.ID)
		if err != nil {
			continue
		}

		go func(r Run) {
			ctx, cancel := context.WithTimeout(context.Background(), e.leaseDuration)
			defer cancel()

			result, inTok, outTok, cost, execErr := executeFunc(ctx, r)
			if execErr != nil {
				_ = e.Fail(r.ID, execErr.Error(), 1)
			} else {
				_ = e.Complete(r.ID, result, inTok, outTok, cost)
			}
		}(claimed)
	}
}

func (e *Engine) isAtMaxConcurrent(agentID string) bool {
	maxConc := 1
	if m, ok := e.maxConcurrent[agentID]; ok {
		maxConc = m
	}

	running := 0
	for _, r := range e.runs.List() {
		if r.AgentID == agentID && r.Status == RunRunning {
			running++
		}
	}
	return running >= maxConc
}
