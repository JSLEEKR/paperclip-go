// Package routine manages scheduled/recurring tasks with cron expressions,
// timezone support, catch-up policies, and concurrency control.
package routine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

// ConcurrencyPolicy defines behavior when a routine triggers while previous is running.
type ConcurrencyPolicy string

const (
	PolicyAlwaysEnqueue ConcurrencyPolicy = "always_enqueue"
	PolicySkipIfActive  ConcurrencyPolicy = "skip_if_active"
	PolicyCoalesced     ConcurrencyPolicy = "coalesced"
)

// CatchUpPolicy defines behavior for missed scheduled runs.
type CatchUpPolicy string

const (
	CatchUpSkip       CatchUpPolicy = "skip_missed"
	CatchUpEnqueue    CatchUpPolicy = "enqueue_missed_with_cap"
)

// TriggerKind indicates the trigger type.
type TriggerKind string

const (
	TriggerSchedule TriggerKind = "schedule"
	TriggerWebhook  TriggerKind = "webhook"
)

// RunStatus represents a routine run's state.
type RunStatus string

const (
	RunReceived    RunStatus = "received"
	RunIssueCreated RunStatus = "issue_created"
	RunCompleted   RunStatus = "completed"
	RunFailed      RunStatus = "failed"
	RunSkipped     RunStatus = "skipped"
	RunCoalesced   RunStatus = "coalesced"
)

// Routine represents a scheduled/recurring automated task.
type Routine struct {
	ID                string            `json:"id"`
	CompanyID         string            `json:"company_id"`
	Title             string            `json:"title"`
	Description       string            `json:"description,omitempty"`
	AssigneeAgentID   string            `json:"assignee_agent_id"`
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrency_policy"`
	CatchUpPolicy     CatchUpPolicy     `json:"catch_up_policy"`
	CatchUpCap        int               `json:"catch_up_cap"`
	Enabled           bool              `json:"enabled"`
	Triggers          []Trigger         `json:"triggers,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// GetID implements store.Record.
func (r Routine) GetID() string { return r.ID }

// GetCompanyID implements store.Record.
func (r Routine) GetCompanyID() string { return r.CompanyID }

// Validate checks required fields.
func (r *Routine) Validate() error {
	if r.ID == "" {
		return errors.New("routine id is required")
	}
	if r.CompanyID == "" {
		return errors.New("routine company_id is required")
	}
	if strings.TrimSpace(r.Title) == "" {
		return errors.New("routine title is required")
	}
	if r.AssigneeAgentID == "" {
		return errors.New("routine assignee_agent_id is required")
	}
	if r.ConcurrencyPolicy == "" {
		r.ConcurrencyPolicy = PolicyAlwaysEnqueue
	}
	if r.CatchUpPolicy == "" {
		r.CatchUpPolicy = CatchUpSkip
	}
	if r.CatchUpCap <= 0 {
		r.CatchUpCap = 25
	}
	return nil
}

// Trigger defines when a routine fires.
type Trigger struct {
	ID             string      `json:"id"`
	RoutineID      string      `json:"routine_id"`
	Kind           TriggerKind `json:"kind"`
	CronExpression string      `json:"cron_expression,omitempty"`
	Timezone       string      `json:"timezone,omitempty"`
	NextRunAt      *time.Time  `json:"next_run_at,omitempty"`
	WebhookSecret  string      `json:"webhook_secret,omitempty"`
}

// RoutineRun represents a single execution of a routine.
type RoutineRun struct {
	ID             string    `json:"id"`
	CompanyID      string    `json:"company_id"`
	RoutineID      string    `json:"routine_id"`
	TriggerID      string    `json:"trigger_id"`
	Status         RunStatus `json:"status"`
	TaskID         string    `json:"task_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// GetID implements store.Record.
func (r RoutineRun) GetID() string { return r.ID }

// GetCompanyID implements store.Record.
func (r RoutineRun) GetCompanyID() string { return r.CompanyID }

// ParsedCron represents a parsed cron expression (minute, hour, day, month, weekday).
type ParsedCron struct {
	Minutes  []int
	Hours    []int
	Days     []int
	Months   []int
	Weekdays []int
}

// ParseCron parses a standard 5-field cron expression.
func ParseCron(expr string) (ParsedCron, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return ParsedCron{}, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	minutes, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return ParsedCron{}, fmt.Errorf("invalid minute field: %w", err)
	}

	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return ParsedCron{}, fmt.Errorf("invalid hour field: %w", err)
	}

	days, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return ParsedCron{}, fmt.Errorf("invalid day field: %w", err)
	}

	months, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return ParsedCron{}, fmt.Errorf("invalid month field: %w", err)
	}

	weekdays, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return ParsedCron{}, fmt.Errorf("invalid weekday field: %w", err)
	}

	return ParsedCron{
		Minutes:  minutes,
		Hours:    hours,
		Days:     days,
		Months:   months,
		Weekdays: weekdays,
	}, nil
}

// NextTick calculates the next matching time after 'after'.
func (c *ParsedCron) NextTick(after time.Time) time.Time {
	// Start from the next minute
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Limit search to 4 years
	limit := after.Add(4 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		if c.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}

	return time.Time{} // no match within 4 years
}

func (c *ParsedCron) matches(t time.Time) bool {
	return contains(c.Minutes, t.Minute()) &&
		contains(c.Hours, t.Hour()) &&
		contains(c.Days, t.Day()) &&
		contains(c.Months, int(t.Month())) &&
		contains(c.Weekdays, int(t.Weekday()))
}

func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// parseCronField parses a single cron field (supports *, ranges, steps, lists).
func parseCronField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return makeRange(min, max), nil
	}

	var result []int
	parts := strings.Split(field, ",")

	for _, part := range parts {
		// Check for step
		stepParts := strings.SplitN(part, "/", 2)
		base := stepParts[0]
		step := 1

		if len(stepParts) == 2 {
			s, err := strconv.Atoi(stepParts[1])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step: %s", stepParts[1])
			}
			step = s
		}

		// Check for range
		if strings.Contains(base, "-") {
			rangeParts := strings.SplitN(base, "-", 2)
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", rangeParts[0])
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", rangeParts[1])
			}
			if start < min || end > max || start > end {
				return nil, fmt.Errorf("range %d-%d out of bounds [%d-%d]", start, end, min, max)
			}
			for i := start; i <= end; i += step {
				result = append(result, i)
			}
		} else if base == "*" {
			for i := min; i <= max; i += step {
				result = append(result, i)
			}
		} else {
			val, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("invalid value: %s", base)
			}
			if val < min || val > max {
				return nil, fmt.Errorf("value %d out of bounds [%d-%d]", val, min, max)
			}
			result = append(result, val)
		}
	}

	if len(result) == 0 {
		return nil, errors.New("empty field result")
	}
	return result, nil
}

func makeRange(min, max int) []int {
	r := make([]int, max-min+1)
	for i := range r {
		r[i] = min + i
	}
	return r
}

// CreateTaskFunc is called to create a task from a routine trigger.
type CreateTaskFunc func(routineID, routineTitle, companyID, agentID string) (taskID string, err error)

// Service manages routines and scheduling.
type Service struct {
	mu           sync.RWMutex
	routines     *store.Collection[Routine]
	runs         *store.Collection[RoutineRun]
	st           *store.Store
	createTask   CreateTaskFunc
	nextRunID    int
}

// NewService creates a new routine service.
func NewService(s *store.Store) *Service {
	svc := &Service{
		routines: store.NewCollection[Routine](),
		runs:     store.NewCollection[RoutineRun](),
		st:       s,
	}
	_ = store.LoadCollection(s, "routines", svc.routines)
	_ = store.LoadCollection(s, "routine_runs", svc.runs)
	return svc
}

// SetCreateTaskFunc sets the callback for creating tasks.
func (s *Service) SetCreateTaskFunc(fn CreateTaskFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createTask = fn
}

// Create adds a new routine.
func (s *Service) Create(r Routine) (Routine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.Enabled = true

	if err := r.Validate(); err != nil {
		return Routine{}, fmt.Errorf("validate routine: %w", err)
	}

	// Initialize trigger next run times
	for i := range r.Triggers {
		t := &r.Triggers[i]
		if t.Kind == TriggerSchedule && t.CronExpression != "" {
			cron, err := ParseCron(t.CronExpression)
			if err != nil {
				return Routine{}, fmt.Errorf("parse cron for trigger %s: %w", t.ID, err)
			}
			loc := time.UTC
			if t.Timezone != "" {
				l, err := time.LoadLocation(t.Timezone)
				if err == nil {
					loc = l
				}
			}
			next := cron.NextTick(now.In(loc))
			if !next.IsZero() {
				nextUTC := next.UTC()
				t.NextRunAt = &nextUTC
			}
		}
	}

	s.routines.Put(r)
	_ = store.SaveCollection(s.st, "routines", s.routines)
	return r, nil
}

// Get retrieves a routine by ID.
func (s *Service) Get(id string) (Routine, error) {
	r, ok := s.routines.Get(id)
	if !ok {
		return Routine{}, fmt.Errorf("routine %q not found", id)
	}
	return r, nil
}

// List returns all routines.
func (s *Service) List() []Routine {
	return s.routines.List()
}

// ListByCompany returns routines for a company.
func (s *Service) ListByCompany(companyID string) []Routine {
	return s.routines.Filter(func(r Routine) bool {
		return r.CompanyID == companyID
	})
}

// Update modifies a routine.
func (s *Service) Update(id string, fn func(*Routine) error) (Routine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.routines.Get(id)
	if !ok {
		return Routine{}, fmt.Errorf("routine %q not found", id)
	}

	if err := fn(&r); err != nil {
		return Routine{}, err
	}
	r.UpdatedAt = time.Now().UTC()

	if err := r.Validate(); err != nil {
		return Routine{}, fmt.Errorf("validate routine: %w", err)
	}

	s.routines.Put(r)
	_ = store.SaveCollection(s.st, "routines", s.routines)
	return r, nil
}

// Delete removes a routine.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.routines.Delete(id) {
		return fmt.Errorf("routine %q not found", id)
	}
	_ = store.SaveCollection(s.st, "routines", s.routines)
	return nil
}

// Enable enables a routine.
func (s *Service) Enable(id string) error {
	_, err := s.Update(id, func(r *Routine) error {
		r.Enabled = true
		return nil
	})
	return err
}

// Disable disables a routine.
func (s *Service) Disable(id string) error {
	_, err := s.Update(id, func(r *Routine) error {
		r.Enabled = false
		return nil
	})
	return err
}

// TickScheduledTriggers processes due scheduled triggers.
func (s *Service) TickScheduledTriggers() []RoutineRun {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	var fired []RoutineRun

	for _, r := range s.routines.List() {
		if !r.Enabled {
			continue
		}

		for ti, trigger := range r.Triggers {
			if trigger.Kind != TriggerSchedule {
				continue
			}
			if trigger.NextRunAt == nil || now.Before(*trigger.NextRunAt) {
				continue
			}

			// Check concurrency policy
			if r.ConcurrencyPolicy == PolicySkipIfActive {
				if s.hasActiveRun(r.ID) {
					// Skip this trigger
					s.advanceTrigger(&r, ti)
					s.routines.Put(r)
					continue
				}
			}

			// Create routine run
			run := s.createRoutineRun(r, trigger)
			fired = append(fired, run)

			// Handle catch-up
			if r.CatchUpPolicy == CatchUpEnqueue {
				missedCount := 0
				cron, err := ParseCron(trigger.CronExpression)
				if err == nil {
					checkTime := *trigger.NextRunAt
					for missedCount < r.CatchUpCap {
						next := cron.NextTick(checkTime)
						if next.IsZero() || next.After(now) {
							break
						}
						missedRun := s.createRoutineRun(r, trigger)
						fired = append(fired, missedRun)
						checkTime = next
						missedCount++
					}
				}
			}

			s.advanceTrigger(&r, ti)
			s.routines.Put(r)
		}
	}

	if len(fired) > 0 {
		_ = store.SaveCollection(s.st, "routines", s.routines)
		_ = store.SaveCollection(s.st, "routine_runs", s.runs)
	}
	return fired
}

// GetRun retrieves a routine run by ID.
func (s *Service) GetRun(id string) (RoutineRun, error) {
	r, ok := s.runs.Get(id)
	if !ok {
		return RoutineRun{}, fmt.Errorf("routine run %q not found", id)
	}
	return r, nil
}

// ListRuns returns runs for a routine.
func (s *Service) ListRuns(routineID string) []RoutineRun {
	return s.runs.Filter(func(r RoutineRun) bool {
		return r.RoutineID == routineID
	})
}

// CompleteRun marks a routine run as completed.
func (s *Service) CompleteRun(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.runs.Get(id)
	if !ok {
		return fmt.Errorf("routine run %q not found", id)
	}

	now := time.Now().UTC()
	r.Status = RunCompleted
	r.CompletedAt = &now
	s.runs.Put(r)
	_ = store.SaveCollection(s.st, "routine_runs", s.runs)
	return nil
}

// Count returns total routine count.
func (s *Service) Count() int {
	return s.routines.Count()
}

func (s *Service) hasActiveRun(routineID string) bool {
	for _, r := range s.runs.List() {
		if r.RoutineID == routineID &&
			(r.Status == RunReceived || r.Status == RunIssueCreated) {
			return true
		}
	}
	return false
}

func (s *Service) createRoutineRun(r Routine, trigger Trigger) RoutineRun {
	s.nextRunID++
	now := time.Now().UTC()
	run := RoutineRun{
		ID:             fmt.Sprintf("rrun-%d", s.nextRunID),
		CompanyID:      r.CompanyID,
		RoutineID:      r.ID,
		TriggerID:      trigger.ID,
		Status:         RunReceived,
		IdempotencyKey: fmt.Sprintf("%s-%s-%d", r.ID, trigger.ID, now.Unix()),
		CreatedAt:      now,
	}

	// Create task if callback is set
	if s.createTask != nil {
		taskID, err := s.createTask(r.ID, r.Title, r.CompanyID, r.AssigneeAgentID)
		if err == nil {
			run.TaskID = taskID
			run.Status = RunIssueCreated
		} else {
			run.Status = RunFailed
		}
	}

	s.runs.Put(run)
	return run
}

func (s *Service) advanceTrigger(r *Routine, triggerIdx int) {
	trigger := &r.Triggers[triggerIdx]
	if trigger.CronExpression == "" {
		return
	}

	cron, err := ParseCron(trigger.CronExpression)
	if err != nil {
		return
	}

	loc := time.UTC
	if trigger.Timezone != "" {
		l, err := time.LoadLocation(trigger.Timezone)
		if err == nil {
			loc = l
		}
	}

	from := time.Now().In(loc)
	next := cron.NextTick(from)
	if !next.IsZero() {
		nextUTC := next.UTC()
		trigger.NextRunAt = &nextUTC
	}
}
