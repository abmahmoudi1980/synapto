// Package store defines the persistence layer for the assistant service.
//
// The package exposes repository interfaces (ChannelRepo, CategoryRepo,
// SettingsRepo, CycleRepo, DigestRepo, CursorRepo, HealthRepo) and a set
// of entity types. The concrete implementation lives under store/sqlite.
package store

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by the repository implementations.
var (
	// ErrNotFound is returned when a single-row lookup misses.
	ErrNotFound = errors.New("store: not found")
	// ErrChannelHasHistory is returned when removing a channel that has
	// digest_items referencing it (FK is ON DELETE RESTRICT).
	ErrChannelHasHistory = errors.New("store: channel has history")
	// ErrCannotRemoveDefault is returned when trying to delete a default category.
	ErrCannotRemoveDefault = errors.New("store: cannot remove a default category")
	// ErrInvalidInterval is returned when the digest interval is out of range.
	ErrInvalidInterval = errors.New("store: invalid digest interval (must be 60..86400 seconds)")
	// ErrInvalidDeliveryMode is returned when SettingsUpdate.DeliveryMode
	// is set to a value other than "bundled" or "per_post".
	ErrInvalidDeliveryMode = errors.New("store: invalid delivery mode (must be 'bundled' or 'per_post')")
)

// ChannelStatus is the lifecycle state of a source channel.
type ChannelStatus string

const (
	ChannelActive       ChannelStatus = "active"
	ChannelInaccessible ChannelStatus = "inaccessible"
	ChannelBanned       ChannelStatus = "banned"
)

// Channel is a Telegram channel the subscriber wants monitored.
type Channel struct {
	ID             string
	Handle         string
	DisplayName    string
	Status         ChannelStatus
	LastSeenMsgID  int64
	LastObservedAt time.Time // zero value when never observed
	LastError      string    // empty when no error
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Category groups digest items in the rendered output.
type Category struct {
	ID        string
	Name      string
	Ordering  int
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeliveryMode is how the cycle packages a window of posts for delivery
// to the subscriber chat. See specs/001-telegram-news-assistant/data-model.md
// (v2 amendment) and contracts/telegram-render.md.
type DeliveryMode string

const (
	// DeliveryBundled: all unsent posts in the window are grouped into
	// a single MarkdownV2 digest message (or a bounded set when the
	// rendered text would exceed Telegram's per-message cap). Original
	// v1 behavior. Retained for the operator to compare against.
	DeliveryBundled DeliveryMode = "bundled"
	// DeliveryPerPost: each post is sent as its own Telegram message.
	// Failures are isolated per post. Default for v2.
	DeliveryPerPost DeliveryMode = "per_post"
)

// Settings is the operator-configured runtime configuration row.
// Credential fields hold references (e.g. "env:TELEGRAM_BOT_TOKEN"), never
// raw secrets.
type Settings struct {
	DigestIntervalSeconds  int
	TelegramBotTokenRef    string
	TelegramSubscriberChat int64
	AIProvider             string
	AIModel                string
	AIAPIKeyRef            string
	AIBaseURL              string
	UncategorizedLabel     string
	DeliveryMode           DeliveryMode
	UpdatedAt              time.Time
}

// CycleStatus is the state of one scheduled digest execution.
type CycleStatus string

const (
	CyclePending        CycleStatus = "pending"
	CycleSucceeded      CycleStatus = "succeeded"
	CycleFailed         CycleStatus = "failed"
	CycleDegraded       CycleStatus = "degraded"
	CycleSkippedNoItems CycleStatus = "skipped_no_items"
)

// Cycle is one scheduled execution of the digest loop.
type Cycle struct {
	ID            string
	WindowStart   time.Time
	WindowEnd     time.Time
	Status        CycleStatus
	InputMsgCount int
	OutputItems   int
	Error         string // empty unless Status is failed
	StartedAt     time.Time
	FinishedAt    time.Time // zero value while pending
}

// SendStatus is the delivery outcome for a digest.
type SendStatus string

const (
	SendOK      SendStatus = "ok"
	SendFailed  SendStatus = "failed"
	SendBlocked SendStatus = "blocked"
)

// Digest is the delivered artifact for one cycle.
type Digest struct {
	ID            string
	CycleID       string
	RenderedText  string
	Degraded      bool
	TelegramMsgID int64 // 0 when not yet sent
	SentAt        time.Time
	SendStatus    SendStatus
}

// MediaKind describes the primary content type of a source message.
type MediaKind string

const (
	MediaText  MediaKind = "text"
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
	MediaVoice MediaKind = "voice"
	MediaOther MediaKind = "other"
)

// DigestItem is one summarized, categorized entry in a digest.
type DigestItem struct {
	ID          string
	CycleID     string
	ChannelID   string
	CategoryID  string // empty when uncategorized
	SourceMsgID int64
	PostID      string // back-reference to the persistent posts row (added in 0002)
	DedupKey    string
	RawText     string
	MediaKind   MediaKind
	Summary     string
	Confidence  float64 // 0 when unknown
	Ordering    int
}

// PostStatus is the lifecycle state of a single source-channel post.
// See specs/001-telegram-news-assistant/data-model.md for the
// transition diagram and the cycle that drives state changes.
type PostStatus string

const (
	PostReceived         PostStatus = "received"          // fetched, needs summarization
	PostSummarized       PostStatus = "summarized"        // AI returned; ready to bundle
	PostIncludedInDigest PostStatus = "included_in_digest" // bundled into a digest row that hasn't been ack'd yet
	PostSent             PostStatus = "sent"              // Telegram ack'd
	PostSendFailed       PostStatus = "send_failed"       // Telegram returned an error; will retry on next cycle
	PostFilteredOut      PostStatus = "filtered_out"      // AI returned ErrInvalidInput; intentionally dropped
	PostDead             PostStatus = "dead"              // operator-marked; not retried (future use)
)

// Post is one Telegram channel post, durable across cycles. One row per
// (channel_id, source_msg_id) — the unique constraint at the SQL level
// prevents duplicates.
type Post struct {
	ID            string
	ChannelID     string
	SourceMsgID   int64
	DedupKey      string
	Link          string
	RawText       string
	MediaKind     MediaKind
	CapturedAt    time.Time
	Status        PostStatus
	CategoryID    string // empty when uncategorized
	Summary       string // empty until AI returns
	Confidence    float64
	Attempts      int
	LastAttemptAt time.Time // zero when never tried
	SentAt        time.Time // zero when not yet sent
	TelegramMsgID int64     // 0 when not yet sent
	SendError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DigestListEntry is a row in the history list view: the cycle plus a few
// scalar fields from the digest row, without the full rendered text.
type DigestListEntry struct {
	Cycle      Cycle
	DigestID   string // empty when no digest was produced
	Degraded   bool
	SentAt     time.Time // zero when no digest
	SendStatus SendStatus
	ItemCount  int
}

// OpEvent is one entry in the operational audit log.
type OpEvent struct {
	ID         int64
	OccurredAt time.Time
	Level      string // "info" | "warn" | "error"
	Kind       string // e.g. "cycle.start", "telegram.send.failed"
	CycleID    string // empty when not tied to a cycle
	Message    string
	Context    string // JSON blob, may be empty
}

// Health is the read-only operational snapshot.
type Health struct {
	LastSuccessfulCycleAt time.Time // zero when never
	LastFailureAt         time.Time // zero when never
	LastFailureReason     string
	ChannelStatuses       []ChannelStatusEntry
}

// ChannelStatusEntry is one row in Health.ChannelStatuses.
type ChannelStatusEntry struct {
	Handle         string
	DisplayName    string
	Status         ChannelStatus
	LastObservedAt time.Time
	LastError      string
}

// ChannelRepo persists the subscriber's selected channels.
type ChannelRepo interface {
	List(ctx context.Context) ([]Channel, error)
	Get(ctx context.Context, id string) (Channel, error)
	GetByHandle(ctx context.Context, handle string) (Channel, error)
	Add(ctx context.Context, handle, displayName string) (Channel, error)
	Remove(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id string, status ChannelStatus, errMsg string) error
	AdvanceCursor(ctx context.Context, id string, lastSeenMsgID int64, observedAt time.Time) error
}

// CategoryRepo persists the category set used to group digest items.
type CategoryRepo interface {
	List(ctx context.Context) ([]Category, error)
	Add(ctx context.Context, name string) (Category, error)
	Rename(ctx context.Context, id, newName string) (Category, error)
	Remove(ctx context.Context, id string) error
	EnsureDefaults(ctx context.Context, defaults []string) error
}

// SettingsUpdate carries the settable fields of Settings. Pointer fields
// let the caller express "leave unchanged" (nil) vs "clear" (pointer to zero).
type SettingsUpdate struct {
	DigestIntervalSeconds  *int
	TelegramSubscriberChat *int64
	UncategorizedLabel     *string
	DeliveryMode           *DeliveryMode
}

// SettingsRepo persists the singleton operator-configuration row.
type SettingsRepo interface {
	Get(ctx context.Context) (Settings, error)
	Update(ctx context.Context, u SettingsUpdate) (Settings, error)
}

// CycleRepo persists one row per scheduled digest execution.
type CycleRepo interface {
	Create(ctx context.Context, c Cycle) error
	Finish(ctx context.Context, id string, status CycleStatus, inputCount, outputItems int, errMsg string) error
	LastSuccessfulWindowEnd(ctx context.Context) (time.Time, bool, error)
	List(ctx context.Context, limit, offset int) ([]Cycle, error)
	Get(ctx context.Context, id string) (Cycle, error)
	Count(ctx context.Context) (int, error)
	ListWithDegraded(ctx context.Context, limit, offset int) ([]DigestListEntry, error)
}

// DigestRepo persists delivered digests and their items.
type DigestRepo interface {
	Create(ctx context.Context, d Digest) error
	AddItem(ctx context.Context, item DigestItem) error
	UpdateSendResult(ctx context.Context, id string, telegramMsgID int64, status SendStatus) error
	ListItemsByCycle(ctx context.Context, cycleID string) ([]DigestItem, error)
	ListRecent(ctx context.Context, limit int) ([]DigestListEntry, error)
	GetByCycle(ctx context.Context, cycleID string) (Digest, error)
}

// CursorRepo is a thin convenience on top of ChannelRepo for the scheduler
// and fetcher, which only need to read and advance the per-channel cursor.
type CursorRepo interface {
	Get(ctx context.Context, channelID string) (int64, error)
	Advance(ctx context.Context, channelID string, toMsgID int64) error
}

// HealthRepo computes the operational health snapshot and records events.
type HealthRepo interface {
	Snapshot(ctx context.Context) (Health, error)
	RecordEvent(ctx context.Context, e OpEvent) error
	RecentEvents(ctx context.Context, limit int) ([]OpEvent, error)
}

// PostRepo persists one row per unique channel post. The cycle uses it
// to (1) dedupe fetches across cycles via Upsert, (2) drive the
// summarize step from ListReceived, (3) drive the send step from
// ListUnsent, and (4) record per-post send outcomes.
type PostRepo interface {
	// Upsert inserts a post if (channel_id, source_msg_id) is new, or
	// returns the existing row untouched. The bool result is true when
	// the row was newly created.
	Upsert(ctx context.Context, p Post) (Post, bool, error)

	// Get returns one post by id, or ErrNotFound.
	Get(ctx context.Context, id string) (Post, error)

	// GetByChannelMsg returns one post by (channel, source_msg_id), or
	// ErrNotFound.
	GetByChannelMsg(ctx context.Context, channelID string, sourceMsgID int64) (Post, error)

	// ListReceived returns posts that still need summarization,
	// status='received', ordered by captured_at ASC. Limit caps the
	// result (caller-supplied, typically 200 per cycle).
	ListReceived(ctx context.Context, limit int) ([]Post, error)

	// ListUnsent returns posts the cycle should bundle this round:
	// status IN ('summarized','send_failed','included_in_digest') AND
	// (last_attempt_at IS NULL OR last_attempt_at < cutoff). Ordered
	// by captured_at ASC, capped at limit.
	ListUnsent(ctx context.Context, cutoff time.Time, limit int) ([]Post, error)

	// ListByStatus returns posts filtered by status, newest first,
	// for the admin view.
	ListByStatus(ctx context.Context, status PostStatus, limit int) ([]Post, error)

	// ListAll returns posts in reverse captured_at order, capped at
	// limit. Used by the admin history view.
	ListAll(ctx context.Context, limit int) ([]Post, error)

	// GetFirstTerminalByDedupKey returns the earliest post with the
	// given dedup_key and a terminal status ('sent' or 'filtered_out'),
	// or ErrNotFound. Used by the cycle to skip cross-channel
	// duplicates: when a freshly-fetched post's content has already
	// been delivered (or intentionally dropped) via another channel,
	// the cycle marks the new post as filtered_out instead of
	// summarizing and sending it again.
	GetFirstTerminalByDedupKey(ctx context.Context, dedupKey string) (Post, error)

	// MarkSummarized sets summary + category + confidence, and
	// transitions status from 'received' to 'summarized'.
	MarkSummarized(ctx context.Context, id string, categoryID, summary string, confidence float64) error

	// MarkIncluded transitions N posts from 'summarized' to
	// 'included_in_digest' and bumps last_attempt_at to now. Called
	// when the cycle creates a digest row that hasn't been sent yet.
	MarkIncluded(ctx context.Context, postIDs []string) error

	// MarkSent transitions a post to 'sent' and records the Telegram
	// message id + sent_at + attempts+1.
	MarkSent(ctx context.Context, id string, telegramMsgID int64) error

	// MarkSendFailed transitions a post to 'send_failed', increments
	// attempts, and stores the error message.
	MarkSendFailed(ctx context.Context, id string, errMsg string) error

	// MarkFiltered sets status='filtered_out' (ErrInvalidInput).
	MarkFiltered(ctx context.Context, id string) error

	// MarkDead sets status='dead'. Called by the per-post cycle
	// after a post has exceeded maxSendAttempts consecutive send
	// failures. The post is excluded from ListUnsent and will not
	// be retried by future cycles.
	MarkDead(ctx context.Context, id string) error
}
