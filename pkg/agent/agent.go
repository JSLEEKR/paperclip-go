// Package agent manages AI agent registration, org hierarchy,
// capabilities, and lifecycle (hire/fire/pause/resume).
package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Status represents an agent's operational status.
type Status string

const (
	StatusIdle    Status = "idle"
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusFired   Status = "fired"
)

// Role represents an agent's role within the company.
type Role string

const (
	RoleGeneral  Role = "general"
	RoleManager  Role = "manager"
	RoleExecutor Role = "executor"
	RoleLead     Role = "lead"
)

// Agent represents an AI employee in a company.
type Agent struct {
	ID               string            `json:"id"`
	CompanyID        string            `json:"company_id"`
	Name             string            `json:"name"`
	Role             Role              `json:"role"`
	Title            string            `json:"title,omitempty"`
	Status           Status            `json:"status"`
	ReportsTo        string            `json:"reports_to,omitempty"`
	Capabilities     []string          `json:"capabilities,omitempty"`
	AdapterType      string            `json:"adapter_type"`
	AdapterConfig    map[string]string `json:"adapter_config,omitempty"`
	BudgetMonthlyCents int64           `json:"budget_monthly_cents"`
	SpentMonthlyCents  int64           `json:"spent_monthly_cents"`
	PauseReason      string            `json:"pause_reason,omitempty"`
	MaxConcurrentRuns int              `json:"max_concurrent_runs"`
	LastHeartbeatAt  *time.Time        `json:"last_heartbeat_at,omitempty"`
	HiredAt          time.Time         `json:"hired_at"`
	FiredAt          *time.Time        `json:"fired_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// GetID implements store.Record.
func (a Agent) GetID() string { return a.ID }

// GetCompanyID implements store.Record.
func (a Agent) GetCompanyID() string { return a.CompanyID }

// Validate checks required fields.
func (a *Agent) Validate() error {
	if a.ID == "" {
		return errors.New("agent id is required")
	}
	if a.CompanyID == "" {
		return errors.New("agent company_id is required")
	}
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("agent name is required")
	}
	if a.Role == "" {
		a.Role = RoleGeneral
	}
	if a.AdapterType == "" {
		a.AdapterType = "process"
	}
	if a.MaxConcurrentRuns <= 0 {
		a.MaxConcurrentRuns = 1
	}
	if a.MaxConcurrentRuns > 10 {
		a.MaxConcurrentRuns = 10
	}
	return nil
}

// IsActive returns true if the agent can accept work.
func (a *Agent) IsActive() bool {
	return a.Status == StatusIdle || a.Status == StatusActive
}

// HasCapability checks if the agent has a specific capability.
func (a *Agent) HasCapability(cap string) bool {
	for _, c := range a.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// Service manages agent CRUD and org hierarchy.
type Service struct {
	mu     sync.RWMutex
	agents *store.Collection[Agent]
	st     *store.Store
}

// NewService creates a new agent service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		agents: store.NewCollection[Agent](),
		st:     s,
	}
	_ = store.LoadCollection(s, "agents", svc.agents)
	return svc
}

// Hire adds a new agent to a company.
func (s *Service) Hire(a Agent) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	a.HiredAt = now
	if a.Status == "" {
		a.Status = StatusIdle
	}

	if err := a.Validate(); err != nil {
		return Agent{}, fmt.Errorf("validate agent: %w", err)
	}

	// Check for reporting cycle
	if a.ReportsTo != "" {
		if err := s.checkCycle(a.ID, a.ReportsTo); err != nil {
			return Agent{}, err
		}
	}

	s.agents.Put(a)
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return a, nil
}

// Fire marks an agent as fired.
func (s *Service) Fire(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	now := time.Now().UTC()
	a.Status = StatusFired
	a.FiredAt = &now
	a.UpdatedAt = now
	s.agents.Put(a)
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return nil
}

// Pause pauses an agent with a reason.
func (s *Service) Pause(id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	a.Status = StatusPaused
	a.PauseReason = reason
	a.UpdatedAt = time.Now().UTC()
	s.agents.Put(a)
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return nil
}

// Resume resumes a paused agent.
func (s *Service) Resume(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	if a.Status != StatusPaused {
		return fmt.Errorf("agent %q is not paused (status: %s)", id, a.Status)
	}

	a.Status = StatusIdle
	a.PauseReason = ""
	a.UpdatedAt = time.Now().UTC()
	s.agents.Put(a)
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return nil
}

// Get retrieves an agent by ID.
func (s *Service) Get(id string) (Agent, error) {
	a, ok := s.agents.Get(id)
	if !ok {
		return Agent{}, fmt.Errorf("agent %q not found", id)
	}
	return a, nil
}

// List returns all agents.
func (s *Service) List() []Agent {
	return s.agents.List()
}

// ListByCompany returns agents belonging to a company.
func (s *Service) ListByCompany(companyID string) []Agent {
	return s.agents.Filter(func(a Agent) bool {
		return a.CompanyID == companyID
	})
}

// ListActive returns agents that can accept work for a company.
func (s *Service) ListActive(companyID string) []Agent {
	return s.agents.Filter(func(a Agent) bool {
		return a.CompanyID == companyID && a.IsActive()
	})
}

// DirectReports returns agents reporting to a given agent.
func (s *Service) DirectReports(agentID string) []Agent {
	return s.agents.Filter(func(a Agent) bool {
		return a.ReportsTo == agentID
	})
}

// ChainOfCommand returns the reporting chain from an agent up to the root.
func (s *Service) ChainOfCommand(agentID string) []Agent {
	var chain []Agent
	visited := make(map[string]bool)
	current := agentID

	for current != "" && !visited[current] {
		visited[current] = true
		a, ok := s.agents.Get(current)
		if !ok {
			break
		}
		chain = append(chain, a)
		current = a.ReportsTo
	}
	return chain
}

// Update modifies an agent.
func (s *Service) Update(id string, fn func(*Agent) error) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return Agent{}, fmt.Errorf("agent %q not found", id)
	}

	if err := fn(&a); err != nil {
		return Agent{}, err
	}
	a.UpdatedAt = time.Now().UTC()

	if err := a.Validate(); err != nil {
		return Agent{}, fmt.Errorf("validate agent: %w", err)
	}

	if a.ReportsTo != "" {
		if err := s.checkCycle(a.ID, a.ReportsTo); err != nil {
			return Agent{}, err
		}
	}

	s.agents.Put(a)
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return a, nil
}

// RecordHeartbeat updates the last heartbeat timestamp.
func (s *Service) RecordHeartbeat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	now := time.Now().UTC()
	a.LastHeartbeatAt = &now
	a.UpdatedAt = now
	s.agents.Put(a)
	return nil
}

// AddSpending adds to an agent's monthly spending.
func (s *Service) AddSpending(id string, cents int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents.Get(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	a.SpentMonthlyCents += cents
	a.UpdatedAt = time.Now().UTC()
	s.agents.Put(a)
	return nil
}

// Count returns total agent count.
func (s *Service) Count() int {
	return s.agents.Count()
}

// Delete removes an agent.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.agents.Delete(id) {
		return fmt.Errorf("agent %q not found", id)
	}
	_ = store.SaveCollection(s.st, "agents", s.agents)
	return nil
}

// checkCycle detects reporting cycles.
func (s *Service) checkCycle(agentID, reportsTo string) error {
	if agentID == reportsTo {
		return errors.New("agent cannot report to itself")
	}

	visited := map[string]bool{agentID: true}
	current := reportsTo
	for current != "" {
		if visited[current] {
			return fmt.Errorf("reporting cycle detected: %s -> %s", agentID, reportsTo)
		}
		visited[current] = true
		a, ok := s.agents.Get(current)
		if !ok {
			break
		}
		current = a.ReportsTo
	}
	return nil
}
