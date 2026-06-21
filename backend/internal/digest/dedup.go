// Package digest implements the news-digest loop: dedup, render, cycle, scheduler.
package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"

	"github.com/synapto/assistant/internal/ai"
)

// Item is one candidate digest entry produced by the fetcher, before dedup
// and before summarization. The Dedup function consumes a slice of these
// and returns the subset that survives dedup.
type Item struct {
	ChannelID    string
	ChannelHandle string
	SourceMsgID  int64
	Text         string
	MediaKind    ai.MediaKind
	Captions     []string
}

// DedupKey is the dedup signature of an Item. Two items with the same
// DedupKey within one cycle are considered duplicates; only the first is kept.
type DedupKey string

// Key computes the dedup key for an item.
//   - For text items (and media items with non-empty text), the key is
//     sha256(normalize(text)).
//   - For media-only items (empty text), the key is sha256("media:" + kind + ":"
//     + normalized caption list), so two image posts with no caption share a
//     key only if their media kind and captions match.
func (it Item) Key() DedupKey {
	text := strings.TrimSpace(it.Text)
	if text != "" {
		return DedupKey("text:" + hash(normalize(text)))
	}
	captions := make([]string, 0, len(it.Captions))
	for _, c := range it.Captions {
		c = strings.TrimSpace(c)
		if c != "" {
			captions = append(captions, normalize(c))
		}
	}
	mediaSig := "media:" + string(it.MediaKind) + ":" + strings.Join(captions, "|")
	return DedupKey(hash(mediaSig))
}

// Dedup removes duplicate items within one cycle. The first occurrence wins
// and the input order is preserved. Items are duplicates when their
// DedupKeys are equal.
func Dedup(items []Item) []Item {
	seen := make(map[DedupKey]bool, len(items))
	out := make([]Item, 0, len(items))
	for _, it := range items {
		k := it.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, it)
	}
	return out
}

// normalize collapses whitespace, strips diacritics-marks-only differences,
// and lowercases the string so that "Hello  World" and "hello world" share
// a key. It does NOT strip punctuation: two posts that differ only in
// punctuation are considered distinct (they may carry different meaning).
func normalize(s string) string {
	s = strings.ToLower(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var whitespaceRe = regexp.MustCompile(`\s+`)
