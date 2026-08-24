package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/api"
	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_RejudgeRecheckEvidenceRecoveredAcrossResponses(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "olivepress.db")
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedCatalog(t, ctx, st)

	handler := api.NewServer(st).Handler()
	batch := "model-recheck"
	taskID := createTaskAtIndependentReview(t, handler, batch)

	affected := store.ForeignRefs{
		CrateSeals: []string{batch + "-s2", batch + "-s1"},
		BlindCodes: []string{batch + "-b2", batch + "-b1"},
		TestHoles:  []string{"th-2", "th-1"},
	}
	rejudgeView := postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/rejudge", store.RejudgeRequest{
		OperationNo: "op-model-rejudge",
		Generation:  1,
		Reason:      arbiter.RejudgeOxidation,
		Affected:    affected,
	}, http.StatusOK)
	getView := getTaskView(t, handler, taskID)

	if err := st.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	reopened, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedHandler := api.NewServer(reopened).Handler()
	recoveredView := getTaskView(t, reopenedHandler, taskID)

	wantRecheck := &store.RecheckView{
		Reason:     arbiter.RejudgeOxidation,
		CrateSeals: []string{batch + "-s1", batch + "-s2"},
		BlindCodes: []string{batch + "-b1", batch + "-b2"},
		TestHoles:  []string{"th-1", "th-2"},
	}
	cases := []struct {
		name string
		view *store.TaskView
	}{
		{name: "post rejudge response", view: rejudgeView},
		{name: "subsequent get response", view: getView},
		{name: "reopened get recovery", view: recoveredView},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.view.Generation != 2 || tc.view.Task.Generation != 2 {
				t.Fatalf("generation = view:%d task:%d, want 2", tc.view.Generation, tc.view.Task.Generation)
			}
			if tc.view.State != task.StatePendingIndependentReview {
				t.Fatalf("state = %s, want %s", tc.view.State, task.StatePendingIndependentReview)
			}
			if !reflect.DeepEqual(tc.view.Recheck, wantRecheck) {
				t.Fatalf("recheck = %#v, want %#v", tc.view.Recheck, wantRecheck)
			}
		})
	}
}

func seedCatalog(t *testing.T, ctx context.Context, st *store.SQLite) {
	t.Helper()
	c := catalog.SeedCatalog()
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
}

func createTaskAtIndependentReview(t *testing.T, handler http.Handler, batch string) task.TaskID {
	t.Helper()
	rule := firstSeedRule(t)
	lockView := postTaskView(t, handler, http.MethodPost, "/v1/tasks/lock", store.LockRequest{
		OperationNo: "op-" + batch + "-lock",
		PlotID:      "plot-picual-1",
		CultivarID:  "picual",
		HarvestAt:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		IntakeBatch: batch,
		CrateSeals:  []string{batch + "-s1", batch + "-s2"},
		BlindCodes:  []string{batch + "-b1", batch + "-b2"},
		Thresholds:  rule.Thresholds,
		ReviewerIDs: []catalog.ReviewerID{"rev-a", "rev-b"},
		RuleDigest:  rule.Digest,
	}, http.StatusCreated)
	taskID := lockView.Task.ID

	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/sample-confirm", store.SampleConfirmRequest{
		OperationNo: "op-" + batch + "-sample",
		ReviewerA:   "rev-a",
		ReviewerB:   "rev-b",
	}, http.StatusOK)
	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/split-samples", store.SplitSamplesRequest{
		OperationNo: "op-" + batch + "-split",
	}, http.StatusOK)
	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/start-resources", store.StartResourcesRequest{
		OperationNo: "op-" + batch + "-resources",
	}, http.StatusOK)
	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/maturity-counts", store.MaturityCountsRequest{
		OperationNo: "op-" + batch + "-maturity",
		Total: map[string]int{
			batch + "-s1": 100,
			batch + "-s2": 100,
		},
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
	}, http.StatusOK)
	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/readings", store.ReadingsRequest{
		OperationNo: "op-" + batch + "-readings",
		Acid:        "0.42",
		Peroxide:    "24.50",
		Polyphenol:  "220",
		Moisture:    "32.5",
		FruitTemp:   "21.0",
	}, http.StatusOK)
	postTaskView(t, handler, http.MethodPost, "/v1/tasks/"+string(taskID)+"/foreign-matter", store.ForeignMatterRequest{
		OperationNo: "op-" + batch + "-foreign",
		Finding:     "clear",
	}, http.StatusOK)

	return taskID
}

func firstSeedRule(t *testing.T) catalog.Rule {
	t.Helper()
	for _, r := range catalog.SeedCatalog().Rules() {
		return r
	}
	t.Fatal("seed catalog has no rules")
	return catalog.Rule{}
}

func postTaskView(t *testing.T, handler http.Handler, method, path string, body any, wantStatus int) *store.TaskView {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	var view store.TaskView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("decode %s response: %v", path, err)
	}
	return &view
}

func getTaskView(t *testing.T, handler http.Handler, id task.TaskID) *store.TaskView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+string(id), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET task status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var view store.TaskView
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("decode GET task response: %v", err)
	}
	return &view
}
