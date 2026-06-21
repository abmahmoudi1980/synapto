package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/store"
)

type categoryRow struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	Ordering  int    `db:"ordering"`
	IsDefault int    `db:"is_default"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

func (r categoryRow) toEntity() store.Category {
	return store.Category{
		ID:        r.ID,
		Name:      r.Name,
		Ordering:  r.Ordering,
		IsDefault: r.IsDefault == 1,
		CreatedAt: parseTimeStr(r.CreatedAt),
		UpdatedAt: parseTimeStr(r.UpdatedAt),
	}
}

// List returns categories ordered by ordering then name.
func (s *Store) ListCategories(ctx context.Context) ([]store.Category, error) {
	var rows []categoryRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM categories ORDER BY ordering ASC, name ASC`); err != nil {
		return nil, err
	}
	out := make([]store.Category, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntity())
	}
	return out, nil
}

// AddCategory inserts a custom (non-default) category.
func (s *Store) AddCategory(ctx context.Context, name string) (store.Category, error) {
	n := strings.TrimSpace(name)
	if err := validateCategoryName(n); err != nil {
		return store.Category{}, err
	}
	// Pick ordering = max+1.
	var maxOrder int
	_ = s.db.GetContext(ctx, &maxOrder, `SELECT COALESCE(MAX(ordering), -1) FROM categories`)
	now := time.Now().UTC()
	c := store.Category{
		ID:        uuid.NewString(),
		Name:      n,
		Ordering:  maxOrder + 1,
		IsDefault: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO categories (id, name, ordering, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)`,
		c.ID, c.Name, c.Ordering, timeStr(now), timeStr(now))
	if err != nil {
		return store.Category{}, err
	}
	return c, nil
}

// RenameCategory updates a category's name.
func (s *Store) RenameCategory(ctx context.Context, id, newName string) (store.Category, error) {
	n := strings.TrimSpace(newName)
	if err := validateCategoryName(n); err != nil {
		return store.Category{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE categories SET name = ?, updated_at = ? WHERE id = ?`,
		n, nowISO(), id)
	if err != nil {
		return store.Category{}, err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return store.Category{}, store.ErrNotFound
	}
	return s.GetCategory(ctx, id)
}

// GetCategory returns a category by id.
func (s *Store) GetCategory(ctx context.Context, id string) (store.Category, error) {
	var r categoryRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM categories WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Category{}, store.ErrNotFound
		}
		return store.Category{}, err
	}
	return r.toEntity(), nil
}

// RemoveCategory deletes a custom category. Refuses to delete defaults.
func (s *Store) RemoveCategory(ctx context.Context, id string) error {
	var isDefault int
	if err := s.db.GetContext(ctx, &isDefault, `SELECT is_default FROM categories WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}
		return err
	}
	if isDefault == 1 {
		return store.ErrCannotRemoveDefault
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	return err
}

// EnsureDefaults inserts any missing default categories. Idempotent.
func (s *Store) EnsureDefaults(ctx context.Context, defaults []string) error {
	for i, name := range defaults {
		now := time.Now().UTC()
		_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO categories
			(id, name, ordering, is_default, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)`,
			uuid.NewString(), name, i, timeStr(now), timeStr(now))
		if err != nil {
			return err
		}
	}
	return nil
}

// validateCategoryName enforces the rules from data-model.md.
func validateCategoryName(name string) error {
	if name == "" {
		return errors.New("category name must not be empty")
	}
	if len(name) > 40 {
		return errors.New("category name must be at most 40 characters")
	}
	return nil
}
