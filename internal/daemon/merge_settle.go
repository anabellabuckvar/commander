package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example-user/commander/internal/api"
	"github.com/example-user/commander/internal/livedata"
	"github.com/example-user/commander/internal/store"
	"github.com/example-user/commander/internal/workflowtemplate"
)

// mergeAskRE is a card that asks the user to merge (or land) a pull request.
var mergeAskRE = regexp.MustCompile(`(?i)\b(merg(e|es|ed|ing)|land(s|ed|ing)?)\b`)

// prMerge is what GitHub says about a merged pull request.
type prMerge struct {
	State string
	By    string
	At    string
	SHA   string
}

// mergeSettler answers an open merge card once the user merged its pull
// requests on GitHub: the merge is the user's direction, so the card's
// approving option is recorded through the same typed path as the app's click
// (cmdr internal-resolve-needs, run as the user). A pull request someone else
// merged is left to the Commander (the stale-card mail): cmdrd never decides
// for the user on anyone else's action.
type mergeSettler struct {
	viewer func(ctx context.Context) (string, error)                       // the user's GitHub login
	readPR func(ctx context.Context, repo, number string) (prMerge, error) // one pull request's merge facts
	answer func(ctx context.Context, argv []string) api.CmdResult

	mu       sync.Mutex
	login    string
	merges   map[string]prMerge // repo#N -> merge facts; a merge is final, so read once
	done     map[string]bool    // card generations settled, or left to the Commander
	attempts map[string]int
}

func newMergeSettler() *mergeSettler {
	return &mergeSettler{
		viewer: func(ctx context.Context) (string, error) {
			out, err := workflowtemplate.RunGH(ctx, "api", "user", "--jq", ".login")
			return strings.TrimSpace(string(out)), err
		},
		readPR: ghPRMerge,
		answer: api.ExecRunner(cmdrPath()),
	}
}

func ghPRMerge(ctx context.Context, repo, number string) (prMerge, error) {
	out, err := workflowtemplate.RunGH(ctx, "pr", "view", number, "--repo", repo, "--json", "state,mergedBy,mergedAt,mergeCommit")
	if err != nil {
		return prMerge{}, err
	}
	var v struct {
		State    string `json:"state"`
		MergedAt string `json:"mergedAt"`
		MergedBy *struct {
			Login string `json:"login"`
		} `json:"mergedBy"`
		MergeCommit *struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return prMerge{}, err
	}
	m := prMerge{State: v.State, At: v.MergedAt}
	if v.MergedBy != nil {
		m.By = v.MergedBy.Login
	}
	if v.MergeCommit != nil {
		m.SHA = v.MergeCommit.OID
	}
	return m, nil
}

// approvingOption is the card's option that approves: the recommended one
// first.
func approvingOption(d *store.Decision) (store.DecisionOption, bool) {
	var first *store.DecisionOption
	for i, o := range d.Options {
		if !o.Approves {
			continue
		}
		if o.Recommended {
			return o, true
		}
		if first == nil {
			first = &d.Options[i]
		}
	}
	if first == nil {
		return store.DecisionOption{}, false
	}
	return *first, true
}

// mergedPRs are the board-tracked pull requests the card names (repo#N, from
// its basis), when every PR the card's words name is one of them and merged.
func mergedPRs(card livedata.NeedsYou, states map[string]string) []string {
	if card.Decision == nil || card.Basis == nil {
		return nil
	}
	refs := livedata.PRRefs(card.Decision, nil)
	if len(refs) == 0 {
		return nil
	}
	var keys []string
	for n := range refs {
		key := ""
		for k := range card.Basis.PRs {
			if strings.HasSuffix(k, "#"+n) {
				key = k
				break
			}
		}
		if key == "" || states[key] != "MERGED" {
			return nil // a PR the board does not track, or one not merged
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// settle answers every open merge card whose pull requests the user merged,
// and returns the cards it answered.
func (m *mergeSettler) settle(ctx context.Context, snap livedata.Snapshot) map[string]bool {
	settled := map[string]bool{}
	if m == nil {
		return settled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.merges == nil {
		m.merges, m.done, m.attempts = map[string]prMerge{}, map[string]bool{}, map[string]int{}
	}
	for _, card := range snap.NeedsYou {
		key := livedata.StaleNoticeKey(card)
		if m.done[key] || card.Decision == nil || !mergeAskRE.MatchString(card.Decision.Question+"\n"+card.Title) {
			continue
		}
		opt, ok := approvingOption(card.Decision)
		if !ok {
			continue
		}
		prs := mergedPRs(card, snap.PRStates)
		if len(prs) == 0 {
			continue
		}
		if m.login == "" {
			login, err := m.viewer(ctx)
			if err != nil || login == "" {
				log.Printf("daemon: merge settle: read the user's GitHub login: %v", err)
				return settled
			}
			m.login = login
		}
		var why []string
		mine := true
		for _, pr := range prs {
			f, ok := m.merges[pr]
			if !ok {
				repo, n, _ := strings.Cut(pr, "#")
				var err error
				if f, err = m.readPR(ctx, repo, n); err != nil {
					log.Printf("daemon: merge settle: read %s: %v", pr, err)
					mine = false
					break
				}
				if f.State == "MERGED" {
					m.merges[pr] = f
				}
			}
			if f.State != "MERGED" || !strings.EqualFold(f.By, m.login) {
				if f.State == "MERGED" {
					log.Printf("daemon: merge settle: %s:%s left to the Commander: %s merged by %s, not the user", card.Project, card.BeadID, pr, f.By)
					m.done[key] = true
				}
				mine = false
				break
			}
			_, n, _ := strings.Cut(pr, "#")
			line := fmt.Sprintf("PR #%s merged on GitHub by %s", n, f.By)
			if t, err := time.Parse(time.RFC3339, f.At); err == nil {
				line += " at " + t.UTC().Format("Jan 2 15:04 UTC")
			}
			if f.SHA != "" {
				line += fmt.Sprintf(" (%.10s)", f.SHA)
			}
			why = append(why, line)
		}
		if !mine {
			continue
		}
		argv := []string{"internal-resolve-needs", "--project", card.Project, "--bead", card.BeadID, "--action", "decide", "--choice", opt.Label, "--text", strings.Join(why, "; ")}
		if card.PlanRevision > 0 {
			argv = append(argv, "--revision", fmt.Sprint(card.PlanRevision))
		}
		res := m.answer(ctx, argv)
		if !res.OK {
			m.attempts[key]++
			log.Printf("daemon: merge settle: answer %s:%s (try %d): %s %s", card.Project, card.BeadID, m.attempts[key], strings.TrimSpace(res.Stderr), res.Error)
			if m.attempts[key] >= 3 {
				m.done[key] = true // the Commander's stale-card mail takes it from here
			}
			continue
		}
		log.Printf("daemon: merge settle: answered %s:%s %q: %s", card.Project, card.BeadID, opt.Label, strings.Join(why, "; "))
		m.done[key] = true
		settled[key] = true
	}
	return settled
}
