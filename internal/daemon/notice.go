package daemon

import (
	"context"
	"log"
	"strings"
	"time"
)

// Agent mail to the Commander and Managers is retired. What it used to carry
// now has its own place: an agent's question is the user's card, a result is
// the step's final report, a wake-up is an event wait the watcher resumes, and
// a structure problem is refused by validation or shown as a red invalid-state
// row on the board the Commander reads every turn.
//
// cmdrdNotice is the one channel left for cmdrd's notices: it wakes a
// running agent in its own session (a resolved wait on the step it sleeps on)
// and drops anything addressed to the Commander or a Manager, with a log line.
func cmdrdNotice(send func(ctx context.Context, role, text string) error) func(ctx context.Context, role, subject, body string) error {
	return func(ctx context.Context, role, subject, body string) error {
		if strings.HasPrefix(role, "operator-") && send != nil {
			if err := send(ctx, role, "[cmdrd] "+subject+"\n\n"+body); err != nil {
				log.Printf("daemon: notice to %s not delivered: %v", role, err)
			}
			return nil
		}
		log.Printf("daemon: notice for %s not mailed (the board carries it): %s", role, subject)
		return nil
	}
}

// healthLog records a health problem; the board shows it, nobody is mailed.
func healthLog(key, detail string, since time.Time) error {
	head, _, _ := strings.Cut(detail, "\n")
	log.Printf("daemon: health %s (since %s, on the board, not mailed): %s", key, since.Format(time.Kitchen), head)
	return nil
}
