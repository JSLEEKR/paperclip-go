package agent

import (
	"strings"
	"testing"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestHireAgent(t *testing.T) {
	svc := newTestService()
	a, err := svc.Hire(Agent{
		ID:        "ag-1",
		CompanyID: "co-1",
		Name:      "Claude",
		Role:      RoleExecutor,
	})
	if err != nil {
		t.Fatalf("hire: %v", err)
	}
	if a.Status != StatusIdle {
		t.Errorf("status = %q, want %q", a.Status, StatusIdle)
	}
	if a.AdapterType != "process" {
		t.Errorf("adapter_type = %q, want %q", a.AdapterType, "process")
	}
	if a.MaxConcurrentRuns != 1 {
		t.Errorf("max_concurrent = %d, want 1", a.MaxConcurrentRuns)
	}
}

func TestHireAgentValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{"no id", Agent{CompanyID: "co-1", Name: "A"}, "id is required"},
		{"no company", Agent{ID: "1", Name: "A"}, "company_id is required"},
		{"no name", Agent{ID: "1", CompanyID: "co-1"}, "name is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Hire(tt.agent)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestFireAgent(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "Claude"})

	if err := svc.Fire("ag-1"); err != nil {
		t.Fatalf("fire: %v", err)
	}
	a, _ := svc.Get("ag-1")
	if a.Status != StatusFired {
		t.Errorf("status = %q, want %q", a.Status, StatusFired)
	}
	if a.FiredAt == nil {
		t.Error("expected non-nil fired_at")
	}
}

func TestPauseAndResume(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "Claude"})

	if err := svc.Pause("ag-1", "budget exceeded"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	a, _ := svc.Get("ag-1")
	if a.Status != StatusPaused {
		t.Errorf("status = %q, want %q", a.Status, StatusPaused)
	}
	if a.PauseReason != "budget exceeded" {
		t.Errorf("reason = %q", a.PauseReason)
	}

	if err := svc.Resume("ag-1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	a, _ = svc.Get("ag-1")
	if a.Status != StatusIdle {
		t.Errorf("status = %q, want %q", a.Status, StatusIdle)
	}
	if a.PauseReason != "" {
		t.Errorf("reason should be cleared, got %q", a.PauseReason)
	}
}

func TestResumeNonPaused(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "Claude"})
	err := svc.Resume("ag-1")
	if err == nil {
		t.Fatal("expected error resuming non-paused agent")
	}
}

func TestListByCompany(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A"})
	_, _ = svc.Hire(Agent{ID: "ag-2", CompanyID: "co-1", Name: "B"})
	_, _ = svc.Hire(Agent{ID: "ag-3", CompanyID: "co-2", Name: "C"})

	agents := svc.ListByCompany("co-1")
	if len(agents) != 2 {
		t.Errorf("count = %d, want 2", len(agents))
	}
}

func TestListActive(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A"})
	_, _ = svc.Hire(Agent{ID: "ag-2", CompanyID: "co-1", Name: "B"})
	_ = svc.Pause("ag-2", "testing")

	active := svc.ListActive("co-1")
	if len(active) != 1 {
		t.Errorf("active count = %d, want 1", len(active))
	}
}

func TestIsActive(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusIdle, true},
		{StatusActive, true},
		{StatusPaused, false},
		{StatusFired, false},
	}
	for _, tt := range tests {
		a := Agent{Status: tt.status}
		if got := a.IsActive(); got != tt.want {
			t.Errorf("IsActive(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestHasCapability(t *testing.T) {
	a := Agent{Capabilities: []string{"code", "review", "test"}}
	if !a.HasCapability("code") {
		t.Error("expected agent to have 'code' capability")
	}
	if a.HasCapability("deploy") {
		t.Error("expected agent to not have 'deploy' capability")
	}
}

func TestReportingHierarchy(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ceo", CompanyID: "co-1", Name: "CEO"})
	_, _ = svc.Hire(Agent{ID: "mgr", CompanyID: "co-1", Name: "Manager", ReportsTo: "ceo"})
	_, _ = svc.Hire(Agent{ID: "dev", CompanyID: "co-1", Name: "Developer", ReportsTo: "mgr"})

	reports := svc.DirectReports("ceo")
	if len(reports) != 1 || reports[0].ID != "mgr" {
		t.Errorf("expected manager as direct report")
	}

	chain := svc.ChainOfCommand("dev")
	if len(chain) != 3 {
		t.Fatalf("chain length = %d, want 3", len(chain))
	}
	if chain[0].ID != "dev" || chain[1].ID != "mgr" || chain[2].ID != "ceo" {
		t.Error("unexpected chain order")
	}
}

func TestReportingCycleDetection(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "a", CompanyID: "co-1", Name: "A"})
	_, _ = svc.Hire(Agent{ID: "b", CompanyID: "co-1", Name: "B", ReportsTo: "a"})

	// Try to make A report to B (cycle: A → B → A)
	_, err := svc.Update("a", func(ag *Agent) error {
		ag.ReportsTo = "b"
		return nil
	})
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want cycle error", err)
	}
}

func TestSelfReportingRejected(t *testing.T) {
	svc := newTestService()
	_, err := svc.Hire(Agent{ID: "a", CompanyID: "co-1", Name: "A", ReportsTo: "a"})
	if err == nil {
		t.Fatal("expected self-reporting error")
	}
}

func TestRecordHeartbeat(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A"})

	if err := svc.RecordHeartbeat("ag-1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	a, _ := svc.Get("ag-1")
	if a.LastHeartbeatAt == nil {
		t.Error("expected non-nil last_heartbeat_at")
	}
}

func TestAddSpending(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A"})

	_ = svc.AddSpending("ag-1", 100)
	_ = svc.AddSpending("ag-1", 250)

	a, _ := svc.Get("ag-1")
	if a.SpentMonthlyCents != 350 {
		t.Errorf("spent = %d, want 350", a.SpentMonthlyCents)
	}
}

func TestMaxConcurrentRuns(t *testing.T) {
	svc := newTestService()
	a, _ := svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A", MaxConcurrentRuns: 0})
	if a.MaxConcurrentRuns != 1 {
		t.Errorf("max_concurrent = %d, want 1 (default)", a.MaxConcurrentRuns)
	}

	a2, _ := svc.Hire(Agent{ID: "ag-2", CompanyID: "co-1", Name: "B", MaxConcurrentRuns: 20})
	if a2.MaxConcurrentRuns != 10 {
		t.Errorf("max_concurrent = %d, want 10 (capped)", a2.MaxConcurrentRuns)
	}
}

func TestDeleteAgent(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "A"})
	if err := svc.Delete("ag-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.Count() != 0 {
		t.Errorf("count = %d, want 0", svc.Count())
	}
}

func TestAgentUpdate(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Hire(Agent{ID: "ag-1", CompanyID: "co-1", Name: "Claude"})

	a, err := svc.Update("ag-1", func(a *Agent) error {
		a.Title = "Senior Engineer"
		a.Capabilities = []string{"code", "review"}
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if a.Title != "Senior Engineer" {
		t.Errorf("title = %q", a.Title)
	}
	if len(a.Capabilities) != 2 {
		t.Errorf("capabilities = %v", a.Capabilities)
	}
}
