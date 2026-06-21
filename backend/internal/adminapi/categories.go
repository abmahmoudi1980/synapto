package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/synapto/assistant/internal/store"
)

// categoryJSON is the API response shape for one category, per
// contracts/admin-api.md.
type categoryJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Ordering  int    `json:"ordering"`
	IsDefault bool   `json:"is_default"`
}

func categoryToJSON(c store.Category) categoryJSON {
	return categoryJSON{
		ID:        c.ID,
		Name:      c.Name,
		Ordering:  c.Ordering,
		IsDefault: c.IsDefault,
	}
}

// handleListCategories: GET /api/categories
func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.deps.Categories.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	out := make([]categoryJSON, 0, len(cats))
	for _, c := range cats {
		out = append(out, categoryToJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": out})
}

// handleAddCategory: POST /api/categories
func (s *Server) handleAddCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), "name")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "category name must not be empty", "name")
		return
	}
	if len(name) > 40 {
		writeError(w, http.StatusBadRequest, "name_too_long", "category name must be at most 40 characters", "name")
		return
	}

	c, err := s.deps.Categories.Add(r.Context(), name)
	if err != nil {
		if isDuplicate(err) {
			writeError(w, http.StatusConflict, "duplicate_name", "a category with this name already exists", "name")
			return
		}
		if strings.Contains(err.Error(), "at most 40") {
			writeError(w, http.StatusBadRequest, "name_too_long", err.Error(), "name")
			return
		}
		if strings.Contains(err.Error(), "must not be empty") {
			writeError(w, http.StatusBadRequest, "invalid_name", err.Error(), "name")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": categoryToJSON(c)})
}

// handlePatchCategory: PATCH /api/categories/{id}
func (s *Server) handlePatchCategory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "category id is required", "id")
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), "name")
		return
	}
	if req.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "name is required", "name")
		return
	}
	newName := strings.TrimSpace(*req.Name)
	if newName == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "category name must not be empty", "name")
		return
	}
	if len(newName) > 40 {
		writeError(w, http.StatusBadRequest, "name_too_long", "category name must be at most 40 characters", "name")
		return
	}

	c, err := s.deps.Categories.Rename(r.Context(), id, newName)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "category_not_found", "category not found", "id")
		case isDuplicate(err):
			writeError(w, http.StatusConflict, "duplicate_name", "a category with this name already exists", "name")
		case strings.Contains(err.Error(), "at most 40"):
			writeError(w, http.StatusBadRequest, "name_too_long", err.Error(), "name")
		case strings.Contains(err.Error(), "must not be empty"):
			writeError(w, http.StatusBadRequest, "invalid_name", err.Error(), "name")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": categoryToJSON(c)})
}

// handleDeleteCategory: DELETE /api/categories/{id}
func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "category id is required", "id")
		return
	}
	err := s.deps.Categories.Remove(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "category_not_found", "category not found", "id")
		case errors.Is(err, store.ErrCannotRemoveDefault):
			writeError(w, http.StatusConflict, "cannot_remove_default",
				"cannot remove a default category; rename it instead", "id")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// registerCategoryRoutes wires the category endpoints onto the /api router.
func (s *Server) registerCategoryRoutes(r chi.Router) {
	r.Get("/categories", s.handleListCategories)
	r.Post("/categories", s.handleAddCategory)
	r.Patch("/categories/{id}", s.handlePatchCategory)
	r.Delete("/categories/{id}", s.handleDeleteCategory)
}
