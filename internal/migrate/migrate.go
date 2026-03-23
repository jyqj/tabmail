package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var filePattern = regexp.MustCompile(`^(\d{3})_(.+?)(\.down)?\.sql$`)

type File struct {
	Version  int
	Name     string
	UpPath   string
	DownPath string
}

type StatusRow struct {
	Version   int
	Name      string
	Applied   bool
	AppliedAt *time.Time
	HasDown   bool
}

type Migrator struct {
	pool  *pgxpool.Pool
	files []File
}

func New(ctx context.Context, dsn, dir string) (*Migrator, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	files, err := discover(dir)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	m := &Migrator{pool: pool, files: files}
	if err := m.ensureTable(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return m, nil
}

func (m *Migrator) Close() {
	if m != nil && m.pool != nil {
		m.pool.Close()
	}
}

func (m *Migrator) Status(ctx context.Context) ([]StatusRow, error) {
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]StatusRow, 0, len(m.files))
	for _, f := range m.files {
		row := StatusRow{Version: f.Version, Name: f.Name, HasDown: f.DownPath != ""}
		if t, ok := applied[f.Version]; ok {
			row.Applied = true
			row.AppliedAt = &t
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (m *Migrator) CurrentVersion(ctx context.Context) (int, error) {
	var version int
	if err := m.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (m *Migrator) Up(ctx context.Context, to int) ([]int, error) {
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	appliedVersions := []int{}
	for _, f := range m.files {
		if to > 0 && f.Version > to {
			continue
		}
		if _, ok := applied[f.Version]; ok {
			continue
		}
		sqlBytes, err := os.ReadFile(f.UpPath)
		if err != nil {
			return appliedVersions, err
		}
		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return appliedVersions, err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return appliedVersions, fmt.Errorf("apply %s: %w", filepath.Base(f.UpPath), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1,$2) ON CONFLICT (version) DO NOTHING`, f.Version, filepath.Base(f.UpPath)); err != nil {
			_ = tx.Rollback(ctx)
			return appliedVersions, err
		}
		if err := tx.Commit(ctx); err != nil {
			return appliedVersions, err
		}
		appliedVersions = append(appliedVersions, f.Version)
	}
	return appliedVersions, nil
}

func (m *Migrator) Down(ctx context.Context, steps int) ([]int, error) {
	if steps <= 0 {
		return nil, fmt.Errorf("steps must be greater than 0")
	}
	status, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	applied := make([]StatusRow, 0)
	for _, row := range status {
		if row.Applied {
			applied = append(applied, row)
		}
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].Version > applied[j].Version })
	if steps > len(applied) {
		steps = len(applied)
	}
	reverted := []int{}
	for _, row := range applied[:steps] {
		file, ok := m.fileByVersion(row.Version)
		if !ok {
			return reverted, fmt.Errorf("migration file not found for version %d", row.Version)
		}
		if file.DownPath == "" {
			return reverted, fmt.Errorf("migration %03d %s has no down script", file.Version, file.Name)
		}
		sqlBytes, err := os.ReadFile(file.DownPath)
		if err != nil {
			return reverted, err
		}
		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return reverted, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, file.Version); err != nil {
			_ = tx.Rollback(ctx)
			return reverted, err
		}
		if strings.TrimSpace(string(sqlBytes)) != "" {
			if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
				_ = tx.Rollback(ctx)
				return reverted, fmt.Errorf("revert %s: %w", filepath.Base(file.DownPath), err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return reverted, err
		}
		reverted = append(reverted, file.Version)
	}
	return reverted, nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

func (m *Migrator) appliedVersions(ctx context.Context) (map[int]time.Time, error) {
	rows, err := m.pool.Query(ctx, `SELECT version, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]time.Time{}
	for rows.Next() {
		var version int
		var appliedAt time.Time
		if err := rows.Scan(&version, &appliedAt); err != nil {
			return nil, err
		}
		out[version] = appliedAt
	}
	return out, rows.Err()
}

func (m *Migrator) fileByVersion(version int) (File, bool) {
	for _, f := range m.files {
		if f.Version == version {
			return f, true
		}
	}
	return File{}, false
}

func discover(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := map[int]*File{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		matches := filePattern.FindStringSubmatch(name)
		if matches == nil {
			continue
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, err
		}
		item := files[version]
		if item == nil {
			item = &File{Version: version, Name: matches[2]}
			files[version] = item
		}
		fullPath := filepath.Join(dir, name)
		if matches[3] == ".down" {
			item.DownPath = fullPath
		} else {
			item.UpPath = fullPath
			item.Name = strings.TrimSuffix(name, ".sql")
		}
	}
	ordered := make([]File, 0, len(files))
	for _, f := range files {
		if f.UpPath == "" {
			return nil, fmt.Errorf("missing up migration for version %03d", f.Version)
		}
		ordered = append(ordered, *f)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	if len(ordered) == 0 {
		return nil, errors.New("no migrations found")
	}
	return ordered, nil
}
