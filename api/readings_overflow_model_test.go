package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/olivepress/fruit-intake-gate/api"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_ReadingsFixedPointOverflow(t *testing.T) {
	ctx := context.Background()

	seed := func(t *testing.T, s store.Store) catalog.Rule {
		t.Helper()
		c := catalog.SeedCatalog()
		var rule catalog.Rule
		for _, r := range c.Rules() {
			rule = r
			if err := s.PutRule(ctx, r); err != nil {
				t.Fatalf("seed rule: %v", err)
			}
		}
		for _, p := range c.Plots() {
			if err := s.PutPlot(ctx, p); err != nil {
				t.Fatalf("seed plot: %v", err)
			}
		}
		if rule.Digest == "" {
			t.Fatal("seed catalog did not provide a rule")
		}
		return rule
	}

	prepareOxidationTask := func(t *testing.T, s *store.Memory, batch string, rule catalog.Rule) task.TaskID {
		t.Helper()
		locked, err := s.LockTask(ctx, store.LockRequest{
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
			t.Fatalf("lock: %v", err)
		}
		id := locked.Task.ID
		if _, err := s.SampleConfirm(ctx, id, store.SampleConfirmRequest{OperationNo: "op-sample-" + batch, ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
			t.Fatalf("sample confirm: %v", err)
		}
		if _, err := s.SplitSamples(ctx, id, store.SplitSamplesRequest{OperationNo: "op-split-" + batch}); err != nil {
			t.Fatalf("split samples: %v", err)
		}
		if _, err := s.StartResources(ctx, id, store.StartResourcesRequest{OperationNo: "op-resources-" + batch}); err != nil {
			t.Fatalf("start resources: %v", err)
		}
		_, err = s.MaturityCounts(ctx, id, store.MaturityCountsRequest{
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
		})
		if err != nil {
			t.Fatalf("maturity counts: %v", err)
		}
		return id
	}

	postReadings := func(t *testing.T, h http.Handler, id task.TaskID, req store.ReadingsRequest) (int, []byte) {
		t.Helper()
		body, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal readings: %v", err)
		}
		rec := httptest.NewRecorder()
		httpReq := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+string(id)+"/readings", bytes.NewReader(body))
		h.ServeHTTP(rec, httpReq)
		return rec.Code, rec.Body.Bytes()
	}

	baseReadings := func(operationNo string) store.ReadingsRequest {
		return store.ReadingsRequest{
			OperationNo: operationNo,
			Acid:        "0.42",
			Peroxide:    "12.50",
			Polyphenol:  "220",
			Moisture:    "32.5",
			FruitTemp:   "21.0",
		}
	}

	cases := []struct {
		name             string
		slug             string
		mutate           func(*store.ReadingsRequest)
		wantStatus       int
		wantCode         string
		wantState        task.State
		wantReadings     int
		wantReasons      []string
		wantAcidValue    int64
		retryAfterReject bool
	}{
		{
			name: "overflow acid rejected without side effects",
			slug: "acid-overflow",
			mutate: func(req *store.ReadingsRequest) {
				req.Acid = "999999999999999999.99"
			},
			wantStatus:       http.StatusBadRequest,
			wantCode:         store.CodeFixedPointOverflow,
			wantState:        task.StateOxidationVerifying,
			wantReadings:     0,
			retryAfterReject: true,
		},
		{
			name: "overflow peroxide rejected without partial readings",
			slug: "peroxide-overflow",
			mutate: func(req *store.ReadingsRequest) {
				req.Peroxide = "999999999999999999.99"
			},
			wantStatus:       http.StatusBadRequest,
			wantCode:         store.CodeFixedPointOverflow,
			wantState:        task.StateOxidationVerifying,
			wantReadings:     0,
			retryAfterReject: true,
		},
		{
			name: "legal readings advance",
			slug: "legal",
			mutate: func(req *store.ReadingsRequest) {
			},
			wantStatus:    http.StatusOK,
			wantState:     task.StateForeignMatterRetesting,
			wantReadings:  5,
			wantAcidValue: 42,
		},
		{
			name: "representable threshold breach records out of range",
			slug: "acid-threshold",
			mutate: func(req *store.ReadingsRequest) {
				req.Acid = "0.81"
			},
			wantStatus:    http.StatusOK,
			wantState:     task.StateForeignMatterRetesting,
			wantReadings:  5,
			wantReasons:   []string{"out-of-range"},
			wantAcidValue: 81,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := store.NewMemory()
			defer s.Close()
			rule := seed(t, s)
			id := prepareOxidationTask(t, s, "batch-model-"+tc.slug, rule)
			srv := api.NewServer(s)

			req := baseReadings("op-read-" + tc.slug)
			tc.mutate(&req)
			status, body := postReadings(t, srv.Handler(), id, req)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", status, tc.wantStatus, string(body))
			}

			if tc.wantCode != "" {
				var got api.ErrorResponse
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if got.Code != tc.wantCode {
					t.Fatalf("code = %s, want %s", got.Code, tc.wantCode)
				}
			} else {
				var got store.TaskView
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode success response: %v", err)
				}
				if got.State != tc.wantState {
					t.Fatalf("response state = %s, want %s", got.State, tc.wantState)
				}
			}

			view, err := s.GetTaskView(ctx, id)
			if err != nil {
				t.Fatalf("get task view: %v", err)
			}
			if view.State != tc.wantState {
				t.Fatalf("stored state = %s, want %s", view.State, tc.wantState)
			}
			if len(view.Readings) != tc.wantReadings {
				t.Fatalf("stored readings = %d, want %d: %+v", len(view.Readings), tc.wantReadings, view.Readings)
			}
			if !reflect.DeepEqual(view.Reasons, tc.wantReasons) {
				t.Fatalf("reasons = %v, want %v", view.Reasons, tc.wantReasons)
			}
			for _, reading := range view.Readings {
				if reading.Kind == "acid" && reading.Value.Value != tc.wantAcidValue {
					t.Fatalf("acid value = %d, want %d", reading.Value.Value, tc.wantAcidValue)
				}
			}

			if tc.retryAfterReject {
				retry := baseReadings(req.OperationNo)
				status, body = postReadings(t, srv.Handler(), id, retry)
				if status != http.StatusOK {
					t.Fatalf("retry with same operation number status = %d, want 200; body=%s", status, string(body))
				}
				var retried store.TaskView
				if err := json.Unmarshal(body, &retried); err != nil {
					t.Fatalf("decode retry response: %v", err)
				}
				if retried.State != task.StateForeignMatterRetesting || len(retried.Readings) != 5 {
					t.Fatalf("retry view state/readings = %s/%d, want foreign-matter-retesting/5", retried.State, len(retried.Readings))
				}
			}
		})
	}
}
