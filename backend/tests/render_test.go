package digest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/digest"
)

func TestRender_SingleMessageTwoCategories(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "8a3f1c20-1234-5678-9abc-def012345678",
		Uncategorized: "Uncategorized",
		Items: []digest.RenderItem{
			{Summary: "Telegram rolls out scheduled messages in channels", CategoryName: "Technology", CategoryOrder: 0, ChannelHandle: "telegram", MediaKind: ai.MediaText},
			{Summary: "A new open-source LLM beats GPT-4 on a public benchmark", CategoryName: "Technology", CategoryOrder: 0, ChannelHandle: "ml_news", MediaKind: ai.MediaText},
			{Summary: "EU parliament passes the AI Liability Directive", CategoryName: "Politics", CategoryOrder: 1, ChannelHandle: "eu_updates", MediaKind: ai.MediaText},
		},
	}
	msgs := digest.Render(in)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	want := `📰 News digest — 2026\-06\-21 07:20 UTC

\# Technology
• Telegram rolls out scheduled messages in channels  \_\(telegram\)
• A new open\-source LLM beats GPT\-4 on a public benchmark  \_\(ml\_news\)

\# Politics
• EU parliament passes the AI Liability Directive  \_\(eu\_updates\)

— cycle 8a3f1c20 · 3 items · ok`
	if msgs[0] != want {
		t.Errorf("render mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, msgs[0])
	}
}

func TestRender_EmptyItemsReturnsNil(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd: time.Now().UTC(),
		CycleID:   "abc",
	}
	if msgs := digest.Render(in); msgs != nil {
		t.Errorf("expected nil for empty items, got %v", msgs)
	}
}

func TestRender_DegradedMode(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "8a3f1c20abcd",
		Degraded:      true,
		Uncategorized: "Uncategorized",
		Items: []digest.RenderItem{
			{Summary: "Raw headline text here", CategoryName: "Technology", CategoryOrder: 0, ChannelHandle: "telegram", MediaKind: ai.MediaText},
		},
	}
	msgs := digest.Render(in)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.HasPrefix(msgs[0], "📰 News digest —") {
		t.Errorf("missing header: %s", msgs[0])
	}
	if !strings.Contains(msgs[0], "⚠️") {
		t.Errorf("degraded mode should use ⚠️ bullet: %s", msgs[0])
	}
	if !strings.Contains(msgs[0], `degraded \(AI unavailable\)`) {
		t.Errorf("footer should mention degraded: %s", msgs[0])
	}
}

func TestRender_SplitAcrossMessages(t *testing.T) {
	// Create enough items to force a split. Each item line is ~100 chars;
	// with the 3900-char soft limit we need ~40+ items.
	var items []digest.RenderItem
	for i := 0; i < 50; i++ {
		items = append(items, digest.RenderItem{
			Summary:       strings.Repeat("x", 80) + " end",
			CategoryName:  "Tech",
			CategoryOrder: 0,
			ChannelHandle: "chan",
			MediaKind:     ai.MediaText,
		})
	}
	in := digest.RenderInput{
		WindowEnd:     time.Now().UTC(),
		CycleID:       "split123",
		Uncategorized: "Uncategorized",
		Items:         items,
	}
	msgs := digest.Render(in)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages for 50 long items, got %d", len(msgs))
	}
	// First message has the header, last has the footer.
	if !strings.HasPrefix(msgs[0], "📰 News digest —") {
		t.Errorf("first message missing header")
	}
	if strings.Contains(msgs[0], "— cycle") {
		t.Errorf("first message should not have footer")
	}
	if !strings.HasPrefix(msgs[1], "📰 News digest (continued) —") {
		t.Errorf("second message should have continued header: %s", msgs[1][:50])
	}
	// Last message has the footer.
	last := msgs[len(msgs)-1]
	if !strings.Contains(last, "— cycle split123") {
		t.Errorf("last message missing footer: %s", last)
	}
}

func TestRender_NonTextItemPrefix(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "media1234",
		Uncategorized: "Uncategorized",
		Items: []digest.RenderItem{
			{Summary: "Nice sunset", CategoryName: "Other", CategoryOrder: 0, ChannelHandle: "photo_chan", MediaKind: ai.MediaImage},
		},
	}
	msgs := digest.Render(in)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0], "\\[Image\\] Nice sunset") {
		t.Errorf("image item should have [Image] prefix (escaped): %s", msgs[0])
	}
}

func TestRender_UncategorizedFallback(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "unc12345",
		Uncategorized: "Other",
		Items: []digest.RenderItem{
			{Summary: "Some news", CategoryName: "", CategoryOrder: 0, ChannelHandle: "chan"},
		},
	}
	msgs := digest.Render(in)
	if !strings.Contains(msgs[0], "# Other") {
		t.Errorf("empty category should fall back to uncategorized label: %s", msgs[0])
	}
}

func TestRender_SummaryTruncation(t *testing.T) {
	longSummary := strings.Repeat("a", 300) // 300 chars, exceeds 280 limit
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "trunc1234",
		Uncategorized: "Uncategorized",
		Items: []digest.RenderItem{
			{Summary: longSummary, CategoryName: "Tech", ChannelHandle: "chan"},
		},
	}
	msgs := digest.Render(in)
	// The summary should be truncated; the "…" should appear.
	if !strings.Contains(msgs[0], "…") {
		t.Errorf("long summary should be truncated with ellipsis")
	}
}

func TestRender_NewlinesInSummaryCollapsed(t *testing.T) {
	in := digest.RenderInput{
		WindowEnd:     time.Date(2026, 6, 21, 7, 20, 0, 0, time.UTC),
		CycleID:       "nl123456",
		Uncategorized: "Uncategorized",
		Items: []digest.RenderItem{
			{Summary: "Line one\nLine two\nLine three", CategoryName: "Tech", ChannelHandle: "chan"},
		},
	}
	msgs := digest.Render(in)
	// The item line should be on one line (no embedded newlines in the summary).
	for _, line := range strings.Split(msgs[0], "\n") {
		if strings.Contains(line, "Line one") {
			if strings.Contains(line, "Line two") && strings.Contains(line, "Line three") {
				return // pass: all three on the same line
			}
		}
	}
	t.Errorf("newlines in summary should be collapsed to spaces: %s", msgs[0])
}
