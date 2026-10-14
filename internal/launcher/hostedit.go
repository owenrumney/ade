package launcher

import (
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/settings"
)

// Adding a host was a whole-object settings save, which loses whatever else
// changed in between. These two touch the one field, under settings.Update.
func (s *Server) handleAddHost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := settings.CheckHostName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The local machine is in the list every time it is drawn, never stored.
	// Bookmarking it would give it two rows and one of them would not connect.
	if strings.EqualFold(name, remote.LocalName) {
		writeError(w, http.StatusBadRequest, remote.LocalName+" is this machine; it is always in the list")
		return
	}

	taken := false
	err := settings.Update(func(cfg *settings.Settings) error {
		for _, host := range cfg.Hosts {
			if strings.EqualFold(strings.TrimSpace(host.Name), name) {
				taken = true
				return nil
			}
		}
		cfg.Hosts = append(cfg.Hosts, settings.Host{Name: name, Label: strings.TrimSpace(req.Label)})
		return nil
	})
	if taken {
		writeError(w, http.StatusConflict, name+" is already a machine ade knows")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.hub.Broadcast("hosts", nil)
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "label": req.Label})
}

// handleRemoveHost forgets a bookmark. The connection goes first: a machine
// dropped from the list while still dialled leaves a master nothing names.
func (s *Server) handleRemoveHost(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name a machine to forget")
		return
	}

	// The stored spelling: s.hosts is keyed by the name the connection was
	// made under, and the other one leaves an ssh master nothing names.
	var stored string
	err := settings.Update(func(cfg *settings.Settings) error {
		stored = storedName(cfg.Hosts, name)
		cfg.Hosts = without(cfg.Hosts, name)
		return nil
	})
	// The error first: a settings file that would not load never ran the
	// change, and an empty stored name then reads as no such machine.
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stored == "" {
		writeError(w, http.StatusNotFound, name+" is not one of the machines ade knows")
		return
	}

	// After the save: a failed one would otherwise drop the connection and
	// keep the host, which is the worst of both.
	s.disconnect(stored)

	s.hub.Broadcast("hosts", nil)
	w.WriteHeader(http.StatusNoContent)
}

// storedName is the bookmark's own spelling of a name asked for in any case,
// or empty if there is no such bookmark.
func storedName(hosts []settings.Host, asked string) string {
	for _, host := range hosts {
		if name := strings.TrimSpace(host.Name); strings.EqualFold(name, asked) {
			return name
		}
	}
	return ""
}

func without(hosts []settings.Host, asked string) []settings.Host {
	kept := make([]settings.Host, 0, len(hosts))
	for _, host := range hosts {
		if strings.EqualFold(strings.TrimSpace(host.Name), asked) {
			continue
		}
		kept = append(kept, host)
	}
	return kept
}

// handleLabelHost renames a bookmark's row. The name is what ssh dials and
// what the connection is keyed by, so it stays and the connection with it.
func (s *Server) handleLabelHost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	label := strings.TrimSpace(req.Label)

	var stored string
	err := settings.Update(func(cfg *settings.Settings) error {
		for i, host := range cfg.Hosts {
			if strings.EqualFold(strings.TrimSpace(host.Name), name) {
				stored = strings.TrimSpace(host.Name)
				cfg.Hosts[i].Label = label
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stored == "" {
		writeError(w, http.StatusNotFound, name+" is not one of the machines ade knows")
		return
	}

	s.hub.Broadcast("hosts", nil)
	writeJSON(w, http.StatusOK, map[string]any{"name": stored, "label": label})
}
