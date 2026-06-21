package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FakeSeedMessage is one message in the fake seed file.
type FakeSeedMessage struct {
	ID      int64  `yaml:"id" json:"id"`
	Text    string `yaml:"text" json:"text"`
	Media   string `yaml:"media" json:"media"`
	Caption string `yaml:"caption" json:"caption"`
	SentAt  string `yaml:"sent_at" json:"sent_at"`
}

// FakeSeedChannel groups seed messages under a channel handle.
type FakeSeedChannel struct {
	Channel  string            `yaml:"channel" json:"channel"`
	Messages []FakeSeedMessage `yaml:"messages" json:"messages"`
}

// FakeSeed is the top-level shape of the seed file. It supports both
// YAML-ish and JSON-ish parsing: we read the file as JSON for simplicity,
// and accept a YAML file only when a tiny preprocessor can convert it.
type FakeSeed struct {
	Channels []FakeSeedChannel `yaml:"channels" json:"channels"`
}

// Fake implements Client for local development and tests. It has no network
// dependency: GetChat returns a canned ChannelInfo for any well-formed
// handle, FetchNewPosts returns messages from a seed file, and SendMessage
// appends a JSONL record to a sink file.
type Fake struct {
	mu       sync.Mutex
	channels map[string]ChannelInfo // by handle
	posts    map[int64][]Post       // by channelID
	nextID   int64

	outPath string // where SendMessage records land
}

// NewFake constructs a Fake client. seedPath may be empty; if it is, the
// fake starts with no channels and no posts (the admin API can still add
// channels, which will appear as ChannelInfo with no messages).
// outPath may be empty; if it is, SendMessage is a no-op that returns OK.
func NewFake(seedPath, outPath string) (*Fake, error) {
	f := &Fake{
		channels: make(map[string]ChannelInfo),
		posts:    make(map[int64][]Post),
		outPath:  outPath,
		nextID:   1000,
	}
	if seedPath != "" {
		if err := f.loadSeed(seedPath); err != nil {
			return nil, fmt.Errorf("telegram fake: load seed: %w", err)
		}
	}
	return f, nil
}

// loadSeed reads a JSON seed file and populates the in-memory state.
// The YAML format used in quickstart.md is converted to JSON on the fly
// by a tiny preprocessor that handles the simple structure we emit.
func (f *Fake) loadSeed(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty seed is fine
		}
		return err
	}
	text := string(data)
	// Accept JSON directly. The quickstart uses YAML; for the fake we
	// support a minimal subset by trying JSON first.
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var seed FakeSeed
	// Try JSON array-of-channels shape used by tests.
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &seed.Channels); err != nil {
			// Try the wrapped shape.
			if err2 := json.Unmarshal([]byte(text), &seed); err2 != nil {
				return fmt.Errorf("telegram fake: parse seed: %w", err)
			}
		}
	} else {
		// YAML: convert the simple subset we use to JSON. This is a
		// pragmatic parser for the quickstart's source-messages.yaml.
		seed = parseYAMLSeed(text)
	}
	for _, ch := range seed.Channels {
		handle := strings.TrimPrefix(ch.Channel, "@")
		ci := ChannelInfo{
			ID:       f.nextID,
			Username: handle,
			Title:    handle,
		}
		f.nextID++
		f.channels[handle] = ci
		var posts []Post
		for _, m := range ch.Messages {
			p := Post{
				ChannelID: ci.ID,
				MessageID: m.ID,
				Text:      m.Text,
				MediaKind: orDefault(m.Media, "text"),
				Captions:  nonEmpty(m.Caption),
				SentAt:    parseTime(m.SentAt),
			}
			posts = append(posts, p)
		}
		f.posts[ci.ID] = posts
	}
	return nil
}

// GetChat returns a canned ChannelInfo for any handle that looks well-formed.
func (f *Fake) GetChat(_ context.Context, handle string) (ChannelInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle = strings.TrimPrefix(handle, "@")
	if ci, ok := f.channels[handle]; ok {
		return ci, nil
	}
	// Synthesize a channel the first time we see a handle.
	ci := ChannelInfo{
		ID:       f.nextID,
		Username: handle,
		Title:    handle,
	}
	f.nextID++
	f.channels[handle] = ci
	return ci, nil
}

// FetchNewPosts returns posts with id > sinceMsgID in ascending order.
func (f *Fake) FetchNewPosts(_ context.Context, channelHandle string, sinceMsgID int64) ([]Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ci, ok := f.channels[channelHandle]
	if !ok {
		return nil, nil // unknown channel → no posts
	}
	posts := f.posts[ci.ID]
	var out []Post
	for _, p := range posts {
		if p.MessageID > sinceMsgID {
			out = append(out, p)
		}
	}
	return out, nil
}

// SendMessage appends a JSONL record to the sink file and returns OK.
func (f *Fake) SendMessage(_ context.Context, chatID int64, text string, parseMode string) (SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outPath == "" {
		return SendResult{MessageID: 1, OK: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(f.outPath), 0o755); err != nil {
		return SendResult{}, fmt.Errorf("telegram fake: mkdir: %w", err)
	}
	rec := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
		"sent_at":    time.Now().UTC().Format(time.RFC3339),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return SendResult{}, err
	}
	line = append(line, '\n')
	if err := appendFile(f.outPath, line); err != nil {
		return SendResult{}, err
	}
	return SendResult{MessageID: 1, OK: true}, nil
}

// Close is a no-op for the fake.
func (f *Fake) Close() error { return nil }

// Helpers --------------------------------------------------------------------

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nonEmpty(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return []string{s}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().UTC()
	}
	return t
}

func appendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// parseYAMLSeed converts the simple YAML subset used in quickstart.md into
// a FakeSeed. It is intentionally limited: it handles the `- channel: name`
// / `  messages:` / `    - id: N` / `      text: ...` / `      media: ...`
// / `      caption: ...` structure. For anything richer, use JSON.
func parseYAMLSeed(text string) FakeSeed {
	var seed FakeSeed
	var cur *FakeSeedChannel
	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		ln := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- channel:") {
			if cur != nil {
				seed.Channels = append(seed.Channels, *cur)
			}
			c := &FakeSeedChannel{}
			c.Channel = strings.TrimSpace(strings.TrimPrefix(trimmed, "- channel:"))
			cur = c
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(trimmed, "messages:") {
			continue
		}
		if strings.HasPrefix(trimmed, "- id:") {
			cur.Messages = append(cur.Messages, FakeSeedMessage{})
			i := len(cur.Messages) - 1
			cur.Messages[i].ID = parseInt64(strings.TrimPrefix(trimmed, "- id:"))
			continue
		}
		if len(cur.Messages) == 0 {
			continue
		}
		i := len(cur.Messages) - 1
		switch {
		case strings.HasPrefix(trimmed, "text:"):
			cur.Messages[i].Text = unquoteYAML(strings.TrimPrefix(trimmed, "text:"))
		case strings.HasPrefix(trimmed, "media:"):
			cur.Messages[i].Media = unquoteYAML(strings.TrimPrefix(trimmed, "media:"))
		case strings.HasPrefix(trimmed, "caption:"):
			cur.Messages[i].Caption = unquoteYAML(strings.TrimPrefix(trimmed, "caption:"))
		case strings.HasPrefix(trimmed, "sent_at:"):
			cur.Messages[i].SentAt = unquoteYAML(strings.TrimPrefix(trimmed, "sent_at:"))
		}
	}
	if cur != nil {
		seed.Channels = append(seed.Channels, *cur)
	}
	return seed
}

func unquoteYAML(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, `"`)
	s = strings.TrimSuffix(s, `"`)
	return s
}

func parseInt64(s string) int64 {
	var n int64
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
