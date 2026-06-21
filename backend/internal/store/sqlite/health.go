package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/synapto/assistant/internal/store"
)

type opEventRow struct {
	ID         int64          `db:"id"`
	OccurredAt string         `db:"occurred_at"`
	Level      string         `db:"level"`
	Kind       string         `db:"kind"`
	CycleID    sql.NullString `db:"cycle_id"`
	Message    string         `db:"message"`
	Context    sql.NullString `db:"context"`
}

func (r opEventRow) toEntity() store.OpEvent {
	return store.OpEvent{
		ID:         r.ID,
		OccurredAt: parseTimeStr(r.OccurredAt),
		Level:      r.Level,
		Kind:       r.Kind,
		CycleID:    r.CycleID.String,
		Message:    r.Message,
		Context:    r.Context.String,
	}
}

// RecordEvent appends an entry to the op_events audit log.
func (s *Store) RecordEvent(ctx context.Context, e store.OpEvent) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	var cycleID interface{}
	if e.CycleID != "" {
		cycleID = e.CycleID
	}
	var ctxBlob interface{}
	if e.Context != "" {
		ctxBlob = e.Context
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO op_events (occurred_at, level, kind, cycle_id, message, context)
		VALUES (?, ?, ?, ?, ?, ?)`,
		timeStr(e.OccurredAt), e.Level, e.Kind, cycleID, e.Message, ctxBlob)
	return err
}

// RecentEvents returns the most recent op_events rows.
func (s *Store) RecentEvents(ctx context.Context, limit int) ([]store.OpEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []opEventRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM op_events ORDER BY occurred_at DESC, id DESC LIMIT ?`, limit); err != nil {
		return nil, err
	}
	out := make([]store.OpEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// Snapshot computes the operational health view.
func (s *Store) Snapshot(ctx context.Context) (store.Health, error) {
	var h store.Health

	// Last successful cycle.
	var lastSuccess string
	_ = s.db.GetContext(ctx, &lastSuccess, `SELECT window_end FROM cycles
		WHERE status IN ('succeeded','degraded') ORDER BY window_end DESC LIMIT 1`)
	if lastSuccess != "" {
		h.LastSuccessfulCycleAt = parseTimeStr(lastSuccess)
	}

	// Last failure + reason.
	var lastFail string
	var failReason string
	_ = s.db.GetContext(ctx, &lastFail, `SELECT finished_at FROM cycles
		WHERE status = 'failed' ORDER BY finished_at DESC LIMIT 1`)
	_ = s.db.GetContext(ctx, &failReason, `SELECT error FROM cycles
		WHERE status = 'failed' ORDER BY finished_at DESC LIMIT 1`)
	if lastFail != "" {
		h.LastFailureAt = parseTimeStr(lastFail)
	}
	h.LastFailureReason = failReason

	// Per-channel status.
	type chRow struct {
		Handle         string         `db:"handle"`
		DisplayName    string         `db:"display_name"`
		Status         string         `db:"status"`
		LastObservedAt sql.NullString `db:"last_observed_at"`
		LastError      sql.NullString `db:"last_error"`
	}
	var chs []chRow
	if err := s.db.SelectContext(ctx, &chs, `SELECT handle, display_name, status, last_observed_at, last_error FROM channels ORDER BY handle ASC`); err != nil {
		return h, err
	}
	for _, c := range chs {
		h.ChannelStatuses = append(h.ChannelStatuses, store.ChannelStatusEntry{
			Handle:         c.Handle,
			DisplayName:    c.DisplayName,
			Status:         store.ChannelStatus(c.Status),
			LastObservedAt: parseTime(c.LastObservedAt),
			LastError:      c.LastError.String,
		})
	}
	return h, nil
}
