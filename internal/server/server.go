// Package server implements the control-plane HTTP API from api/openapi.yaml on top
// of the vm.Manager. It is deliberately thin: decode, authorize, delegate, encode.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Leovoss/anviq-microvm-engine/internal/vm"
)

type Server struct {
	mgr   *vm.Manager
	token string
	mux   *http.ServeMux
}

func New(mgr *vm.Manager, token string) *Server {
	s := &Server{mgr: mgr, token: token, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.auth(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("POST /v1/sandboxes", s.createSandbox)
	s.mux.HandleFunc("GET /v1/sandboxes/{id}", s.getSandbox)
	s.mux.HandleFunc("DELETE /v1/sandboxes/{id}", s.destroySandbox)
	s.mux.HandleFunc("POST /v1/sandboxes/{id}/prepare", s.prepareSandbox)
	s.mux.HandleFunc("POST /v1/sandboxes/{id}/exec", s.execSandbox)
	s.mux.HandleFunc("POST /v1/sandboxes/{id}/stop", s.stopSandbox)
	s.mux.HandleFunc("GET /v1/sandboxes/{id}/fs", s.fsGet)
	s.mux.HandleFunc("PUT /v1/sandboxes/{id}/fs", s.fsPut)
}

// auth enforces the single host-to-host bearer token. The engine never authenticates
// end users — that is the client's job (rakazo Spaces). Tenants are isolated by the
// microVM boundary, not by request identity.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.token == "" || got != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) createSandbox(w http.ResponseWriter, r *http.Request) {
	var req vm.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == "" {
		http.Error(w, "team_id is required", http.StatusBadRequest)
		return
	}
	box, err := s.mgr.Create(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, box)
}

func (s *Server) getSandbox(w http.ResponseWriter, r *http.Request) {
	box := s.mgr.Get(r.PathValue("id"))
	if box == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, box)
}

func (s *Server) prepareSandbox(w http.ResponseWriter, r *http.Request) {
	// Idempotent guest setup goes here once the guest agent lands. No-op today.
	if s.mgr.Get(r.PathValue("id")) == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) execSandbox(w http.ResponseWriter, r *http.Request) {
	var req vm.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad exec request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	err := s.mgr.Exec(r.Context(), r.PathValue("id"), req, func(ev vm.ProcessEvent) error {
		if err := enc.Encode(ev); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		// Stream already started; surface as a trailing stderr+exit is preferable,
		// but for pre-stream errors (unknown/paused VM) fall back to 404-ish text.
		http.Error(w, err.Error(), http.StatusConflict)
	}
}

func (s *Server) stopSandbox(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.Stop(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) destroySandbox(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.Destroy(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fsGet serves list (mode=list) or read (mode=read) of a path inside the guest.
func (s *Server) fsGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("mode") == "read" {
		data, err := s.mgr.ReadFile(r.Context(), id, path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
		return
	}
	entries, err := s.mgr.ListFiles(r.Context(), id, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// fsPut writes the request body to a path inside the guest.
func (s *Server) fsPut(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.mgr.WriteFile(r.Context(), r.PathValue("id"), path, body); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
