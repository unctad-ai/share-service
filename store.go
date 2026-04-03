package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

type Publisher struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Document struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Format       string    `json:"format"`
	Visibility   string    `json:"visibility"`
	SizeBytes    int       `json:"size_bytes"`
	PublisherID  string    `json:"publisher_id,omitempty"`
	Project      string    `json:"project,omitempty"`
	DocType      string    `json:"doc_type,omitempty"`
	AgentSession string    `json:"agent_session,omitempty"`
	Tags         string    `json:"tags,omitempty"`
	Pinned       bool      `json:"pinned"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateParams struct {
	Title        string
	Format       string
	Content      []byte
	Visibility   string
	PublisherID  string
	Project      string
	DocType      string
	AgentSession string
	Tags         string
}

type UpdateParams struct {
	Title      *string
	Visibility *string
	Pinned     *bool
}

type ListFilter struct {
	Query   string
	Project string
	DocType string
}

type Store struct {
	db      *sql.DB
	docsDir string
}

func NewStore(dataDir string) (*Store, error) {
	docsDir := filepath.Join(dataDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return nil, fmt.Errorf("create docs dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "share.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS publishers (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL DEFAULT '',
			token_hash  TEXT NOT NULL UNIQUE,
			created_at  TEXT NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("create publishers table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			id           TEXT PRIMARY KEY,
			title        TEXT NOT NULL,
			format       TEXT NOT NULL CHECK(format IN ('html', 'md')),
			visibility   TEXT NOT NULL DEFAULT 'private' CHECK(visibility IN ('public', 'private')),
			secret_hash  TEXT NOT NULL,
			size_bytes   INTEGER NOT NULL,
			publisher_id TEXT REFERENCES publishers(id),
			created_at   TEXT NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("create documents table: %w", err)
	}

	// Migrate: add metadata columns (idempotent — ALTER fails silently if column exists)
	for _, col := range []struct{ name, def string }{
		{"project", "TEXT NOT NULL DEFAULT ''"},
		{"doc_type", "TEXT NOT NULL DEFAULT ''"},
		{"agent_session", "TEXT NOT NULL DEFAULT ''"},
		{"tags", "TEXT NOT NULL DEFAULT ''"},
		{"pinned", "INTEGER NOT NULL DEFAULT 0"},
	} {
		db.Exec(fmt.Sprintf(`ALTER TABLE documents ADD COLUMN %s %s`, col.name, col.def))
	}

	return &Store{db: db, docsDir: docsDir}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Register(name string) (*Publisher, string, error) {
	id, err := gonanoid.Generate("0123456789abcdefghijklmnopqrstuvwxyz", 12)
	if err != nil {
		return nil, "", fmt.Errorf("generate id: %w", err)
	}
	id = "pub_" + id

	token := generateToken()
	hash := hashSecret(token)
	now := time.Now().UTC()

	if _, err := s.db.Exec(
		`INSERT INTO publishers (id, name, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, name, hash, now.Format(time.RFC3339Nano),
	); err != nil {
		return nil, "", fmt.Errorf("insert publisher: %w", err)
	}

	pub := &Publisher{ID: id, Name: name, CreatedAt: now}
	return pub, token, nil
}

func (s *Store) GetPublisher(token string) (*Publisher, error) {
	hash := hashSecret(token)
	pub := &Publisher{}
	var createdAt string
	err := s.db.QueryRow(
		`SELECT id, name, created_at FROM publishers WHERE token_hash = ?`, hash,
	).Scan(&pub.ID, &pub.Name, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query publisher: %w", err)
	}
	pub.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return pub, nil
}

func (s *Store) Create(title, format string, content []byte, visibility string) (*Document, string, error) {
	return s.CreateWithPublisher(CreateParams{Title: title, Format: format, Content: content, Visibility: visibility})
}

func (s *Store) CreateWithPublisher(p CreateParams) (*Document, string, error) {
	id, err := gonanoid.New(10)
	if err != nil {
		return nil, "", fmt.Errorf("generate id: %w", err)
	}

	secret := generateSecret()
	hash := hashSecret(secret)
	now := time.Now().UTC()

	var pubID *string
	if p.PublisherID != "" {
		pubID = &p.PublisherID
	}

	if _, err := s.db.Exec(
		`INSERT INTO documents (id, title, format, visibility, secret_hash, size_bytes, publisher_id, project, doc_type, agent_session, tags, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.Title, p.Format, p.Visibility, hash, len(p.Content), pubID, p.Project, p.DocType, p.AgentSession, p.Tags, now.Format(time.RFC3339Nano),
	); err != nil {
		return nil, "", fmt.Errorf("insert: %w", err)
	}

	path := filepath.Join(s.docsDir, id+"."+p.Format)
	if err := os.WriteFile(path, p.Content, 0644); err != nil {
		s.db.Exec(`DELETE FROM documents WHERE id = ?`, id)
		return nil, "", fmt.Errorf("write file: %w", err)
	}

	// Pre-render markdown to HTML for instant serving
	if p.Format == "md" {
		if rendered, err := RenderMarkdown(p.Content); err == nil {
			os.WriteFile(filepath.Join(s.docsDir, id+".rendered.html"), rendered, 0644)
		}
	}

	doc := &Document{
		ID:           id,
		Title:        p.Title,
		Format:       p.Format,
		Visibility:   p.Visibility,
		SizeBytes:    len(p.Content),
		PublisherID:  p.PublisherID,
		Project:      p.Project,
		DocType:      p.DocType,
		AgentSession: p.AgentSession,
		Tags:         p.Tags,
		CreatedAt:    now,
	}
	return doc, secret, nil
}

func (s *Store) Get(id string) (*Document, error) {
	doc := &Document{}
	var createdAt string
	var pubID sql.NullString
	var pinned int
	err := s.db.QueryRow(
		`SELECT id, title, format, visibility, size_bytes, publisher_id, project, doc_type, agent_session, tags, pinned, created_at FROM documents WHERE id = ?`, id,
	).Scan(&doc.ID, &doc.Title, &doc.Format, &doc.Visibility, &doc.SizeBytes, &pubID, &doc.Project, &doc.DocType, &doc.AgentSession, &doc.Tags, &pinned, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	doc.PublisherID = pubID.String
	doc.Pinned = pinned != 0
	doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return doc, nil
}

func (s *Store) ReadContent(id, format string) ([]byte, error) {
	path := filepath.Join(s.docsDir, id+"."+format)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return data, err
}

// ReadRendered returns pre-rendered HTML for a markdown document.
// Falls back to on-the-fly rendering (and caches the result) for old docs.
func (s *Store) ReadRendered(id string) ([]byte, error) {
	path := filepath.Join(s.docsDir, id+".rendered.html")
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	raw, err := s.ReadContent(id, "md")
	if err != nil {
		return nil, err
	}
	rendered, err := RenderMarkdown(raw)
	if err != nil {
		return nil, err
	}
	os.WriteFile(path, rendered, 0644) // cache for next time
	return rendered, nil
}

func (s *Store) Delete(id, secret string, publisherID string) error {
	if err := s.verifyAccess(id, secret, publisherID); err != nil {
		return err
	}

	doc, err := s.Get(id)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	os.Remove(filepath.Join(s.docsDir, id+"."+doc.Format))
	os.Remove(filepath.Join(s.docsDir, id+".rendered.html"))
	return nil
}

func (s *Store) Update(id, secret string, publisherID string, params *UpdateParams) error {
	if err := s.verifyAccess(id, secret, publisherID); err != nil {
		return err
	}

	if params.Title != nil {
		if _, err := s.db.Exec(`UPDATE documents SET title = ? WHERE id = ?`, *params.Title, id); err != nil {
			return fmt.Errorf("update title: %w", err)
		}
	}
	if params.Visibility != nil {
		if _, err := s.db.Exec(`UPDATE documents SET visibility = ? WHERE id = ?`, *params.Visibility, id); err != nil {
			return fmt.Errorf("update visibility: %w", err)
		}
	}
	if params.Pinned != nil {
		pinVal := 0
		if *params.Pinned {
			pinVal = 1
		}
		if _, err := s.db.Exec(`UPDATE documents SET pinned = ? WHERE id = ?`, pinVal, id); err != nil {
			return fmt.Errorf("update pinned: %w", err)
		}
	}
	return nil
}

func (s *Store) List(page, limit int, filter ListFilter) ([]Document, int, error) {
	offset := (page - 1) * limit

	var where []string
	var args []any
	where = append(where, "visibility = 'public'")
	// Retention: exclude unpinned docs older than 90 days
	where = append(where, "(pinned = 1 OR created_at > ?)")
	args = append(args, time.Now().UTC().Add(-90*24*time.Hour).Format(time.RFC3339Nano))

	if filter.Query != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+filter.Query+"%")
	}
	if filter.Project != "" {
		where = append(where, "project = ?")
		args = append(args, filter.Project)
	}
	if filter.DocType != "" {
		where = append(where, "doc_type = ?")
		args = append(args, filter.DocType)
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE `+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT id, title, format, visibility, size_bytes, project, doc_type, tags, pinned, created_at FROM documents WHERE `+whereClause+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		var createdAt string
		var pinned int
		if err := rows.Scan(&d.ID, &d.Title, &d.Format, &d.Visibility, &d.SizeBytes, &d.Project, &d.DocType, &d.Tags, &pinned, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan: %w", err)
		}
		d.Pinned = pinned != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		docs = append(docs, d)
	}
	return docs, total, nil
}

func (s *Store) DistinctProjects() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT project FROM documents WHERE visibility = 'public' AND project != '' ORDER BY project`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *Store) ListByPublisher(publisherID string, page, limit int) ([]Document, int, error) {
	offset := (page - 1) * limit

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM documents WHERE publisher_id = ?`, publisherID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT id, title, format, visibility, size_bytes, project, doc_type, tags, pinned, created_at FROM documents WHERE publisher_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		publisherID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		var createdAt string
		var pinned int
		if err := rows.Scan(&d.ID, &d.Title, &d.Format, &d.Visibility, &d.SizeBytes, &d.Project, &d.DocType, &d.Tags, &pinned, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan: %w", err)
		}
		d.PublisherID = publisherID
		d.Pinned = pinned != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		docs = append(docs, d)
	}
	return docs, total, nil
}

// verifyAccess checks authorization: either per-document secret or publisher ownership.
func (s *Store) verifyAccess(id, secret, publisherID string) error {
	var storedHash string
	var docPubID sql.NullString
	err := s.db.QueryRow(
		`SELECT secret_hash, publisher_id FROM documents WHERE id = ?`, id,
	).Scan(&storedHash, &docPubID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query secret: %w", err)
	}

	// Per-document secret takes priority
	if secret != "" && hashSecret(secret) == storedHash {
		return nil
	}

	// Publisher ownership
	if publisherID != "" && docPubID.Valid && docPubID.String == publisherID {
		return nil
	}

	return ErrForbidden
}

func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "sk_" + hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "tok_" + hex.EncodeToString(b)
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
