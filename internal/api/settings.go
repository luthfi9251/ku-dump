package api

import (
	"net/http"
)

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	out := map[string][]string{}
	for name, eng := range s.Engines {
		missing := eng.ToolsMissing()
		if missing == nil {
			missing = []string{}
		}
		out[name] = missing
	}
	jsonOut(w, http.StatusOK, out)
}
