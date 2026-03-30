package goal

import (
	"strings"
	"testing"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestCreateGoal(t *testing.T) {
	svc := newTestService()
	g, err := svc.Create(Goal{
		ID:        "goal-1",
		CompanyID: "co-1",
		Title:     "Ship v1.0",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.Status != StatusActive {
		t.Errorf("status = %q, want active", g.Status)
	}
	if g.Priority != 5 {
		t.Errorf("priority = %d, want 5 (default)", g.Priority)
	}
	if g.Progress != 0 {
		t.Errorf("progress = %f, want 0", g.Progress)
	}
}

func TestCreateGoalValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		goal    Goal
		wantErr string
	}{
		{"no id", Goal{CompanyID: "co-1", Title: "A"}, "id is required"},
		{"no company", Goal{ID: "1", Title: "A"}, "company_id is required"},
		{"no title", Goal{ID: "1", CompanyID: "co-1"}, "title is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(tt.goal)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestCompleteGoal(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "goal-1", CompanyID: "co-1", Title: "Ship"})

	if err := svc.Complete("goal-1"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	g, _ := svc.Get("goal-1")
	if g.Status != StatusCompleted {
		t.Errorf("status = %q", g.Status)
	}
	if g.Progress != 1.0 {
		t.Errorf("progress = %f, want 1.0", g.Progress)
	}
	if g.CompletedAt == nil {
		t.Error("expected completed_at")
	}
}

func TestAbandonGoal(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "goal-1", CompanyID: "co-1", Title: "Ship"})

	if err := svc.Abandon("goal-1"); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	g, _ := svc.Get("goal-1")
	if g.Status != StatusAbandoned {
		t.Errorf("status = %q", g.Status)
	}
}

func TestUpdateProgress(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "goal-1", CompanyID: "co-1", Title: "Ship"})

	if err := svc.UpdateProgress("goal-1", 0.5); err != nil {
		t.Fatalf("update progress: %v", err)
	}
	g, _ := svc.Get("goal-1")
	if g.Progress != 0.5 {
		t.Errorf("progress = %f, want 0.5", g.Progress)
	}
}

func TestProgressClamping(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "goal-1", CompanyID: "co-1", Title: "Ship"})

	svc.UpdateProgress("goal-1", 1.5)
	g, _ := svc.Get("goal-1")
	if g.Progress != 1.0 {
		t.Errorf("progress = %f, want 1.0 (clamped)", g.Progress)
	}

	svc.UpdateProgress("goal-1", -0.5)
	g, _ = svc.Get("goal-1")
	if g.Progress != 0.0 {
		t.Errorf("progress = %f, want 0.0 (clamped)", g.Progress)
	}
}

func TestHierarchicalGoals(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Company Goal"})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-1", Title: "Sub-goal 1", ParentID: "g-1"})
	svc.Create(Goal{ID: "g-3", CompanyID: "co-1", Title: "Sub-goal 2", ParentID: "g-1"})

	children := svc.ListChildren("g-1")
	if len(children) != 2 {
		t.Errorf("children = %d, want 2", len(children))
	}
}

func TestTopLevel(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Top 1"})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-1", Title: "Top 2"})
	svc.Create(Goal{ID: "g-3", CompanyID: "co-1", Title: "Sub", ParentID: "g-1"})

	top := svc.TopLevel("co-1")
	if len(top) != 2 {
		t.Errorf("top level = %d, want 2", len(top))
	}
}

func TestParentNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Sub", ParentID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
}

func TestDeleteGoalWithChildren(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Parent"})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-1", Title: "Child", ParentID: "g-1"})

	err := svc.Delete("g-1")
	if err == nil {
		t.Fatal("expected error deleting goal with children")
	}
	if !strings.Contains(err.Error(), "has child goals") {
		t.Errorf("error = %q", err)
	}
}

func TestDeleteLeafGoal(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Leaf"})

	if err := svc.Delete("g-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.Count() != 0 {
		t.Errorf("count = %d, want 0", svc.Count())
	}
}

func TestDefaultGoal(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Low priority", Priority: 10})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-1", Title: "High priority", Priority: 1})
	svc.Create(Goal{ID: "g-3", CompanyID: "co-1", Title: "Medium priority", Priority: 5})

	g, err := svc.DefaultGoal("co-1")
	if err != nil {
		t.Fatalf("default goal: %v", err)
	}
	if g.ID != "g-2" {
		t.Errorf("default goal = %q, want g-2 (highest priority)", g.ID)
	}
}

func TestDefaultGoalNoActive(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Done", Status: StatusCompleted})

	_, err := svc.DefaultGoal("co-1")
	if err == nil {
		t.Fatal("expected error with no active goals")
	}
}

func TestListActive(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "Active"})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-1", Title: "Done"})
	svc.Complete("g-2")

	active := svc.ListActive("co-1")
	if len(active) != 1 {
		t.Errorf("active = %d, want 1", len(active))
	}
}

func TestListByCompany(t *testing.T) {
	svc := newTestService()
	svc.Create(Goal{ID: "g-1", CompanyID: "co-1", Title: "A"})
	svc.Create(Goal{ID: "g-2", CompanyID: "co-2", Title: "B"})

	goals := svc.ListByCompany("co-1")
	if len(goals) != 1 {
		t.Errorf("goals = %d, want 1", len(goals))
	}
}

func TestIsActive(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusActive, true},
		{StatusCompleted, false},
		{StatusAbandoned, false},
	}
	for _, tt := range tests {
		g := Goal{Status: tt.status}
		if got := g.IsActive(); got != tt.want {
			t.Errorf("IsActive(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
