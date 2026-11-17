package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Writers waiting for the custody lock hold every connection in the shared pool.
// A board read through the shared pool then misses its deadline; through
// ReadPool it is answered at once.
func TestReadPoolAnswersWhileWritersHoldTheSharedPool(t *testing.T) {
	st := postgresTestStore(t)
	ctx := context.Background()
	holder, err := st.ReadPool(1)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	lock, err := holder.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(734031823)`); err != nil {
		t.Fatal(err)
	}

	writers := st.DB().Stats().MaxOpenConnections
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.WithTx(ctx, func(Querier) error { return nil })
		}()
	}
	defer func() {
		_ = lock.Rollback()
		wg.Wait()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for st.DB().Stats().InUse < writers {
		if time.Now().After(deadline) {
			t.Fatalf("writers holding connections = %d, want %d", st.DB().Stats().InUse, writers)
		}
		time.Sleep(10 * time.Millisecond)
	}

	shared, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, err = st.ListProjects(shared)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared pool read = %v, want it to wait out its deadline (the stale board)", err)
	}

	board, err := st.ReadPool(4)
	if err != nil {
		t.Fatal(err)
	}
	defer board.Close()
	read, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := board.ListProjects(read); err != nil {
		t.Fatalf("read pool read = %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("read pool read took %s", took)
	}
}
