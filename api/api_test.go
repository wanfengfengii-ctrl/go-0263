package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/olivepress/fruit-intake-gate/store"
)

func TestHealthCheck(t *testing.T) {
	srv := NewServer(store.NewMemory())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %v, want status ok", body)
	}
}

func TestTaskNotFoundEnvelope(t *testing.T) {
	srv := NewServer(store.NewMemory())
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/t1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code == "" {
		t.Fatal("expected stable error code in response")
	}
}
