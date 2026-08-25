package store

import (
	"context"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// Store is the persistence boundary for the OlivePress runtime. Every
// mutating operation runs in a single SQLite transaction, records idempotency
// before commit, and rebuilds open occupancy and final barriers from persisted
// rows only after restart.
type Store interface {
	Ping(ctx context.Context) error
	Close() error

	// Catalogue management.
	PutPlot(ctx context.Context, p catalog.Plot) error
	PutRule(ctx context.Context, r catalog.Rule) error
	GetPlot(ctx context.Context, id catalog.PlotID) (catalog.Plot, error)
	GetRule(ctx context.Context, d catalog.RuleDigest) (catalog.Rule, error)

	// Task lifecycle operations (each is one transaction).
	LockTask(ctx context.Context, req LockRequest) (*TaskView, error)
	SampleConfirm(ctx context.Context, id task.TaskID, req SampleConfirmRequest) (*TaskView, error)
	SplitSamples(ctx context.Context, id task.TaskID, req SplitSamplesRequest) (*TaskView, error)
	RevealSamples(ctx context.Context, id task.TaskID, req RevealRequest) (*TaskView, error)
	StartResources(ctx context.Context, id task.TaskID, req StartResourcesRequest) (*TaskView, error)
	MaturityCounts(ctx context.Context, id task.TaskID, req MaturityCountsRequest) (*TaskView, error)
	SubmitReadings(ctx context.Context, id task.TaskID, req ReadingsRequest) (*TaskView, error)
	ForeignMatter(ctx context.Context, id task.TaskID, req ForeignMatterRequest) (*TaskView, error)
	Rejudge(ctx context.Context, id task.TaskID, req RejudgeRequest) (*TaskView, error)
	Review(ctx context.Context, id task.TaskID, req ReviewRequest) (*TaskView, error)
	Finalize(ctx context.Context, id task.TaskID, req FinalizeRequest) (*TaskView, error)

	// GetTaskView returns the fully recovered state of a task.
	GetTaskView(ctx context.Context, id task.TaskID) (*TaskView, error)
}
