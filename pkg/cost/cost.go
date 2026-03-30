// Package cost tracks per-agent, per-task, per-provider cost attribution
// with billing code support and multi-dimensional aggregation.
package cost

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// BillingType categorizes costs.
type BillingType string

const (
	BillingLLM       BillingType = "llm"
	BillingCompute   BillingType = "compute"
	BillingStorage   BillingType = "storage"
	BillingNetwork   BillingType = "network"
	BillingOther     BillingType = "other"
)

// Event represents a single cost occurrence.
type Event struct {
	ID          string      `json:"id"`
	CompanyID   string      `json:"company_id"`
	AgentID     string      `json:"agent_id"`
	TaskID      string      `json:"task_id,omitempty"`
	RunID       string      `json:"run_id,omitempty"`
	ProjectID   string      `json:"project_id,omitempty"`
	Provider    string      `json:"provider"`
	BillingType BillingType `json:"billing_type"`
	BillingCode string      `json:"billing_code,omitempty"`
	AmountCents int64       `json:"amount_cents"`
	InputTokens int64       `json:"input_tokens,omitempty"`
	OutputTokens int64      `json:"output_tokens,omitempty"`
	Model       string      `json:"model,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
}

// GetID implements store.Record.
func (e Event) GetID() string { return e.ID }

// GetCompanyID implements store.Record.
func (e Event) GetCompanyID() string { return e.CompanyID }

// Validate checks required fields.
func (e *Event) Validate() error {
	if e.ID == "" {
		return errors.New("event id is required")
	}
	if e.CompanyID == "" {
		return errors.New("event company_id is required")
	}
	if e.AgentID == "" {
		return errors.New("event agent_id is required")
	}
	if e.Provider == "" {
		return errors.New("event provider is required")
	}
	if e.AmountCents < 0 {
		return errors.New("event amount cannot be negative")
	}
	if e.BillingType == "" {
		e.BillingType = BillingLLM
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	return nil
}

// Summary holds aggregated cost data.
type Summary struct {
	TotalCents   int64 `json:"total_cents"`
	EventCount   int   `json:"event_count"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// AgentSummary is a cost summary for a specific agent.
type AgentSummary struct {
	AgentID string  `json:"agent_id"`
	Summary Summary `json:"summary"`
}

// ProviderSummary is a cost summary for a specific provider.
type ProviderSummary struct {
	Provider string  `json:"provider"`
	Summary  Summary `json:"summary"`
}

// Service manages cost event recording and aggregation.
type Service struct {
	mu     sync.RWMutex
	events *store.Collection[Event]
	st     *store.Store
}

// NewService creates a new cost service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		events: store.NewCollection[Event](),
		st:     s,
	}
	_ = store.LoadCollection(s, "cost_events", svc.events)
	return svc
}

// Record records a cost event.
func (s *Service) Record(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := e.Validate(); err != nil {
		return Event{}, fmt.Errorf("validate event: %w", err)
	}

	s.events.Put(e)
	_ = store.SaveCollection(s.st, "cost_events", s.events)
	return e, nil
}

// Get retrieves an event by ID.
func (s *Service) Get(id string) (Event, error) {
	e, ok := s.events.Get(id)
	if !ok {
		return Event{}, fmt.Errorf("event %q not found", id)
	}
	return e, nil
}

// List returns all events.
func (s *Service) List() []Event {
	return s.events.List()
}

// ListByCompany returns events for a company.
func (s *Service) ListByCompany(companyID string) []Event {
	return s.events.Filter(func(e Event) bool {
		return e.CompanyID == companyID
	})
}

// ListByAgent returns events for an agent.
func (s *Service) ListByAgent(agentID string) []Event {
	return s.events.Filter(func(e Event) bool {
		return e.AgentID == agentID
	})
}

// ListByBillingCode returns events with a specific billing code.
func (s *Service) ListByBillingCode(code string) []Event {
	return s.events.Filter(func(e Event) bool {
		return e.BillingCode == code
	})
}

// TotalByCompany returns total cost for a company in a time range.
func (s *Service) TotalByCompany(companyID string, from, to time.Time) Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.aggregate(func(e Event) bool {
		return e.CompanyID == companyID &&
			!e.Timestamp.Before(from) &&
			e.Timestamp.Before(to)
	})
}

// TotalByAgent returns total cost for an agent in a time range.
func (s *Service) TotalByAgent(agentID string, from, to time.Time) Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.aggregate(func(e Event) bool {
		return e.AgentID == agentID &&
			!e.Timestamp.Before(from) &&
			e.Timestamp.Before(to)
	})
}

// TotalByTask returns total cost for a task.
func (s *Service) TotalByTask(taskID string) Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.aggregate(func(e Event) bool {
		return e.TaskID == taskID
	})
}

// ByAgent returns per-agent summaries for a company in a time range.
func (s *Service) ByAgent(companyID string, from, to time.Time) []AgentSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agentMap := make(map[string]*Summary)

	for _, e := range s.events.List() {
		if e.CompanyID != companyID || e.Timestamp.Before(from) || !e.Timestamp.Before(to) {
			continue
		}
		sum, ok := agentMap[e.AgentID]
		if !ok {
			sum = &Summary{}
			agentMap[e.AgentID] = sum
		}
		sum.TotalCents += e.AmountCents
		sum.EventCount++
		sum.InputTokens += e.InputTokens
		sum.OutputTokens += e.OutputTokens
	}

	result := make([]AgentSummary, 0, len(agentMap))
	for id, sum := range agentMap {
		result = append(result, AgentSummary{AgentID: id, Summary: *sum})
	}
	return result
}

// ByProvider returns per-provider summaries for a company in a time range.
func (s *Service) ByProvider(companyID string, from, to time.Time) []ProviderSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provMap := make(map[string]*Summary)

	for _, e := range s.events.List() {
		if e.CompanyID != companyID || e.Timestamp.Before(from) || !e.Timestamp.Before(to) {
			continue
		}
		sum, ok := provMap[e.Provider]
		if !ok {
			sum = &Summary{}
			provMap[e.Provider] = sum
		}
		sum.TotalCents += e.AmountCents
		sum.EventCount++
		sum.InputTokens += e.InputTokens
		sum.OutputTokens += e.OutputTokens
	}

	result := make([]ProviderSummary, 0, len(provMap))
	for prov, sum := range provMap {
		result = append(result, ProviderSummary{Provider: prov, Summary: *sum})
	}
	return result
}

// MonthlyTotal returns total cost for the current calendar month.
func (s *Service) MonthlyTotal(companyID string) Summary {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	return s.TotalByCompany(companyID, from, to)
}

// Count returns total event count.
func (s *Service) Count() int {
	return s.events.Count()
}

func (s *Service) aggregate(match func(Event) bool) Summary {
	var sum Summary
	for _, e := range s.events.List() {
		if match(e) {
			sum.TotalCents += e.AmountCents
			sum.EventCount++
			sum.InputTokens += e.InputTokens
			sum.OutputTokens += e.OutputTokens
		}
	}
	return sum
}
