package daemon

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/example-user/commander/internal/store"
)

// questionCard is the card for an agent's blocking question: the question in
// its own words, the agent's recommendation as context, answered in free text.
func questionCard(q store.OpenQuestion) (ask, decision string) {
	line := strings.TrimSpace(strings.SplitN(q.Question, "\n", 2)[0])
	question := line
	if len(question) > 240 {
		question = question[:240] + "…"
	}
	context := "The agent on “" + q.Title + "” asks: " + strings.TrimSpace(q.Question)
	if r := strings.TrimSpace(q.Recommendation); r != "" {
		context += "\n\nIts recommendation: " + r
	}
	if len(context) > 3500 {
		context = context[:3500] + "…"
	}
	b, _ := json.Marshal(map[string]any{"question": question, "context": context, "input": true})
	ask = "Answer the agent's question"
	return ask, string(b)
}

// raiseQuestionCards puts every agent's blocking question on the user's list
// as a card, so the row reads "Needs you" with the question, never an orange
// wait no one can answer.
func raiseQuestionCards(ctx context.Context, list func(context.Context) ([]store.OpenQuestion, error), run func(ctx context.Context, args ...string) error) int {
	qs, err := list(ctx)
	if err != nil || len(qs) == 0 {
		return 0
	}
	n := 0
	for _, q := range qs {
		ask, decision := questionCard(q)
		if err := run(ctx, "internal-set-objective", "--project", q.Project, "--bead", q.Bead, "--state", "needs-user", "--ask", ask, "--decision", decision, "--no-notify"); err != nil {
			log.Printf("question-card: %s %s: %v", q.Project, q.Bead, err)
			continue
		}
		log.Printf("question-card: %s %s: the agent's question is on the user's list", q.Project, q.Bead)
		n++
	}
	return n
}

// answerText is what the user's card answer tells the agent.
func answerText(choice, text string) string {
	choice, text = strings.TrimSpace(choice), strings.TrimSpace(text)
	switch {
	case choice != "" && text != "":
		return choice + ": " + text
	case text != "":
		return text
	}
	return choice
}
