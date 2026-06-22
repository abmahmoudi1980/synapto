package adminapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
)

func TestPosts_ListEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/posts")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Posts []json.RawMessage `json:"posts"`
		Count int               `json:"count"`
	}
	decodeBody(t, res, &body)
	if body.Count != 0 || len(body.Posts) != 0 {
		t.Errorf("expected 0 posts, got count=%d", body.Count)
	}
}

func TestPosts_GetNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/posts/nonexistent-id")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "post_not_found" {
		t.Errorf("expected post_not_found, got %q", er.Error.Code)
	}
}

func TestPosts_ListAndGetSeededPost(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "seed_chan", "Seed")
	ps := sqlite.PostStore{S: st}
	post, created, err := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 1234,
		Link:        "https://t.me/seed_chan/1234",
		RawText:     "first text",
		MediaKind:   store.MediaText,
		Status:      store.PostReceived,
	})
	if err != nil || !created {
		t.Fatalf("upsert: err=%v created=%v", err, created)
	}

	// GET /api/posts
	res := tsGET(t, ts, "/api/posts")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var list struct {
		Posts []struct {
			ID            string `json:"id"`
			Link          string `json:"link"`
			SourceMsgID   int64  `json:"source_msg_id"`
			Status        string `json:"status"`
			ChannelHandle *string `json:"channel_handle"`
		} `json:"posts"`
		Count int `json:"count"`
	}
	decodeBody(t, res, &list)
	if list.Count != 1 || len(list.Posts) != 1 {
		t.Fatalf("expected 1 post, got count=%d", list.Count)
	}
	if list.Posts[0].ID != post.ID {
		t.Errorf("id mismatch: %q vs %q", list.Posts[0].ID, post.ID)
	}
	if list.Posts[0].Status != "received" {
		t.Errorf("status: got %q", list.Posts[0].Status)
	}
	if list.Posts[0].Link != "https://t.me/seed_chan/1234" {
		t.Errorf("link: got %q", list.Posts[0].Link)
	}
	if list.Posts[0].ChannelHandle == nil || *list.Posts[0].ChannelHandle != "seed_chan" {
		t.Errorf("channel_handle: got %v", list.Posts[0].ChannelHandle)
	}

	// GET /api/posts/{id}
	res2 := tsGET(t, ts, "/api/posts/"+post.ID)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res2.StatusCode)
	}
	var single struct {
		Post struct {
			ID            string `json:"id"`
			SourceMsgID   int64  `json:"source_msg_id"`
			RawText       string `json:"raw_text"`
			MediaKind     string `json:"media_kind"`
			Status        string `json:"status"`
		} `json:"post"`
	}
	decodeBody(t, res2, &single)
	if single.Post.ID != post.ID {
		t.Errorf("id mismatch: %q vs %q", single.Post.ID, post.ID)
	}
	if single.Post.RawText != "first text" {
		t.Errorf("raw_text: got %q", single.Post.RawText)
	}
	if single.Post.MediaKind != "text" {
		t.Errorf("media_kind: got %q", single.Post.MediaKind)
	}
}

func TestPosts_ListByStatusFilter(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "filt_chan", "Filt")
	ps := sqlite.PostStore{S: st}
	// Two posts: one stays 'received', one is moved to 'sent'.
	p1, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 1, RawText: "a", Status: store.PostReceived,
	})
	p2, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 2, RawText: "b", Status: store.PostReceived,
	})
	_ = ps.MarkSummarized(ctx, p2.ID, "", "summary", 0.9)
	_ = ps.MarkIncluded(ctx, []string{p2.ID})
	_ = ps.MarkSent(ctx, p2.ID, 555)
	_ = p1

	// All
	res := tsGET(t, ts, "/api/posts")
	var all struct {
		Count int `json:"count"`
	}
	decodeBody(t, res, &all)
	if all.Count != 2 {
		t.Errorf("all: expected 2, got %d", all.Count)
	}

	// Filter: status=sent
	res = tsGET(t, ts, "/api/posts?status=sent")
	var sent struct {
		Posts []struct {
			Status string `json:"status"`
		} `json:"posts"`
		Count int `json:"count"`
	}
	decodeBody(t, res, &sent)
	if sent.Count != 1 {
		t.Errorf("sent: expected 1, got %d", sent.Count)
	}
	if len(sent.Posts) > 0 && sent.Posts[0].Status != "sent" {
		t.Errorf("status: got %q", sent.Posts[0].Status)
	}

	// Filter: status=received
	res = tsGET(t, ts, "/api/posts?status=received")
	var recv struct {
		Count int `json:"count"`
	}
	decodeBody(t, res, &recv)
	if recv.Count != 1 {
		t.Errorf("received: expected 1, got %d", recv.Count)
	}
}
