package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
)

func TestModel_LockTaskCanceledRequestLeavesNoOpenArtifacts(t *testing.T) {
	srv := NewServer(seedMemory(t))

	var rule catalog.Rule
	for _, r := range catalog.SeedCatalog().Rules() {
		rule = r
		break
	}

	const batch = "picual-cancelled-client-timeout"
	bodyFor := func(operationNo string) []byte {
		body := map[string]any{
			"operation_no": operationNo,
			"plot_id":      "plot-picual-1",
			"cultivar_id":  "picual",
			"harvest_at":   "2026-10-01T00:00:00Z",
			"intake_batch": batch,
			"crate_seals":  []string{batch + "-seal-1", batch + "-seal-2"},
			"blind_codes":  []string{batch + "-blind-1", batch + "-blind-2"},
			"thresholds":   rule.Thresholds,
			"reviewer_ids": []string{"rev-a", "rev-b"},
			"rule_digest":  string(rule.Digest),
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal lock body: %v", err)
		}
		return raw
	}

	canceledContext := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	expiredContext := func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)
		return ctx
	}

	cases := []struct {
		name        string
		ctx         context.Context
		operationNo string
		wantStatus  int
		wantCode    string
		wantTask    bool
	}{
		{
			name:        "canceled request does not create a lock",
			ctx:         canceledContext(),
			operationNo: "op-timeout-canceled",
			wantStatus:  http.StatusBadRequest,
			wantCode:    store.CodeInvalidRequest,
		},
		{
			name:        "expired request does not create a lock",
			ctx:         expiredContext(),
			operationNo: "op-deadline-expired",
			wantStatus:  http.StatusBadRequest,
			wantCode:    store.CodeInvalidRequest,
		},
		{
			name:        "fresh operation locks same intake after cancellation",
			ctx:         context.Background(),
			operationNo: "op-after-timeout",
			wantStatus:  http.StatusCreated,
			wantTask:    true,
		},
		{
			name:        "successful open task still blocks the canceled operation number",
			ctx:         context.Background(),
			operationNo: "op-timeout-canceled",
			wantStatus:  http.StatusConflict,
			wantCode:    store.CodeIntakeBatchDuplicate,
		},
		{
			name:        "successful open task blocks a new operation number",
			ctx:         context.Background(),
			operationNo: "op-duplicate-after-success",
			wantStatus:  http.StatusConflict,
			wantCode:    store.CodeIntakeBatchDuplicate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", bytes.NewReader(bodyFor(tc.operationNo))).WithContext(tc.ctx)
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantTask {
				var view store.TaskView
				if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
					t.Fatalf("decode task view: %v", err)
				}
				if view.Task.ID == "" || view.Task.IntakeBatch != batch {
					t.Fatalf("unexpected task view: %+v", view.Task)
				}
				return
			}

			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if resp.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s; body=%s", resp.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}
