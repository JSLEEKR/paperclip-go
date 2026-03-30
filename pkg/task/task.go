// Package task manages the task lifecycle — creation, assignment, delegation,
// atomic checkout with lease-based locking, and audit logging.
package task

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Status represents a task's lifecycle state.
type Status string

const (
	StatusBacklog    Status = "backlog"
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"
)

// ValidTransitions defines allowed status transitions.
var ValidTransitions = map[Status][]Status{
	StatusBacklog:    {StatusTodo, StatusCancelled},
	StatusTodo:       {StatusInProgress, StatusBacklog, StatusCancelled},
	StatusInProgress: {StatusInReview, StatusBlocked, StatusDone, StatusCancelled, StatusTodo},
	StatusInReview:   {StatusDone, StatusInProgress, StatusCancelled},
	StatusBlocked:    {StatusTodo, StatusInProgress, StatusCancelled},
	StatusDone:       {StatusTodo}, // reopen
	StatusCancelled:  {StatusTodo}, // reopen
}

// Priority represents task priority.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// OriginKind indicates how a task was created.
type OriginKind string

const (
	OriginManual   OriginKind = "manual"
	OriginRoutine  OriginKind = "routine_execution"
	OriginDelegate OriginKind = "delegation"
)

// Task represents a work item (issue) in the system.
type Task struct {
	ID               string     `json:"id"`
	CompanyID        string     `json:"company_id"`
	ProjectID        string     `json:"project_id,omitempty"`
	ParentID         string     `json:"parent_id,omitempty"`
	Title            string     `json:"title"`
	Description      string     `json:"description,omitempty"`
	Status           Status     `json:"status"`
	Priority         Priority   `json:"priority"`
	AssigneeAgentID  string     `json:"assignee_agent_id,omitempty"`
	CheckoutRunID    string     `json:"checkout_run_id,omitempty"`
	CheckoutLeaseExp *time.Time `json:"checkout_lease_exp,omitempty"`
	RequestDepth     int        `json:"request_depth"`
	BillingCode      string     `json:"billing_code,omitempty"`
	OriginKind       OriginKind `json:"origin_kind"`
	Tags             []string   `json:"tags,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// GetID implements store.Record.
func (t Task) GetID() string { return t.ID }

// GetCompanyID implements store.Record.
func (t Task) GetCompanyID() string { return t.CompanyID }

// Validate checks required fields.
func (t *Task) Validate() error {
	if t.ID == "" {
		return errors.New("task id is required")
	}
	if t.CompanyID == "" {
		return errors.New("task company_id is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return errors.New("task title is required")
	}
	if t.Priority == "" {
		t.Priority = PriorityMedium
	}
	if t.OriginKind == "" {
		t.OriginKind = OriginManual
	}
	return nil
}

// IsTerminal returns true if the task is in a terminal state.
func (t *Task) IsTerminal() bool {
	return t.Status == StatusDone || t.Status == StatusCancelled
}

// IsCheckedOut returns true if an active lease exists.
func (t *Task) IsCheckedOut() bool {
	if t.CheckoutRunID == "" {
		return false
	}
	if t.CheckoutLeaseExp != nil && time.Now().After(*t.CheckoutLeaseExp) {
		return false // lease expired
	}
	return true
}

// CanTransition checks if a status transition is valid.
func (t *Task) CanTransition(to Status) bool {
	valid, ok := ValidTransitions[t.Status]
	if !ok {
		return false
	}
	for _, s := range valid {
		if s == to {
			return true
		}
	}
	return false
}

// AuditEntry represents a task lifecycle event.
type AuditEntry struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	CompanyID string    `json:"company_id"`
	Action    string    `json:"action"`
	AgentID   string    `json:"agent_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	OldStatus Status    `json:"old_status,omitempty"`
	NewStatus Status    `json:"new_status,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GetID implements store.Record.
func (e AuditEntry) GetID() string { return e.ID }

// GetCompanyID implements store.Record.
func (e AuditEntry) GetCompanyID() string { return e.CompanyID }

// DefaultLeaseDuration is the default checkout lease duration.
const DefaultLeaseDuration = 30 * time.Minute

// Service manages task lifecycle and checkout.
type Service struct {
	mu            sync.RWMutex
	tasks         *store.Collection[Task]
	audit         *store.Collection[AuditEntry]
	st            *store.Store
	leaseDuration time.Duration
	nextAuditID   int
}

// NewService creates a new task service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		tasks:         store.NewCollection[Task](),
		audit:         store.NewCollection[AuditEntry](),
		st:            s,
		leaseDuration: DefaultLeaseDuration,
	}
	_ = store.LoadCollection(s, "tasks", svc.tasks)
	_ = store.LoadCollection(s, "task_audit", svc.audit)
	return svc
}

// SetLeaseDuration configures the checkout lease duration.
func (s *Service) SetLeaseDuration(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaseDuration = d
}

// Create adds a new task.
func (s *Service) Create(t Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = StatusBacklog
	}

	if err := t.Validate(); err != nil {
		return Task{}, fmt.Errorf("validate task: %w", err)
	}

	// Validate parent exists if specified
	if t.ParentID != "" {
		if _, ok := s.tasks.Get(t.ParentID); !ok {
			return Task{}, fmt.Errorf("parent task %q not found", t.ParentID)
		}
	}

	s.tasks.Put(t)
	s.recordAudit(t.ID, t.CompanyID, "created", "", "", "", Status(t.Status), "")
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return t, nil
}

// Get retrieves a task by ID.
func (s *Service) Get(id string) (Task, error) {
	t, ok := s.tasks.Get(id)
	if !ok {
		return Task{}, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

// List returns all tasks.
func (s *Service) List() []Task {
	return s.tasks.List()
}

// ListByCompany returns tasks for a company.
func (s *Service) ListByCompany(companyID string) []Task {
	return s.tasks.Filter(func(t Task) bool {
		return t.CompanyID == companyID
	})
}

// ListByAssignee returns tasks assigned to an agent.
func (s *Service) ListByAssignee(agentID string) []Task {
	return s.tasks.Filter(func(t Task) bool {
		return t.AssigneeAgentID == agentID
	})
}

// ListSubtasks returns child tasks.
func (s *Service) ListSubtasks(parentID string) []Task {
	return s.tasks.Filter(func(t Task) bool {
		return t.ParentID == parentID
	})
}

// Assign assigns a task to an agent.
func (s *Service) Assign(taskID, agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if t.IsTerminal() {
		return fmt.Errorf("cannot assign terminal task %q (status: %s)", taskID, t.Status)
	}

	oldAgent := t.AssigneeAgentID
	t.AssigneeAgentID = agentID
	if t.Status == StatusBacklog {
		t.Status = StatusTodo
	}
	t.UpdatedAt = time.Now().UTC()

	s.tasks.Put(t)
	s.recordAudit(taskID, t.CompanyID, "assigned", agentID, "", "", "", fmt.Sprintf("from=%s", oldAgent))
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

// Checkout atomically claims a task with a lease. Returns error if already checked out.
func (s *Service) Checkout(taskID, agentID, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	// Check if already checked out with valid lease
	if t.IsCheckedOut() && t.CheckoutRunID != runID {
		return fmt.Errorf("task %q already checked out by run %q", taskID, t.CheckoutRunID)
	}

	oldStatus := t.Status
	exp := time.Now().UTC().Add(s.leaseDuration)
	t.CheckoutRunID = runID
	t.CheckoutLeaseExp = &exp
	t.AssigneeAgentID = agentID
	t.Status = StatusInProgress
	t.UpdatedAt = time.Now().UTC()

	s.tasks.Put(t)
	s.recordAudit(taskID, t.CompanyID, "checkout", agentID, runID, oldStatus, StatusInProgress, "")
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

// RenewLease extends the checkout lease.
func (s *Service) RenewLease(taskID, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if t.CheckoutRunID != runID {
		return fmt.Errorf("run %q does not own checkout on task %q", runID, taskID)
	}

	exp := time.Now().UTC().Add(s.leaseDuration)
	t.CheckoutLeaseExp = &exp
	t.UpdatedAt = time.Now().UTC()
	s.tasks.Put(t)
	return nil
}

// Release releases a task checkout, returning it to todo.
func (s *Service) Release(taskID, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if t.CheckoutRunID != "" && t.CheckoutRunID != runID {
		return fmt.Errorf("run %q does not own checkout on task %q", runID, taskID)
	}

	oldStatus := t.Status
	t.CheckoutRunID = ""
	t.CheckoutLeaseExp = nil
	t.AssigneeAgentID = ""
	t.Status = StatusTodo
	t.UpdatedAt = time.Now().UTC()

	s.tasks.Put(t)
	s.recordAudit(taskID, t.CompanyID, "released", "", runID, oldStatus, StatusTodo, "")
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

// Complete marks a task as done.
func (s *Service) Complete(taskID, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if t.CheckoutRunID != "" && t.CheckoutRunID != runID {
		return fmt.Errorf("run %q does not own checkout on task %q", runID, taskID)
	}

	oldStatus := t.Status
	now := time.Now().UTC()
	t.Status = StatusDone
	t.CompletedAt = &now
	t.CheckoutRunID = ""
	t.CheckoutLeaseExp = nil
	t.UpdatedAt = now

	s.tasks.Put(t)
	s.recordAudit(taskID, t.CompanyID, "completed", t.AssigneeAgentID, runID, oldStatus, StatusDone, "")
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

// Transition changes a task's status with validation.
func (s *Service) Transition(taskID string, newStatus Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Get(taskID)
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if !t.CanTransition(newStatus) {
		return fmt.Errorf("invalid transition %s → %s for task %q", t.Status, newStatus, taskID)
	}

	oldStatus := t.Status
	t.Status = newStatus
	t.UpdatedAt = time.Now().UTC()

	if newStatus == StatusDone {
		now := time.Now().UTC()
		t.CompletedAt = &now
	}

	s.tasks.Put(t)
	s.recordAudit(taskID, t.CompanyID, "transition", "", "", oldStatus, newStatus, "")
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

// Delegate creates a sub-task and assigns it to another agent.
func (s *Service) Delegate(parentID, childID, childTitle, fromAgent, toAgent string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parent, ok := s.tasks.Get(parentID)
	if !ok {
		return Task{}, fmt.Errorf("parent task %q not found", parentID)
	}

	now := time.Now().UTC()
	child := Task{
		ID:              childID,
		CompanyID:       parent.CompanyID,
		ProjectID:       parent.ProjectID,
		ParentID:        parentID,
		Title:           childTitle,
		Status:          StatusTodo,
		Priority:        parent.Priority,
		AssigneeAgentID: toAgent,
		RequestDepth:    parent.RequestDepth + 1,
		BillingCode:     parent.BillingCode,
		OriginKind:      OriginDelegate,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := child.Validate(); err != nil {
		return Task{}, fmt.Errorf("validate delegated task: %w", err)
	}

	s.tasks.Put(child)
	s.recordAudit(childID, child.CompanyID, "delegated", toAgent, "",
		"", StatusTodo, fmt.Sprintf("from=%s parent=%s depth=%d", fromAgent, parentID, child.RequestDepth))
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return child, nil
}

// ReapExpiredLeases finds and releases tasks with expired leases.
func (s *Service) ReapExpiredLeases() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var reaped []string

	for _, t := range s.tasks.List() {
		if t.CheckoutRunID != "" && t.CheckoutLeaseExp != nil && now.After(*t.CheckoutLeaseExp) {
			oldStatus := t.Status
			t.CheckoutRunID = ""
			t.CheckoutLeaseExp = nil
			t.Status = StatusTodo
			t.UpdatedAt = time.Now().UTC()
			s.tasks.Put(t)
			s.recordAudit(t.ID, t.CompanyID, "lease_expired", "", "", oldStatus, StatusTodo, "auto-reaped")
			reaped = append(reaped, t.ID)
		}
	}

	if len(reaped) > 0 {
		_ = store.SaveCollection(s.st, "tasks", s.tasks)
	}
	return reaped
}

// GetAuditLog returns audit entries for a task.
func (s *Service) GetAuditLog(taskID string) []AuditEntry {
	return s.audit.Filter(func(e AuditEntry) bool {
		return e.TaskID == taskID
	})
}

// Count returns total task count.
func (s *Service) Count() int {
	return s.tasks.Count()
}

// Delete removes a task.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.tasks.Delete(id) {
		return fmt.Errorf("task %q not found", id)
	}
	_ = store.SaveCollection(s.st, "tasks", s.tasks)
	return nil
}

func (s *Service) recordAudit(taskID, companyID, action, agentID, runID string, oldStatus, newStatus Status, detail string) {
	s.nextAuditID++
	entry := AuditEntry{
		ID:        fmt.Sprintf("audit-%d", s.nextAuditID),
		TaskID:    taskID,
		CompanyID: companyID,
		Action:    action,
		AgentID:   agentID,
		RunID:     runID,
		OldStatus: oldStatus,
		NewStatus: newStatus,
		Detail:    detail,
		Timestamp: time.Now().UTC(),
	}
	s.audit.Put(entry)
}
