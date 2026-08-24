// Package api exposes the OlivePress JSON HTTP interface: stable response
// contracts, sorted reason lists, transaction boundaries and the health check.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/olivepress/fruit-intake-gate/store"
)

// Server is the HTTP runtime wrapping a persistence Store.
type Server struct {
	store store.Store
	mux   *http.ServeMux
}

// NewServer builds the HTTP server and registers its routes.
func NewServer(s store.Store) *Server {
	srv := &Server{store: s, mux: http.NewServeMux()}
	srv.mux.HandleFunc("GET /healthz", srv.handleHealth)
	srv.mux.HandleFunc("GET /v1/catalog/plots/{id}", srv.handleGetPlot)
	srv.mux.HandleFunc("GET /v1/catalog/rules/{digest}", srv.handleGetRule)
	srv.mux.HandleFunc("POST /v1/tasks/lock", srv.handleLock)
	srv.mux.HandleFunc("GET /v1/tasks/{id}", srv.handleGetTask)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/sample-confirm", srv.handleSampleConfirm)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/split-samples", srv.handleSplitSamples)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/reveal", srv.handleReveal)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/start-resources", srv.handleStartResources)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/maturity-counts", srv.handleMaturityCounts)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/readings", srv.handleReadings)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/foreign-matter", srv.handleForeignMatter)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/rejudge", srv.handleRejudge)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/reviews", srv.handleReview)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/finalize", srv.handleFinalize)
	return srv
}

// Handler returns the root HTTP handler for the server.
func (s *Server) Handler() http.Handler { return s.mux }

// ErrorResponse is the stable JSON error envelope returned by the API.
type ErrorResponse struct {
	Code    string   `json:"code"`
	Reason  string   `json:"reason"`
	Reasons []string `json:"reasons,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeStoreError converts a store error into the stable JSON envelope and an
// appropriate HTTP status.
func writeStoreError(w http.ResponseWriter, err error) {
	var ce *store.CodedError
	if errors.As(err, &ce) {
		writeJSON(w, httpStatusFor(ce.Code), ErrorResponse{
			Code:    ce.Code,
			Reason:  ce.Reason,
			Reasons: ce.Causes,
		})
		return
	}
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{
		Code:   store.CodeInvalidRequest,
		Reason: err.Error(),
	})
}

// httpStatusFor maps a stable error code to an HTTP status.
func httpStatusFor(code string) int {
	switch code {
	case store.CodeNotFound:
		return http.StatusNotFound
	case store.CodeInvalidRequest,
		store.CodeFixedPointInvalid,
		store.CodeFixedPointOverflow:
		return http.StatusBadRequest
	default:
		return http.StatusConflict
	}
}

// decodeBody decodes a JSON request body, returning a stable bad-request error
// on malformed input.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeStoreError(w, store.NewCodedError(store.CodeInvalidRequest, "malformed request body"))
		return false
	}
	return true
}
