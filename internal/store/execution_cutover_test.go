package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// cutoverRun claims the fixture's one ready bead and starts a native run on
// runtime for it.
func cutoverRun(t *testing.T, s *Store, pid int64, ctx context.Context, runtime string) (*ExecutionClaim, AgentRun, int64) {
	t.Helper()
	now := time.Now()
	if _, e := s.DB().ExecContext(ctx, `UPDATE bead_tasks SET execution_ready=FALSE WHERE project_id=? AND id<>'leaf'`, pid); e != nil {
		t.Fatal(e)
	}
	c, e := s.ClaimNextExecution(ctx, "vm-0", 1, now)
	if e != nil || c == nil || c.BeadID != "leaf" {
		t.Fatal(c, e)
	}
	run := AgentRun{ExecutionClaimToken: c.Token, ProjectID: pid, TaskBeadID: c.BeadID, AgentName: "operator-cutover", OpencodeSessionID: "codex-session", ExecutionDriver: "native", ExecutionRuntime: runtime, ExecutionAccount: "default", ExecutionModel: "gpt-test", SpawnTime: now.Unix(), WorktreePath: sql.NullString{String: "/private/retained-workspace", Valid: true}}
	rid, e := s.InsertAgentRun(ctx, run)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB().ExecContext(ctx, `UPDATE bead_tasks SET status='in_progress' WHERE project_id=? AND id=?`, pid, c.BeadID); e != nil {
		t.Fatal(e)
	}
	return c, run, rid
}

func TestPostgresCutoverMovesARunningStepToTheNewRuntime(t *testing.T) {
	s, pid, ctx := executionFixture(t)
	_, run, rid := cutoverRun(t, s, pid, ctx, "codex")

	if got, e := s.RunningUnder(ctx, pid, "leaf"); e != nil || len(got) != 1 || got[0].RunID != rid || got[0].Runtime != "codex" {
		t.Fatalf("running under: %+v %v", got, e)
	}
	if got, e := s.RequestExecutionCutover(ctx, pid, "leaf", "codex", "", "commander"); e != nil || got != 0 {
		t.Fatalf("a run already on the runtime was asked to move: %d %v", got, e)
	}
	for i := 0; i < 2; i++ {
		if got, e := s.RequestExecutionCutover(ctx, pid, "leaf", "claude", "Commander switched this step to claude", "commander"); e != nil || got != rid {
			t.Fatalf("request %d: %d %v", i, got, e)
		}
	}
	var asked int
	if e := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM bead_comments WHERE run_id=? AND fact_kind=?`, rid, CutoverFactKind).Scan(&asked); e != nil || asked != 1 {
		t.Fatalf("asking twice recorded %d requests: %v", asked, e)
	}
	all, e := s.ExecutionCutovers(ctx)
	if e != nil || len(all) != 1 || !all[0].Running || all[0].To != "claude" || all[0].From != "codex" || all[0].ToldAt != 0 || all[0].Agent != "operator-cutover" || all[0].RequestedAt == 0 {
		t.Fatalf("pending cutover: %+v %v", all, e)
	}
	if e = s.RecordCutoverStep(ctx, all[0], CutoverToldKind, "told"); e != nil {
		t.Fatal(e)
	}
	if all, e = s.ExecutionCutovers(ctx); e != nil || len(all) != 1 || all[0].ToldAt == 0 {
		t.Fatalf("told not recorded: %+v %v", all, e)
	}

	// The agent saves its work and ends its turn: the step is queued to resume
	// from this run, not handed to Commander as a result to review.
	if e = s.FinishNativeExecutionReport(ctx, run.AgentName, rid, run.OpencodeSessionID, "exited", "Final report: pushed work in progress"); e != nil {
		t.Fatal(e)
	}
	var ready bool
	var resume, result sql.NullInt64
	if e = s.DB().QueryRowContext(ctx, `SELECT execution_ready,resume_run_id,result_run_id FROM bead_tasks WHERE project_id=? AND id='leaf'`, pid).Scan(&ready, &resume, &result); e != nil {
		t.Fatal(e)
	}
	if !ready || resume.Int64 != rid || result.Valid {
		t.Fatalf("after exit: ready=%v resume=%v result=%v", ready, resume, result)
	}
	if yes, e := s.UsageRecoverySource(ctx, rid); e != nil || !yes {
		t.Fatalf("a stopped cutover run is not a fresh-session source: %v %v", yes, e)
	}
	if to, e := s.CutoverTarget(ctx, rid); e != nil || to != "claude" {
		t.Fatalf("target: %q %v", to, e)
	}
	if all, e = s.ExecutionCutovers(ctx); e != nil || len(all) != 1 || all[0].Running {
		t.Fatalf("queued cutover: %+v %v", all, e)
	}

	if e = s.ReapExecutionStartupClaims(ctx, time.Now()); e != nil {
		t.Fatal(e)
	}
	next, e := s.ClaimNextExecution(ctx, "vm-0", 1, time.Now())
	if e != nil || next == nil || next.ResumeRunID != rid {
		t.Fatalf("relaunch claim: %+v %v", next, e)
	}
	run.ExecutionClaimToken, run.ResumeSourceRunID = next.Token, rid
	run.OpencodeSessionID = "other-session"
	if _, e = s.InsertAgentRun(ctx, run); e == nil {
		t.Fatal("a cutover to Claude resumed on Codex")
	}
	run.ExecutionRuntime, run.OpencodeSessionID, run.ExecutionModel = "claude", "claude-session", "sonnet"
	if _, e = s.InsertAgentRun(ctx, run); e != nil {
		t.Fatalf("the new runtime could not take over the retained workspace: %v", e)
	}
	if all, e = s.ExecutionCutovers(ctx); e != nil || len(all) != 0 {
		t.Fatalf("cutover still shown after the new run bound: %+v %v", all, e)
	}
}

func TestPostgresStopLeavesTheStepStoppedForReplanning(t *testing.T) {
	s, pid, ctx := executionFixture(t)
	_, run, rid := cutoverRun(t, s, pid, ctx, "codex")
	if got, e := s.RequestExecutionCutover(ctx, pid, "leaf", "", "Eric closed this follow-up", "commander"); e != nil || got != rid {
		t.Fatalf("stop: %d %v", got, e)
	}
	if e := s.FinishNativeExecutionReport(ctx, run.AgentName, rid, run.OpencodeSessionID, "exited", "Final report: stopped"); e != nil {
		t.Fatal(e)
	}
	var ready bool
	var why string
	var result sql.NullInt64
	if e := s.DB().QueryRowContext(ctx, `SELECT execution_ready,execution_error,result_run_id FROM bead_tasks WHERE project_id=? AND id='leaf'`, pid).Scan(&ready, &why, &result); e != nil {
		t.Fatal(e)
	}
	if ready || result.Valid || !strings.HasPrefix(why, StoppedByCommander) || !strings.Contains(why, "Eric closed") {
		t.Fatalf("after stop: ready=%v result=%v why=%q", ready, result, why)
	}
	if all, e := s.ExecutionCutovers(ctx); e != nil || len(all) != 0 {
		t.Fatalf("a finished stop is still shown: %+v %v", all, e)
	}
	if yes, e := s.UsageRecoverySource(ctx, rid); e != nil || yes {
		t.Fatalf("a stop became a relaunch source: %v %v", yes, e)
	}
	if e := s.ReapExecutionStartupClaims(ctx, time.Now()); e != nil {
		t.Fatal(e)
	}
	if c, e := s.ClaimNextExecution(ctx, "vm-0", 1, time.Now()); e != nil || c != nil {
		t.Fatalf("a stopped step relaunched: %+v %v", c, e)
	}
	if got, e := s.RequestExecutionCutover(ctx, pid, "leaf", "", "", "commander"); e != nil || got != 0 {
		t.Fatalf("stopping a step with nothing running: %d %v", got, e)
	}
}

// A run without a cutover keeps today's exit: its report is a result to review.
func TestPostgresExitWithoutCutoverIsAResult(t *testing.T) {
	s, pid, ctx := executionFixture(t)
	_, run, rid := cutoverRun(t, s, pid, ctx, "codex")
	if e := s.FinishNativeExecutionReport(ctx, run.AgentName, rid, run.OpencodeSessionID, "exited", "Final report: done"); e != nil {
		t.Fatal(e)
	}
	var result sql.NullInt64
	if e := s.DB().QueryRowContext(ctx, `SELECT result_run_id FROM bead_tasks WHERE project_id=? AND id='leaf'`, pid).Scan(&result); e != nil || result.Int64 != rid {
		t.Fatalf("result: %v %v", result, e)
	}
}
