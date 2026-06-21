package digest

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/synapto/assistant/internal/ai"
)

// RenderItem is one categorized, summarized item ready for rendering.
type RenderItem struct {
	Summary       string
	CategoryName  string
	CategoryOrder int
	ChannelHandle string
	MediaKind     ai.MediaKind
}

// RenderInput is the data handed to Render.
type RenderInput struct {
	WindowEnd     time.Time
	CycleID       string
	Items         []RenderItem
	Degraded      bool
	Uncategorized string // label used when CategoryName is empty
}

// maxSummaryRunes is the per-item summary cap from the contract.
const maxSummaryRunes = 280

// telegramHardLimit is Telegram's per-message character cap.
const telegramHardLimit = 4096

// telegramSoftLimit leaves headroom for header/footer/protocol overhead.
const telegramSoftLimit = 3900

// singleItemLimit is the cap for an oversized single item.
const singleItemLimit = 3500

// Render produces one or more Telegram message texts from the input.
// The first message carries the header; the last carries the footer.
// See contracts/telegram-render.md for the format and split rules.
func Render(in RenderInput) []string {
	if len(in.Items) == 0 {
		return nil
	}
	if in.Uncategorized == "" {
		in.Uncategorized = "Uncategorized"
	}

	// Group items by category in order.
	type catGroup struct {
		name  string
		order int
		items []RenderItem
	}
	cats := make(map[string]*catGroup)
	var catOrder []string
	for _, it := range in.Items {
		name := it.CategoryName
		if name == "" {
			name = in.Uncategorized
		}
		g, ok := cats[name]
		if !ok {
			g = &catGroup{name: name, order: it.CategoryOrder}
			cats[name] = g
			catOrder = append(catOrder, name)
		}
		g.items = append(g.items, it)
	}

	// Build a flat list of "lines" (category headers + item lines) so the
	// splitter can pack them greedily.
	type line struct {
		text string
	}
	var lines []line
	for _, name := range catOrder {
		g := cats[name]
		lines = append(lines, line{text: "# " + g.name})
		for _, it := range g.items {
			lines = append(lines, line{text: formatItemLine(it, in.Degraded)})
		}
		// Blank line between categories.
		lines = append(lines, line{text: ""})
	}
	// Trim trailing blank line.
	if len(lines) > 0 && lines[len(lines)-1].text == "" {
		lines = lines[:len(lines)-1]
	}

	header := formatHeader(in.WindowEnd, false)
	continuedHeader := formatHeader(in.WindowEnd, true)
	footer := formatFooter(in.CycleID, len(in.Items), in.Degraded)

	// Greedy pack into messages.
	var messages []string
	current := header + "\n\n"
	first := true
	for _, ln := range lines {
		piece := ln.text + "\n"
		// If a single item line is absurdly long, truncate it.
		if utf8.RuneCountInString(ln.text) > singleItemLimit && ln.text != "" && !strings.HasPrefix(ln.text, "# ") {
			truncated := truncateRunes(ln.text, singleItemLimit-1) + "…"
			piece = truncated + "\n"
		}
		if utf8.RuneCountInString(current)+utf8.RuneCountInString(piece) > telegramSoftLimit && current != header+"\n\n" {
			// Close current message, start a new one.
			messages = append(messages, strings.TrimRight(current, "\n"))
			current = continuedHeader + "\n\n"
			_ = first
		}
		current += piece
	}
	// Append footer to the last message.
	if current != header+"\n\n" {
		current += "\n" + footer
		messages = append(messages, strings.TrimRight(current, "\n"))
	}

	// Hard cap: if any message exceeds telegramHardLimit, split it harshly.
	for i, msg := range messages {
		if utf8.RuneCountInString(msg) > telegramHardLimit {
			messages[i] = truncateRunes(msg, telegramHardLimit)
		}
	}
	return messages
}

// formatHeader returns the top line of a digest message.
func formatHeader(windowEnd time.Time, continued bool) string {
	ts := windowEnd.UTC().Format("2006-01-02 15:04 UTC")
	if continued {
		return "📰 News digest (continued) — " + ts
	}
	return "📰 News digest — " + ts
}

// formatFooter returns the closing line of a digest message.
func formatFooter(cycleID string, itemCount int, degraded bool) string {
	short := cycleID
	if len(short) > 8 {
		short = short[:8]
	}
	status := "ok"
	if degraded {
		status = "degraded (AI unavailable)"
	}
	return "— cycle " + short + " · " + itoa(itemCount) + " items · " + status
}

// formatItemLine renders one bullet line: "• <summary>  _(handle)_" or
// "⚠️ <summary>  _(handle)_" in degraded mode. The handle suffix is fully
// escaped (including the wrapping _ and parens) so it renders as literal
// text in Telegram MarkdownV2.
func formatItemLine(it RenderItem, degraded bool) string {
	summary := cleanSummary(it.Summary, it.MediaKind)
	bullet := "• "
	if degraded {
		bullet = "⚠️ "
	}
	return bullet + summary + "  \\_\\(" + escapeHandle(it.ChannelHandle) + "\\)"
}

// cleanSummary applies the summary rules from the contract:
// - single line (collapse newlines)
// - trim
// - truncate to maxSummaryRunes
// - prefix with [MediaKind] for non-text items
// - escape MarkdownV2 special chars
func cleanSummary(s string, kind ai.MediaKind) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	s = collapseSpaces(s)
	if utf8.RuneCountInString(s) > maxSummaryRunes {
		s = truncateRunes(s, maxSummaryRunes-1) + "…"
	}
	if kind != ai.MediaText && kind != "" {
		prefix := "[" + mediaLabel(kind) + "] "
		s = prefix + s
	}
	return escapeMarkdownV2(s)
}

// mediaLabel returns the bracketed label for a non-text media kind.
func mediaLabel(k ai.MediaKind) string {
	switch k {
	case ai.MediaImage:
		return "Image"
	case ai.MediaVideo:
		return "Video"
	case ai.MediaVoice:
		return "Voice"
	default:
		return "Media"
	}
}

// escapeMarkdownV2 escapes the characters that Telegram MarkdownV2 treats
// as special: _ * [ ] ( ) ~ ` > # + - = | { } . !
// We use the ZWSP-before approach from the contract for ` and * in summary
// text (so they remain visible but don't trigger formatting), and the
// backslash escape for _ . ! and the others that would break the message.
func escapeMarkdownV2(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '_':
			b.WriteString(`\_`)
		case '*':
			b.WriteString("*\u200b")
		case '`':
			b.WriteString("`\u200b")
		case '[':
			b.WriteString(`\[`)
		case ']':
			b.WriteString(`\]`)
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		case '~':
			b.WriteString(`\~`)
		case '>':
			b.WriteString(`\>`)
		case '#':
			b.WriteString(`\#`)
		case '+':
			b.WriteString(`\+`)
		case '-':
			b.WriteString(`\-`)
		case '=':
			b.WriteString(`\=`)
		case '|':
			b.WriteString(`\|`)
		case '{':
			b.WriteString(`\{`)
		case '}':
			b.WriteString(`\}`)
		case '.':
			b.WriteString(`\.`)
		case '!':
			b.WriteString(`\!`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeHandle lowercases and escapes the handle for the _(handle)_ suffix.
func escapeHandle(h string) string {
	return escapeMarkdownV2(strings.ToLower(strings.TrimPrefix(h, "@")))
}

func collapseSpaces(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
