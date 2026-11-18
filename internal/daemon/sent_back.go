package daemon

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/example-user/commander/internal/beads"
	"github.com/example-user/commander/internal/store"
)

// reviseOps are the ops that answer a sent-back review: an Implementer step
// under the same parent whose brief is the review's findings, which the review
// then waits on.
func reviseOps(r store.SentBackReview) [][]string {
	subject := strings.TrimSpace(reviewPrefix.ReplaceAllString(r.Title, ""))
	if subject == "" {
		subject = "the reviewed work"
	}
	create := []string{"create", "Revise " + subject + " to answer the review", "--labels", "kind:objective,lane:agents,revises:" + r.Bead, "--ref", "revision", "--silent",
		"--description", "The review " + r.Bead + " (\"" + r.Title + "\") sent this back. Answer every finding below (fix it, with evidence, or give the reason it stands), then write your final report listing each finding and how it was answered. The review runs again when this closes.\n\nThe review's report:\n" + strings.TrimSpace(r.Summary),
		"--acceptance", "Every finding in the review is answered (fixed with evidence, or a stated reason), and the final report lists each one."}
	if r.Parent != "" {
		create = append(create, "--parent", r.Parent)
	}
	return [][]string{create, {"dep", "add", r.Bead, "$revision"}}
}

var reviewPrefix = regexp.MustCompile(`(?i)^\s*review\s*:?\s*`)

// reviseSentBackReviews writes the revision of every review that sent its work
// back, and makes the review run again once the revision closes.
func reviseSentBackReviews(ctx context.Context, list func(context.Context) ([]store.SentBackReview, error), apply func(ctx context.Context, dir string, ops [][]string) error, rerun func(ctx context.Context, pid int64, bead string) error) int {
	reviews, err := list(ctx)
	if err != nil {
		log.Printf("sent-back: list: %v", err)
		return 0
	}
	n := 0
	for _, r := range reviews {
		if err := apply(ctx, r.Path, reviseOps(r)); err != nil {
			log.Printf("sent-back: revision for %s refused: %v", r.Bead, err)
			continue
		}
		if err := rerun(ctx, r.ProjectID, r.Bead); err != nil {
			log.Printf("sent-back: %s will not rerun: %v", r.Bead, err)
		}
		log.Printf("sent-back: %s sent its work back; a revision step now comes before it", r.Bead)
		n++
	}
	return n
}

// applySystemPlan applies ops as one plan batch through the validated door.
func applySystemPlan(ctx context.Context, dir string, ops [][]string) error {
	basis, err := beads.CurrentPlanBasis(ctx, dir, ops)
	if err != nil {
		return err
	}
	res, err := beads.ApplyPlan(ctx, dir, ops, basis, nil)
	if err == nil && res.Exit != 0 {
		err = errorString(strings.TrimSpace(res.Error))
	}
	return err
}

type errorString string

func (e errorString) Error() string { return string(e) }

// rerunUnblocked dispatches again every step whose last run ended blocked once
// a blocker of it closed after that run: the event it waited for happened.
func rerunUnblocked(ctx context.Context, list func(context.Context) ([]store.SentBackReview, error), rerun func(ctx context.Context, pid int64, bead string) error) int {
	steps, err := list(ctx)
	if err != nil {
		log.Printf("rerun-unblocked: list: %v", err)
		return 0
	}
	n := 0
	for _, s := range steps {
		if err := rerun(ctx, s.ProjectID, s.Bead); err != nil {
			log.Printf("rerun-unblocked: %s: %v", s.Bead, err)
			continue
		}
		log.Printf("rerun-unblocked: %s ended blocked and what it waited on has closed; it runs again", s.Bead)
		n++
	}
	return n
}
