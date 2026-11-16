package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// A run that crashes mid-work, with its claim held, must not leave the step
// "working" or turn its last interim message into a result: it is restarted
// once on a fresh agent, and a second crash goes to the Commander with its cause.
func TestPostgresCrashedRunReleasesItsClaimRestartsOnceThenEscalates(t *testing.T) {
	s, pid, ctx := executionFixture(t)
	now := time.Now()
	claim, e := s.ClaimNextExecution(ctx, "vm-0", 1, now)
	if e != nil || claim == nil {
		t.Fatal(claim, e)
	}
	if _, e = s.DB().Exec(`UPDATE bead_tasks SET execution_ready=FALSE WHERE project_id=? AND id<>?`, pid, claim.BeadID); e != nil {
		t.Fatal(e)
	}
	run := AgentRun{ExecutionClaimToken: claim.Token, ProjectID: pid, TaskBeadID: claim.BeadID, AgentName: "operator-crash-first", OpencodeSessionID: "crash-session-1", ExecutionDriver: "native", ExecutionRuntime: "claude", ExecutionAccount: "default", ExecutionModel: "haiku", SpawnTime: now.Unix()}
	rid, e := s.InsertAgentRun(ctx, run)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB().Exec(`UPDATE bead_tasks SET status='in_progress' WHERE project_id=? AND id=?`, pid, claim.BeadID); e != nil {
		t.Fatal(e)
	}
	interim := "The regression run is still going; I'll write the handoff when it finishes."
	if e = s.FinishNativeExecutionExit(ctx, run.AgentName, rid, run.OpencodeSessionID, "native-error", interim, "exit code 1"); e != nil {
		t.Fatal(e)
	}

	type step struct {
		ready   bool
		result  sql.NullInt64
		dueAt   int64
		errText string
	}
	read := func() step {
		t.Helper()
		var st step
		if e := s.DB().QueryRow(`SELECT execution_ready,result_run_id,execution_due_at,execution_error FROM bead_tasks WHERE project_id=? AND id=?`, pid, claim.BeadID).Scan(&st.ready, &st.result, &st.dueAt, &st.errText); e != nil {
			t.Fatal(e)
		}
		return st
	}
	first := read()
	if first.result.Valid {
		t.Fatal("a crash's interim message was recorded as the step's result")
	}
	if !first.ready || first.dueAt < now.Add(CrashRestartDelay-time.Second).Unix() {
		t.Fatalf("first crash was not queued for one delayed restart: %+v", first)
	}
	if !strings.HasPrefix(first.errText, AgentStopped+"it crashed (exit code 1)") {
		t.Fatalf("step does not say why its agent stopped: %q", first.errText)
	}
	if old, e := s.GetAgentRun(ctx, rid); e != nil || old.Status != "native-error" {
		t.Fatal("crashed run still holds the step", old, e)
	}
	if c, e := s.ClaimNextExecution(ctx, "vm-1", 1, now); e != nil || c != nil {
		t.Fatal("restart dispatched before its backoff", c, e)
	}

	later := now.Add(CrashRestartDelay + time.Second)
	if e = s.ReapExecutionStartupClaims(ctx, later); e != nil {
		t.Fatal(e)
	}
	again, e := s.ClaimNextExecution(ctx, "vm-1", 1, later)
	if e != nil || again == nil || again.BeadID != claim.BeadID || again.ResumeRunID != 0 {
		t.Fatal("crashed step was not restarted on a fresh agent", again, e)
	}
	second := AgentRun{ExecutionClaimToken: again.Token, ProjectID: pid, TaskBeadID: claim.BeadID, AgentName: "operator-crash-second", OpencodeSessionID: "crash-session-2", ExecutionDriver: "native", ExecutionRuntime: "claude", ExecutionAccount: "default", ExecutionModel: "haiku", SpawnTime: later.Unix()}
	rid2, e := s.InsertAgentRun(ctx, second)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishNativeExecutionExit(ctx, second.AgentName, rid2, second.OpencodeSessionID, "native-error", "", "signal killed"); e != nil {
		t.Fatal(e)
	}
	final := read()
	if final.result.Valid || final.ready {
		t.Fatalf("second crash was restarted again or became a result: %+v", final)
	}
	if !strings.HasPrefix(final.errText, AgentStopped+"it crashed (signal killed) again after its automatic restart") {
		t.Fatalf("escalation does not carry the cause: %q", final.errText)
	}
	var toSupervisor bool
	if e = s.DB().QueryRow(`SELECT responsible_agent_id=commander_supervisor(project_id,id) FROM bead_tasks WHERE project_id=? AND id=?`, pid, claim.BeadID).Scan(&toSupervisor); e != nil || !toSupervisor {
		t.Fatal("second crash was not handed to the Commander", toSupervisor, e)
	}
	if c, e := s.ClaimNextExecution(ctx, "vm-0", 1, later.Add(time.Hour)); e != nil || c != nil {
		t.Fatal("escalated step was dispatched without the Commander", c, e)
	}
}

// A clean exit is the agent's own handoff and still reaches review.
func TestPostgresCleanExitWithoutFinalReportStillHandsOff(t *testing.T) {
	s, pid, ctx := executionFixture(t)
	now := time.Now()
	claim, e := s.ClaimNextExecution(ctx, "vm-0", 1, now)
	if e != nil || claim == nil {
		t.Fatal(claim, e)
	}
	run := AgentRun{ExecutionClaimToken: claim.Token, ProjectID: pid, TaskBeadID: claim.BeadID, AgentName: "operator-clean-exit", OpencodeSessionID: "clean-session", ExecutionDriver: "native", ExecutionRuntime: "claude", ExecutionAccount: "default", ExecutionModel: "haiku", SpawnTime: now.Unix()}
	rid, e := s.InsertAgentRun(ctx, run)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishNativeExecutionExit(ctx, run.AgentName, rid, run.OpencodeSessionID, "exited", "TRAIN READY: branch pushed, tests pass.", ""); e != nil {
		t.Fatal(e)
	}
	var result sql.NullInt64
	var errText string
	if e = s.DB().QueryRow(`SELECT result_run_id,execution_error FROM bead_tasks WHERE project_id=? AND id=?`, pid, claim.BeadID).Scan(&result, &errText); e != nil {
		t.Fatal(e)
	}
	if !result.Valid || result.Int64 != rid || strings.HasPrefix(errText, AgentStopped) {
		t.Fatal("a clean handoff did not reach review", result, errText)
	}
}
