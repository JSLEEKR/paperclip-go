// Package goal manages company goals — hierarchical objectives that tasks align to.
package goal

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Status represents a goal's state.
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusAbandoned Status = "abandoned"
)

// Goal represents a company objective.
type Goal struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	ParentID    string    `json:"parent_id,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      Status    `json:"status"`
	Priority    int       `json:"priority"` // 1 = highest
	Progress    float64   `json:"progress"` // 0.0 - 1.0
	DueAt       *time.Time `json:"due_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// GetID implements store.Record.
func (g Goal) GetID() string { return g.ID }

// GetCompanyID implements store.Record.
func (g Goal) GetCompanyID() string { return g.CompanyID }

// Validate checks required fields.
func (g *Goal) Validate() error {
	if g.ID == "" {
		return errors.New("goal id is required")
	}
	if g.CompanyID == "" {
		return errors.New("goal company_id is required")
	}
	if strings.TrimSpace(g.Title) == "" {
		return errors.New("goal title is required")
	}
	if g.Priority <= 0 {
		g.Priority = 5
	}
	if g.Progress < 0 {
		g.Progress = 0
	}
	if g.Progress > 1 {
		g.Progress = 1
	}
	return nil
}

// IsActive returns true if the goal is active.
func (g *Goal) IsActive() bool {
	return g.Status == StatusActive
}

// Service manages goal CRUD and hierarchy.
type Service struct {
	mu    sync.RWMutex
	goals *store.Collection[Goal]
	st    *store.Store
}

// NewService creates a new goal service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		goals: store.NewCollection[Goal](),
		st:    s,
	}
	_ = store.LoadCollection(s, "goals", svc.goals)
	return svc
}

// Create adds a new goal.
func (s *Service) Create(g Goal) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	g.CreatedAt = now
	g.UpdatedAt = now
	if g.Status == "" {
		g.Status = StatusActive
	}

	if err := g.Validate(); err != nil {
		return Goal{}, fmt.Errorf("validate goal: %w", err)
	}

	// Validate parent exists
	if g.ParentID != "" {
		if _, ok := s.goals.Get(g.ParentID); !ok {
			return Goal{}, fmt.Errorf("parent goal %q not found", g.ParentID)
		}
	}

	s.goals.Put(g)
	_ = store.SaveCollection(s.st, "goals", s.goals)
	return g, nil
}

// Get retrieves a goal by ID.
func (s *Service) Get(id string) (Goal, error) {
	g, ok := s.goals.Get(id)
	if !ok {
		return Goal{}, fmt.Errorf("goal %q not found", id)
	}
	return g, nil
}

// List returns all goals.
func (s *Service) List() []Goal {
	return s.goals.List()
}

// ListByCompany returns goals for a company.
func (s *Service) ListByCompany(companyID string) []Goal {
	return s.goals.Filter(func(g Goal) bool {
		return g.CompanyID == companyID
	})
}

// ListActive returns active goals for a company.
func (s *Service) ListActive(companyID string) []Goal {
	return s.goals.Filter(func(g Goal) bool {
		return g.CompanyID == companyID && g.IsActive()
	})
}

// ListChildren returns sub-goals of a parent.
func (s *Service) ListChildren(parentID string) []Goal {
	return s.goals.Filter(func(g Goal) bool {
		return g.ParentID == parentID
	})
}

// TopLevel returns top-level goals (no parent) for a company.
func (s *Service) TopLevel(companyID string) []Goal {
	return s.goals.Filter(func(g Goal) bool {
		return g.CompanyID == companyID && g.ParentID == ""
	})
}

// Update modifies a goal.
func (s *Service) Update(id string, fn func(*Goal) error) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	g, ok := s.goals.Get(id)
	if !ok {
		return Goal{}, fmt.Errorf("goal %q not found", id)
	}

	if err := fn(&g); err != nil {
		return Goal{}, err
	}
	g.UpdatedAt = time.Now().UTC()

	if err := g.Validate(); err != nil {
		return Goal{}, fmt.Errorf("validate goal: %w", err)
	}

	s.goals.Put(g)
	_ = store.SaveCollection(s.st, "goals", s.goals)
	return g, nil
}

// Complete marks a goal as completed.
func (s *Service) Complete(id string) error {
	_, err := s.Update(id, func(g *Goal) error {
		now := time.Now().UTC()
		g.Status = StatusCompleted
		g.Progress = 1.0
		g.CompletedAt = &now
		return nil
	})
	return err
}

// Abandon marks a goal as abandoned.
func (s *Service) Abandon(id string) error {
	_, err := s.Update(id, func(g *Goal) error {
		g.Status = StatusAbandoned
		return nil
	})
	return err
}

// UpdateProgress sets a goal's progress.
func (s *Service) UpdateProgress(id string, progress float64) error {
	_, err := s.Update(id, func(g *Goal) error {
		g.Progress = progress
		return nil
	})
	return err
}

// Delete removes a goal.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for children
	for _, g := range s.goals.List() {
		if g.ParentID == id {
			return fmt.Errorf("cannot delete goal %q: has child goals", id)
		}
	}

	if !s.goals.Delete(id) {
		return fmt.Errorf("goal %q not found", id)
	}
	_ = store.SaveCollection(s.st, "goals", s.goals)
	return nil
}

// DefaultGoal returns the highest-priority active goal for a company.
// This is the fallback goal for tasks without explicit goal alignment.
func (s *Service) DefaultGoal(companyID string) (Goal, error) {
	active := s.ListActive(companyID)
	if len(active) == 0 {
		return Goal{}, fmt.Errorf("no active goals for company %q", companyID)
	}

	best := active[0]
	for _, g := range active[1:] {
		if g.Priority < best.Priority {
			best = g
		}
	}
	return best, nil
}

// Count returns total goal count.
func (s *Service) Count() int {
	return s.goals.Count()
}
