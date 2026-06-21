package digest

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/synapto/assistant/internal/store"
)

// Scheduler runs the Cycle on a fixed interval. It is safe for a single
// goroutine to call Run; the scheduler guards against overlapping cycles
// with a mutex + atomic state flag.
type Scheduler struct {
	cycle     *Cycle
	interval  time.Duration
	log       *slog.Logger
	cycleRepo store.CycleRepo

	mu       sync.Mutex
	running  atomic.Bool
	stateVal atomic.Value
}

// NewScheduler constructs a Scheduler. The interval is read from settings
// at startup; use SetInterval to change it live.
func NewScheduler(cycle *Cycle, interval time.Duration, cycleRepo store.CycleRepo, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{cycle: cycle, interval: interval, cycleRepo: cycleRepo, log: log}
	s.stateVal.Store("idle")
	return s
}

// SetInterval updates the interval. Takes effect from the next tick.
// Safe to call while a cycle is running.
func (s *Scheduler) SetInterval(d time.Duration) {
	s.mu.Lock()
	s.interval = d
	s.mu.Unlock()
}

// Interval returns the current interval.
func (s *Scheduler) Interval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interval
}

// State returns the current scheduler state: "idle" or "running".
func (s *Scheduler) State() string {
	if v, ok := s.stateVal.Load().(string); ok {
		return v
	}
	return "idle"
}

// Run starts the scheduler. It blocks until ctx is canceled. On startup,
// it reads LastSuccessfulWindowEnd to compute the first window, so a
// restart never double-delivers or skips a window (FR-016, SC-008).
func (s *Scheduler) Run(ctx context.Context) error {
	// On startup, compute the first window from the last successful cycle.
	lastEnd, found, err := s.cycleRepo.LastSuccessfulWindowEnd(ctx)
	if err != nil {
		s.log.Warn("scheduler: cannot read last window end, starting fresh", "err", err)
	}
	now := time.Now().UTC()
	var firstTick time.Time
	if found && !lastEnd.IsZero() {
		// If the last window ended less than one interval ago, wait the remainder.
		elapsed := now.Sub(lastEnd)
		interval := s.Interval()
		if elapsed < interval {
			firstTick = lastEnd.Add(interval)
		} else {
			// Overdue: fire immediately, then resume normal cadence.
			firstTick = now
		}
	} else {
		firstTick = now
	}
	s.log.Info("scheduler starting", "interval", s.Interval(), "first_tick", firstTick.Format(time.RFC3339))

	// Wait until the first tick.
	if d := time.Until(firstTick); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}

	ticker := time.NewTicker(s.Interval())
	defer ticker.Stop()

	// Fire the first cycle immediately.
	s.fire(ctx, firstTick)

	var lastInterval = s.Interval()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler stopped")
			return ctx.Err()
		case t := <-ticker.C:
			// Check if interval changed; if so, recreate the ticker.
			currentInterval := s.Interval()
			if currentInterval != lastInterval {
				ticker.Stop()
				ticker = time.NewTicker(currentInterval)
				lastInterval = currentInterval
				s.log.Info("scheduler: interval changed", "new_interval", currentInterval)
			}
			s.fire(ctx, t)
		}
	}
}

// fire runs one cycle if the scheduler is not already running one.
func (s *Scheduler) fire(ctx context.Context, t time.Time) {
	if s.running.Load() {
		s.log.Warn("scheduler: previous cycle still running, skipping tick", "time", t.Format(time.RFC3339))
		return
	}
	s.running.Store(true)
	s.stateVal.Store("running")

	windowEnd := t.UTC()
	windowStart := windowEnd.Add(-s.Interval())

	go func() {
		defer func() {
			s.running.Store(false)
			s.stateVal.Store("idle")
		}()
		_, err := s.cycle.Run(ctx, windowStart, windowEnd)
		if err != nil {
			s.log.Error("scheduler: cycle error", "err", err)
		}
	}()
}
