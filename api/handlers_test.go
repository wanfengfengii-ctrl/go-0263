package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
)

func seedMemory(t *testing.T) store.Store {
	t.Helper()
	st := store.NewMemory()
	c := catalog.SeedCatalog()
	ctx := context.Background()
	for _, p := range c.Plots() {
		if err := st.PutPlot(ctx, p); err != nil {
			t.Fatalf("seed plot: %v", err)
		}
	}
	for _, r := range c.Rules() {
		if err := st.PutRule(ctx, r); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
	}
	return st
}

func TestLockAndGetTaskOverHTTP(t *testing.T) {
	srv := NewServer(seedMemory(t))

	rule := catalog.SeedCatalog()
	var digest catalog.RuleDigest
	for _, r := range rule.Rules() {
		digest = r.Digest
	}

	body := map[string]any{
		"operation_no": "op-http",
		"plot_id":      "plot-picual-1",
		"cultivar_id":  "picual",
		"harvest_at":   "2026-10-01T00:00:00Z",
		"intake_batch": "http-batch",
		"crate_seals":  []string{"http-s1", "http-s2"},
		"blind_codes":  []string{"http-b1", "http-b2"},
		"thresholds": map[string]any{
			"acid":       map[string]any{"scale": 2, "min": 0, "max": 80},
			"peroxide":   map[string]any{"scale": 2, "min": 0, "max": 2000},
			"polyphenol": map[string]any{"scale": 0, "min": 150, "max": 0},
			"moisture":   map[string]any{"scale": 1, "min": 0, "max": 550},
			"fruit_temp": map[string]any{"scale": 1, "min": 0, "max": 350},
		},
		"reviewer_ids": []string{"rev-a", "rev-b"},
		"rule_digest":  string(digest),
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("lock status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	var view store.TaskView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("decode lock body: %v", err)
	}
	if view.Task.ID == "" {
		t.Fatalf("lock response missing task id")
	}

	// GET the recovered state.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+string(view.Task.ID), nil)
	getRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", getRec.Code)
	}
	var got store.TaskView
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode get body: %v", err)
	}
	if got.Task.ID != view.Task.ID || got.Task.IntakeBatch != "http-batch" {
		t.Fatalf("recovered task mismatch: %+v", got.Task)
	}
}

func TestLockRejectsStaleDigestOverHTTP(t *testing.T) {
	srv := NewServer(seedMemory(t))

	body := map[string]any{
		"operation_no": "op-http-stale",
		"plot_id":      "plot-picual-1",
		"cultivar_id":  "picual",
		"harvest_at":   "2026-10-01T00:00:00Z",
		"intake_batch": "stale-batch",
		"crate_seals":  []string{"stale-s1"},
		"blind_codes":  []string{"stale-b1"},
		"reviewer_ids": []string{"rev-a"},
		"rule_digest":  "deadbeef",
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp.Code != "ERR_STALE_RULE_DIGEST" {
		t.Fatalf("code = %s, want ERR_STALE_RULE_DIGEST", resp.Code)
	}
}

func TestCatalogPlotEndpoint(t *testing.T) {
	srv := NewServer(seedMemory(t))

	req := httptest.NewRequest(http.MethodGet, "/v1/catalog/plots/plot-picual-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var p catalog.Plot
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode plot: %v", err)
	}
	if p.CultivarID != "picual" {
		t.Fatalf("cultivar = %s, want picual", p.CultivarID)
	}
}
