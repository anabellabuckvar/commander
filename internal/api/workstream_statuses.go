package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// serveWorkstreamStatuses is "workstream statuses": the user's sidebar as
// JSON at the current board version, for the app to attach to each of the
// user's messages (?role= the chat, default commander; ?last= the version it
// last attached, which turns the block into a one-line "unchanged").
func (s *Server) serveWorkstreamStatuses(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	if role == "" {
		role = "commander"
	}
	st := livedata.WorkstreamStatuses(s.current(r.Context()), role, time.Now().Unix())
	block := st.Block()
	if last, err := strconv.Atoi(r.URL.Query().Get("last")); err == nil && last != 0 && last == st.Version {
		block = livedata.UnchangedBlock(st.Version)
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": st.Version, "block": block, "statuses": st})
}
