package launcher

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/owenrumney/ade/internal/provision"
)

var (
	provisionClone  = provision.Clone
	provisionBranch = provision.Branch
)

func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repository string `json:"repository"`
		Token      string `json:"token"`
		// Branch is the design document the work is for, which names its branch.
		Branch string `json:"branch,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}
	dir, err := provisionClone(r.Context(), req.Repository, token, provision.Run)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reply := map[string]string{"path": dir}
	if branch := provision.BranchName(req.Branch); branch != "" {
		// The clone is usable either way, so a branch that failed is said and
		// left for the push to sort out, as it was before there was one.
		if err := provisionBranch(r.Context(), dir, branch, provision.Run); err != nil {
			slog.Warn("the clone stays on its default branch", "dir", dir, "err", err)
		} else {
			reply["branch"] = branch
		}
	}
	s.hub.Broadcast("repos", nil)
	writeJSON(w, http.StatusCreated, reply)
}
