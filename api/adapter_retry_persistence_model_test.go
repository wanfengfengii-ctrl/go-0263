package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_AdapterRetryFailuresPersistAcrossTaskViews(t *testing.T) {
	cases := []struct {
		name    string
		outcome evidence.AdapterOutcome
		target  string
		attempt int
	}{
		{name: "reject", outcome: evidence.OutcomeReject, target: "nir-reject-target", attempt: 1},
		{name: "disconnect", outcome: evidence.OutcomeDisconnect, target: "nir-disconnect-target", attempt: 2},
		{name: "timeout", outcome: evidence.OutcomeTimeout, target: "nir-timeout-target", attempt: 3},
		{name: "malformed", outcome: evidence.OutcomeMalformed, target: "nir-malformed-target", attempt: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "olivepress.db")
			st, err := store.NewSQLite(dbPath)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			rule := seedModelCatalog(t, st)
			batch := "model-" + tc.name
			id := advanceModelTaskToOxidation(t, ctx, st, rule, batch)
			srv := NewServer(st)

			postBody, err := json.Marshal(store.ReadingsRequest{
				OperationNo: "op-read-" + tc.name,
				Acid:        "0.42",
				Peroxide:    "12.50",
				Polyphenol:  "220",
				Moisture:    "32.5",
				FruitTemp:   "21.0",
				Adapters: []store.AdapterInput{{
					Reading: evidence.ReadingPolyphenol,
					Kind:    evidence.AdapterNearInfrared,
					Target:  tc.target,
					Attempt: tc.attempt,
					Outcome: tc.outcome,
				}},
			})
			if err != nil {
				t.Fatalf("marshal readings: %v", err)
			}
			postReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+string(id)+"/readings", bytes.NewReader(postBody))
			postRec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(postRec, postReq)
			if postRec.Code != http.StatusConflict {
				t.Fatalf("readings status = %d, want 409; body=%s", postRec.Code, postRec.Body.String())
			}
			var errBody ErrorResponse
			if err := json.NewDecoder(postRec.Body).Decode(&errBody); err != nil {
				t.Fatalf("decode retry error: %v", err)
			}
			if errBody.Code != store.CodeAdapterRetryPending {
				t.Fatalf("error code = %s, want %s", errBody.Code, store.CodeAdapterRetryPending)
			}

			got := getModelTaskOverHTTP(t, srv, id)
			assertModelRetryView(t, got, tc.target, tc.attempt, tc.outcome)

			if err := st.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}
			reopened, err := store.NewSQLite(dbPath)
			if err != nil {
				t.Fatalf("reopen sqlite: %v", err)
			}
			defer reopened.Close()
			recovered := getModelTaskOverHTTP(t, NewServer(reopened), id)
			assertModelRetryView(t, recovered, tc.target, tc.attempt, tc.outcome)
		})
	}
}

func seedModelCatalog(t *testing.T, st *store.SQLite) catalog.Rule {
	t.Helper()
	c := catalog.SeedCatalog()
	ctx := context.Background()
	for _, p := range c.Plots() {
		if err := st.PutPlot(ctx, p); err != nil {
			t.Fatalf("seed plot: %v", err)
		}
	}
	var rule catalog.Rule
	for _, r := range c.Rules() {
		rule = r
		if err := st.PutRule(ctx, r); err != nil {
			t.Fatalf("seed rule: %v", err)
		}
	}
	return rule
}

func advanceModelTaskToOxidation(t *testing.T, ctx context.Context, st *store.SQLite, rule catalog.Rule, batch string) task.TaskID {
	t.Helper()
	view, err := st.LockTask(ctx, store.LockRequest{
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
	})
	if err != nil {
		t.Fatalf("lock task: %v", err)
	}
	id := view.Task.ID
	if _, err := st.SampleConfirm(ctx, id, store.SampleConfirmRequest{OperationNo: "op-sample-" + batch, ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
		t.Fatalf("sample confirm: %v", err)
	}
	if _, err := st.SplitSamples(ctx, id, store.SplitSamplesRequest{OperationNo: "op-split-" + batch}); err != nil {
		t.Fatalf("split samples: %v", err)
	}
	if _, err := st.StartResources(ctx, id, store.StartResourcesRequest{OperationNo: "op-resources-" + batch}); err != nil {
		t.Fatalf("start resources: %v", err)
	}
	if _, err := st.MaturityCounts(ctx, id, store.MaturityCountsRequest{
		OperationNo: "op-maturity-" + batch,
		Total:       map[string]int{batch + "-s1": 100, batch + "-s2": 100},
		Cells: []store.MaturityCellInput{
			{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorGreen, Count: 60},
			{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorTurning, Count: 20},
			{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorPurpleBlack, Count: 15},
			{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorDamaged, Count: 4},
			{CrateSeal: batch + "-s1", ColorGrade: catalog.ColorMoldy, Count: 1},
			{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorGreen, Count: 65},
			{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorTurning, Count: 18},
			{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorPurpleBlack, Count: 12},
			{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorDamaged, Count: 3},
			{CrateSeal: batch + "-s2", ColorGrade: catalog.ColorMoldy, Count: 2},
		},
	}); err != nil {
		t.Fatalf("maturity counts: %v", err)
	}
	return id
}

func getModelTaskOverHTTP(t *testing.T, srv *Server, id task.TaskID) store.TaskView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+string(id), nil)
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

func assertModelRetryView(t *testing.T, view store.TaskView, target string, attempt int, outcome evidence.AdapterOutcome) {
	t.Helper()
	if view.State != task.StateOxidationVerifying {
		t.Fatalf("state = %s, want %s", view.State, task.StateOxidationVerifying)
	}
	if len(view.Readings) != 0 {
		t.Fatalf("readings length = %d, want 0: %+v", len(view.Readings), view.Readings)
	}
	if len(view.Leases) == 0 {
		t.Fatal("expected active leases to remain visible")
	}
	for _, lease := range view.Leases {
		if lease.Released {
			t.Fatalf("lease was released after adapter retry: %+v", lease)
		}
	}
	if len(view.Retries) != 1 {
		t.Fatalf("adapter_retries length = %d, want 1: %+v", len(view.Retries), view.Retries)
	}
	retry := view.Retries[0]
	if retry.AdapterKind != evidence.AdapterNearInfrared {
		t.Fatalf("adapter kind = %s, want %s", retry.AdapterKind, evidence.AdapterNearInfrared)
	}
	if retry.TargetKey != target {
		t.Fatalf("retry target = %s, want %s", retry.TargetKey, target)
	}
	if retry.AttemptNo != attempt {
		t.Fatalf("retry attempt = %d, want %d", retry.AttemptNo, attempt)
	}
	if retry.Outcome != outcome {
		t.Fatalf("retry outcome = %s, want %s", retry.Outcome, outcome)
	}
	if retry.PlannedTick != 6+evidence.RetryBackoffTicks {
		t.Fatalf("retry planned tick = %d, want %d", retry.PlannedTick, 6+evidence.RetryBackoffTicks)
	}
}
