package livedata

import (
	"regexp"
	"slices"
	"strings"

	"github.com/example-user/commander/internal/workflowtemplate"
)

// mergeAskWords: a card that asks the user to merge (or land) pull requests.
var mergeAskWords = regexp.MustCompile(`(?i)\b(merg(e|es|ing)|land|lands|landing)\b`)

// HoldRedMergeCards keeps a merge card off NEEDS YOU while a pull request it
// names is open and not green, or conflicts: the user merges only what is
// ready, so until then the step waits on those checks (Snapshot.MergeHeld).
// A PR whose facts are not read yet does not hold the card.
func HoldRedMergeCards(snap *Snapshot, heads func(pr string) (workflowtemplate.Head, bool)) {
	if heads == nil || len(snap.NeedsYou) == 0 {
		return
	}
	repos := map[string]string{}
	for _, ws := range snap.Workstreams {
		if ws.Repo != "" {
			repos[ws.Project] = ws.Repo
		}
	}
	keep := snap.NeedsYou[:0]
	for _, n := range snap.NeedsYou {
		if why := mergeHold(n, repos[n.Project], heads); why != "" {
			if snap.MergeHeld == nil {
				snap.MergeHeld = map[string]string{}
			}
			snap.MergeHeld[n.Project+"\x00"+n.BeadID] = why
			continue
		}
		keep = append(keep, n)
	}
	snap.NeedsYou = keep
}

// mergeHold is what a merge card waits on before the user can merge, or "".
func mergeHold(n NeedsYou, repo string, heads func(pr string) (workflowtemplate.Head, bool)) string {
	text := n.Title + "\n" + n.Ask
	if n.Decision != nil {
		text += "\n" + n.Decision.Question
	}
	if repo == "" || !mergeAskWords.MatchString(text) {
		return ""
	}
	refs := addPRWords(PRRefs(n.Decision, nil), n.Title+"\n"+n.Ask)
	nums := make([]string, 0, len(refs))
	for num := range refs {
		nums = append(nums, num)
	}
	slices.Sort(nums)
	var parts []string
	for _, num := range nums {
		h, ok := heads("https://github.com/" + strings.ToLower(repo) + "/pull/" + num)
		if !ok || h.State != "OPEN" {
			continue
		}
		if h.Mergeable == "CONFLICTING" {
			parts = append(parts, "PR #"+num+" to resolve its merge conflicts")
			continue
		}
		if workflowtemplate.CIGreen(h) != nil {
			bad := workflowtemplate.NotGreen(h)
			if len(bad) == 0 {
				bad = []string{"CI to start"}
			}
			parts = append(parts, "PR #"+num+"'s "+strings.Join(bad, ", "))
		}
	}
	return strings.Join(parts, "; ")
}
