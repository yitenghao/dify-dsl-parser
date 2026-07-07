package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"dify-dsl-parser/engine"
)

// Server wires the store + engine clients behind an http.Handler.
type Server struct {
	store *Store
	llm   engine.LLMClient // nil => llm nodes fail with a clear error
	tool  engine.ToolClient
}

// New builds a Server. Pass a real LLMClient/ToolClient to make interactive
// tests hit live models; pass nil (or a mock) for offline editing.
func New(store *Store, llm engine.LLMClient, tool engine.ToolClient) *Server {
	return &Server{store: store, llm: llm, tool: tool}
}

// Handler returns the routed http.Handler (Go 1.22 method+pattern mux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/flows", s.handleList)
	mux.HandleFunc("POST /api/flows", s.handleCreate)
	mux.HandleFunc("GET /api/flows/{id}", s.handleGet)
	mux.HandleFunc("PUT /api/flows/{id}", s.handleSave)
	mux.HandleFunc("DELETE /api/flows/{id}", s.handleDelete)

	mux.HandleFunc("POST /api/flows/{id}/publish", s.handlePublish)
	mux.HandleFunc("GET /api/flows/{id}/versions", s.handleVersions)
	mux.HandleFunc("GET /api/flows/{id}/versions/{v}", s.handleVersion)
	mux.HandleFunc("POST /api/flows/{id}/versions/{v}/restore", s.handleRestore)

	mux.HandleFunc("GET /api/flows/{id}/export", s.handleExport)
	mux.HandleFunc("POST /api/flows/{id}/run", s.handleRun)

	// stateless helpers
	mux.HandleFunc("POST /api/dsl/validate", s.handleValidate)
	mux.HandleFunc("POST /api/dsl/import", s.handleImport)

	return withCORS(mux)
}

// ---- flow CRUD ----------------------------------------------------------

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	flows, err := s.store.List()
	if err != nil {
		httpError(w, err)
		return
	}
	if flows == nil {
		flows = []FlowMeta{}
	}
	writeJSON(w, http.StatusOK, flows)
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name, Mode string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	m, err := s.store.Create(body.Name, body.Mode)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// flowResponse bundles metadata with the draft graph (as Dify DSL JSON).
type flowResponse struct {
	Meta  *FlowMeta      `json:"meta"`
	Graph map[string]any `json:"graph"`
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.store.Meta(id)
	if err != nil {
		httpError(w, err)
		return
	}
	yml, err := s.store.Draft(id)
	if err != nil {
		httpError(w, err)
		return
	}
	graph, err := yamlToJSONMap(yml)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flowResponse{Meta: m, Graph: graph})
}

// handleSave persists the draft. Body is the full DSL as JSON. We validate
// before writing and return any soft issues (but still save, matching Dify:
// a draft may be incomplete while you work on it).
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	yml, err := jsonToYAML(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	issues, parseErr := validateYAML(yml)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "parse: " + parseErr.Error()})
		return
	}
	name, mode := appNameMode(body)
	if err := s.store.SaveDraft(id, yml, name, mode); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "issues": issues})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- publish / versions -------------------------------------------------

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	var body struct{ Note string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	vi, err := s.store.Publish(r.PathValue("id"), body.Note)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, vi)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Meta(r.PathValue("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	vs := m.Versions
	if vs == nil {
		vs = []VersionInfo{}
	}
	writeJSON(w, http.StatusOK, vs)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad version"})
		return
	}
	yml, err := s.store.Version(r.PathValue("id"), v)
	if err != nil {
		httpError(w, err)
		return
	}
	graph, err := yamlToJSONMap(yml)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "graph": graph})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad version"})
		return
	}
	if err := s.store.Restore(r.PathValue("id"), v); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restoredFrom": v})
}

// ---- import / export / validate ----------------------------------------

// handleExport streams a flow's draft (or ?version=N) as a downloadable YAML
// file, byte-compatible with Dify's DSL export.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var (
		yml []byte
		err error
	)
	if vs := r.URL.Query().Get("version"); vs != "" && vs != "draft" {
		v, e := strconv.Atoi(vs)
		if e != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad version"})
			return
		}
		yml, err = s.store.Version(id, v)
	} else {
		yml, err = s.store.Draft(id)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	m, _ := s.store.Meta(id)
	name := "workflow"
	if m != nil {
		name = m.Name
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".yml"))
	_, _ = w.Write(yml)
}

// handleImport accepts raw YAML (text/plain or {"yaml": "..."} JSON) and
// returns the parsed graph as JSON plus validation issues, without persisting.
// The editor can then choose to create a flow from it.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	yml := body
	// allow a JSON envelope {"yaml": "..."} too
	var env struct {
		YAML string `json:"yaml"`
	}
	if json.Unmarshal(body, &env) == nil && env.YAML != "" {
		yml = []byte(env.YAML)
	}
	graph, err := yamlToJSONMap(yml)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	issues, parseErr := validateYAML(yml)
	resp := map[string]any{"graph": graph, "issues": issues}
	if parseErr != nil {
		resp["error"] = parseErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleValidate validates a DSL JSON body without saving.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	yml, err := jsonToYAML(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	issues, parseErr := validateYAML(yml)
	resp := map[string]any{"issues": issues, "valid": parseErr == nil && len(issues) == 0}
	if parseErr != nil {
		resp["error"] = parseErr.Error()
		resp["valid"] = false
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- helpers ------------------------------------------------------------

// appNameMode pulls app.name / app.mode out of a DSL JSON body so the store's
// list view reflects renames done in the editor.
func appNameMode(body []byte) (name, mode string) {
	var doc struct {
		App struct {
			Name string `json:"name"`
			Mode string `json:"mode"`
		} `json:"app"`
	}
	_ = json.Unmarshal(body, &doc)
	return doc.App.Name, doc.App.Mode
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
