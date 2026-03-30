// Package company manages AI company entities — creation, configuration,
// multi-company isolation, and goal alignment.
package company

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Status represents a company's operational status.
type Status string

const (
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusArchived Status = "archived"
)

// Company represents an AI company — a tenant isolation boundary.
type Company struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Description string            `json:"description"`
	Status      Status            `json:"status"`
	Config      Config            `json:"config"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Config holds company-level configuration.
type Config struct {
	IssuePrefix        string `json:"issue_prefix"`
	MaxAgents          int    `json:"max_agents"`
	MaxConcurrentRuns  int    `json:"max_concurrent_runs"`
	DefaultBudgetCents int64  `json:"default_budget_cents"`
	TimezoneOffset     int    `json:"timezone_offset"` // minutes from UTC
}

// GetID implements store.Record.
func (c Company) GetID() string { return c.ID }

// GetCompanyID implements store.Record.
func (c Company) GetCompanyID() string { return c.ID }

// Validate checks that a company has the minimum required fields.
func (c *Company) Validate() error {
	if c.ID == "" {
		return errors.New("company id is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("company name is required")
	}
	if strings.TrimSpace(c.Slug) == "" {
		return errors.New("company slug is required")
	}
	if c.Config.MaxAgents <= 0 {
		c.Config.MaxAgents = 50
	}
	if c.Config.MaxConcurrentRuns <= 0 {
		c.Config.MaxConcurrentRuns = 10
	}
	return nil
}

// Service manages company CRUD operations.
type Service struct {
	mu         sync.RWMutex
	companies  *store.Collection[Company]
	store      *store.Store
	slugIndex  map[string]string // slug → id for uniqueness
}

// NewService creates a new company service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		companies: store.NewCollection[Company](),
		store:     s,
		slugIndex: make(map[string]string),
	}
	_ = store.LoadCollection(s, "companies", svc.companies)
	// rebuild slug index
	for _, c := range svc.companies.List() {
		svc.slugIndex[c.Slug] = c.ID
	}
	return svc
}

// Create adds a new company.
func (s *Service) Create(c Company) (Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = StatusActive
	}

	if err := c.Validate(); err != nil {
		return Company{}, fmt.Errorf("validate company: %w", err)
	}

	if existingID, ok := s.slugIndex[c.Slug]; ok && existingID != c.ID {
		return Company{}, fmt.Errorf("company slug %q already exists", c.Slug)
	}

	s.companies.Put(c)
	s.slugIndex[c.Slug] = c.ID
	_ = store.SaveCollection(s.store, "companies", s.companies)
	return c, nil
}

// Get retrieves a company by ID.
func (s *Service) Get(id string) (Company, error) {
	c, ok := s.companies.Get(id)
	if !ok {
		return Company{}, fmt.Errorf("company %q not found", id)
	}
	return c, nil
}

// GetBySlug retrieves a company by slug.
func (s *Service) GetBySlug(slug string) (Company, error) {
	s.mu.RLock()
	id, ok := s.slugIndex[slug]
	s.mu.RUnlock()
	if !ok {
		return Company{}, fmt.Errorf("company with slug %q not found", slug)
	}
	return s.Get(id)
}

// List returns all companies.
func (s *Service) List() []Company {
	return s.companies.List()
}

// Update modifies an existing company.
func (s *Service) Update(id string, fn func(*Company) error) (Company, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.companies.Get(id)
	if !ok {
		return Company{}, fmt.Errorf("company %q not found", id)
	}

	oldSlug := c.Slug
	if err := fn(&c); err != nil {
		return Company{}, err
	}
	c.UpdatedAt = time.Now().UTC()

	if err := c.Validate(); err != nil {
		return Company{}, fmt.Errorf("validate company: %w", err)
	}

	// Update slug index if changed
	if c.Slug != oldSlug {
		if existingID, exists := s.slugIndex[c.Slug]; exists && existingID != id {
			return Company{}, fmt.Errorf("company slug %q already exists", c.Slug)
		}
		delete(s.slugIndex, oldSlug)
		s.slugIndex[c.Slug] = id
	}

	s.companies.Put(c)
	_ = store.SaveCollection(s.store, "companies", s.companies)
	return c, nil
}

// Delete removes a company by ID.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.companies.Get(id)
	if !ok {
		return fmt.Errorf("company %q not found", id)
	}

	delete(s.slugIndex, c.Slug)
	s.companies.Delete(id)
	_ = store.SaveCollection(s.store, "companies", s.companies)
	return nil
}

// Count returns the number of companies.
func (s *Service) Count() int {
	return s.companies.Count()
}
