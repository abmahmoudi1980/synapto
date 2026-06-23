package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/store"
)

// postRow maps the posts table to store.Post.
type postRow struct {
	ID            string         `db:"id"`
	ChannelID     string         `db:"channel_id"`
	SourceMsgID   int64          `db:"source_msg_id"`
	DedupKey      string         `db:"dedup_key"`
	Link          string         `db:"link"`
	RawText       string         `db:"raw_text"`
	MediaKind     string         `db:"media_kind"`
	CapturedAt    string         `db:"captured_at"`
	Status        string         `db:"status"`
	CategoryID    sql.NullString `db:"category_id"`
	Summary       sql.NullString `db:"summary"`
	Confidence    sql.NullFloat64 `db:"confidence"`
	Attempts      int            `db:"attempts"`
	LastAttemptAt sql.NullString `db:"last_attempt_at"`
	SentAt        sql.NullString `db:"sent_at"`
	TelegramMsgID sql.NullInt64  `db:"telegram_msg_id"`
	SendError     sql.NullString `db:"send_error"`
	CreatedAt     string         `db:"created_at"`
	UpdatedAt     string         `db:"updated_at"`
}

func (r postRow) toEntity() store.Post {
	return store.Post{
		ID:            r.ID,
		ChannelID:     r.ChannelID,
		SourceMsgID:   r.SourceMsgID,
		DedupKey:      r.DedupKey,
		Link:          r.Link,
		RawText:       r.RawText,
		MediaKind:     store.MediaKind(r.MediaKind),
		CapturedAt:    parseTimeStr(r.CapturedAt),
		Status:        store.PostStatus(r.Status),
		CategoryID:    r.CategoryID.String,
		Summary:       r.Summary.String,
		Confidence:    r.Confidence.Float64,
		Attempts:      r.Attempts,
		LastAttemptAt: parseTime(r.LastAttemptAt),
		SentAt:        parseTime(r.SentAt),
		TelegramMsgID: r.TelegramMsgID.Int64,
		SendError:     r.SendError.String,
		CreatedAt:     parseTimeStr(r.CreatedAt),
		UpdatedAt:     parseTimeStr(r.UpdatedAt),
	}
}

// Upsert inserts a post if (channel_id, source_msg_id) is new; otherwise
// returns the existing row. The bool is true when the row was newly
// created.
func (s *Store) UpsertPost(ctx context.Context, p store.Post) (store.Post, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.CapturedAt.IsZero() {
		p.CapturedAt = time.Now().UTC()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	p.UpdatedAt = time.Now().UTC()
	if p.Status == "" {
		p.Status = store.PostReceived
	}
	if p.MediaKind == "" {
		p.MediaKind = store.MediaText
	}

	// Look up first: if the row exists, return it untouched.
	existing, err := s.getPostByChannelMsgLocked(ctx, p.ChannelID, p.SourceMsgID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Post{}, false, err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO posts (
		id, channel_id, source_msg_id, dedup_key, link, raw_text, media_kind,
		captured_at, status, category_id, summary, confidence, attempts,
		last_attempt_at, sent_at, telegram_msg_id, send_error, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ChannelID, p.SourceMsgID, p.DedupKey, p.Link, p.RawText, string(p.MediaKind),
		timeStr(p.CapturedAt), string(p.Status),
		nullableString(p.CategoryID), nullableString(p.Summary), nullableFloat(p.Confidence),
		p.Attempts, nullTime(p.LastAttemptAt), nullTime(p.SentAt), nullInt64(p.TelegramMsgID),
		nullableString(p.SendError), timeStr(p.CreatedAt), timeStr(p.UpdatedAt))
	if err != nil {
		return store.Post{}, false, err
	}
	return p, true, nil
}

// GetPost returns one post by id.
func (s *Store) GetPost(ctx context.Context, id string) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getPostLocked(ctx, "id", id)
}

// GetPostByChannelMsg returns one post by (channel, source_msg_id).
func (s *Store) GetPostByChannelMsg(ctx context.Context, channelID string, sourceMsgID int64) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getPostByChannelMsgLocked(ctx, channelID, sourceMsgID)
}

func (s *Store) getPostByChannelMsgLocked(ctx context.Context, channelID string, sourceMsgID int64) (store.Post, error) {
	var r postRow
	err := s.db.GetContext(ctx, &r, `SELECT * FROM posts WHERE channel_id = ? AND source_msg_id = ?`, channelID, sourceMsgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Post{}, store.ErrNotFound
		}
		return store.Post{}, err
	}
	return r.toEntity(), nil
}

func (s *Store) getPostLocked(ctx context.Context, field, value string) (store.Post, error) {
	q := fmt.Sprintf(`SELECT * FROM posts WHERE %s = ?`, field)
	if field != "id" && field != "channel_id" && field != "dedup_key" {
		return store.Post{}, fmt.Errorf("unsupported lookup field %q", field)
	}
	var r postRow
	err := s.db.GetContext(ctx, &r, q, value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Post{}, store.ErrNotFound
		}
		return store.Post{}, err
	}
	return r.toEntity(), nil
}

// ListReceived returns posts with status='received' (still need
// summarization), ordered by captured_at ASC. Limit caps the result.
func (s *Store) ListReceivedPosts(ctx context.Context, limit int) ([]store.Post, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []postRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT * FROM posts WHERE status = 'received' ORDER BY captured_at ASC LIMIT ?`, limit); err != nil {
		return nil, err
	}
	out := make([]store.Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// ListUnsent returns posts the cycle should bundle this round:
// status IN ('summarized','send_failed','included_in_digest'). The
// status field is the source of truth — once a post is sent, its
// status is 'sent' and it is excluded automatically.
//
// The `cutoff` argument is preserved for API symmetry / future use
// but is currently unused; a post with `last_attempt_at = now` is
// still re-bundlable on the next call. (The cycle calls ListUnsent
// at most once per Run(), so there is no same-cycle re-bundle risk.)
func (s *Store) ListUnsentPosts(ctx context.Context, cutoff time.Time, limit int) ([]store.Post, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	_ = cutoff
	var rows []postRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT * FROM posts
		 WHERE status IN ('summarized','send_failed','included_in_digest')
		 ORDER BY captured_at ASC LIMIT ?`, limit); err != nil {
		return nil, err
	}
	out := make([]store.Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// ListPostsByStatus returns posts filtered by status, newest first.
func (s *Store) ListPostsByStatus(ctx context.Context, status store.PostStatus, limit int) ([]store.Post, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var rows []postRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT * FROM posts WHERE status = ? ORDER BY captured_at DESC LIMIT ?`, string(status), limit); err != nil {
		return nil, err
	}
	out := make([]store.Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// ListAllPosts returns posts in reverse captured_at order, capped at
// limit. Used by the admin history view.
func (s *Store) ListAllPosts(ctx context.Context, limit int) ([]store.Post, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []postRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT * FROM posts ORDER BY captured_at DESC LIMIT ?`, limit); err != nil {
		return nil, err
	}
	out := make([]store.Post, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// GetFirstTerminalByDedupKey returns the earliest post with the given
// dedup_key and a terminal status ('sent' or 'filtered_out'), or
// ErrNotFound. Used by the cycle to detect cross-channel content
// duplicates: a freshly-fetched post whose text/media signature
// matches a post already delivered (or dropped) via another channel
// is marked filtered_out instead of being summarized and sent again.
func (s *Store) GetFirstTerminalByDedupKey(ctx context.Context, dedupKey string) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r postRow
	err := s.db.GetContext(ctx, &r, `SELECT * FROM posts
		WHERE dedup_key = ? AND status IN ('sent', 'filtered_out')
		ORDER BY captured_at ASC LIMIT 1`, dedupKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Post{}, store.ErrNotFound
		}
		return store.Post{}, err
	}
	return r.toEntity(), nil
}

// MarkSummarized transitions a post from 'received' to 'summarized'
// and records summary + category + confidence. No-op if the post is
// not in 'received' (e.g. it was already summarized in a previous cycle).
func (s *Store) MarkPostSummarized(ctx context.Context, id string, categoryID, summary string, confidence float64) error {
	now := nowISO()
	res, err := s.db.ExecContext(ctx, `UPDATE posts SET
		status = 'summarized', category_id = ?, summary = ?, confidence = ?, updated_at = ?
		WHERE id = ? AND status = 'received'`,
		nullableString(categoryID), nullableString(summary), nullableFloat(confidence), now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either the post is gone (treat as not-found) or it was
		// already in a later state; the caller can decide what to do.
		// We don't error here because a retry of the same item is
		// expected to be a no-op.
	}
	return nil
}

// MarkIncluded transitions posts from 'summarized' (or 'send_failed')
// to 'included_in_digest' and bumps last_attempt_at to now. Called
// when the cycle has built a digest row but hasn't sent it yet.
func (s *Store) MarkPostsIncluded(ctx context.Context, postIDs []string) error {
	if len(postIDs) == 0 {
		return nil
	}
	q := `UPDATE posts SET
		status = 'included_in_digest', last_attempt_at = ?, updated_at = ?
		WHERE id IN (?` + repeatQ(len(postIDs)-1) + `)`
	args := []any{nowISO(), nowISO()}
	for _, id := range postIDs {
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// MarkPostSent transitions a post to 'sent' and records the Telegram
// message id, sent_at, and increments attempts.
func (s *Store) MarkPostSent(ctx context.Context, id string, telegramMsgID int64) error {
	now := nowISO()
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET
		status = 'sent', telegram_msg_id = ?, sent_at = ?, last_attempt_at = ?,
		attempts = attempts + 1, send_error = NULL, updated_at = ?
		WHERE id = ?`,
		nullInt64(telegramMsgID), now, now, now, id)
	return err
}

// MarkPostSendFailed transitions a post to 'send_failed', increments
// attempts, and stores the error message.
func (s *Store) MarkPostSendFailed(ctx context.Context, id string, errMsg string) error {
	now := nowISO()
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET
		status = 'send_failed', attempts = attempts + 1,
		last_attempt_at = ?, send_error = ?, updated_at = ?
		WHERE id = ?`,
		now, errMsg, now, id)
	return err
}

// MarkPostFiltered sets status='filtered_out' (ErrInvalidInput).
func (s *Store) MarkPostFiltered(ctx context.Context, id string) error {
	now := nowISO()
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET
		status = 'filtered_out', updated_at = ?
		WHERE id = ? AND status = 'received'`, now, id)
	return err
}

// MarkPostDead sets status='dead'. Called by the per-post cycle
// when a post has exceeded maxSendAttempts consecutive failures
// (see per_post_cycle.go). The post is excluded from ListUnsent
// automatically. The transition is one-way: a 'dead' post is
// never re-summarized or re-sent by the cycle.
func (s *Store) MarkPostDead(ctx context.Context, id string) error {
	now := nowISO()
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET
		status = 'dead', updated_at = ?
		WHERE id = ? AND status IN ('received', 'summarized',
		    'included_in_digest', 'send_failed')`, now, id)
	return err
}

// repeatQ returns "?," repeated n times (used to build IN (?, ?, ...)
// placeholders).
func repeatQ(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		out = append(out, ',', '?')
	}
	return string(out)
}

// nullableString returns nil when s is empty, otherwise s. SQLite
// stores an empty string as '' so callers may want NULL instead.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableFloat returns nil when f is the zero value, otherwise f.
func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// nullInt64 returns nil when n is zero, otherwise n.
func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
