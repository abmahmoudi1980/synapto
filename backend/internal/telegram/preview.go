package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPPreview implements Client by reading public Telegram channels via the
// public web preview at t.me/s/<handle>. The bot is NOT required to be a
// member of the channel. SendMessage is delivered through the standard
// Bot API (HTTPS POST) using the configured token.
//
// The web preview is an unofficial surface; t.me may change its markup or
// rate-limit. This client is rate-limited (1 request per handle per second)
// and parses defensively: any structural change that breaks the regex
// yields zero posts rather than a crash.
//
// Cursor: the per-channel `last_seen_msg_id` in the DB is the high-water
// mark. FetchNewPosts walks preview pages from newest to oldest and returns
// every post with MessageID > sinceMsgID in ascending order.
type HTTPPreview struct {
	previewBase string // e.g. "https://t.me"
	apiBase     string // e.g. "https://api.telegram.org"
	botToken    string
	httpc       *http.Client

	// Per-handle rate limiter: ensure we wait at least minInterval between
	// successive fetches of the same handle.
	mu          sync.Mutex
	lastFetch   map[string]time.Time
	minInterval time.Duration
}

// NewHTTPPreview constructs a preview client with the given bot token.
// The token is only used for SendMessage; reads work without the bot being
// a member of any channel.
func NewHTTPPreview(botToken string) *HTTPPreview {
	return &HTTPPreview{
		previewBase: "https://t.me",
		apiBase:     "https://api.telegram.org",
		botToken:    botToken,
		httpc: &http.Client{
			Timeout: 15 * time.Second,
		},
		lastFetch:   make(map[string]time.Time),
		minInterval: 1 * time.Second,
	}
}

// NewHTTPPreviewWithBases is like NewHTTPPreview but lets callers (mainly
// tests) override the preview and API base URLs.
func NewHTTPPreviewWithBases(botToken, previewBase, apiBase string) *HTTPPreview {
	c := NewHTTPPreview(botToken)
	if previewBase != "" {
		c.previewBase = strings.TrimRight(previewBase, "/")
	}
	if apiBase != "" {
		c.apiBase = strings.TrimRight(apiBase, "/")
	}
	return c
}

// throttle sleeps until at least minInterval has passed since the last
// fetch of this handle.
func (c *HTTPPreview) throttle(handle string) {
	c.mu.Lock()
	last := c.lastFetch[handle]
	c.mu.Unlock()
	if !last.IsZero() {
		if d := c.minInterval - time.Since(last); d > 0 {
			time.Sleep(d)
		}
	}
	c.mu.Lock()
	c.lastFetch[handle] = time.Now()
	c.mu.Unlock()
}

// fetchPage GETs one preview page for handle. before=0 returns the newest
// page; before>0 returns the page of posts older than that id.
func (c *HTTPPreview) fetchPage(ctx context.Context, handle string, before int64) (string, error) {
	c.throttle(handle)
	u := c.previewBase + "/s/" + handle
	if before > 0 {
		u += "?before=" + strconv.FormatInt(before, 10)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SynaptoAssistant/0.1)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("http preview: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("http preview: read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrChannelNotFound
	}
	if resp.StatusCode >= 500 {
		return "", ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http preview: status %d", resp.StatusCode)
	}
	return string(body), nil
}

// previewPost is a parsed post from a preview page.
type previewPost struct {
	id      int64
	when    time.Time
	text    string
	media   string
	hasText bool
}

// postWrapStartRe matches the opening tag of a post wrap. We use it to
// split the page into wrap blocks without relying on regex lookahead
// (Go's RE2 doesn't support (?= ... )).
var postWrapStartRe = regexp.MustCompile(`<div class="tgme_widget_message_wrap[^"]*"[^>]*>`)

// splitWraps returns the inner content of each post wrap on the page,
// in the order they appear in the HTML (newest first). It locates each
// wrap-start tag and slices the HTML from that tag to the next wrap-start
// (or </main>), then strips the leading open tag.
func splitWraps(html string) []string {
	idxs := postWrapStartRe.FindAllStringIndex(html, -1)
	if len(idxs) == 0 {
		return nil
	}
	endAnchor := strings.Index(html, "</main>")
	out := make([]string, 0, len(idxs))
	for i, m := range idxs {
		openEnd := m[1]
		var blockEnd int
		if i+1 < len(idxs) {
			blockEnd = idxs[i+1][0]
		} else if endAnchor >= 0 {
			blockEnd = endAnchor
		} else {
			blockEnd = len(html)
		}
		// Inner content = between the open tag's `>` and the next wrap / </main>.
		if blockEnd > openEnd {
			out = append(out, html[openEnd:blockEnd])
		}
	}
	return out
}

var (
	dataPostRe = regexp.MustCompile(`data-post="[^/]+/(\d+)"`)
	dateTimeRe = regexp.MustCompile(`datetime="([^"]+)"`)
	// textRe matches the text container. The content stops at the next
	// <div class="tgme_widget_message_footer which always follows the
	// text node in t.me's markup.
	textRe  = regexp.MustCompile(`(?s)js-message_text[^>]*">(.+?)<div class="tgme_widget_message_footer`)
	mediaRe = regexp.MustCompile(`tgme_widget_message_(photo|video)`)
)

// parsePage returns posts on a page in descending id order (newest first,
// matching the HTML's order).
func parsePage(html string) []previewPost {
	wraps := splitWraps(html)
	out := make([]previewPost, 0, len(wraps))
	for _, inner := range wraps {
		idM := dataPostRe.FindStringSubmatch(inner)
		if idM == nil {
			continue
		}
		id, err := strconv.ParseInt(idM[1], 10, 64)
		if err != nil {
			continue
		}
		pp := previewPost{id: id}

		if dtM := dateTimeRe.FindStringSubmatch(inner); dtM != nil {
			if t, err := time.Parse(time.RFC3339, dtM[1]); err == nil {
				pp.when = t
			}
		}
		if pp.when.IsZero() {
			pp.when = time.Now().UTC()
		}

		if tM := textRe.FindStringSubmatch(inner); tM != nil {
			pp.text = cleanPreviewText(tM[1])
			pp.hasText = true
		}
		if mM := mediaRe.FindStringSubmatch(inner); mM != nil {
			pp.media = mM[1]
		}
		out = append(out, pp)
	}
	return out
}

// loadMoreRe detects the "show older posts" link in the page. Telegram
// embeds it as <a href="/s/<handle>?before=<id>" class="tme_messages_more">
var loadMoreRe = regexp.MustCompile(`class="tme_messages_more[^"]*"[^>]*data-before="(\d+)"`)

// cleanPreviewText normalizes the raw text node: HTML entities decoded,
// <br> -> newline, <a> -> "label (url)" form, all other tags stripped,
// whitespace collapsed.
func cleanPreviewText(s string) string {
	// <br> -> newline (handle <br/>, <br />, <br>).
	brRe := regexp.MustCompile(`<br\s*/?>`)
	s = brRe.ReplaceAllString(s, "\n")
	// <a href="URL">label</a> -> "label (URL)" so the summarizer sees
	// the link text inline.
	aRe := regexp.MustCompile(`<a [^>]*href="([^"]+)"[^>]*>([^<]*)</a>`)
	s = aRe.ReplaceAllString(s, "$2 ($1)")
	// Strip remaining tags.
	tagRe := regexp.MustCompile(`<[^>]+>`)
	s = tagRe.ReplaceAllString(s, "")
	// HTML entities.
	s = strings.NewReplacer(
		"&amp;", "&",
		"&quot;", `"`,
		"&#39;", "'",
		"&lt;", "<",
		"&gt;", ">",
	).Replace(s)
	// Collapse 3+ blank lines to 2.
	wsRe := regexp.MustCompile(`\n{3,}`)
	s = wsRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// GetChat resolves a public channel by its handle. Returns ChannelInfo
// populated from the page's og:title and og:description meta tags, or
// from the first post's author block if the meta tags are absent.
func (c *HTTPPreview) GetChat(ctx context.Context, handle string) (ChannelInfo, error) {
	h := normalizeHandle(handle)
	html, err := c.fetchPage(ctx, h, 0)
	if err != nil {
		return ChannelInfo{}, err
	}
	info := ChannelInfo{Username: h, ID: 0}
	if m := regexp.MustCompile(`<meta property="og:title" content="([^"]+)"`).FindStringSubmatch(html); m != nil {
		info.Title = htmlUnescape(m[1])
	}
	if m := regexp.MustCompile(`<meta property="og:description" content="([^"]+)"`).FindStringSubmatch(html); m != nil {
		info.Description = htmlUnescape(m[1])
	}
	if info.Title == "" {
		// Fall back to the first post's author name.
		if m := regexp.MustCompile(`tgme_widget_message_author[^>]*>\s*<a[^>]*>\s*<span[^>]*>([^<]+)</span>`).FindStringSubmatch(html); m != nil {
			info.Title = htmlUnescape(strings.TrimSpace(m[1]))
		}
	}
	if info.Title == "" {
		info.Title = h
	}
	return info, nil
}

func htmlUnescape(s string) string {
	s = strings.NewReplacer(
		"&amp;", "&", "&quot;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">",
		"&nbsp;", " ", "&#x27;", "'", "&#x2F;", "/",
	).Replace(s)
	// Strip emoji <i class="emoji" ...><b>?</b></i> wrappers — the
	// unicode replacement char is OK to keep; Telegram clients render it
	// from the URL on the <i> element.
	s = regexp.MustCompile(`<i class="emoji"[^>]*><b>[^<]*</b></i>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	return s
}

// FetchNewPosts returns posts in `handle` with MessageID > sinceMsgID in
// ascending order, by walking preview pages newest-to-oldest. Stops as
// soon as it sees a post with MessageID <= sinceMsgID (the boundary).
func (c *HTTPPreview) FetchNewPosts(ctx context.Context, handle string, sinceMsgID int64) ([]Post, error) {
	h := normalizeHandle(handle)
	var collected []Post
	before := int64(0)
	pages := 0
	const maxPages = 20 // safety cap: ~400 posts
	for {
		if pages >= maxPages {
			break
		}
		html, err := c.fetchPage(ctx, h, before)
		if err != nil {
			return collected, err
		}
		descPosts := parsePage(html) // descending id order
		if len(descPosts) == 0 {
			break
		}
		var anyNew bool
		for _, pp := range descPosts {
			if pp.id <= sinceMsgID {
				// Boundary reached. We're done; collected already
				// contains everything newer, in descending order.
				asc := reversePosts(collected)
				return asc, nil
			}
			anyNew = true
			text := pp.text
			media := "text"
			switch pp.media {
			case "photo":
				media = "image"
			case "video":
				media = "video"
			}
			if !pp.hasText {
				// Media-only post: surface an explicit marker so the
				// digest doesn't drop the item silently (FR-017).
				if media == "text" {
					media = "other"
				}
				text = ""
			}
			collected = append(collected, Post{
				ChannelID: 0, // not used by cycle; channel id is resolved by repo
				MessageID: pp.id,
				Text:      text,
				MediaKind: media,
				Captions:  nil,
				SentAt:    pp.when,
			})
		}
		if !anyNew {
			break
		}
		// Determine whether to paginate: the "load more" link's data-before
		// is the oldest id on this page.
		if m := loadMoreRe.FindStringSubmatch(html); m != nil {
			next, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil || next <= 0 {
				break
			}
			before = next
		} else {
			break
		}
		pages++
	}
	return reversePosts(collected), nil
}

func reversePosts(in []Post) []Post {
	out := make([]Post, len(in))
	for i, p := range in {
		out[len(in)-1-i] = p
	}
	return out
}

// SendMessage delivers one message to chatID via the Bot API. The bot
// is required (no preview fallback for sending). Honors parseMode values
// "" (plain), "MarkdownV2", and "HTML".
func (c *HTTPPreview) SendMessage(ctx context.Context, chatID int64, text, parseMode string) (SendResult, error) {
	if chatID == 0 {
		return SendResult{}, errors.New("telegram preview: chat id is 0")
	}
	form := url.Values{}
	form.Set("chat_id", strconv.FormatInt(chatID, 10))
	form.Set("text", text)
	if parseMode != "" {
		form.Set("parse_mode", parseMode)
	}
	endpoint := c.apiBase + "/bot" + c.botToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return SendResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("telegram preview: send: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return SendResult{}, fmt.Errorf("telegram preview: decode: %w (body=%s)", err, truncate(body, 200))
	}
	if !parsed.OK {
		desc := strings.ToLower(parsed.Description)
		if strings.Contains(desc, "blocked") || strings.Contains(desc, "forbidden") || strings.Contains(desc, "deactivated") || strings.Contains(desc, "chat not found") || strings.Contains(desc, "have no rights") {
			return SendResult{Blocked: true}, ErrBlocked
		}
		if strings.Contains(desc, "too many requests") || strings.Contains(desc, "retry after") {
			return SendResult{}, ErrUnavailable
		}
		return SendResult{}, fmt.Errorf("telegram preview: api error: %s", parsed.Description)
	}
	return SendResult{MessageID: parsed.Result.MessageID, OK: true}, nil
}

// Close is a no-op for HTTPPreview (no long-poll goroutine, no persistent
// connections to close). Kept for symmetry with the Client interface.
func (c *HTTPPreview) Close() error { return nil }

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
