package task

import (
	"strings"
	"testing"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestCreateTask(t *testing.T) {
	svc := newTestService()
	tk, err := svc.Create(Task{
		ID:        "task-1",
		CompanyID: "co-1",
		Title:     "Build feature X",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tk.Status != StatusBacklog {
		t.Errorf("status = %q, want %q", tk.Status, StatusBacklog)
	}
	if tk.Priority != PriorityMedium {
		t.Errorf("priority = %q, want %q", tk.Priority, PriorityMedium)
	}
	if tk.OriginKind != OriginManual {
		t.Errorf("origin = %q, want %q", tk.OriginKind, OriginManual)
	}
}

func TestCreateTaskValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		task    Task
		wantErr string
	}{
		{"no id", Task{CompanyID: "co-1", Title: "A"}, "id is required"},
		{"no company", Task{ID: "1", Title: "A"}, "company_id is required"},
		{"no title", Task{ID: "1", CompanyID: "co-1"}, "title is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(tt.task)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckoutAndRelease(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})

	// Checkout
	err := svc.Checkout("task-1", "ag-1", "run-1")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	tk, _ := svc.Get("task-1")
	if tk.Status != StatusInProgress {
		t.Errorf("status = %q, want %q", tk.Status, StatusInProgress)
	}
	if tk.CheckoutRunID != "run-1" {
		t.Errorf("run_id = %q, want %q", tk.CheckoutRunID, "run-1")
	}
	if tk.CheckoutLeaseExp == nil {
		t.Error("expected non-nil lease expiry")
	}

	// Release
	err = svc.Release("task-1", "run-1")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	tk, _ = svc.Get("task-1")
	if tk.Status != StatusTodo {
		t.Errorf("status = %q, want %q", tk.Status, StatusTodo)
	}
	if tk.CheckoutRunID != "" {
		t.Errorf("run_id should be empty, got %q", tk.CheckoutRunID)
	}
}

func TestCheckoutConflict(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})

	_ = svc.Checkout("task-1", "ag-1", "run-1")

	// Second checkout should fail
	err := svc.Checkout("task-1", "ag-2", "run-2")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "already checked out") {
		t.Errorf("error = %q, want checkout conflict", err)
	}
}

func TestCheckoutSameRunAllowed(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})

	_ = svc.Checkout("task-1", "ag-1", "run-1")
	// Same run ID should succeed (idempotent)
	err := svc.Checkout("task-1", "ag-1", "run-1")
	if err != nil {
		t.Fatalf("same run checkout should succeed: %v", err)
	}
}

func TestLeaseExpiry(t *testing.T) {
	svc := newTestService()
	svc.SetLeaseDuration(1 * time.Millisecond) // Very short lease

	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	time.Sleep(5 * time.Millisecond)

	// Task should be checkable by another run (lease expired)
	tk, _ := svc.Get("task-1")
	if tk.IsCheckedOut() {
		t.Error("expected lease to be expired")
	}
}

func TestReapExpiredLeases(t *testing.T) {
	svc := newTestService()
	svc.SetLeaseDuration(1 * time.Millisecond)

	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_, _ = svc.Create(Task{ID: "task-2", CompanyID: "co-1", Title: "B"})

	_ = svc.Checkout("task-1", "ag-1", "run-1")
	_ = svc.Checkout("task-2", "ag-2", "run-2")

	time.Sleep(5 * time.Millisecond)

	reaped := svc.ReapExpiredLeases()
	if len(reaped) != 2 {
		t.Errorf("reaped %d tasks, want 2", len(reaped))
	}

	for _, id := range reaped {
		tk, _ := svc.Get(id)
		if tk.Status != StatusTodo {
			t.Errorf("reaped task %s status = %q, want todo", id, tk.Status)
		}
	}
}

func TestRenewLease(t *testing.T) {
	svc := newTestService()
	svc.SetLeaseDuration(100 * time.Millisecond)

	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	// Get original expiry
	tk1, _ := svc.Get("task-1")
	origExp := *tk1.CheckoutLeaseExp

	time.Sleep(10 * time.Millisecond)
	_ = svc.RenewLease("task-1", "run-1")

	tk2, _ := svc.Get("task-1")
	if !tk2.CheckoutLeaseExp.After(origExp) {
		t.Error("lease should be extended")
	}
}

func TestRenewLeaseWrongRun(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	err := svc.RenewLease("task-1", "run-2")
	if err == nil {
		t.Fatal("expected error for wrong run ID")
	}
}

func TestComplete(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	err := svc.Complete("task-1", "run-1")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	tk, _ := svc.Get("task-1")
	if tk.Status != StatusDone {
		t.Errorf("status = %q, want %q", tk.Status, StatusDone)
	}
	if tk.CompletedAt == nil {
		t.Error("expected non-nil completed_at")
	}
	if tk.CheckoutRunID != "" {
		t.Error("checkout should be cleared")
	}
}

func TestCompleteWrongRun(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	err := svc.Complete("task-1", "run-2")
	if err == nil {
		t.Fatal("expected error for wrong run")
	}
}

func TestAssign(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})

	err := svc.Assign("task-1", "ag-1")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	tk, _ := svc.Get("task-1")
	if tk.AssigneeAgentID != "ag-1" {
		t.Errorf("assignee = %q, want %q", tk.AssigneeAgentID, "ag-1")
	}
	if tk.Status != StatusTodo {
		t.Errorf("status = %q, want %q (auto-promoted from backlog)", tk.Status, StatusTodo)
	}
}

func TestAssignTerminalTask(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A", Status: StatusDone})

	err := svc.Assign("task-1", "ag-1")
	if err == nil {
		t.Fatal("expected error assigning terminal task")
	}
}

func TestTransition(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A", Status: StatusTodo})

	err := svc.Transition("task-1", StatusInProgress)
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	tk, _ := svc.Get("task-1")
	if tk.Status != StatusInProgress {
		t.Errorf("status = %q", tk.Status)
	}
}

func TestInvalidTransition(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})

	// backlog → in_progress is not allowed
	err := svc.Transition("task-1", StatusInProgress)
	if err == nil {
		t.Fatal("expected invalid transition error")
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from Status
		to   Status
		want bool
	}{
		{StatusBacklog, StatusTodo, true},
		{StatusBacklog, StatusCancelled, true},
		{StatusBacklog, StatusInProgress, false},
		{StatusTodo, StatusInProgress, true},
		{StatusInProgress, StatusDone, true},
		{StatusInProgress, StatusBlocked, true},
		{StatusDone, StatusTodo, true},
		{StatusDone, StatusInProgress, false},
	}
	for _, tt := range tests {
		tk := Task{Status: tt.from}
		if got := tk.CanTransition(tt.to); got != tt.want {
			t.Errorf("CanTransition(%s→%s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestDelegate(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{
		ID:          "task-1",
		CompanyID:   "co-1",
		Title:       "Parent task",
		BillingCode: "BC-001",
		Priority:    PriorityHigh,
	})

	child, err := svc.Delegate("task-1", "task-2", "Sub-task", "ag-1", "ag-2")
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if child.ParentID != "task-1" {
		t.Errorf("parent_id = %q", child.ParentID)
	}
	if child.RequestDepth != 1 {
		t.Errorf("request_depth = %d, want 1", child.RequestDepth)
	}
	if child.BillingCode != "BC-001" {
		t.Errorf("billing_code = %q, want %q", child.BillingCode, "BC-001")
	}
	if child.OriginKind != OriginDelegate {
		t.Errorf("origin_kind = %q, want %q", child.OriginKind, OriginDelegate)
	}
	if child.Priority != PriorityHigh {
		t.Errorf("priority = %q, should inherit from parent", child.Priority)
	}
}

func TestDelegateChain(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "t-1", CompanyID: "co-1", Title: "Root"})
	_, _ = svc.Delegate("t-1", "t-2", "Child 1", "ag-1", "ag-2")
	child2, _ := svc.Delegate("t-2", "t-3", "Grandchild", "ag-2", "ag-3")

	if child2.RequestDepth != 2 {
		t.Errorf("request_depth = %d, want 2", child2.RequestDepth)
	}
}

func TestListByAssignee(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "t-1", CompanyID: "co-1", Title: "A", AssigneeAgentID: "ag-1"})
	_, _ = svc.Create(Task{ID: "t-2", CompanyID: "co-1", Title: "B", AssigneeAgentID: "ag-1"})
	_, _ = svc.Create(Task{ID: "t-3", CompanyID: "co-1", Title: "C", AssigneeAgentID: "ag-2"})

	tasks := svc.ListByAssignee("ag-1")
	if len(tasks) != 2 {
		t.Errorf("count = %d, want 2", len(tasks))
	}
}

func TestListSubtasks(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "t-1", CompanyID: "co-1", Title: "Parent"})
	_, _ = svc.Delegate("t-1", "t-2", "Child 1", "ag-1", "ag-2")
	_, _ = svc.Delegate("t-1", "t-3", "Child 2", "ag-1", "ag-3")

	subs := svc.ListSubtasks("t-1")
	if len(subs) != 2 {
		t.Errorf("subtask count = %d, want 2", len(subs))
	}
}

func TestAuditLog(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")
	_ = svc.Complete("task-1", "run-1")

	entries := svc.GetAuditLog("task-1")
	if len(entries) < 3 {
		t.Errorf("audit entries = %d, want >= 3 (created, checkout, completed)", len(entries))
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusBacklog, false},
		{StatusTodo, false},
		{StatusInProgress, false},
		{StatusDone, true},
		{StatusCancelled, true},
	}
	for _, tt := range tests {
		tk := Task{Status: tt.status}
		if got := tk.IsTerminal(); got != tt.want {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestReleaseWrongRun(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Task{ID: "task-1", CompanyID: "co-1", Title: "A"})
	_ = svc.Checkout("task-1", "ag-1", "run-1")

	err := svc.Release("task-1", "run-2")
	if err == nil {
		t.Fatal("expected error for wrong run")
	}
}

func TestParentTaskNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Create(Task{ID: "t-1", CompanyID: "co-1", Title: "A", ParentID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
}
