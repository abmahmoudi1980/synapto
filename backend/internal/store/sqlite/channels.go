package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/store"
)

// channelRow maps the channels table to store.Channel.
type channelRow struct {
	ID             string         `db:"id"`
	Handle         string         `db:"handle"`
	DisplayName    string         `db:"display_name"`
	Status         string         `db:"status"`
	LastSeenMsgID  int64          `db:"last_seen_msg_id"`
	LastObservedAt sql.NullString `db:"last_observed_at"`
	LastError      sql.NullString `db:"last_error"`
	CreatedAt      string         `db:"created_at"`
	UpdatedAt      string         `db:"updated_at"`
}

func (r channelRow) toEntity() store.Channel {
	return store.Channel{
		ID:             r.ID,
		Handle:         r.Handle,
		DisplayName:    r.DisplayName,
		Status:         store.ChannelStatus(r.Status),
		LastSeenMsgID:  r.LastSeenMsgID,
		LastObservedAt: parseTime(r.LastObservedAt),
		LastError:      r.LastError.String,
		CreatedAt:      parseTimeStr(r.CreatedAt),
		UpdatedAt:      parseTimeStr(r.UpdatedAt),
	}
}

var handleRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}[A-Za-z0-9]$`)

// validateHandle returns a clean handle (without leading @) or an error.
func validateHandle(handle string) (string, error) {
	h := handle
	if len(h) > 0 && h[0] == '@' {
		h = h[1:]
	}
	if !handleRe.MatchString(h) {
		return "", errInvalidHandle
	}
	return h, nil
}

// errInvalidHandle is returned when a handle fails validation.
var errInvalidHandle = errors.New("invalid channel handle")

// ListChannels returns all channels ordered by handle.
func (s *Store) ListChannels(ctx context.Context) ([]store.Channel, error) {
	var rows []channelRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM channels ORDER BY handle ASC`); err != nil {
		return nil, err
	}
	out := make([]store.Channel, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// GetChannel returns a channel by id.
func (s *Store) GetChannel(ctx context.Context, id string) (store.Channel, error) {
	var r channelRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM channels WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Channel{}, store.ErrNotFound
		}
		return store.Channel{}, err
	}
	return r.toEntity(), nil
}

// GetChannelByHandle returns a channel by handle.
func (s *Store) GetChannelByHandle(ctx context.Context, handle string) (store.Channel, error) {
	var r channelRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM channels WHERE handle = ?`, handle); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Channel{}, store.ErrNotFound
		}
		return store.Channel{}, err
	}
	return r.toEntity(), nil
}

// AddChannel inserts a new channel in the active state.
func (s *Store) AddChannel(ctx context.Context, handle, displayName string) (store.Channel, error) {
	h, err := validateHandle(handle)
	if err != nil {
		return store.Channel{}, err
	}
	if displayName == "" {
		displayName = h
	}
	now := time.Now().UTC()
	c := store.Channel{
		ID:          uuid.NewString(),
		Handle:      h,
		DisplayName: displayName,
		Status:      store.ChannelActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO channels
		(id, handle, display_name, status, last_seen_msg_id, last_observed_at, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, NULL, NULL, ?, ?)`,
		c.ID, c.Handle, c.DisplayName, string(c.Status), timeStr(now), timeStr(now))
	if err != nil {
		return store.Channel{}, err
	}
	return c, nil
}

// RemoveChannel deletes a channel. Refuses if the channel has digest items.
func (s *Store) RemoveChannel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	if err := s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM digest_items WHERE channel_id = ?`, id); err != nil {
		return err
	}
	if count > 0 {
		return store.ErrChannelHasHistory
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// UpdateChannelStatus sets the lifecycle state and last error of a channel.
func (s *Store) UpdateChannelStatus(ctx context.Context, id string, status store.ChannelStatus, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET status = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		string(status), errMsg, nowISO(), id)
	return err
}

// AdvanceCursor records that we have seen up to lastSeenMsgID from the channel.
func (s *Store) AdvanceCursor(ctx context.Context, id string, lastSeenMsgID int64, observedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET last_seen_msg_id = ?, last_observed_at = ?, updated_at = ? WHERE id = ?`,
		lastSeenMsgID, timeStr(observedAt), nowISO(), id)
	return err
}

// GetCursor returns the last-seen message id for a channel.
func (s *Store) GetCursor(ctx context.Context, channelID string) (int64, error) {
	var n int64
	err := s.db.GetContext(ctx, &n, `SELECT last_seen_msg_id FROM channels WHERE id = ?`, channelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, store.ErrNotFound
		}
		return 0, err
	}
	return n, nil
}
