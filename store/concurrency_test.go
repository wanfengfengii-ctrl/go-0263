package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/olivepress/fruit-intake-gate/task"
)

func newFileStore(t *testing.T) (*SQLite, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olivepress.db")
	s, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seedTestCatalog(t, s)
	return s, path
}

func TestConcurrentLockDuplicateSeal(t *testing.T) {
	ctx := context.Background()
	s, path := newFileStore(t)
	defer s.Close()

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := validLock("race-" + string(rune('a'+i)))
			req.CrateSeals = []string{"shared-seal", "race-" + string(rune('a'+i)) + "-uniq"}
			_, errs[i] = s.LockTask(ctx, req)
		}(i)
	}
	close(start)
	wg.Wait()

	winners, losers := 0, 0
	for _, err := range errs {
		if err == nil {
			winners++
		} else if codeOf(t, err) == CodeDuplicateSeal {
			losers++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
	if losers != n-1 {
		t.Fatalf("losers = %d, want %d", losers, n-1)
	}

	// After restart the winning occupancy persists: a new lock with the shared
	// seal is deterministically rejected.
	s.Close()
	s2, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	again := validLock("race-later")
	again.CrateSeals = []string{"shared-seal", "race-later-uniq"}
	if _, err := s2.LockTask(ctx, again); codeOf(t, err) != CodeDuplicateSeal {
		t.Fatalf("after restart want ERR_DUPLICATE_SEAL, got %v", err)
	}
}

func TestConcurrentStartResources(t *testing.T) {
	ctx := context.Background()
	s, path := newFileStore(t)
	defer s.Close()

	// Two tasks with distinct seals but the same frozen resources.
	var ids [2]task.TaskID
	for i := range ids {
		v, err := s.LockTask(ctx, validLock("res-batch-"+string(rune('a'+i))))
		if err != nil {
			t.Fatalf("lock %d: %v", i, err)
		}
		ids[i] = v.Task.ID
		if _, err := s.SampleConfirm(ctx, ids[i], SampleConfirmRequest{OperationNo: "op-sc-" + string(rune('a'+i)), ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
			t.Fatalf("confirm %d: %v", i, err)
		}
		if _, err := s.SplitSamples(ctx, ids[i], SplitSamplesRequest{OperationNo: "op-split-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("split %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = s.StartResources(ctx, ids[i], StartResourcesRequest{OperationNo: "op-res-" + string(rune('a'+i))})
		}(i)
	}
	close(start)
	wg.Wait()

	winners, losers := 0, 0
	for _, err := range errs {
		if err == nil {
			winners++
		} else if codeOf(t, err) == CodeResourceBusy {
			losers++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners=%d losers=%d, want 1/1", winners, losers)
	}

	// After restart the resource occupancy is rebuilt from persisted rows.
	s.Close()
	s2, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	later, err := s2.LockTask(ctx, validLock("res-later"))
	if err != nil {
		t.Fatalf("lock later: %v", err)
	}
	if _, err := s2.SampleConfirm(ctx, later.Task.ID, SampleConfirmRequest{OperationNo: "op-sc-later", ReviewerA: "rev-a", ReviewerB: "rev-b"}); err != nil {
		t.Fatalf("confirm later: %v", err)
	}
	if _, err := s2.SplitSamples(ctx, later.Task.ID, SplitSamplesRequest{OperationNo: "op-split-later"}); err != nil {
		t.Fatalf("split later: %v", err)
	}
	if _, err := s2.StartResources(ctx, later.Task.ID, StartResourcesRequest{OperationNo: "op-res-later"}); codeOf(t, err) != CodeResourceBusy {
		t.Fatalf("after restart want ERR_RESOURCE_BUSY, got %v", err)
	}
}
