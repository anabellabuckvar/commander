package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/example-user/commander/internal/store"
)

// vibe-apps-zq9h.6.63.36 (Nov 18): the review sent the plan back and sat at
// "Not started" forever. The system writes the revision before it, briefed with
// the findings, and the review reruns once the revision closes.
func TestSentBackReviewGetsItsRevision(t *testing.T) {
	review := store.SentBackReview{ProjectID: 2, Project: "vibe-apps", Path: "/p", Bead: "vibe-apps-zq9h.6.63.36", Title: "Review: the chart-open-under-2s plan", Parent: "vibe-apps-zq9h.6.63",
		Summary: "Final report: **SEND BACK**. Findings:\n1. The p95 target is measured on the wrong route."}
	var applied [][]string
	var reran []string
	n := reviseSentBackReviews(context.Background(), func(context.Context) ([]store.SentBackReview, error) { return []store.SentBackReview{review}, nil },
		func(_ context.Context, dir string, ops [][]string) error { applied = ops; return nil },
		func(_ context.Context, _ int64, bead string) error { reran = append(reran, bead); return nil })
	if n != 1 || len(applied) != 2 || len(reran) != 1 || reran[0] != review.Bead {
		t.Fatalf("n=%d ops=%v reran=%v", n, applied, reran)
	}
	create := strings.Join(applied[0], " ")
	for _, want := range []string{"create Revise the chart-open-under-2s plan to answer the review", "lane:agents,revises:vibe-apps-zq9h.6.63.36", "--parent vibe-apps-zq9h.6.63", "measured on the wrong route", "--acceptance"} {
		if !strings.Contains(create, want) {
			t.Errorf("revision lacks %q: %s", want, create)
		}
	}
	if strings.Join(applied[1], " ") != "dep add vibe-apps-zq9h.6.63.36 $revision" {
		t.Fatalf("the review does not wait on its revision: %v", applied[1])
	}
}

// vibe-apps-596t.12.14 (Nov 18) ended blocked waiting for a deploy; the Automatic
// step in front of it closed when the deploy landed; it runs again.
func TestBlockedStepRerunsWhenItsBlockerCloses(t *testing.T) {
	var reran []string
	n := rerunUnblocked(context.Background(), func(context.Context) ([]store.SentBackReview, error) {
		return []store.SentBackReview{{ProjectID: 2, Bead: "vibe-apps-596t.12.14"}}, nil
	}, func(_ context.Context, _ int64, bead string) error { reran = append(reran, bead); return nil })
	if n != 1 || len(reran) != 1 || reran[0] != "vibe-apps-596t.12.14" {
		t.Fatalf("reran %v", reran)
	}
}
