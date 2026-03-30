package cost

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestRecordEvent(t *testing.T) {
	svc := newTestService()
	e, err := svc.Record(Event{
		ID:           "ce-1",
		CompanyID:    "co-1",
		AgentID:      "ag-1",
		Provider:     "anthropic",
		AmountCents:  150,
		InputTokens:  1000,
		OutputTokens: 500,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if e.BillingType != BillingLLM {
		t.Errorf("billing_type = %q, want llm", e.BillingType)
	}
	if e.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestRecordEventValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		event   Event
		wantErr string
	}{
		{"no id", Event{CompanyID: "co-1", AgentID: "a", Provider: "p", AmountCents: 1}, "id is required"},
		{"no company", Event{ID: "1", AgentID: "a", Provider: "p", AmountCents: 1}, "company_id is required"},
		{"no agent", Event{ID: "1", CompanyID: "co-1", Provider: "p", AmountCents: 1}, "agent_id is required"},
		{"no provider", Event{ID: "1", CompanyID: "co-1", AgentID: "a", AmountCents: 1}, "provider is required"},
		{"negative amount", Event{ID: "1", CompanyID: "co-1", AgentID: "a", Provider: "p", AmountCents: -1}, "cannot be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Record(tt.event)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestTotalByCompany(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "anthropic", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-2", Provider: "openai", AmountCents: 200, Timestamp: now})
	svc.Record(Event{ID: "ce-3", CompanyID: "co-2", AgentID: "ag-3", Provider: "anthropic", AmountCents: 300, Timestamp: now})

	sum := svc.TotalByCompany("co-1", from, to)
	if sum.TotalCents != 300 {
		t.Errorf("total = %d, want 300", sum.TotalCents)
	}
	if sum.EventCount != 2 {
		t.Errorf("count = %d, want 2", sum.EventCount)
	}
}

func TestTotalByAgent(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 100, InputTokens: 500, OutputTokens: 200, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 50, InputTokens: 300, OutputTokens: 100, Timestamp: now})

	sum := svc.TotalByAgent("ag-1", from, to)
	if sum.TotalCents != 150 {
		t.Errorf("total = %d, want 150", sum.TotalCents)
	}
	if sum.InputTokens != 800 {
		t.Errorf("input_tokens = %d, want 800", sum.InputTokens)
	}
	if sum.OutputTokens != 300 {
		t.Errorf("output_tokens = %d, want 300", sum.OutputTokens)
	}
}

func TestTotalByTask(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", TaskID: "task-1", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", TaskID: "task-1", AmountCents: 50, Timestamp: now})
	svc.Record(Event{ID: "ce-3", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", TaskID: "task-2", AmountCents: 200, Timestamp: now})

	sum := svc.TotalByTask("task-1")
	if sum.TotalCents != 150 {
		t.Errorf("total = %d, want 150", sum.TotalCents)
	}
}

func TestByAgent(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 50, Timestamp: now})
	svc.Record(Event{ID: "ce-3", CompanyID: "co-1", AgentID: "ag-2", Provider: "p", AmountCents: 200, Timestamp: now})

	summaries := svc.ByAgent("co-1", from, to)
	if len(summaries) != 2 {
		t.Fatalf("agent summaries = %d, want 2", len(summaries))
	}
}

func TestByProvider(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "anthropic", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", Provider: "openai", AmountCents: 200, Timestamp: now})
	svc.Record(Event{ID: "ce-3", CompanyID: "co-1", AgentID: "ag-1", Provider: "anthropic", AmountCents: 50, Timestamp: now})

	summaries := svc.ByProvider("co-1", from, to)
	if len(summaries) != 2 {
		t.Fatalf("provider summaries = %d, want 2", len(summaries))
	}

	for _, s := range summaries {
		if s.Provider == "anthropic" && s.Summary.TotalCents != 150 {
			t.Errorf("anthropic total = %d, want 150", s.Summary.TotalCents)
		}
		if s.Provider == "openai" && s.Summary.TotalCents != 200 {
			t.Errorf("openai total = %d, want 200", s.Summary.TotalCents)
		}
	}
}

func TestListByBillingCode(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", BillingCode: "BC-001", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", BillingCode: "BC-001", AmountCents: 50, Timestamp: now})
	svc.Record(Event{ID: "ce-3", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", BillingCode: "BC-002", AmountCents: 200, Timestamp: now})

	events := svc.ListByBillingCode("BC-001")
	if len(events) != 2 {
		t.Errorf("events = %d, want 2", len(events))
	}
}

func TestListByAgent(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 100, Timestamp: now})
	svc.Record(Event{ID: "ce-2", CompanyID: "co-1", AgentID: "ag-2", Provider: "p", AmountCents: 200, Timestamp: now})

	events := svc.ListByAgent("ag-1")
	if len(events) != 1 {
		t.Errorf("events = %d, want 1", len(events))
	}
}

func TestMonthlyTotal(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		svc.Record(Event{
			ID: fmt.Sprintf("ce-%d", i), CompanyID: "co-1", AgentID: "ag-1",
			Provider: "p", AmountCents: 100, Timestamp: now,
		})
	}

	sum := svc.MonthlyTotal("co-1")
	if sum.TotalCents != 500 {
		t.Errorf("monthly = %d, want 500", sum.TotalCents)
	}
}

func TestTimeRangeFilter(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	// Event in the past (outside range)
	svc.Record(Event{ID: "ce-old", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 100, Timestamp: now.Add(-48 * time.Hour)})
	// Event now (inside range)
	svc.Record(Event{ID: "ce-now", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 200, Timestamp: now})

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)
	sum := svc.TotalByCompany("co-1", from, to)
	if sum.TotalCents != 200 {
		t.Errorf("total = %d, want 200 (only recent event)", sum.TotalCents)
	}
}

func TestCount(t *testing.T) {
	svc := newTestService()
	if svc.Count() != 0 {
		t.Errorf("initial count = %d", svc.Count())
	}
	svc.Record(Event{ID: "ce-1", CompanyID: "co-1", AgentID: "a", Provider: "p", AmountCents: 1})
	if svc.Count() != 1 {
		t.Errorf("count = %d, want 1", svc.Count())
	}
}

func TestZeroAmountAllowed(t *testing.T) {
	svc := newTestService()
	_, err := svc.Record(Event{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", Provider: "p", AmountCents: 0,
	})
	if err != nil {
		t.Fatalf("zero amount should be allowed: %v", err)
	}
}
