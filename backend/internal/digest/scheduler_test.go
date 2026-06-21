package digest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/digest"
)

// stubCycleRunner is a minimal CycleRunner for scheduler tests. It records
// the timestamps of each Run call so we can assert on fire count + cadence.
type stubCycleRunner struct {
	mu        sync.Mutex
	fireTimes []time.Time
}

func (s *stubCycleRunner) Run(_ context.Context, _, _ time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fireTimes = append(s.fireTimes, time.Now())
	return "", nil
}

func (s *stubCycleRunner) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.fireTimes)
}

// TestScheduler_SetInterval_AppliesOnNextFire verifies that the live-reload
// behavior of SetInterval works correctly: while the scheduler is running
// with one interval, calling SetInterval with a different interval must
// cause subsequent cycles to fire at the new cadence. (SC-005, SC-010 —
// interval change is observable without restart.)
func TestScheduler_SetInterval_AppliesOnNextFire(t *testing.T) {
	stub := &stubCycleRunner{}
	// Start with a short interval so we can observe cadence without
	// waiting an hour. The first cycle fires immediately per Scheduler.Run.
	sch := digest.NewScheduler(stub, 100*time.Millisecond, nil, nil)
	sch.SetFirstTickDelay(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() { doneCh <- sch.Run(ctx) }()

	// Give the first fire (immediate) a moment to complete.
	time.Sleep(30 * time.Millisecond)
	if got := stub.Count(); got != 1 {
		t.Fatalf("expected 1 fire after Run, got %d", got)
	}

	// Now reload to a different (longer) interval. Subsequent fires
	// should respect the new cadence.
	sch.SetInterval(300 * time.Millisecond)

	// Wait for at least 3 fires under the new cadence.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if stub.Count() >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-doneCh

	if got := stub.Count(); got < 3 {
		t.Fatalf("expected at least 3 fires after SetInterval(300ms), got %d", got)
	}

	// Verify cadence: gaps between fires 2+ should be ~300ms (new
	// interval), not 100ms (old). Allow generous bound for jitter.
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.fireTimes) < 3 {
		t.Fatalf("need at least 3 fire timestamps, have %d", len(stub.fireTimes))
	}
	for i := 2; i < len(stub.fireTimes); i++ {
		gap := stub.fireTimes[i].Sub(stub.fireTimes[i-1])
		if gap < 200*time.Millisecond {
			t.Errorf("gap between fire %d and %d was %v, expected near 300ms", i-1, i, gap)
		}
	}
}

// TestScheduler_SetInterval_DoesNotPanicIfConcurrent verifies the lock-free
// concurrency contract: SetInterval may be called from any goroutine
// without racing with the Run loop. SC-009: admin panel can change the
// interval at any time without taking the service down.
func TestScheduler_SetInterval_DoesNotPanicIfConcurrent(t *testing.T) {
	stub := &stubCycleRunner{}
	sch := digest.NewScheduler(stub, 200*time.Millisecond, nil, nil)
	sch.SetFirstTickDelay(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() { doneCh <- sch.Run(ctx) }()

	// Hammer SetInterval from another goroutine for 300ms.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				sch.SetInterval(100 * time.Millisecond)
				sch.SetInterval(500 * time.Millisecond)
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	<-done
	cancel()
	<-doneCh

	// No assertion on count — just that no panic occurred and the scheduler
	// shut down cleanly.
}
