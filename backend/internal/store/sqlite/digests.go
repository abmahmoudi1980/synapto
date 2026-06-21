package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/store"
)

type digestRow struct {
	ID            string         `db:"id"`
	CycleID       string         `db:"cycle_id"`
	RenderedText  string         `db:"rendered_text"`
	Degraded      int            `db:"degraded"`
	TelegramMsgID sql.NullInt64  `db:"telegram_msg_id"`
	SentAt        string         `db:"sent_at"`
	SendStatus    string         `db:"send_status"`
}

func (r digestRow) toEntity() store.Digest {
	return store.Digest{
		ID:            r.ID,
		CycleID:       r.CycleID,
		RenderedText:  r.RenderedText,
		Degraded:      r.Degraded == 1,
		TelegramMsgID: r.TelegramMsgID.Int64,
		SentAt:        parseTimeStr(r.SentAt),
		SendStatus:    store.SendStatus(r.SendStatus),
	}
}

type digestItemRow struct {
	ID          string         `db:"id"`
	CycleID     string         `db:"cycle_id"`
	ChannelID   string         `db:"channel_id"`
	CategoryID  sql.NullString `db:"category_id"`
	SourceMsgID int64          `db:"source_msg_id"`
	DedupKey    string         `db:"dedup_key"`
	RawText     string         `db:"raw_text"`
	MediaKind   string         `db:"media_kind"`
	Summary     string         `db:"summary"`
	Confidence  sql.NullFloat64 `db:"confidence"`
	Ordering    int            `db:"ordering"`
}

func (r digestItemRow) toEntity() store.DigestItem {
	return store.DigestItem{
		ID:          r.ID,
		CycleID:     r.CycleID,
		ChannelID:   r.ChannelID,
		CategoryID:  r.CategoryID.String,
		SourceMsgID: r.SourceMsgID,
		DedupKey:    r.DedupKey,
		RawText:     r.RawText,
		MediaKind:   store.MediaKind(r.MediaKind),
		Summary:     r.Summary,
		Confidence:  r.Confidence.Float64,
		Ordering:    r.Ordering,
	}
}

// CreateDigest inserts a digest row for a cycle.
func (s *Store) CreateDigest(ctx context.Context, d store.Digest) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO digests
		(id, cycle_id, rendered_text, degraded, telegram_msg_id, sent_at, send_status)
		VALUES (?, ?, ?, ?, NULL, ?, ?)`,
		d.ID, d.CycleID, d.RenderedText, boolToInt(d.Degraded), timeStr(d.SentAt), string(d.SendStatus))
	return err
}

// AddDigestItem inserts one digest item. Uses ON CONFLICT DO NOTHING so a
// partial cycle replay is safe (see data-model.md "Cycle idempotency").
func (s *Store) AddDigestItem(ctx context.Context, item store.DigestItem) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	var catID interface{}
	if item.CategoryID != "" {
		catID = item.CategoryID
	}
	var conf interface{}
	if item.Confidence > 0 {
		conf = item.Confidence
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO digest_items
		(id, cycle_id, channel_id, category_id, source_msg_id, dedup_key, raw_text, media_kind, summary, confidence, ordering)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (cycle_id, channel_id, source_msg_id) DO NOTHING`,
		item.ID, item.CycleID, item.ChannelID, catID, item.SourceMsgID, item.DedupKey,
		item.RawText, string(item.MediaKind), item.Summary, conf, item.Ordering)
	return err
}

// UpdateDigestSendResult records the Telegram-assigned message id and send status.
func (s *Store) UpdateDigestSendResult(ctx context.Context, id string, telegramMsgID int64, status store.SendStatus) error {
	var msgID interface{}
	if telegramMsgID > 0 {
		msgID = telegramMsgID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE digests SET telegram_msg_id = ?, send_status = ? WHERE id = ?`,
		msgID, string(status), id)
	return err
}

// ListDigestItemsByCycle returns all items for a cycle, ordered by ordering.
func (s *Store) ListDigestItemsByCycle(ctx context.Context, cycleID string) ([]store.DigestItem, error) {
	var rows []digestItemRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM digest_items WHERE cycle_id = ? ORDER BY ordering ASC`, cycleID); err != nil {
		return nil, err
	}
	out := make([]store.DigestItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// GetDigestByCycle returns the digest row for a cycle, if any.
func (s *Store) GetDigestByCycle(ctx context.Context, cycleID string) (store.Digest, error) {
	var r digestRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM digests WHERE cycle_id = ?`, cycleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Digest{}, store.ErrNotFound
		}
		return store.Digest{}, err
	}
	return r.toEntity(), nil
}

// ListRecentDigests returns the most recent digests joined with their cycle.
type recentRow struct {
	CycleID     string `db:"cycle_id"`
	WindowEnd   string `db:"window_end"`
	Status      string `db:"status"`
	InputCount  int    `db:"input_msg_count"`
	OutputItems int    `db:"output_items"`
	DigestID    sql.NullString `db:"digest_id"`
	Degraded    sql.NullInt64  `db:"degraded"`
	SentAt      sql.NullString `db:"sent_at"`
	SendStatus  sql.NullString `db:"send_status"`
}

// ListRecentDigests returns up to limit recent digest list entries.
func (s *Store) ListRecentDigests(ctx context.Context, limit int) ([]store.DigestListEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT c.id AS cycle_id, c.window_end AS window_end, c.status AS status,
		c.input_msg_count AS input_msg_count, c.output_items AS output_items,
		d.id AS digest_id, d.degraded AS degraded, d.sent_at AS sent_at, d.send_status AS send_status
		FROM cycles c
		LEFT JOIN digests d ON d.cycle_id = c.id
		ORDER BY c.window_end DESC LIMIT ?`
	var rows []recentRow
	if err := s.db.SelectContext(ctx, &rows, q, limit); err != nil {
		return nil, err
	}
	out := make([]store.DigestListEntry, 0, len(rows))
	for _, r := range rows {
		entry := store.DigestListEntry{
			Cycle: store.Cycle{
				ID:            r.CycleID,
				WindowEnd:     parseTimeStr(r.WindowEnd),
				Status:        store.CycleStatus(r.Status),
				InputMsgCount: r.InputCount,
				OutputItems:   r.OutputItems,
			},
			Degraded:   r.Degraded.Int64 == 1,
			ItemCount:  r.OutputItems,
		}
		if r.DigestID.Valid {
			entry.DigestID = r.DigestID.String
		}
		if r.SentAt.Valid {
			entry.SentAt = parseTimeStr(r.SentAt.String)
		}
		if r.SendStatus.Valid {
			entry.SendStatus = store.SendStatus(r.SendStatus.String)
		}
		out = append(out, entry)
	}
	return out, nil
}

// boolToInt converts a bool to the 0/1 integer SQLite stores.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
