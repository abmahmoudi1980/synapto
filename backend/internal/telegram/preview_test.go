package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const samplePage = `<!doctype html>
<html>
<head>
  <meta property="og:title" content="Tweety"/>
  <meta property="og:description" content="A test channel."/>
</head>
<body>
  <main>
    <section>
      <div class="tgme_widget_message_wrap js-widget_message_wrap">
        <div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="TweetyChannel/300">
          <div class="tgme_widget_message_bubble">
            <div class="tgme_widget_message_author accent_color"><a><span>Tweety</span></a></div>
            <div class="tgme_widget_message_text js-message_text" dir="auto">Latest post — about something interesting.<br/>A second line.</div>
            <div class="tgme_widget_message_footer compact js-message_footer">
              <time datetime="2026-06-22T02:08:21+00:00" class="time">02:08</time>
            </div>
          </div>
        </div>
      </div>
      <div class="tgme_widget_message_wrap js-widget_message_wrap">
        <div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="TweetyChannel/200">
          <div class="tgme_widget_message_bubble">
            <div class="tgme_widget_message_author accent_color"><a><span>Tweety</span></a></div>
            <div class="tgme_widget_message_text js-message_text" dir="auto">Middle post with <a href="https://example.com">a link</a> inside.</div>
            <div class="tgme_widget_message_footer compact js-message_footer">
              <time datetime="2026-06-22T01:00:00+00:00" class="time">01:00</time>
            </div>
          </div>
        </div>
      </div>
      <div class="tgme_widget_message_wrap js-widget_message_wrap">
        <div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="TweetyChannel/100">
          <div class="tgme_widget_message_bubble">
            <div class="tgme_widget_message_photo" style="padding-top:50%"></div>
            <div class="tgme_widget_message_footer compact js-message_footer">
              <time datetime="2026-06-22T00:00:00+00:00" class="time">00:00</time>
            </div>
          </div>
        </div>
      </div>
      <div class="tgme_widget_message_centered js-messages_more_wrap">
        <a href="/s/TweetyChannel?before=100" class="tme_messages_more js-messages_more" data-before="100"></a>
      </div>
    </section>
  </div>
</main>
</body>
</html>
`

const olderPage = `<!doctype html>
<html>
<body>
  <main>
    <section>
      <div class="tgme_widget_message_wrap js-widget_message_wrap">
        <div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="TweetyChannel/050">
          <div class="tgme_widget_message_bubble">
            <div class="tgme_widget_message_text js-message_text" dir="auto">Oldest post in our test set.</div>
            <div class="tgme_widget_message_footer compact js-message_footer">
              <time datetime="2026-06-21T22:00:00+00:00" class="time">22:00</time>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</main>
</body>
</html>
`

func TestHTTPPreview_FetchNewPosts_Pagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// handle is normalized to lowercase; the samplePage IDs use
		// "TweetyChannel" but the URL path is the lowercased handle.
		if r.URL.Path != "/s/tweetychannel" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("before") == "100" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(olderPage))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(samplePage))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 0 // no throttle in tests

	// sinceMsgID = 100 means we want only posts > 100.
	posts, err := c.FetchNewPosts(context.Background(), "TweetyChannel", 100)
	if err != nil {
		t.Fatalf("FetchNewPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d: %+v", len(posts), posts)
	}
	if posts[0].MessageID != 200 || posts[1].MessageID != 300 {
		t.Fatalf("expected ascending order 200,300; got %d,%d", posts[0].MessageID, posts[1].MessageID)
	}
	if posts[0].MediaKind != "text" {
		t.Fatalf("post 200 should be text, got %q", posts[0].MediaKind)
	}
	if !strings.Contains(posts[0].Text, "a link (https://example.com)") {
		t.Fatalf("post 200 should contain normalized link, got %q", posts[0].Text)
	}
	if posts[1].Text == "" {
		t.Fatalf("post 300 should have text, got empty")
	}
}

func TestHTTPPreview_FetchNewPosts_BoundaryStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(samplePage))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 0

	// sinceMsgID = 200: should include post 300 but stop at 200 (boundary).
	posts, err := c.FetchNewPosts(context.Background(), "TweetyChannel", 200)
	if err != nil {
		t.Fatalf("FetchNewPosts: %v", err)
	}
	if len(posts) != 1 || posts[0].MessageID != 300 {
		t.Fatalf("expected [300], got %+v", posts)
	}
}

func TestHTTPPreview_FetchNewPosts_MediaOnly(t *testing.T) {
	page := `<!doctype html><html><body><main><section>
      <div class="tgme_widget_message_wrap js-widget_message_wrap">
        <div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="X/42">
          <div class="tgme_widget_message_bubble">
            <div class="tgme_widget_message_photo" style="padding-top:50%"></div>
            <div class="tgme_widget_message_footer compact js-message_footer">
              <time datetime="2026-06-22T00:00:00+00:00" class="time">00:00</time>
            </div>
          </div>
        </div>
      </div>
    </section></main></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 0

	posts, err := c.FetchNewPosts(context.Background(), "X", 0)
	if err != nil {
		t.Fatalf("FetchNewPosts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].MediaKind != "image" {
		t.Fatalf("expected media=image, got %q", posts[0].MediaKind)
	}
	if posts[0].Text != "" {
		t.Fatalf("media-only post should have empty text, got %q", posts[0].Text)
	}
}

func TestHTTPPreview_GetChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(samplePage))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 0

	info, err := c.GetChat(context.Background(), "TweetyChannel")
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if info.Username != "tweetychannel" {
		t.Fatalf("Username: got %q", info.Username)
	}
	if info.Title != "Tweety" {
		t.Fatalf("Title: got %q", info.Title)
	}
	if info.Description != "A test channel." {
		t.Fatalf("Description: got %q", info.Description)
	}
}

func TestHTTPPreview_GetChat_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 0

	_, err := c.GetChat(context.Background(), "missing")
	if err != ErrChannelNotFound {
		t.Fatalf("expected ErrChannelNotFound, got %v", err)
	}
}

func TestHTTPPreview_SendMessage(t *testing.T) {
	var gotPath string
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotForm = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", "https://t.me", srv.URL)
	res, err := c.SendMessage(context.Background(), 42, "hello", "MarkdownV2")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if res.MessageID != 7 || !res.OK {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.HasSuffix(gotPath, "/sendMessage") {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if !strings.Contains(gotForm, "chat_id=42") || !strings.Contains(gotForm, "text=hello") || !strings.Contains(gotForm, "parse_mode=MarkdownV2") {
		t.Fatalf("form did not include expected fields: %q", gotForm)
	}
}

func TestHTTPPreview_SendMessage_Blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", "https://t.me", srv.URL)
	_, err := c.SendMessage(context.Background(), 42, "hi", "")
	if err != ErrBlocked {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}

func TestHTTPPreview_Throttle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(samplePage))
	}))
	defer srv.Close()

	c := NewHTTPPreviewWithBases("TOKEN", srv.URL, srv.URL)
	c.minInterval = 50 * time.Millisecond

	start := time.Now()
	_, _ = c.FetchNewPosts(context.Background(), "TweetyChannel", 0)
	_, _ = c.FetchNewPosts(context.Background(), "TweetyChannel", 0)
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("expected at least one throttle delay, elapsed=%v", elapsed)
	}
}
