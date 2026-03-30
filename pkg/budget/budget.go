// Package budget enforces 3-tier budget management — visibility, soft alerts,
// and hard ceilings with auto-pause.
package budget

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Scope defines what a budget policy applies to.
type Scope string

const (
	ScopeCompany Scope = "company"
	ScopeAgent   Scope = "agent"
	ScopeProject Scope = "project"
)

// Window defines the budget time window.
type Window string

const (
	WindowMonthly  Window = "calendar_month"
	WindowLifetime Window = "lifetime"
)

// IncidentSeverity indicates budget incident severity.
type IncidentSeverity string

const (
	SeveritySoft IncidentSeverity = "soft"
	SeverityHard IncidentSeverity = "hard"
)

// IncidentStatus tracks incident lifecycle.
type IncidentStatus string

const (
	IncidentOpen     IncidentStatus = "open"
	IncidentResolved IncidentStatus = "resolved"
	IncidentDismissed IncidentStatus = "dismissed"
)

// Policy defines a budget constraint.
type Policy struct {
	ID           string  `json:"id"`
	CompanyID    string  `json:"company_id"`
	Scope        Scope   `json:"scope"`
	ScopeID      string  `json:"scope_id"` // agent/project/company ID
	AmountCents  int64   `json:"amount_cents"`
	WarnPercent  float64 `json:"warn_percent"` // 0.0 - 1.0
	Window       Window  `json:"window"`
	Enabled      bool    `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GetID implements store.Record.
func (p Policy) GetID() string { return p.ID }

// GetCompanyID implements store.Record.
func (p Policy) GetCompanyID() string { return p.CompanyID }

// Validate checks policy fields.
func (p *Policy) Validate() error {
	if p.ID == "" {
		return errors.New("policy id is required")
	}
	if p.CompanyID == "" {
		return errors.New("policy company_id is required")
	}
	if p.ScopeID == "" {
		return errors.New("policy scope_id is required")
	}
	if p.AmountCents <= 0 {
		return errors.New("policy amount must be positive")
	}
	if p.WarnPercent < 0 || p.WarnPercent > 1.0 {
		p.WarnPercent = 0.8
	}
	if p.Window == "" {
		p.Window = WindowMonthly
	}
	return nil
}

// CostEvent represents a single cost occurrence.
type CostEvent struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	AgentID     string    `json:"agent_id"`
	ProjectID   string    `json:"project_id,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	AmountCents int64     `json:"amount_cents"`
	BillingCode string    `json:"billing_code,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// GetID implements store.Record.
func (e CostEvent) GetID() string { return e.ID }

// GetCompanyID implements store.Record.
func (e CostEvent) GetCompanyID() string { return e.CompanyID }

// Incident represents a budget threshold breach.
type Incident struct {
	ID         string           `json:"id"`
	CompanyID  string           `json:"company_id"`
	PolicyID   string           `json:"policy_id"`
	Severity   IncidentSeverity `json:"severity"`
	Status     IncidentStatus   `json:"status"`
	Observed   int64            `json:"observed_cents"`
	Limit      int64            `json:"limit_cents"`
	Message    string           `json:"message"`
	Resolution string           `json:"resolution,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	ResolvedAt *time.Time       `json:"resolved_at,omitempty"`
}

// GetID implements store.Record.
func (i Incident) GetID() string { return i.ID }

// GetCompanyID implements store.Record.
func (i Incident) GetCompanyID() string { return i.CompanyID }

// EvalResult is the outcome of evaluating a cost event against policies.
type EvalResult struct {
	Blocked   bool       `json:"blocked"`
	Incidents []Incident `json:"incidents,omitempty"`
}

// Service manages budget policies, cost events, and incidents.
type Service struct {
	mu             sync.RWMutex
	policies       *store.Collection[Policy]
	events         *store.Collection[CostEvent]
	incidents      *store.Collection[Incident]
	st             *store.Store
	nextIncidentID int
	// PauseCallback is called when a hard budget limit triggers auto-pause.
	PauseCallback func(scope Scope, scopeID, reason string)
}

// NewService creates a new budget service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		policies:  store.NewCollection[Policy](),
		events:    store.NewCollection[CostEvent](),
		incidents: store.NewCollection[Incident](),
		st:        s,
	}
	_ = store.LoadCollection(s, "budget_policies", svc.policies)
	_ = store.LoadCollection(s, "cost_events", svc.events)
	_ = store.LoadCollection(s, "budget_incidents", svc.incidents)
	return svc
}

// CreatePolicy adds a new budget policy.
func (s *Service) CreatePolicy(p Policy) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	p.Enabled = true

	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("validate policy: %w", err)
	}

	s.policies.Put(p)
	_ = store.SaveCollection(s.st, "budget_policies", s.policies)
	return p, nil
}

// GetPolicy retrieves a policy by ID.
func (s *Service) GetPolicy(id string) (Policy, error) {
	p, ok := s.policies.Get(id)
	if !ok {
		return Policy{}, fmt.Errorf("policy %q not found", id)
	}
	return p, nil
}

// ListPolicies returns all policies for a company.
func (s *Service) ListPolicies(companyID string) []Policy {
	return s.policies.Filter(func(p Policy) bool {
		return p.CompanyID == companyID && p.Enabled
	})
}

// UpdatePolicy modifies a policy.
func (s *Service) UpdatePolicy(id string, fn func(*Policy) error) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.policies.Get(id)
	if !ok {
		return Policy{}, fmt.Errorf("policy %q not found", id)
	}

	if err := fn(&p); err != nil {
		return Policy{}, err
	}
	p.UpdatedAt = time.Now().UTC()

	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("validate policy: %w", err)
	}

	s.policies.Put(p)
	_ = store.SaveCollection(s.st, "budget_policies", s.policies)
	return p, nil
}

// DeletePolicy removes a policy.
func (s *Service) DeletePolicy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.policies.Delete(id) {
		return fmt.Errorf("policy %q not found", id)
	}
	_ = store.SaveCollection(s.st, "budget_policies", s.policies)
	return nil
}

// RecordCost records a cost event and evaluates it against policies.
func (s *Service) RecordCost(e CostEvent) (EvalResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.ID == "" {
		return EvalResult{}, errors.New("cost event id is required")
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	s.events.Put(e)
	_ = store.SaveCollection(s.st, "cost_events", s.events)

	return s.evaluateUnsafe(e), nil
}

// evaluateUnsafe evaluates a cost event against matching policies. Caller must hold lock.
func (s *Service) evaluateUnsafe(e CostEvent) EvalResult {
	result := EvalResult{}

	matchingPolicies := s.findMatchingPolicies(e)

	for _, policy := range matchingPolicies {
		observed := s.computeObserved(policy)

		if observed >= policy.AmountCents {
			// Hard ceiling
			incident := s.createIncident(policy, observed, SeverityHard,
				fmt.Sprintf("Budget exceeded: %d/%d cents (%s scope: %s)",
					observed, policy.AmountCents, policy.Scope, policy.ScopeID))
			result.Incidents = append(result.Incidents, incident)
			result.Blocked = true

			if s.PauseCallback != nil {
				s.PauseCallback(policy.Scope, policy.ScopeID,
					fmt.Sprintf("budget hard ceiling: %d/%d cents", observed, policy.AmountCents))
			}
		} else if float64(observed) >= policy.WarnPercent*float64(policy.AmountCents) {
			// Soft alert
			incident := s.createIncident(policy, observed, SeveritySoft,
				fmt.Sprintf("Budget warning: %d/%d cents (%.0f%% threshold, %s scope: %s)",
					observed, policy.AmountCents, policy.WarnPercent*100, policy.Scope, policy.ScopeID))
			result.Incidents = append(result.Incidents, incident)
		}
	}

	return result
}

// findMatchingPolicies returns policies that apply to a cost event.
func (s *Service) findMatchingPolicies(e CostEvent) []Policy {
	return s.policies.Filter(func(p Policy) bool {
		if p.CompanyID != e.CompanyID || !p.Enabled {
			return false
		}
		switch p.Scope {
		case ScopeCompany:
			return p.ScopeID == e.CompanyID
		case ScopeAgent:
			return p.ScopeID == e.AgentID
		case ScopeProject:
			return p.ScopeID == e.ProjectID
		}
		return false
	})
}

// computeObserved sums cost events matching a policy within its window.
func (s *Service) computeObserved(p Policy) int64 {
	var windowStart time.Time
	if p.Window == WindowMonthly {
		now := time.Now().UTC()
		windowStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	// WindowLifetime: no start boundary

	var total int64
	for _, e := range s.events.List() {
		if e.CompanyID != p.CompanyID {
			continue
		}
		if !windowStart.IsZero() && e.Timestamp.Before(windowStart) {
			continue
		}

		match := false
		switch p.Scope {
		case ScopeCompany:
			match = true
		case ScopeAgent:
			match = e.AgentID == p.ScopeID
		case ScopeProject:
			match = e.ProjectID == p.ScopeID
		}
		if match {
			total += e.AmountCents
		}
	}
	return total
}

func (s *Service) createIncident(p Policy, observed int64, severity IncidentSeverity, message string) Incident {
	s.nextIncidentID++
	incident := Incident{
		ID:        fmt.Sprintf("incident-%d", s.nextIncidentID),
		CompanyID: p.CompanyID,
		PolicyID:  p.ID,
		Severity:  severity,
		Status:    IncidentOpen,
		Observed:  observed,
		Limit:     p.AmountCents,
		Message:   message,
		CreatedAt: time.Now().UTC(),
	}
	s.incidents.Put(incident)
	return incident
}

// GetInvocationBlock checks if any hard budget block applies.
// Returns true + reason if blocked.
func (s *Service) GetInvocationBlock(companyID, agentID, projectID string) (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check hierarchically: company → agent → project
	for _, incident := range s.incidents.List() {
		if incident.CompanyID != companyID || incident.Severity != SeverityHard || incident.Status != IncidentOpen {
			continue
		}

		policy, ok := s.policies.Get(incident.PolicyID)
		if !ok {
			continue
		}

		switch policy.Scope {
		case ScopeCompany:
			if policy.ScopeID == companyID {
				return true, incident.Message
			}
		case ScopeAgent:
			if policy.ScopeID == agentID {
				return true, incident.Message
			}
		case ScopeProject:
			if policy.ScopeID == projectID {
				return true, incident.Message
			}
		}
	}
	return false, ""
}

// ResolveIncident resolves a budget incident.
func (s *Service) ResolveIncident(id, resolution string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	incident, ok := s.incidents.Get(id)
	if !ok {
		return fmt.Errorf("incident %q not found", id)
	}

	now := time.Now().UTC()
	incident.Status = IncidentResolved
	incident.Resolution = resolution
	incident.ResolvedAt = &now
	s.incidents.Put(incident)
	_ = store.SaveCollection(s.st, "budget_incidents", s.incidents)
	return nil
}

// ListIncidents returns incidents for a company.
func (s *Service) ListIncidents(companyID string) []Incident {
	return s.incidents.Filter(func(i Incident) bool {
		return i.CompanyID == companyID
	})
}

// ListOpenIncidents returns unresolved incidents.
func (s *Service) ListOpenIncidents(companyID string) []Incident {
	return s.incidents.Filter(func(i Incident) bool {
		return i.CompanyID == companyID && i.Status == IncidentOpen
	})
}

// TotalSpent returns total spending for a scope within the current window.
func (s *Service) TotalSpent(companyID string, scope Scope, scopeID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	windowStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var total int64
	for _, e := range s.events.List() {
		if e.CompanyID != companyID || e.Timestamp.Before(windowStart) {
			continue
		}
		match := false
		switch scope {
		case ScopeCompany:
			match = true
		case ScopeAgent:
			match = e.AgentID == scopeID
		case ScopeProject:
			match = e.ProjectID == scopeID
		}
		if match {
			total += e.AmountCents
		}
	}
	return total
}
