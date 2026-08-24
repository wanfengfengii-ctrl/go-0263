package store

import (
	"context"
	"testing"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/task"
)

func TestModel_ColdPressRequiresUnanimousCurrentGenerationApprovals(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		batch         string
		reviews       []ReviewRequest
		wantReady     bool
		wantColdPress bool
	}{
		{
			name:  "two independent approvals allow cold press",
			batch: "model-unanimous-approvals",
			reviews: []ReviewRequest{
				{ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove},
				{ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove},
			},
			wantReady:     true,
			wantColdPress: true,
		},
		{
			name:  "secondary rejection blocks cold press",
			batch: "model-secondary-reject",
			reviews: []ReviewRequest{
				{ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionApprove},
				{ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionReject},
			},
		},
		{
			name:  "primary rejection blocks cold press",
			batch: "model-primary-reject",
			reviews: []ReviewRequest{
				{ReviewerID: "rev-a", Role: arbiter.RolePrimary, Decision: arbiter.DecisionReject},
				{ReviewerID: "rev-b", Role: arbiter.RoleSecondary, Decision: arbiter.DecisionApprove},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemory(t)
			defer s.Close()

			id := lockAndAdvanceToReview(t, s, ctx, tc.batch)

			var view *TaskView
			for i, req := range tc.reviews {
				req.OperationNo = tc.batch + "-review-" + string(rune('a'+i))
				var err error
				view, err = s.Review(ctx, id, req)
				if err != nil {
					t.Fatalf("review %d: %v", i+1, err)
				}
			}

			wantState := task.StatePendingIndependentReview
			if tc.wantReady {
				wantState = task.StateColdPressReady
			}
			if view.State != wantState {
				t.Fatalf("state after reviews = %s, want %s", view.State, wantState)
			}
			if len(view.Reviews) != len(tc.reviews) {
				t.Fatalf("recorded reviews = %d, want %d", len(view.Reviews), len(tc.reviews))
			}
			for _, want := range tc.reviews {
				found := false
				for _, got := range view.Reviews {
					if got.ReviewerID == want.ReviewerID && got.Role == want.Role && got.Decision == want.Decision {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing retained review %+v in %+v", want, view.Reviews)
				}
			}

			finalView, err := s.Finalize(ctx, id, FinalizeRequest{OperationNo: tc.batch + "-finalize", Kind: task.FinalColdPress})
			if tc.wantColdPress {
				if err != nil {
					t.Fatalf("finalize cold press: %v", err)
				}
				if finalView.Task.FinalKind != task.FinalColdPress || finalView.Final == nil || finalView.Final.Credential == "" {
					t.Fatalf("final cold press credential = %+v", finalView.Final)
				}
				return
			}
			if err == nil {
				t.Fatalf("cold press finalized despite a reject vote: %+v", finalView.Final)
			}

			recovered, getErr := s.GetTaskView(ctx, id)
			if getErr != nil {
				t.Fatalf("recover task after rejected finalize: %v", getErr)
			}
			if recovered.State == task.StateColdPressed || recovered.Task.FinalKind == task.FinalColdPress || recovered.Final != nil {
				t.Fatalf("reject vote produced cold press terminal state: state=%s final=%+v", recovered.State, recovered.Final)
			}
			if len(recovered.Reviews) != len(tc.reviews) {
				t.Fatalf("recovered reviews = %d, want %d", len(recovered.Reviews), len(tc.reviews))
			}
		})
	}
}
