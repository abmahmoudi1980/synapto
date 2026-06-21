package adminapi_test

import (
	"net/http"
	"testing"
)

// settingsJSON mirrors the on-the-wire shape of GET/PATCH /api/settings.
type settingsJSON struct {
	DigestIntervalSeconds   int     `json:"digest_interval_seconds"`
	TelegramBotTokenRef      string  `json:"telegram_bot_token_ref"`
	TelegramSubscriberChat   int64   `json:"telegram_subscriber_chat"`
	TelegramBotReachable     *bool   `json:"telegram_bot_reachable"`
	AIProvider               string  `json:"ai_provider"`
	AIModel                  string  `json:"ai_model"`
	AIBaseURL                string  `json:"ai_base_url"`
	AIAPIKeyRef              string  `json:"ai_api_key_ref"`
	AIReachable              *bool   `json:"ai_reachable"`
	UncategorizedLabel       string  `json:"uncategorized_label"`
	UpdatedAt                string  `json:"updated_at"`
}

func TestSettings_GetDefaults(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/settings")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Settings settingsJSON `json:"settings"`
	}
	decodeBody(t, res, &body)
	if body.Settings.DigestIntervalSeconds != 600 {
		t.Errorf("expected default interval 600, got %d", body.Settings.DigestIntervalSeconds)
	}
	if body.Settings.UncategorizedLabel != "Uncategorized" {
		t.Errorf("expected default uncategorized_label 'Uncategorized', got %q", body.Settings.UncategorizedLabel)
	}
	if body.Settings.AIProvider != "fake" {
		t.Errorf("expected default ai_provider 'fake', got %q", body.Settings.AIProvider)
	}
	if body.Settings.UpdatedAt == "" {
		t.Error("expected non-empty updated_at")
	}
}

func TestSettings_PatchInterval(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"digest_interval_seconds":300}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Settings settingsJSON `json:"settings"`
	}
	decodeBody(t, res, &body)
	if body.Settings.DigestIntervalSeconds != 300 {
		t.Errorf("expected interval 300, got %d", body.Settings.DigestIntervalSeconds)
	}
}

func TestSettings_PatchIntervalTooSmall(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"digest_interval_seconds":30}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "invalid_interval" {
		t.Errorf("expected invalid_interval, got %q", er.Error.Code)
	}
}

func TestSettings_PatchIntervalTooLarge(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"digest_interval_seconds":100000}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestSettings_PatchChatID(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"telegram_subscriber_chat":123456789}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Settings settingsJSON `json:"settings"`
	}
	decodeBody(t, res, &body)
	if body.Settings.TelegramSubscriberChat != 123456789 {
		t.Errorf("expected chat id 123456789, got %d", body.Settings.TelegramSubscriberChat)
	}
}

func TestSettings_PatchChatIDNegative(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"telegram_subscriber_chat":-1}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "invalid_chat_id" {
		t.Errorf("expected invalid_chat_id, got %q", er.Error.Code)
	}
}

func TestSettings_PatchUncategorizedLabel(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"uncategorized_label":"Other"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Settings settingsJSON `json:"settings"`
	}
	decodeBody(t, res, &body)
	if body.Settings.UncategorizedLabel != "Other" {
		t.Errorf("expected label 'Other', got %q", body.Settings.UncategorizedLabel)
	}
}

func TestSettings_PatchUncategorizedLabelEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings", `{"uncategorized_label":""}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestSettings_PatchUncategorizedLabelTooLong(t *testing.T) {
	ts, _ := newTestServer(t)
	long := make([]byte, 0, 50)
	for i := 0; i < 50; i++ {
		long = append(long, 'x')
	}
	body := `{"uncategorized_label":"` + string(long) + `"}`
	res := tsPATCH(t, ts, "/api/settings", body)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "name_too_long" {
		t.Errorf("expected name_too_long, got %q", er.Error.Code)
	}
}

func TestSettings_PatchCredentialsIgnored(t *testing.T) {
	// Credentials (ai_api_key_ref, telegram_bot_token_ref) are not settable
	// via PATCH per contracts/admin-api.md. The server should ignore them
	// silently and return 200.
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/settings",
		`{"ai_api_key_ref":"env:NEW_KEY","telegram_bot_token_ref":"env:NEW_TOKEN"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (silently ignored), got %d", res.StatusCode)
	}
}

func TestSettings_TestTelegram_Ok(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPOST(t, ts, "/api/settings/test-telegram", `{}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		OK  bool `json:"ok"`
		Bot struct {
			ID        int64  `json:"id"`
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
		} `json:"bot"`
	}
	decodeBody(t, res, &body)
	if !body.OK {
		t.Error("expected ok=true")
	}
}

func TestSettings_TestAI_Ok(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPOST(t, ts, "/api/settings/test-ai", `{}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		OK        bool   `json:"ok"`
		Model     string `json:"model"`
		LatencyMS int    `json:"latency_ms"`
	}
	decodeBody(t, res, &body)
	if !body.OK {
		t.Error("expected ok=true")
	}
	if body.Model == "" {
		t.Error("expected non-empty model")
	}
}
