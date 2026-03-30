package routine

import (
	"strings"
	"testing"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestParseCronEveryMinute(t *testing.T) {
	c, err := ParseCron("* * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Minutes) != 60 {
		t.Errorf("minutes = %d, want 60", len(c.Minutes))
	}
	if len(c.Hours) != 24 {
		t.Errorf("hours = %d, want 24", len(c.Hours))
	}
}

func TestParseCronEvery5Minutes(t *testing.T) {
	c, err := ParseCron("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	expected := []int{0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55}
	if len(c.Minutes) != len(expected) {
		t.Fatalf("minutes = %d, want %d", len(c.Minutes), len(expected))
	}
	for i, v := range expected {
		if c.Minutes[i] != v {
			t.Errorf("minutes[%d] = %d, want %d", i, c.Minutes[i], v)
		}
	}
}

func TestParseCronSpecificValues(t *testing.T) {
	c, err := ParseCron("0 9 * * 1-5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Minutes) != 1 || c.Minutes[0] != 0 {
		t.Errorf("minutes = %v", c.Minutes)
	}
	if len(c.Hours) != 1 || c.Hours[0] != 9 {
		t.Errorf("hours = %v", c.Hours)
	}
	if len(c.Weekdays) != 5 {
		t.Errorf("weekdays = %v, want Mon-Fri", c.Weekdays)
	}
}

func TestParseCronCommaList(t *testing.T) {
	c, err := ParseCron("0,30 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Minutes) != 2 {
		t.Errorf("minutes = %v", c.Minutes)
	}
}

func TestParseCronRange(t *testing.T) {
	c, err := ParseCron("* 9-17 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Hours) != 9 {
		t.Errorf("hours = %v, want 9-17 (9 values)", c.Hours)
	}
}

func TestParseCronRangeWithStep(t *testing.T) {
	c, err := ParseCron("0-30/10 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	expected := []int{0, 10, 20, 30}
	if len(c.Minutes) != len(expected) {
		t.Fatalf("minutes = %v, want %v", c.Minutes, expected)
	}
}

func TestParseCronInvalid(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"too few fields", "* * *"},
		{"too many fields", "* * * * * *"},
		{"invalid value", "abc * * * *"},
		{"out of range", "60 * * * *"},
		{"invalid range", "5-2 * * * *"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCron(tt.expr)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestNextTick(t *testing.T) {
	c, _ := ParseCron("0 9 * * *")
	// After 8:30 → next is 9:00 same day
	ref := time.Date(2026, 3, 30, 8, 30, 0, 0, time.UTC)
	next := c.NextTick(ref)
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("next = %v, want 9:00", next)
	}
}

func TestNextTickWrapsDay(t *testing.T) {
	c, _ := ParseCron("0 9 * * *")
	// After 10:00 → next is 9:00 next day
	ref := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	next := c.NextTick(ref)
	if next.Day() != 31 || next.Hour() != 9 {
		t.Errorf("next = %v, want Mar 31 9:00", next)
	}
}

func TestNextTickEvery5Min(t *testing.T) {
	c, _ := ParseCron("*/5 * * * *")
	ref := time.Date(2026, 3, 30, 10, 12, 0, 0, time.UTC)
	next := c.NextTick(ref)
	if next.Minute() != 15 {
		t.Errorf("next minute = %d, want 15", next.Minute())
	}
}

func TestCreateRoutine(t *testing.T) {
	svc := newTestService()
	r, err := svc.Create(Routine{
		ID:              "r-1",
		CompanyID:       "co-1",
		Title:           "Daily standup",
		AssigneeAgentID: "ag-1",
		Triggers: []Trigger{{
			ID:             "trig-1",
			Kind:           TriggerSchedule,
			CronExpression: "0 9 * * *",
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !r.Enabled {
		t.Error("expected enabled")
	}
	if r.ConcurrencyPolicy != PolicyAlwaysEnqueue {
		t.Errorf("policy = %q", r.ConcurrencyPolicy)
	}
	if len(r.Triggers) != 1 {
		t.Fatalf("triggers = %d", len(r.Triggers))
	}
	if r.Triggers[0].NextRunAt == nil {
		t.Error("expected next_run_at to be set")
	}
}

func TestCreateRoutineValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		routine Routine
		wantErr string
	}{
		{"no id", Routine{CompanyID: "co-1", Title: "A", AssigneeAgentID: "ag-1"}, "id is required"},
		{"no company", Routine{ID: "1", Title: "A", AssigneeAgentID: "ag-1"}, "company_id is required"},
		{"no title", Routine{ID: "1", CompanyID: "co-1", AssigneeAgentID: "ag-1"}, "title is required"},
		{"no agent", Routine{ID: "1", CompanyID: "co-1", Title: "A"}, "assignee_agent_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(tt.routine)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestInvalidCronOnCreate(t *testing.T) {
	svc := newTestService()
	_, err := svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Bad cron",
		AssigneeAgentID: "ag-1",
		Triggers: []Trigger{{
			ID: "trig-1", Kind: TriggerSchedule, CronExpression: "bad",
		}},
	})
	if err == nil {
		t.Fatal("expected cron parse error")
	}
}

func TestTickScheduledTriggers(t *testing.T) {
	svc := newTestService()
	tasksCreated := 0
	svc.SetCreateTaskFunc(func(routineID, routineTitle, companyID, agentID string) (string, error) {
		tasksCreated++
		return "task-" + routineID, nil
	})

	svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Every min",
		AssigneeAgentID: "ag-1",
		Triggers: []Trigger{{
			ID: "trig-1", Kind: TriggerSchedule,
			CronExpression: "* * * * *",
		}},
	})

	// Manually set NextRunAt to the past to simulate a due trigger
	r, _ := svc.Get("r-1")
	past := time.Now().UTC().Add(-1 * time.Minute)
	r.Triggers[0].NextRunAt = &past
	svc.routines.Put(r)

	fired := svc.TickScheduledTriggers()
	if len(fired) == 0 {
		t.Error("expected at least one fired run")
	}
	if tasksCreated == 0 {
		t.Error("expected task creation callback")
	}
}

func TestSkipIfActive(t *testing.T) {
	svc := newTestService()
	svc.SetCreateTaskFunc(func(routineID, routineTitle, companyID, agentID string) (string, error) {
		return "task-1", nil
	})

	svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Skippable",
		AssigneeAgentID:   "ag-1",
		ConcurrencyPolicy: PolicySkipIfActive,
		Triggers: []Trigger{{
			ID: "trig-1", Kind: TriggerSchedule,
			CronExpression: "* * * * *",
		}},
	})

	// Set NextRunAt to the past
	r, _ := svc.Get("r-1")
	past := time.Now().UTC().Add(-1 * time.Minute)
	r.Triggers[0].NextRunAt = &past
	svc.routines.Put(r)

	// First tick creates a run
	fired1 := svc.TickScheduledTriggers()
	if len(fired1) == 0 {
		t.Fatal("expected first fire")
	}

	// Second tick should skip (run is still active)
	r, _ = svc.Get("r-1")
	past2 := time.Now().UTC().Add(-1 * time.Minute)
	r.Triggers[0].NextRunAt = &past2
	svc.routines.Put(r)

	fired2 := svc.TickScheduledTriggers()
	if len(fired2) != 0 {
		t.Error("expected skip due to active run")
	}
}

func TestEnableDisable(t *testing.T) {
	svc := newTestService()
	svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Test",
		AssigneeAgentID: "ag-1",
	})

	svc.Disable("r-1")
	r, _ := svc.Get("r-1")
	if r.Enabled {
		t.Error("expected disabled")
	}

	svc.Enable("r-1")
	r, _ = svc.Get("r-1")
	if !r.Enabled {
		t.Error("expected enabled")
	}
}

func TestListByCompany(t *testing.T) {
	svc := newTestService()
	svc.Create(Routine{ID: "r-1", CompanyID: "co-1", Title: "A", AssigneeAgentID: "ag-1"})
	svc.Create(Routine{ID: "r-2", CompanyID: "co-1", Title: "B", AssigneeAgentID: "ag-1"})
	svc.Create(Routine{ID: "r-3", CompanyID: "co-2", Title: "C", AssigneeAgentID: "ag-2"})

	routines := svc.ListByCompany("co-1")
	if len(routines) != 2 {
		t.Errorf("count = %d, want 2", len(routines))
	}
}

func TestDeleteRoutine(t *testing.T) {
	svc := newTestService()
	svc.Create(Routine{ID: "r-1", CompanyID: "co-1", Title: "A", AssigneeAgentID: "ag-1"})
	if err := svc.Delete("r-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.Count() != 0 {
		t.Errorf("count = %d, want 0", svc.Count())
	}
}

func TestCompleteRun(t *testing.T) {
	svc := newTestService()
	svc.SetCreateTaskFunc(func(routineID, routineTitle, companyID, agentID string) (string, error) {
		return "task-1", nil
	})

	svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "A", AssigneeAgentID: "ag-1",
		Triggers: []Trigger{{
			ID: "trig-1", Kind: TriggerSchedule,
			CronExpression: "* * * * *",
		}},
	})
	r, _ := svc.Get("r-1")
	past := time.Now().UTC().Add(-1 * time.Minute)
	r.Triggers[0].NextRunAt = &past
	svc.routines.Put(r)
	fired := svc.TickScheduledTriggers()
	if len(fired) == 0 {
		t.Fatal("expected fire")
	}

	err := svc.CompleteRun(fired[0].ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	run, _ := svc.GetRun(fired[0].ID)
	if run.Status != RunCompleted {
		t.Errorf("status = %q", run.Status)
	}
}

func TestCatchUpCap(t *testing.T) {
	svc := newTestService()
	r, _ := svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Test Cap",
		AssigneeAgentID: "ag-1", CatchUpCap: 0,
	})
	if r.CatchUpCap != 25 {
		t.Errorf("catch_up_cap = %d, want 25 (default)", r.CatchUpCap)
	}
}

func TestWebhookTriggerNotScheduled(t *testing.T) {
	svc := newTestService()
	svc.Create(Routine{
		ID: "r-1", CompanyID: "co-1", Title: "Webhook",
		AssigneeAgentID: "ag-1",
		Triggers: []Trigger{{
			ID: "trig-1", Kind: TriggerWebhook,
		}},
	})

	// Set NextRunAt to past for the webhook trigger
	r, _ := svc.Get("r-1")
	past := time.Now().UTC().Add(-1 * time.Minute)
	r.Triggers[0].NextRunAt = &past
	svc.routines.Put(r)

	fired := svc.TickScheduledTriggers()
	if len(fired) != 0 {
		t.Error("webhook triggers should not fire on schedule tick")
	}
}
