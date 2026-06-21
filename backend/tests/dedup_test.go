package digest_test

import (
	"testing"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/digest"
)

func TestDedup_TextItems(t *testing.T) {
	items := []digest.Item{
		{ChannelID: "a", ChannelHandle: "chanA", SourceMsgID: 1, Text: "Hello world", MediaKind: ai.MediaText},
		{ChannelID: "b", ChannelHandle: "chanB", SourceMsgID: 2, Text: "hello   world", MediaKind: ai.MediaText},
		{ChannelID: "c", ChannelHandle: "chanC", SourceMsgID: 3, Text: "Different content", MediaKind: ai.MediaText},
	}
	got := digest.Dedup(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items after dedup, got %d: %+v", len(got), got)
	}
	if got[0].ChannelID != "a" {
		t.Errorf("first item should be from channel a, got %s", got[0].ChannelID)
	}
	if got[1].ChannelID != "c" {
		t.Errorf("second item should be from channel c, got %s", got[1].ChannelID)
	}
}

func TestDedup_MediaOnlyItems(t *testing.T) {
	items := []digest.Item{
		{ChannelID: "a", ChannelHandle: "chanA", SourceMsgID: 1, Text: "", MediaKind: ai.MediaImage, Captions: []string{"Sunset"}},
		{ChannelID: "b", ChannelHandle: "chanB", SourceMsgID: 2, Text: "", MediaKind: ai.MediaImage, Captions: []string{"sunset"}},
		{ChannelID: "c", ChannelHandle: "chanC", SourceMsgID: 3, Text: "", MediaKind: ai.MediaVideo, Captions: []string{"Sunset"}},
	}
	got := digest.Dedup(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items after dedup, got %d: %+v", len(got), got)
	}
	if got[0].ChannelID != "a" {
		t.Errorf("first should be channel a, got %s", got[0].ChannelID)
	}
	if got[1].ChannelID != "c" {
		t.Errorf("second should be channel c (different media kind), got %s", got[1].ChannelID)
	}
}

func TestDedup_PreservesOrder(t *testing.T) {
	items := []digest.Item{
		{ChannelID: "x", ChannelHandle: "x", SourceMsgID: 10, Text: "first", MediaKind: ai.MediaText},
		{ChannelID: "y", ChannelHandle: "y", SourceMsgID: 20, Text: "second", MediaKind: ai.MediaText},
		{ChannelID: "z", ChannelHandle: "z", SourceMsgID: 30, Text: "third", MediaKind: ai.MediaText},
	}
	got := digest.Dedup(items)
	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
	for i, want := range []string{"x", "y", "z"} {
		if got[i].ChannelID != want {
			t.Errorf("position %d: want %s, got %s", i, want, got[i].ChannelID)
		}
	}
}

func TestDedup_EmptyInput(t *testing.T) {
	got := digest.Dedup(nil)
	if len(got) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(got))
	}
}

func TestDedup_PunctuationDistinct(t *testing.T) {
	items := []digest.Item{
		{ChannelID: "a", ChannelHandle: "a", SourceMsgID: 1, Text: "Breaking: market crashes", MediaKind: ai.MediaText},
		{ChannelID: "b", ChannelHandle: "b", SourceMsgID: 2, Text: "Breaking market crashes", MediaKind: ai.MediaText},
	}
	got := digest.Dedup(items)
	if len(got) != 2 {
		t.Errorf("punctuation difference should keep both items, got %d", len(got))
	}
}

func TestItemKey_Deterministic(t *testing.T) {
	it1 := digest.Item{Text: "Hello World", MediaKind: ai.MediaText}
	it2 := digest.Item{Text: "  hello   world  ", MediaKind: ai.MediaText}
	if it1.Key() != it2.Key() {
		t.Errorf("normalized keys should match: %q vs %q", it1.Key(), it2.Key())
	}
}
