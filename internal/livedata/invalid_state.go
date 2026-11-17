package livedata

// InvalidState is one violation of the write validator found by cmdrd's
// continuous audit: the same rules every write is checked against.
type InvalidState struct {
	Project string   `json:"project"`
	Rule    string   `json:"rule"`
	Beads   []string `json:"beads"`
	Why     string   `json:"why"`
	Fix     string   `json:"fix"`
}

// CheckInvalidState is the finding prefix of an audited invalid state.
const CheckInvalidState = "invalid-state: "

// addInvalidState puts the audit's violations in ws on r's diagram, and on
// the diagram of each track holding one, as red findings.
func (b board) addInvalidState(ws SnapWorkstream, r *BoardRow) {
	if r.Diagram == nil {
		return
	}
	for _, iv := range b.s.InvalidState {
		if iv.Project != ws.Project || len(iv.Beads) == 0 {
			continue
		}
		id := ""
		for _, bead := range iv.Beads {
			if bead == ws.BeadID || findSnapStep(ws.Steps, bead) != nil {
				id = bead
				break
			}
		}
		if id == "" {
			continue
		}
		title := ws.Title
		if s := findSnapStep(ws.Steps, id); s != nil {
			title = s.Title
		}
		f := DiagramFinding{Check: CheckInvalidState + iv.Rule, Bead: id, Title: plainTitle(title), Why: iv.Why, Fix: iv.Fix}
		r.Diagram.Findings = append(r.Diagram.Findings, f)
		for i := range r.Diagram.Tracks {
			if t := findSnapStep(ws.Steps, r.Diagram.Tracks[i].ID); t != nil && holds(*t, id) {
				r.Diagram.Tracks[i].Findings = append(r.Diagram.Tracks[i].Findings, f)
			}
		}
	}
}
