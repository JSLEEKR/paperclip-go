package heartbeat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestEngine() *Engine {
	return NewEngine(store.New(""))
}

func TestEnqueue(t *testing.T) {
	eng := newTestEngine()
	run, err := eng.Enqueue(Run{
		ID:        "run-1",
		CompanyID: "co-1",
		AgentID:   "ag-1",
		Source:    SourceOnDemand,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if run.Status != RunQueued {
		t.Errorf("status = %q, want %q", run.Status, RunQueued)
	}
	if run.QueuedAt.IsZero() {
		t.Error("expected non-zero queued_at")
	}
}

func TestEnqueueValidation(t *testing.T) {
	eng := newTestEngine()
	tests := []struct {
		name    string
		run     Run
		wantErr string
	}{
		{"no id", Run{CompanyID: "co-1", AgentID: "ag-1"}, "id is required"},
		{"no agent", Run{ID: "1", CompanyID: "co-1"}, "agent_id is required"},
		{"no company", Run{ID: "1", AgentID: "ag-1"}, "company_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := eng.Enqueue(tt.run)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestClaimRun(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})

	run, err := eng.Claim("run-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if run.Status != RunRunning {
		t.Errorf("status = %q, want %q", run.Status, RunRunning)
	}
	if run.StartedAt == nil {
		t.Error("expected non-nil started_at")
	}
	if run.LeaseExpiresAt == nil {
		t.Error("expected non-nil lease_expires_at")
	}
}

func TestClaimAlreadyRunning(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	_, err := eng.Claim("run-1")
	if err == nil {
		t.Fatal("expected error claiming running run")
	}
}

func TestClaimBlocked(t *testing.T) {
	eng := newTestEngine()
	eng.SetBlockCheck(func(companyID, agentID string) (bool, string) {
		return true, "budget exceeded"
	})
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})

	_, err := eng.Claim("run-1")
	if err == nil {
		t.Fatal("expected block error")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error = %q, want blocked", err)
	}
}

func TestClaimMaxConcurrent(t *testing.T) {
	eng := newTestEngine()
	eng.SetMaxConcurrent("ag-1", 1)

	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})

	eng.Claim("run-1")
	_, err := eng.Claim("run-2")
	if err == nil {
		t.Fatal("expected max concurrent error")
	}
}

func TestClaimNext(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	time.Sleep(time.Millisecond)
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})

	eng.SetMaxConcurrent("ag-1", 2)
	run, err := eng.ClaimNext("ag-1")
	if err != nil {
		t.Fatalf("claim next: %v", err)
	}
	if run.ID != "run-1" {
		t.Errorf("claimed %q, want run-1 (oldest)", run.ID)
	}
}

func TestCompleteRun(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	err := eng.Complete("run-1", `{"ok":true}`, 100, 50, 25)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	run, _ := eng.Get("run-1")
	if run.Status != RunCompleted {
		t.Errorf("status = %q", run.Status)
	}
	if run.ResultJSON != `{"ok":true}` {
		t.Errorf("result = %q", run.ResultJSON)
	}
	if run.InputTokens != 100 || run.OutputTokens != 50 || run.CostCents != 25 {
		t.Error("token/cost counts wrong")
	}
	if run.CompletedAt == nil {
		t.Error("expected completed_at")
	}
	if run.LeaseExpiresAt != nil {
		t.Error("lease should be cleared")
	}
}

func TestFailRun(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	err := eng.Fail("run-1", "out of memory", 137)
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	run, _ := eng.Get("run-1")
	if run.Status != RunFailed {
		t.Errorf("status = %q", run.Status)
	}
	if run.ErrorMessage != "out of memory" {
		t.Errorf("error = %q", run.ErrorMessage)
	}
	if *run.ExitCode != 137 {
		t.Errorf("exit_code = %d", *run.ExitCode)
	}
}

func TestCancelRun(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})

	err := eng.Cancel("run-1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	run, _ := eng.Get("run-1")
	if run.Status != RunCancelled {
		t.Errorf("status = %q", run.Status)
	}
}

func TestCancelTerminalRun(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")
	eng.Complete("run-1", "", 0, 0, 0)

	err := eng.Cancel("run-1")
	if err == nil {
		t.Fatal("expected error cancelling completed run")
	}
}

func TestRenewLease(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	run1, _ := eng.Get("run-1")
	origExp := *run1.LeaseExpiresAt

	time.Sleep(time.Millisecond)
	err := eng.RenewLease("run-1")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	run2, _ := eng.Get("run-1")
	if !run2.LeaseExpiresAt.After(origExp) {
		t.Error("lease should be extended")
	}
}

func TestRenewLeaseNotRunning(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})

	err := eng.RenewLease("run-1")
	if err == nil {
		t.Fatal("expected error renewing queued run")
	}
}

func TestReapExpiredLeases(t *testing.T) {
	eng := newTestEngine()
	eng.SetLeaseDuration(1 * time.Millisecond)

	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-2"})
	eng.SetMaxConcurrent("ag-1", 2)
	eng.SetMaxConcurrent("ag-2", 2)
	eng.Claim("run-1")
	eng.Claim("run-2")

	time.Sleep(5 * time.Millisecond)
	reaped := eng.ReapExpiredLeases()
	if len(reaped) != 2 {
		t.Errorf("reaped = %d, want 2", len(reaped))
	}

	for _, id := range reaped {
		run, _ := eng.Get(id)
		if run.Status != RunTimedOut {
			t.Errorf("run %s status = %q, want timed_out", id, run.Status)
		}
	}
}

func TestListByAgent(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-3", CompanyID: "co-1", AgentID: "ag-2"})

	runs := eng.ListByAgent("ag-1")
	if len(runs) != 2 {
		t.Errorf("count = %d, want 2", len(runs))
	}
}

func TestListQueued(t *testing.T) {
	eng := newTestEngine()
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	queued := eng.ListQueued("ag-1")
	if len(queued) != 1 {
		t.Errorf("queued = %d, want 1", len(queued))
	}
}

func TestRunDuration(t *testing.T) {
	now := time.Now()
	start := now.Add(-5 * time.Minute)
	run := Run{StartedAt: &start, CompletedAt: &now}
	d := run.Duration()
	if d < 4*time.Minute || d > 6*time.Minute {
		t.Errorf("duration = %v, want ~5m", d)
	}
}

func TestRunIsTerminal(t *testing.T) {
	tests := []struct {
		status RunStatus
		want   bool
	}{
		{RunQueued, false},
		{RunRunning, false},
		{RunCompleted, true},
		{RunFailed, true},
		{RunCancelled, true},
		{RunTimedOut, true},
	}
	for _, tt := range tests {
		r := Run{Status: tt.status}
		if got := r.IsTerminal(); got != tt.want {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestActiveRunCount(t *testing.T) {
	eng := newTestEngine()
	eng.SetMaxConcurrent("ag-1", 5)
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-3", CompanyID: "co-1", AgentID: "ag-1"})

	eng.Claim("run-1")
	eng.Claim("run-2")

	if got := eng.ActiveRunCount("ag-1"); got != 2 {
		t.Errorf("active = %d, want 2", got)
	}
}

func TestStartAndStop(t *testing.T) {
	eng := newTestEngine()
	eng.SetExecuteFunc(func(ctx context.Context, run Run) (string, int64, int64, int64, error) {
		return "ok", 0, 0, 0, nil
	})

	ctx := context.Background()
	eng.Start(ctx)
	// Brief wait to ensure goroutine starts
	time.Sleep(10 * time.Millisecond)
	eng.Stop()
}

func TestStartStopRestart(t *testing.T) {
	eng := newTestEngine()
	eng.SetExecuteFunc(func(ctx context.Context, run Run) (string, int64, int64, int64, error) {
		return "ok", 0, 0, 0, nil
	})

	ctx := context.Background()

	// First cycle
	eng.Start(ctx)
	time.Sleep(10 * time.Millisecond)
	eng.Stop()

	// Second cycle — previously panicked with "close of closed channel"
	eng.Start(ctx)
	time.Sleep(10 * time.Millisecond)
	eng.Stop()
}

func TestDefaultSource(t *testing.T) {
	eng := newTestEngine()
	run, _ := eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	if run.Source != SourceOnDemand {
		t.Errorf("source = %q, want %q", run.Source, SourceOnDemand)
	}
}

func TestSetMaxConcurrentBounds(t *testing.T) {
	eng := newTestEngine()
	eng.SetMaxConcurrent("ag-1", -5)  // should be clamped to 1
	eng.SetMaxConcurrent("ag-2", 100) // should be clamped to 10

	// Verify by trying to claim
	eng.Enqueue(Run{ID: "run-1", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Enqueue(Run{ID: "run-2", CompanyID: "co-1", AgentID: "ag-1"})
	eng.Claim("run-1")

	_, err := eng.Claim("run-2")
	if err == nil {
		t.Error("expected max concurrent error (should be clamped to 1)")
	}
}
