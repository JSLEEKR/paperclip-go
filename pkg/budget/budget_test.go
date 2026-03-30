package budget

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

func TestCreatePolicy(t *testing.T) {
	svc := newTestService()
	p, err := svc.CreatePolicy(Policy{
		ID:          "bp-1",
		CompanyID:   "co-1",
		Scope:       ScopeAgent,
		ScopeID:     "ag-1",
		AmountCents: 10000,
		WarnPercent: 0.8,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !p.Enabled {
		t.Error("expected enabled")
	}
	if p.Window != WindowMonthly {
		t.Errorf("window = %q, want %q", p.Window, WindowMonthly)
	}
}

func TestCreatePolicyValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		policy  Policy
		wantErr string
	}{
		{"no id", Policy{CompanyID: "co-1", ScopeID: "x", AmountCents: 100}, "id is required"},
		{"no company", Policy{ID: "1", ScopeID: "x", AmountCents: 100}, "company_id is required"},
		{"no scope_id", Policy{ID: "1", CompanyID: "co-1", AmountCents: 100}, "scope_id is required"},
		{"zero amount", Policy{ID: "1", CompanyID: "co-1", ScopeID: "x", AmountCents: 0}, "amount must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreatePolicy(tt.policy)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRecordCostNoBreach(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 10000, WarnPercent: 0.8,
	})

	result, err := svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 100,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if result.Blocked {
		t.Error("should not be blocked")
	}
	if len(result.Incidents) != 0 {
		t.Errorf("incidents = %d, want 0", len(result.Incidents))
	}
}

func TestSoftCeilingAlert(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000, WarnPercent: 0.8,
	})

	// Record 850 cents (85% of 1000 → exceeds 80% warn)
	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 850,
		Timestamp: time.Now().UTC(),
	})
	if result.Blocked {
		t.Error("should not be blocked (soft alert only)")
	}
	if len(result.Incidents) != 1 {
		t.Fatalf("incidents = %d, want 1", len(result.Incidents))
	}
	if result.Incidents[0].Severity != SeveritySoft {
		t.Errorf("severity = %q, want soft", result.Incidents[0].Severity)
	}
}

func TestHardCeilingBlock(t *testing.T) {
	svc := newTestService()
	paused := false
	svc.PauseCallback = func(scope Scope, scopeID, reason string) {
		paused = true
	}

	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000, WarnPercent: 0.8,
	})

	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 1000,
		Timestamp: time.Now().UTC(),
	})
	if !result.Blocked {
		t.Error("should be blocked")
	}
	if !paused {
		t.Error("pause callback should have been called")
	}
	if len(result.Incidents) != 1 {
		t.Fatalf("incidents = %d, want 1", len(result.Incidents))
	}
	if result.Incidents[0].Severity != SeverityHard {
		t.Errorf("severity = %q, want hard", result.Incidents[0].Severity)
	}
}

func TestAccumulatedCosts(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 500, WarnPercent: 0.8,
	})

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		svc.RecordCost(CostEvent{
			ID: fmt.Sprintf("ce-%d", i), CompanyID: "co-1", AgentID: "ag-1",
			AmountCents: 90, Timestamp: now,
		})
	}
	// 5 × 90 = 450, above 80% of 500 (400)
	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-5", CompanyID: "co-1", AgentID: "ag-1",
		AmountCents: 10, Timestamp: now,
	})
	// 460 cents, above 400 threshold
	if len(result.Incidents) == 0 {
		t.Error("expected soft alert for accumulated costs")
	}
}

func TestGetInvocationBlock(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 100, WarnPercent: 0.8,
	})

	svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 200,
		Timestamp: time.Now().UTC(),
	})

	blocked, reason := svc.GetInvocationBlock("co-1", "ag-1", "")
	if !blocked {
		t.Error("expected invocation block")
	}
	if reason == "" {
		t.Error("expected block reason")
	}
}

func TestGetInvocationBlockNoBlock(t *testing.T) {
	svc := newTestService()
	blocked, _ := svc.GetInvocationBlock("co-1", "ag-1", "")
	if blocked {
		t.Error("should not be blocked with no policies")
	}
}

func TestResolveIncident(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 100, WarnPercent: 0.8,
	})
	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 200,
		Timestamp: time.Now().UTC(),
	})

	if len(result.Incidents) == 0 {
		t.Fatal("expected incident")
	}

	err := svc.ResolveIncident(result.Incidents[0].ID, "raise_budget_and_resume")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Block should be cleared
	blocked, _ := svc.GetInvocationBlock("co-1", "ag-1", "")
	if blocked {
		t.Error("should not be blocked after resolution")
	}
}

func TestListPolicies(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000,
	})
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-2", CompanyID: "co-1", Scope: ScopeCompany,
		ScopeID: "co-1", AmountCents: 50000,
	})
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-3", CompanyID: "co-2", Scope: ScopeAgent,
		ScopeID: "ag-2", AmountCents: 2000,
	})

	policies := svc.ListPolicies("co-1")
	if len(policies) != 2 {
		t.Errorf("count = %d, want 2", len(policies))
	}
}

func TestCompanyScopeBudget(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeCompany,
		ScopeID: "co-1", AmountCents: 1000, WarnPercent: 0.8,
	})

	// Any agent in the company counts
	svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 500,
		Timestamp: time.Now().UTC(),
	})
	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-2", CompanyID: "co-1", AgentID: "ag-2", AmountCents: 600,
		Timestamp: time.Now().UTC(),
	})
	if !result.Blocked {
		t.Error("expected company-wide block")
	}
}

func TestProjectScopeBudget(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeProject,
		ScopeID: "proj-1", AmountCents: 500, WarnPercent: 0.8,
	})

	result, _ := svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1",
		ProjectID: "proj-1", AmountCents: 600,
		Timestamp: time.Now().UTC(),
	})
	if !result.Blocked {
		t.Error("expected project block")
	}

	// Different project should not be blocked
	result2, _ := svc.RecordCost(CostEvent{
		ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1",
		ProjectID: "proj-2", AmountCents: 600,
		Timestamp: time.Now().UTC(),
	})
	if result2.Blocked {
		t.Error("different project should not be blocked")
	}
}

func TestTotalSpent(t *testing.T) {
	svc := newTestService()
	now := time.Now().UTC()

	svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 100, Timestamp: now,
	})
	svc.RecordCost(CostEvent{
		ID: "ce-2", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 200, Timestamp: now,
	})
	svc.RecordCost(CostEvent{
		ID: "ce-3", CompanyID: "co-1", AgentID: "ag-2", AmountCents: 300, Timestamp: now,
	})

	total := svc.TotalSpent("co-1", ScopeAgent, "ag-1")
	if total != 300 {
		t.Errorf("total = %d, want 300", total)
	}

	totalCompany := svc.TotalSpent("co-1", ScopeCompany, "co-1")
	if totalCompany != 600 {
		t.Errorf("total company = %d, want 600", totalCompany)
	}
}

func TestDeletePolicy(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000,
	})

	if err := svc.DeletePolicy("bp-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := svc.GetPolicy("bp-1")
	if err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestUpdatePolicy(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000,
	})

	p, err := svc.UpdatePolicy("bp-1", func(p *Policy) error {
		p.AmountCents = 5000
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if p.AmountCents != 5000 {
		t.Errorf("amount = %d, want 5000", p.AmountCents)
	}
}

func TestListOpenIncidents(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 100, WarnPercent: 0.8,
	})
	svc.RecordCost(CostEvent{
		ID: "ce-1", CompanyID: "co-1", AgentID: "ag-1", AmountCents: 200,
		Timestamp: time.Now().UTC(),
	})

	open := svc.ListOpenIncidents("co-1")
	if len(open) == 0 {
		t.Error("expected open incidents")
	}
}

func TestWarnPercentDefault(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreatePolicy(Policy{
		ID: "bp-1", CompanyID: "co-1", Scope: ScopeAgent,
		ScopeID: "ag-1", AmountCents: 1000, WarnPercent: -1,
	})
	if p.WarnPercent != 0.8 {
		t.Errorf("warn_percent = %f, want 0.8", p.WarnPercent)
	}
}
