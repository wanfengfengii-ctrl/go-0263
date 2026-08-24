package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
)

func TestModel_RevealRejectsConflictingCrateRebind(t *testing.T) {
	ctx := context.Background()

	postJSON := func(t *testing.T, srv *Server, path string, payload any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}

	getTask := func(t *testing.T, srv *Server, taskID string) store.TaskView {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get task status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var view store.TaskView
		if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
			t.Fatalf("decode task view: %v", err)
		}
		return view
	}

	blindCrate := func(t *testing.T, view store.TaskView, blindCode string) string {
		t.Helper()
		for _, sample := range view.BlindSamples {
			if sample.BlindCode == blindCode {
				return sample.RevealedCrateSeal
			}
		}
		t.Fatalf("blind code %q not found in view", blindCode)
		return ""
	}

	newRevealedTask := func(t *testing.T, batch string) (*Server, string) {
		t.Helper()
		st := store.NewMemory()
		t.Cleanup(func() { _ = st.Close() })
		c := catalog.SeedCatalog()
		var rule catalog.Rule
		for _, p := range c.Plots() {
			if err := st.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		for _, r := range c.Rules() {
			rule = r
			if err := st.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
		}

		srv := NewServer(st)
		lock := store.LockRequest{
			OperationNo: "op-lock-" + batch,
			PlotID:      "plot-picual-1",
			CultivarID:  "picual",
			HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			IntakeBatch: batch,
			CrateSeals:  []string{batch + "-s1", batch + "-s2"},
			BlindCodes:  []string{batch + "-b1", batch + "-b2"},
			Thresholds:  rule.Thresholds,
			ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
			RuleDigest:  rule.Digest,
		}
		status, body := postJSON(t, srv, "/v1/tasks/lock", lock)
		if status != http.StatusCreated {
			t.Fatalf("lock status = %d, want 201; body=%s", status, string(body))
		}
		var locked store.TaskView
		if err := json.Unmarshal(body, &locked); err != nil {
			t.Fatalf("decode lock response: %v", err)
		}
		taskID := string(locked.Task.ID)

		confirm := store.SampleConfirmRequest{
			OperationNo: "op-confirm-" + batch,
			ReviewerA:   "rev-a",
			ReviewerB:   "rev-b",
		}
		status, body = postJSON(t, srv, "/v1/tasks/"+taskID+"/sample-confirm", confirm)
		if status != http.StatusOK {
			t.Fatalf("sample-confirm status = %d, want 200; body=%s", status, string(body))
		}

		firstReveal := store.RevealRequest{
			OperationNo: "op-reveal-first",
			Reveals: []store.RevealMapping{{
				BlindCode: batch + "-b1",
				CrateSeal: batch + "-s1",
			}},
		}
		status, body = postJSON(t, srv, "/v1/tasks/"+taskID+"/reveal", firstReveal)
		if status != http.StatusOK {
			t.Fatalf("first reveal status = %d, want 200; body=%s", status, string(body))
		}
		var revealed store.TaskView
		if err := json.Unmarshal(body, &revealed); err != nil {
			t.Fatalf("decode first reveal response: %v", err)
		}
		if got, want := blindCrate(t, revealed, batch+"-b1"), batch+"-s1"; got != want {
			t.Fatalf("first reveal mapped %q, want %q", got, want)
		}
		return srv, taskID
	}

	cases := []struct {
		name       string
		request    func(batch string) store.RevealRequest
		wantStatus int
		wantCode   string
		attempts   int
	}{
		{
			name: "same operation replay returns original reveal",
			request: func(batch string) store.RevealRequest {
				return store.RevealRequest{
					OperationNo: "op-reveal-first",
					Reveals: []store.RevealMapping{{
						BlindCode: batch + "-b1",
						CrateSeal: batch + "-s1",
					}},
				}
			},
			wantStatus: http.StatusOK,
			attempts:   1,
		},
		{
			name: "different crate rebind is rejected every time",
			request: func(batch string) store.RevealRequest {
				return store.RevealRequest{
					OperationNo: "op-rebind",
					Reveals: []store.RevealMapping{{
						BlindCode: batch + "-b1",
						CrateSeal: batch + "-s2",
					}},
				}
			},
			wantStatus: http.StatusConflict,
			wantCode:   store.CodeBlindAlreadyRevealed,
			attempts:   2,
		},
		{
			name: "unknown blind remains rejected",
			request: func(batch string) store.RevealRequest {
				return store.RevealRequest{
					OperationNo: "op-unknown-blind",
					Reveals: []store.RevealMapping{{
						BlindCode: batch + "-missing-blind",
						CrateSeal: batch + "-s1",
					}},
				}
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   store.CodeInvalidRequest,
			attempts:   1,
		},
		{
			name: "unknown crate remains rejected",
			request: func(batch string) store.RevealRequest {
				return store.RevealRequest{
					OperationNo: "op-unknown-crate",
					Reveals: []store.RevealMapping{{
						BlindCode: batch + "-b2",
						CrateSeal: batch + "-missing-crate",
					}},
				}
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   store.CodeInvalidRequest,
			attempts:   1,
		},
		{
			name: "generation mismatch remains rejected",
			request: func(batch string) store.RevealRequest {
				return store.RevealRequest{
					OperationNo: "op-wrong-generation",
					Generation:  2,
					Reveals: []store.RevealMapping{{
						BlindCode: batch + "-b2",
						CrateSeal: batch + "-s2",
					}},
				}
			},
			wantStatus: http.StatusConflict,
			wantCode:   store.CodeGenerationMismatch,
			attempts:   1,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := fmt.Sprintf("model-reveal-%d", i)
			srv, taskID := newRevealedTask(t, batch)
			req := tc.request(batch)
			path := "/v1/tasks/" + taskID + "/reveal"
			for attempt := 0; attempt < tc.attempts; attempt++ {
				status, body := postJSON(t, srv, path, req)
				if status != tc.wantStatus {
					t.Fatalf("attempt %d status = %d, want %d; body=%s", attempt+1, status, tc.wantStatus, string(body))
				}
				if tc.wantCode == "" {
					var view store.TaskView
					if err := json.Unmarshal(body, &view); err != nil {
						t.Fatalf("attempt %d decode success response: %v", attempt+1, err)
					}
					if got, want := blindCrate(t, view, batch+"-b1"), batch+"-s1"; got != want {
						t.Fatalf("attempt %d mapped b1 to %q, want %q", attempt+1, got, want)
					}
				} else {
					var resp ErrorResponse
					if err := json.Unmarshal(body, &resp); err != nil {
						t.Fatalf("attempt %d decode error response: %v", attempt+1, err)
					}
					if resp.Code != tc.wantCode {
						t.Fatalf("attempt %d code = %s, want %s", attempt+1, resp.Code, tc.wantCode)
					}
				}
				view := getTask(t, srv, taskID)
				if got, want := blindCrate(t, view, batch+"-b1"), batch+"-s1"; got != want {
					t.Fatalf("attempt %d persisted b1 mapping = %q, want %q", attempt+1, got, want)
				}
				if got := blindCrate(t, view, batch+"-b2"); got != "" {
					t.Fatalf("attempt %d unexpectedly revealed b2 to %q", attempt+1, got)
				}
			}
		})
	}
}
