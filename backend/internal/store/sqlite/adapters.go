// adapters.go wraps the single *Store with thin types that satisfy the
// store.* repository interfaces. The methods on *Store itself have
// descriptive names (ListChannels, AddChannel, ...) to avoid collisions
// between interfaces that share method names like List/Add/Remove; the
// adapters then expose the interface method names.
package sqlite

import (
	"context"
	"time"

	"github.com/synapto/assistant/internal/store"
)

// ChannelStore adapts *Store to store.ChannelRepo.
type ChannelStore struct{ S *Store }

func (a ChannelStore) List(ctx context.Context) ([]store.Channel, error) {
	return a.S.ListChannels(ctx)
}
func (a ChannelStore) Get(ctx context.Context, id string) (store.Channel, error) {
	return a.S.GetChannel(ctx, id)
}
func (a ChannelStore) GetByHandle(ctx context.Context, handle string) (store.Channel, error) {
	return a.S.GetChannelByHandle(ctx, handle)
}
func (a ChannelStore) Add(ctx context.Context, handle, displayName string) (store.Channel, error) {
	return a.S.AddChannel(ctx, handle, displayName)
}
func (a ChannelStore) Remove(ctx context.Context, id string) error {
	return a.S.RemoveChannel(ctx, id)
}
func (a ChannelStore) UpdateStatus(ctx context.Context, id string, status store.ChannelStatus, errMsg string) error {
	return a.S.UpdateChannelStatus(ctx, id, status, errMsg)
}
func (a ChannelStore) AdvanceCursor(ctx context.Context, id string, lastSeenMsgID int64, observedAt time.Time) error {
	return a.S.AdvanceCursor(ctx, id, lastSeenMsgID, observedAt)
}
func (a ChannelStore) GetCursor(ctx context.Context, channelID string) (int64, error) {
	return a.S.GetCursor(ctx, channelID)
}
func (a ChannelStore) Advance(ctx context.Context, channelID string, toMsgID int64) error {
	return a.S.AdvanceCursor(ctx, channelID, toMsgID, time.Now().UTC())
}

// CategoryStore adapts *Store to store.CategoryRepo.
type CategoryStore struct{ S *Store }

func (a CategoryStore) List(ctx context.Context) ([]store.Category, error) {
	return a.S.ListCategories(ctx)
}
func (a CategoryStore) Add(ctx context.Context, name string) (store.Category, error) {
	return a.S.AddCategory(ctx, name)
}
func (a CategoryStore) Rename(ctx context.Context, id, newName string) (store.Category, error) {
	return a.S.RenameCategory(ctx, id, newName)
}
func (a CategoryStore) Remove(ctx context.Context, id string) error {
	return a.S.RemoveCategory(ctx, id)
}
func (a CategoryStore) EnsureDefaults(ctx context.Context, defaults []string) error {
	return a.S.EnsureDefaults(ctx, defaults)
}

// SettingsStore adapts *Store to store.SettingsRepo.
type SettingsStore struct{ S *Store }

func (a SettingsStore) Get(ctx context.Context) (store.Settings, error) {
	return a.S.GetSettings(ctx)
}
func (a SettingsStore) Update(ctx context.Context, u store.SettingsUpdate) (store.Settings, error) {
	return a.S.UpdateSettings(ctx, u)
}

// CycleStore adapts *Store to store.CycleRepo.
type CycleStore struct{ S *Store }

func (a CycleStore) Create(ctx context.Context, c store.Cycle) error {
	return a.S.CreateCycle(ctx, c)
}
func (a CycleStore) Finish(ctx context.Context, id string, status store.CycleStatus, inputCount, outputItems int, errMsg string) error {
	return a.S.FinishCycle(ctx, id, status, inputCount, outputItems, errMsg)
}
func (a CycleStore) LastSuccessfulWindowEnd(ctx context.Context) (time.Time, bool, error) {
	return a.S.LastSuccessfulWindowEnd(ctx)
}
func (a CycleStore) List(ctx context.Context, limit, offset int) ([]store.Cycle, error) {
	return a.S.ListCycles(ctx, limit, offset)
}
func (a CycleStore) Get(ctx context.Context, id string) (store.Cycle, error) {
	return a.S.GetCycle(ctx, id)
}
func (a CycleStore) Count(ctx context.Context) (int, error) {
	return a.S.CountCycles(ctx)
}
func (a CycleStore) ListWithDegraded(ctx context.Context, limit, offset int) ([]store.DigestListEntry, error) {
	return a.S.ListCyclesWithDegraded(ctx, limit, offset)
}

// DigestStore adapts *Store to store.DigestRepo.
type DigestStore struct{ S *Store }

func (a DigestStore) Create(ctx context.Context, d store.Digest) error {
	return a.S.CreateDigest(ctx, d)
}
func (a DigestStore) AddItem(ctx context.Context, item store.DigestItem) error {
	return a.S.AddDigestItem(ctx, item)
}
func (a DigestStore) UpdateSendResult(ctx context.Context, id string, telegramMsgID int64, status store.SendStatus) error {
	return a.S.UpdateDigestSendResult(ctx, id, telegramMsgID, status)
}
func (a DigestStore) ListItemsByCycle(ctx context.Context, cycleID string) ([]store.DigestItem, error) {
	return a.S.ListDigestItemsByCycle(ctx, cycleID)
}
func (a DigestStore) ListRecent(ctx context.Context, limit int) ([]store.DigestListEntry, error) {
	return a.S.ListRecentDigests(ctx, limit)
}
func (a DigestStore) GetByCycle(ctx context.Context, cycleID string) (store.Digest, error) {
	return a.S.GetDigestByCycle(ctx, cycleID)
}

// HealthStore adapts *Store to store.HealthRepo.
type HealthStore struct{ S *Store }

func (a HealthStore) Snapshot(ctx context.Context) (store.Health, error) {
	return a.S.Snapshot(ctx)
}
func (a HealthStore) RecordEvent(ctx context.Context, e store.OpEvent) error {
	return a.S.RecordEvent(ctx, e)
}
func (a HealthStore) RecentEvents(ctx context.Context, limit int) ([]store.OpEvent, error) {
	return a.S.RecentEvents(ctx, limit)
}

// CursorStore adapts *Store to store.CursorRepo.
type CursorStore struct{ S *Store }

func (a CursorStore) Get(ctx context.Context, channelID string) (int64, error) {
	return a.S.GetCursor(ctx, channelID)
}
func (a CursorStore) Advance(ctx context.Context, channelID string, toMsgID int64) error {
	return a.S.AdvanceCursor(ctx, channelID, toMsgID, time.Now().UTC())
}

// PostStore adapts *Store to store.PostRepo. See posts.go for the
// per-method implementations.
type PostStore struct{ S *Store }

func (a PostStore) Upsert(ctx context.Context, p store.Post) (store.Post, bool, error) {
	return a.S.UpsertPost(ctx, p)
}
func (a PostStore) Get(ctx context.Context, id string) (store.Post, error) {
	return a.S.GetPost(ctx, id)
}
func (a PostStore) GetByChannelMsg(ctx context.Context, channelID string, sourceMsgID int64) (store.Post, error) {
	return a.S.GetPostByChannelMsg(ctx, channelID, sourceMsgID)
}
func (a PostStore) ListReceived(ctx context.Context, limit int) ([]store.Post, error) {
	return a.S.ListReceivedPosts(ctx, limit)
}
func (a PostStore) ListUnsent(ctx context.Context, cutoff time.Time, limit int) ([]store.Post, error) {
	return a.S.ListUnsentPosts(ctx, cutoff, limit)
}
func (a PostStore) ListByStatus(ctx context.Context, status store.PostStatus, limit int) ([]store.Post, error) {
	return a.S.ListPostsByStatus(ctx, status, limit)
}
func (a PostStore) ListAll(ctx context.Context, limit int) ([]store.Post, error) {
	return a.S.ListAllPosts(ctx, limit)
}
func (a PostStore) GetFirstTerminalByDedupKey(ctx context.Context, dedupKey string) (store.Post, error) {
	return a.S.GetFirstTerminalByDedupKey(ctx, dedupKey)
}
func (a PostStore) MarkSummarized(ctx context.Context, id string, categoryID, summary string, confidence float64) error {
	return a.S.MarkPostSummarized(ctx, id, categoryID, summary, confidence)
}
func (a PostStore) MarkIncluded(ctx context.Context, postIDs []string) error {
	return a.S.MarkPostsIncluded(ctx, postIDs)
}
func (a PostStore) MarkSent(ctx context.Context, id string, telegramMsgID int64) error {
	return a.S.MarkPostSent(ctx, id, telegramMsgID)
}
func (a PostStore) MarkSendFailed(ctx context.Context, id string, errMsg string) error {
	return a.S.MarkPostSendFailed(ctx, id, errMsg)
}
func (a PostStore) MarkFiltered(ctx context.Context, id string) error {
	return a.S.MarkPostFiltered(ctx, id)
}

func (a PostStore) MarkDead(ctx context.Context, id string) error {
	return a.S.MarkPostDead(ctx, id)
}

// Compile-time assertions that the adapters satisfy the interfaces.
var (
	_ store.ChannelRepo  = ChannelStore{}
	_ store.CategoryRepo = CategoryStore{}
	_ store.SettingsRepo = SettingsStore{}
	_ store.CycleRepo    = CycleStore{}
	_ store.DigestRepo   = DigestStore{}
	_ store.HealthRepo   = HealthStore{}
	_ store.CursorRepo   = CursorStore{}
	_ store.PostRepo     = PostStore{}
)
