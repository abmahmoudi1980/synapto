package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/synapto/assistant/internal/store"
)

type cycleRow struct {
	ID            string         `db:"id"`
	WindowStart   string         `db:"window_start"`
	WindowEnd     string         `db:"window_end"`
	Status        string         `db:"status"`
	InputMsgCount int            `db:"input_msg_count"`
	OutputItems   int            `db:"output_items"`
	Error         sql.NullString `db:"error"`
	StartedAt     string         `db:"started_at"`
	FinishedAt    sql.NullString `db:"finished_at"`
}

func (r cycleRow) toEntity() store.Cycle {
	return store.Cycle{
		ID:            r.ID,
		WindowStart:   parseTimeStr(r.WindowStart),
		WindowEnd:     parseTimeStr(r.WindowEnd),
		Status:        store.CycleStatus(r.Status),
		InputMsgCount: r.InputMsgCount,
		OutputItems:   r.OutputItems,
		Error:         r.Error.String,
		StartedAt:     parseTimeStr(r.StartedAt),
		FinishedAt:    parseTime(r.FinishedAt),
	}
}

// CreateCycle inserts a new pending cycle row.
func (s *Store) CreateCycle(ctx context.Context, c store.Cycle) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cycles
		(id, window_start, window_end, status, input_msg_count, output_items, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?, NULL)`,
		c.ID, timeStr(c.WindowStart), timeStr(c.WindowEnd), string(c.Status),
		c.InputMsgCount, c.OutputItems, timeStr(c.StartedAt))
	return err
}

// FinishCycle records the terminal state of a cycle.
func (s *Store) FinishCycle(ctx context.Context, id string, status store.CycleStatus, inputCount, outputItems int, errMsg string) error {
	var errVal interface{}
	if errMsg != "" {
		errVal = errMsg
	}
	res, err := s.db.ExecContext(ctx, `UPDATE cycles SET
		status = ?, input_msg_count = ?, output_items = ?, error = ?, finished_at = ?
		WHERE id = ?`,
		string(status), inputCount, outputItems, errVal, nowISO(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// LastSuccessfulWindowEnd returns the window_end of the most recent
// succeeded or degraded cycle, plus a found flag.
func (s *Store) LastSuccessfulWindowEnd(ctx context.Context) (time.Time, bool, error) {
	var ts string
	err := s.db.GetContext(ctx, &ts, `SELECT window_end FROM cycles
		WHERE status IN ('succeeded','degraded')
		ORDER BY window_end DESC LIMIT 1`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return parseTimeStr(ts), true, nil
}

// ListCycles returns cycles newest-first.
func (s *Store) ListCycles(ctx context.Context, limit, offset int) ([]store.Cycle, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []cycleRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM cycles ORDER BY window_end DESC LIMIT ? OFFSET ?`, limit, offset); err != nil {
		return nil, err
	}
	out := make([]store.Cycle, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// GetCycle returns one cycle by id.
func (s *Store) GetCycle(ctx context.Context, id string) (store.Cycle, error) {
	var r cycleRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM cycles WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Cycle{}, store.ErrNotFound
		}
		return store.Cycle{}, err
	}
	return r.toEntity(), nil
}
