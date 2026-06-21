package adminapi_test

import (
	"net/http"
	"testing"
)

// TestCategories_ListDefaultsSeeded verifies that the seeded default
// categories (Politics, Technology, Business, Sports, World, Other) are
// present and ordered by ordering then name.
func TestCategories_ListDefaultsSeeded(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/categories")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Categories []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Ordering  int    `json:"ordering"`
			IsDefault bool   `json:"is_default"`
		} `json:"categories"`
	}
	decodeBody(t, res, &body)
	if len(body.Categories) != 6 {
		t.Fatalf("expected 6 default categories, got %d", len(body.Categories))
	}
	want := []string{"Politics", "Technology", "Business", "Sports", "World", "Other"}
	for i, c := range body.Categories {
		if c.Name != want[i] {
			t.Errorf("position %d: expected %q, got %q", i, want[i], c.Name)
		}
		if !c.IsDefault {
			t.Errorf("position %d (%s): expected is_default=true", i, c.Name)
		}
	}
}

// TestCategories_AddHappyPath verifies that a new custom category is created
// with is_default=false and the supplied name.
func TestCategories_AddHappyPath(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPOST(t, ts, "/api/categories", `{"name":"AI & ML"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Category struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"category"`
	}
	decodeBody(t, res, &body)
	if body.Category.Name != "AI & ML" {
		t.Errorf("expected name 'AI & ML', got %q", body.Category.Name)
	}
	if body.Category.IsDefault {
		t.Error("expected is_default=false for new custom category")
	}
	if body.Category.ID == "" {
		t.Error("expected non-empty id")
	}
}

// TestCategories_AddInvalidName covers the validation error codes from
// contracts/admin-api.md: empty name → invalid_name, > 40 chars → name_too_long.
func TestCategories_AddInvalidName(t *testing.T) {
	ts, _ := newTestServer(t)
	cases := []struct {
		body      string
		wantCode  string
		wantField string
	}{
		{`{"name":""}`, "invalid_name", "name"},
		{`{"name":"   "}`, "invalid_name", "name"},
		{`{"name":"` + stringRepeat("x", 41) + `"}`, "name_too_long", "name"},
	}
	for _, c := range cases {
		res := tsPOST(t, ts, "/api/categories", c.body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("body=%q expected 400, got %d", c.body, res.StatusCode)
			continue
		}
		var er struct {
			Error struct {
				Code  string `json:"code"`
				Field string `json:"field"`
			} `json:"error"`
		}
		decodeBody(t, res, &er)
		if er.Error.Code != c.wantCode {
			t.Errorf("body=%q expected code %q, got %q", c.body, c.wantCode, er.Error.Code)
		}
		if c.wantField != "" && er.Error.Field != c.wantField {
			t.Errorf("body=%q expected field %q, got %q", c.body, c.wantField, er.Error.Field)
		}
	}
}

// TestCategories_AddDuplicate verifies that a duplicate name returns 409.
func TestCategories_AddDuplicate(t *testing.T) {
	ts, _ := newTestServer(t)
	tsPOST(t, ts, "/api/categories", `{"name":"Crypto"}`)
	res := tsPOST(t, ts, "/api/categories", `{"name":"Crypto"}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "duplicate_name" {
		t.Errorf("expected duplicate_name, got %q", er.Error.Code)
	}
}

// TestCategories_RenameHappyPath verifies that PATCH updates the name and
// preserves is_default=true when renaming a default category.
func TestCategories_RenameHappyPath(t *testing.T) {
	ts, _ := newTestServer(t)
	// Get the id of the Politics default category.
	listRes := tsGET(t, ts, "/api/categories")
	var list struct {
		Categories []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"categories"`
	}
	decodeBody(t, listRes, &list)
	var id string
	for _, c := range list.Categories {
		if c.Name == "Politics" {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatal("could not find Politics category id")
	}

	res := tsPATCH(t, ts, "/api/categories/"+id, `{"name":"Policy"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Category struct {
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"category"`
	}
	decodeBody(t, res, &body)
	if body.Category.Name != "Policy" {
		t.Errorf("expected name 'Policy', got %q", body.Category.Name)
	}
	if !body.Category.IsDefault {
		t.Error("expected is_default=true after renaming a default")
	}
}

// TestCategories_RenameDuplicate verifies that renaming into a name that
// already exists returns 409 with duplicate_name.
func TestCategories_RenameDuplicate(t *testing.T) {
	ts, _ := newTestServer(t)
	// Add Crypto so the rename collides.
	tsPOST(t, ts, "/api/categories", `{"name":"Crypto"}`)
	// Rename Technology → Crypto (should conflict).
	listRes := tsGET(t, ts, "/api/categories")
	var list struct {
		Categories []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"categories"`
	}
	decodeBody(t, listRes, &list)
	var techID string
	for _, c := range list.Categories {
		if c.Name == "Technology" {
			techID = c.ID
		}
	}
	res := tsPATCH(t, ts, "/api/categories/"+techID, `{"name":"Crypto"}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.StatusCode)
	}
}

// TestCategories_RenameNotFound verifies that PATCH on a missing id returns 404.
func TestCategories_RenameNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPATCH(t, ts, "/api/categories/nonexistent-id", `{"name":"X"}`)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
}

// TestCategories_DeleteCustom verifies that a custom (non-default) category
// can be deleted with 204.
func TestCategories_DeleteCustom(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPOST(t, ts, "/api/categories", `{"name":"Crypto"}`)
	var body struct {
		Category struct {
			ID string `json:"id"`
		} `json:"category"`
	}
	decodeBody(t, res, &body)

	delRes := tsDELETE(t, ts, "/api/categories/"+body.Category.ID)
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRes.StatusCode)
	}

	// List should no longer include Crypto.
	listRes := tsGET(t, ts, "/api/categories")
	var list struct {
		Categories []struct {
			Name string `json:"name"`
		} `json:"categories"`
	}
	decodeBody(t, listRes, &list)
	for _, c := range list.Categories {
		if c.Name == "Crypto" {
			t.Error("Crypto should be removed from the list")
		}
	}
}

// TestCategories_DeleteDefaultRefused verifies that removing a default
// category returns 409 with cannot_remove_default.
func TestCategories_DeleteDefaultRefused(t *testing.T) {
	ts, _ := newTestServer(t)
	// Find the id of the Politics default category.
	listRes := tsGET(t, ts, "/api/categories")
	var list struct {
		Categories []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"categories"`
	}
	decodeBody(t, listRes, &list)
	var id string
	for _, c := range list.Categories {
		if c.Name == "Politics" {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatal("could not find Politics id")
	}

	delRes := tsDELETE(t, ts, "/api/categories/"+id)
	if delRes.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", delRes.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, delRes, &er)
	if er.Error.Code != "cannot_remove_default" {
		t.Errorf("expected cannot_remove_default, got %q", er.Error.Code)
	}
}

// TestCategories_DeleteNotFound verifies that DELETE on a missing id returns 404.
func TestCategories_DeleteNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsDELETE(t, ts, "/api/categories/nonexistent-id")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
}

// TestCategories_ChangeReflectsInNextDigest covers SC-006: after renaming a
// default category, the next digest should group items under the new heading.
// We exercise the contract by checking that the rename persisted and a fresh
// list shows the new name; the cycle-side wiring is covered by cycle tests.
func TestCategories_RenamePersistsAcrossRequests(t *testing.T) {
	ts, _ := newTestServer(t)
	// Add a custom category then rename it.
	res := tsPOST(t, ts, "/api/categories", `{"name":"OldName"}`)
	var body struct {
		Category struct {
			ID string `json:"id"`
		} `json:"category"`
	}
	decodeBody(t, res, &body)

	tsPATCH(t, ts, "/api/categories/"+body.Category.ID, `{"name":"NewName"}`)

	// List should now show NewName, not OldName.
	listRes := tsGET(t, ts, "/api/categories")
	var list struct {
		Categories []struct {
			Name string `json:"name"`
		} `json:"categories"`
	}
	decodeBody(t, listRes, &list)
	seenNew, seenOld := false, false
	for _, c := range list.Categories {
		if c.Name == "NewName" {
			seenNew = true
		}
		if c.Name == "OldName" {
			seenOld = true
		}
	}
	if !seenNew {
		t.Error("expected NewName in list after rename")
	}
	if seenOld {
		t.Error("did not expect OldName in list after rename")
	}
}

// stringRepeat returns s repeated n times. Used to build an overlong name.
func stringRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
