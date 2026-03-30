// Package approval implements governance gates — approval workflows for
// hiring agents, budget changes, and other sensitive operations.
package approval

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// Status represents an approval's lifecycle state.
type Status string

const (
	StatusPending           Status = "pending"
	StatusApproved          Status = "approved"
	StatusRejected          Status = "rejected"
	StatusRevisionRequested Status = "revision_requested"
)

// Kind represents what type of action requires approval.
type Kind string

const (
	KindHireAgent      Kind = "hire_agent"
	KindBudgetChange   Kind = "budget_change"
	KindBudgetIncident Kind = "budget_incident"
	KindFireAgent      Kind = "fire_agent"
	KindRoleChange     Kind = "role_change"
)

// Request represents an approval request.
type Request struct {
	ID             string            `json:"id"`
	CompanyID      string            `json:"company_id"`
	Kind           Kind              `json:"kind"`
	Status         Status            `json:"status"`
	RequestedBy    string            `json:"requested_by"`
	Title          string            `json:"title"`
	Description    string            `json:"description,omitempty"`
	Payload        map[string]string `json:"payload,omitempty"`
	DecidedBy      string            `json:"decided_by,omitempty"`
	DecisionNote   string            `json:"decision_note,omitempty"`
	DecidedAt      *time.Time        `json:"decided_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// GetID implements store.Record.
func (r Request) GetID() string { return r.ID }

// GetCompanyID implements store.Record.
func (r Request) GetCompanyID() string { return r.CompanyID }

// Validate checks required fields.
func (r *Request) Validate() error {
	if r.ID == "" {
		return errors.New("request id is required")
	}
	if r.CompanyID == "" {
		return errors.New("request company_id is required")
	}
	if r.Kind == "" {
		return errors.New("request kind is required")
	}
	if strings.TrimSpace(r.Title) == "" {
		return errors.New("request title is required")
	}
	if r.RequestedBy == "" {
		return errors.New("request requested_by is required")
	}
	return nil
}

// IsDecided returns true if the request has been decided.
func (r *Request) IsDecided() bool {
	return r.Status == StatusApproved || r.Status == StatusRejected
}

// Comment represents a comment on an approval request.
type Comment struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	CompanyID string    `json:"company_id"`
	AuthorID  string    `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// GetID implements store.Record.
func (c Comment) GetID() string { return c.ID }

// GetCompanyID implements store.Record.
func (c Comment) GetCompanyID() string { return c.CompanyID }

// ApproveCallback is called when a request is approved.
type ApproveCallback func(req Request) error

// RejectCallback is called when a request is rejected.
type RejectCallback func(req Request) error

// Service manages approval workflows.
type Service struct {
	mu         sync.RWMutex
	requests   *store.Collection[Request]
	comments   *store.Collection[Comment]
	st         *store.Store
	onApprove  map[Kind]ApproveCallback
	onReject   map[Kind]RejectCallback
	nextComID  int
}

// NewService creates a new approval service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		requests:  store.NewCollection[Request](),
		comments:  store.NewCollection[Comment](),
		st:        s,
		onApprove: make(map[Kind]ApproveCallback),
		onReject:  make(map[Kind]RejectCallback),
	}
	_ = store.LoadCollection(s, "approval_requests", svc.requests)
	_ = store.LoadCollection(s, "approval_comments", svc.comments)
	return svc
}

// OnApprove registers a callback for when a request of a given kind is approved.
func (s *Service) OnApprove(kind Kind, fn ApproveCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onApprove[kind] = fn
}

// OnReject registers a callback for when a request of a given kind is rejected.
func (s *Service) OnReject(kind Kind, fn RejectCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReject[kind] = fn
}

// Submit creates a new approval request.
func (s *Service) Submit(r Request) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	r.Status = StatusPending
	r.CreatedAt = now
	r.UpdatedAt = now

	if err := r.Validate(); err != nil {
		return Request{}, fmt.Errorf("validate request: %w", err)
	}

	s.requests.Put(r)
	_ = store.SaveCollection(s.st, "approval_requests", s.requests)
	return r, nil
}

// Approve approves a request.
func (s *Service) Approve(id, decidedBy, note string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.requests.Get(id)
	if !ok {
		return Request{}, fmt.Errorf("request %q not found", id)
	}

	if r.IsDecided() {
		return Request{}, fmt.Errorf("request %q already decided (status: %s)", id, r.Status)
	}

	now := time.Now().UTC()
	r.Status = StatusApproved
	r.DecidedBy = decidedBy
	r.DecisionNote = note
	r.DecidedAt = &now
	r.UpdatedAt = now

	s.requests.Put(r)
	_ = store.SaveCollection(s.st, "approval_requests", s.requests)

	// Fire callback
	if cb, ok := s.onApprove[r.Kind]; ok {
		if err := cb(r); err != nil {
			return r, fmt.Errorf("approve callback failed: %w", err)
		}
	}

	return r, nil
}

// Reject rejects a request.
func (s *Service) Reject(id, decidedBy, note string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.requests.Get(id)
	if !ok {
		return Request{}, fmt.Errorf("request %q not found", id)
	}

	if r.IsDecided() {
		return Request{}, fmt.Errorf("request %q already decided (status: %s)", id, r.Status)
	}

	now := time.Now().UTC()
	r.Status = StatusRejected
	r.DecidedBy = decidedBy
	r.DecisionNote = note
	r.DecidedAt = &now
	r.UpdatedAt = now

	s.requests.Put(r)
	_ = store.SaveCollection(s.st, "approval_requests", s.requests)

	// Fire callback
	if cb, ok := s.onReject[r.Kind]; ok {
		if err := cb(r); err != nil {
			return r, fmt.Errorf("reject callback failed: %w", err)
		}
	}

	return r, nil
}

// RequestRevision sends a request back for revision.
func (s *Service) RequestRevision(id, decidedBy, note string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.requests.Get(id)
	if !ok {
		return Request{}, fmt.Errorf("request %q not found", id)
	}

	if r.IsDecided() {
		return Request{}, fmt.Errorf("request %q already decided (status: %s)", id, r.Status)
	}

	now := time.Now().UTC()
	r.Status = StatusRevisionRequested
	r.DecidedBy = decidedBy
	r.DecisionNote = note
	r.DecidedAt = &now
	r.UpdatedAt = now

	s.requests.Put(r)
	_ = store.SaveCollection(s.st, "approval_requests", s.requests)
	return r, nil
}

// Resubmit resubmits a revision-requested item back to pending.
func (s *Service) Resubmit(id string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.requests.Get(id)
	if !ok {
		return Request{}, fmt.Errorf("request %q not found", id)
	}

	if r.Status != StatusRevisionRequested {
		return Request{}, fmt.Errorf("request %q is not in revision_requested status", id)
	}

	r.Status = StatusPending
	r.DecidedBy = ""
	r.DecisionNote = ""
	r.DecidedAt = nil
	r.UpdatedAt = time.Now().UTC()

	s.requests.Put(r)
	_ = store.SaveCollection(s.st, "approval_requests", s.requests)
	return r, nil
}

// AddComment adds a comment to a request.
func (s *Service) AddComment(requestID, companyID, authorID, body string) (Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.requests.Get(requestID); !ok {
		return Comment{}, fmt.Errorf("request %q not found", requestID)
	}

	s.nextComID++
	c := Comment{
		ID:        fmt.Sprintf("comment-%d", s.nextComID),
		RequestID: requestID,
		CompanyID: companyID,
		AuthorID:  authorID,
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	s.comments.Put(c)
	return c, nil
}

// GetComments returns comments for a request.
func (s *Service) GetComments(requestID string) []Comment {
	return s.comments.Filter(func(c Comment) bool {
		return c.RequestID == requestID
	})
}

// Get retrieves a request by ID.
func (s *Service) Get(id string) (Request, error) {
	r, ok := s.requests.Get(id)
	if !ok {
		return Request{}, fmt.Errorf("request %q not found", id)
	}
	return r, nil
}

// List returns all requests.
func (s *Service) List() []Request {
	return s.requests.List()
}

// ListByCompany returns requests for a company.
func (s *Service) ListByCompany(companyID string) []Request {
	return s.requests.Filter(func(r Request) bool {
		return r.CompanyID == companyID
	})
}

// ListPending returns pending requests for a company.
func (s *Service) ListPending(companyID string) []Request {
	return s.requests.Filter(func(r Request) bool {
		return r.CompanyID == companyID && r.Status == StatusPending
	})
}

// Count returns total request count.
func (s *Service) Count() int {
	return s.requests.Count()
}
