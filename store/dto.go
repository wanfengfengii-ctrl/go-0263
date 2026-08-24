package store

import (
	"time"

	"github.com/olivepress/fruit-intake-gate/arbiter"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/evidence"
	"github.com/olivepress/fruit-intake-gate/task"
)

// LockRequest is the input to POST /v1/tasks/lock. It freezes the plot,
// cultivar, harvest shift, crate seals, intake batch, blind codes, thresholds
// and reviewers under a rule digest. The color board, resources and screening
// points are frozen canonically from the referenced rule.
type LockRequest struct {
	OperationNo string               `json:"operation_no"`
	PlotID      catalog.PlotID       `json:"plot_id"`
	CultivarID  catalog.CultivarID   `json:"cultivar_id"`
	HarvestAt   time.Time            `json:"harvest_at"`
	IntakeBatch string               `json:"intake_batch"`
	CrateSeals  []string             `json:"crate_seals"`
	BlindCodes  []string             `json:"blind_codes"`
	Thresholds  catalog.Thresholds   `json:"thresholds"`
	ReviewerIDs []catalog.ReviewerID `json:"reviewer_ids"`
	RuleDigest  catalog.RuleDigest   `json:"rule_digest"`
}

// SampleConfirmRequest records two-person receiving confirmation.
type SampleConfirmRequest struct {
	OperationNo string `json:"operation_no"`
	ReviewerA   string `json:"reviewer_a"`
	ReviewerB   string `json:"reviewer_b"`
}

// SplitSamplesRequest creates blind-code split records without revealing the
// crate mapping.
type SplitSamplesRequest struct {
	OperationNo string `json:"operation_no"`
}

// RevealRequest maps blind codes to crate seals under the current generation.
type RevealRequest struct {
	OperationNo string          `json:"operation_no"`
	Generation  int64           `json:"generation,omitempty"`
	Reveals     []RevealMapping `json:"reveals"`
}

// RevealMapping is one blind-code to crate-seal reveal.
type RevealMapping struct {
	BlindCode string `json:"blind_code"`
	CrateSeal string `json:"crate_seal"`
}

// StartResourcesRequest atomically captures crusher-line, inert-window and
// test-hole leases.
type StartResourcesRequest struct {
	OperationNo string `json:"operation_no"`
}

// MaturityCountsRequest writes full maturity coverage cells.
type MaturityCountsRequest struct {
	OperationNo string              `json:"operation_no"`
	Generation  int64               `json:"generation,omitempty"`
	Total       map[string]int      `json:"total"` // crate seal -> sample grain total
	Cells       []MaturityCellInput `json:"cells"`
}

// MaturityCellInput is one coverage cell input.
type MaturityCellInput struct {
	CrateSeal  string             `json:"crate_seal"`
	ColorGrade catalog.ColorGrade `json:"color_grade"`
	Count      int                `json:"count"`
}

// ReadingsRequest submits fixed-point physicochemical readings.
type ReadingsRequest struct {
	OperationNo string `json:"operation_no"`
	Generation  int64  `json:"generation,omitempty"`
	Acid        string `json:"acid"`
	Peroxide    string `json:"peroxide"`
	Polyphenol  string `json:"polyphenol"`
	Moisture    string `json:"moisture"`
	FruitTemp   string `json:"fruit_temp"`
	// Adapters optionally routes a reading kind through a scripted instrument
	// adapter. A failed adapter outcome (reject/disconnect/timeout/malformed)
	// records a deterministic retry row and leaves that reading unaccepted.
	Adapters []AdapterInput `json:"adapters,omitempty"`
}

// AdapterInput describes one scripted instrument measurement for a reading
// kind. The outcome is fixed by the caller so tests and the smoke script stay
// deterministic.
type AdapterInput struct {
	Reading evidence.ReadingKind    `json:"reading"`
	Kind    evidence.AdapterKind    `json:"kind"`
	Target  string                  `json:"target"`
	Attempt int                     `json:"attempt"`
	Outcome evidence.AdapterOutcome `json:"outcome"`
}

// ForeignMatterRequest submits screening findings, moisture repeat checks and
// affected references.
type ForeignMatterRequest struct {
	OperationNo string      `json:"operation_no"`
	Generation  int64       `json:"generation,omitempty"`
	Finding     string      `json:"finding"`
	MoistureRpt string      `json:"moisture_repeat"`
	Affected    ForeignRefs `json:"affected"`
}

// ForeignRefs references the affected crate seals, blind codes and test holes.
type ForeignRefs struct {
	CrateSeals []string `json:"crate_seals"`
	BlindCodes []string `json:"blind_codes"`
	TestHoles  []string `json:"test_holes"`
}

// RejudgeRequest creates the current-generation deterioration recheck.
type RejudgeRequest struct {
	OperationNo string                `json:"operation_no"`
	Generation  int64                 `json:"generation,omitempty"`
	Reason      arbiter.RejudgeReason `json:"reason"`
	Affected    ForeignRefs           `json:"affected"`
}

// ReviewRequest records an independent review.
type ReviewRequest struct {
	OperationNo string                 `json:"operation_no"`
	ReviewerID  string                 `json:"reviewer_id"`
	Role        arbiter.ReviewRole     `json:"role"`
	Decision    arbiter.ReviewDecision `json:"decision"`
}

// FinalizeRequest competes for the single final outcome.
type FinalizeRequest struct {
	OperationNo string         `json:"operation_no"`
	Kind        task.FinalKind `json:"kind"`
}

// TaskView is the fully recovered state of a task: aggregate, snapshot,
// gates, blind samples, leases, coverage, readings, adapter retries, reviews
// and the final credential.
type TaskView struct {
	Task         task.Task          `json:"task"`
	Snapshot     task.Snapshot      `json:"snapshot"`
	State        task.State         `json:"state"`
	Generation   task.Generation    `json:"generation"`
	CrateGates   []CrateGateView    `json:"crate_gates"`
	BlindSamples []BlindSampleView  `json:"blind_samples"`
	Leases       []LeaseView        `json:"leases"`
	Maturity     []MaturityCellView `json:"maturity_cells"`
	Readings     []ReadingView      `json:"readings"`
	Retries      []AdapterCallView  `json:"adapter_retries"`
	Recheck      *RecheckView       `json:"recheck,omitempty"`
	Reviews      []ReviewView       `json:"reviews"`
	Final        *FinalView         `json:"final,omitempty"`
	Reasons      []string           `json:"reasons,omitempty"`
}

// CrateGateView is a recovered crate gate.
type CrateGateView struct {
	CrateSeal    string `json:"crate_seal"`
	ConfirmedByA string `json:"confirmed_by_a"`
	ConfirmedByB string `json:"confirmed_by_b"`
	Confirmed    bool   `json:"confirmed"`
}

// BlindSampleView is a recovered blind sample.
type BlindSampleView struct {
	BlindCode         string `json:"blind_code"`
	SplitIndex        int    `json:"split_index"`
	RevealedCrateSeal string `json:"revealed_crate_seal"`
}

// LeaseView is a recovered resource lease.
type LeaseView struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	StartTick    int64  `json:"start_tick"`
	ExpireTick   int64  `json:"expire_tick"`
	Released     bool   `json:"released"`
}

// MaturityCellView is a recovered coverage cell.
type MaturityCellView struct {
	CrateSeal  string             `json:"crate_seal"`
	ColorGrade catalog.ColorGrade `json:"color_grade"`
	Count      int                `json:"count"`
}

// ReadingView is a recovered fixed-point reading.
type ReadingView struct {
	Kind  string              `json:"kind"`
	Value evidence.FixedPoint `json:"value"`
}

// AdapterCallView is a recovered adapter retry record.
type AdapterCallView struct {
	AdapterKind evidence.AdapterKind    `json:"adapter_kind"`
	TargetKey   string                  `json:"target_key"`
	AttemptNo   int                     `json:"attempt_no"`
	Outcome     evidence.AdapterOutcome `json:"outcome"`
	PlannedTick int64                   `json:"planned_tick"`
}

// RecheckView is a recovered recheck evidence.
type RecheckView struct {
	Reason     arbiter.RejudgeReason `json:"reason"`
	CrateSeals []string              `json:"crate_seals"`
	BlindCodes []string              `json:"blind_codes"`
	TestHoles  []string              `json:"test_holes"`
}

// ReviewView is a recovered review.
type ReviewView struct {
	ReviewerID string                 `json:"reviewer_id"`
	Role       arbiter.ReviewRole     `json:"role"`
	Decision   arbiter.ReviewDecision `json:"decision"`
}

// FinalView is the recovered terminal credential.
type FinalView struct {
	Kind       task.FinalKind `json:"kind"`
	Credential string         `json:"credential"`
}
