package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example-user/commander/internal/livedata"
)

// serveWorkstreamStatuses is "workstream statuses": the board tree as text at
// the current snapshot version, for the app to attach to each of the user's
// messages (?last= the version it last attached, which turns the block into a
// one-line "unchanged"). It is the same tree the app's tree view draws
// (GET /api/tree) and `cmdr workstream-statuses` prints.
func (s *Server) serveWorkstreamStatuses(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	t := livedata.BuildTree(s.current(r.Context()), now)
	version := int(t.Version)
	block := livedata.TreeBlock(t, now)
	if last, err := strconv.Atoi(r.URL.Query().Get("last")); err == nil && last != 0 && last == version {
		block = livedata.UnchangedBlock(version)
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "block": block})
}

// serveTree is the board tree the app's tree view draws.
func (s *Server) serveTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, livedata.BuildTree(s.current(r.Context()), time.Now().Unix()))
}
