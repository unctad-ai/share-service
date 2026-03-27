# share.eregistrations.dev Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go document sharing service that accepts HTML/MD via API, stores with permanent URLs, and renders in a minimal viewer — deployed on Coolify at share.eregistrations.dev.

**Architecture:** Single Go binary serving REST API + server-rendered HTML pages. SQLite for metadata, flat files for content. Sandboxed iframes for rendering. Deployed as a Docker container on the existing singlewindow Coolify server.

**Tech Stack:** Go 1.22, modernc.org/sqlite, goldmark, go-nanoid, html/template, embed, net/http

**Spec:** `docs/superpowers/specs/2026-03-27-share-service-design.md`

---

## File Map

```
unctad-ai/share-service/
├── main.go                — entry point: CLI flags, router, server startup
├── store.go               — Store struct: SQLite init, CRUD, file read/write
├── store_test.go          — store unit tests
├── handlers.go            — HTTP handlers (API JSON + web HTML)
├── handlers_test.go       — handler integration tests (httptest)
├── ratelimit.go           — IP-based rate limiter (token bucket)
├── ratelimit_test.go      — rate limiter tests
├── templates/             — Go html/template files (embedded via embed.FS)
│   ├── base.html          — shared layout (head, nav bar, footer)
│   ├── home.html          — homepage feed with search
│   ├── view.html          — document viewer (sandboxed iframe)
│   ├── created.html       — one-time confirmation (secret display)
│   └── upload.html        — manual upload form
├── static/                — CSS (embedded via embed.FS)
│   └── style.css          — all styles: nav, feed, viewer, upload, confirmation
├── go.mod
├── go.sum
├── Dockerfile             — multi-stage: golang:1.22-alpine → alpine:3.19
├── docker-compose.yml     — single service with volume mount
└── .gitignore             — data/, *.db
```

Additionally, in `singlewindow-deployments`:
```
projects/share.yml         — Coolify project config
```

And in `UNCTAD-eRegistrations/plugin-marketplace`:
```
plugins/share-service/
├── .claude-plugin/
│   └── plugin.json        — plugin metadata
└── skills/
    └── share/
        └── share.md       — skill definition
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`, `go.sum`, `main.go`, `.gitignore`

- [ ] **Step 1: Create the GitHub repo**

```bash
gh repo create unctad-ai/share-service --public --clone --description "Document sharing service for AI-generated HTML/MD"
cd share-service
```

- [ ] **Step 2: Initialize Go module**

```bash
go mod init github.com/unctad-ai/share-service
```

- [ ] **Step 3: Create .gitignore**

```gitignore
data/
*.db
```

- [ ] **Step 4: Create main.go with minimal server**

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 80, "server port")
	dataDir := flag.String("data", "./data", "data directory")
	baseURL := flag.String("base-url", "http://localhost", "public base URL for generated links")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("share-service starting on %s (data=%s, base-url=%s)", addr, *dataDir, *baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

- [ ] **Step 5: Verify it compiles and runs**

Run: `go build -o share-service && ./share-service -port 8080 &`
Then: `curl -s http://localhost:8080/api/health`
Expected: `{"status":"ok"}`
Kill: `kill %1`

- [ ] **Step 6: Commit**

```bash
git add .gitignore go.mod main.go
git commit -m "feat: project scaffold with health endpoint"
```

---

### Task 2: Storage Layer

**Files:**
- Create: `store.go`, `store_test.go`
- Modify: `go.mod` (new dependencies)

- [ ] **Step 1: Add dependencies**

```bash
go get modernc.org/sqlite
go get github.com/matoous/go-nanoid/v2
```

- [ ] **Step 2: Write failing tests for store**

Create `store_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := testStore(t)

	doc, secret, err := s.Create("Test Title", "html", []byte("<h1>Hello</h1>"), "public")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if doc.ID == "" || doc.Title != "Test Title" || doc.Format != "html" || doc.Visibility != "public" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if secret == "" {
		t.Fatal("expected non-empty secret")
	}

	got, err := s.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Test Title" {
		t.Fatalf("Get title: got %q", got.Title)
	}

	content, err := s.ReadContent(doc.ID, doc.Format)
	if err != nil {
		t.Fatalf("ReadContent: %v", err)
	}
	if string(content) != "<h1>Hello</h1>" {
		t.Fatalf("content: got %q", string(content))
	}
}

func TestGetNotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.Get("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteWithSecret(t *testing.T) {
	s := testStore(t)
	doc, secret, _ := s.Create("To Delete", "md", []byte("# bye"), "public")

	err := s.Delete(doc.ID, secret)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Get(doc.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// Content file should also be gone
	path := filepath.Join(s.docsDir, doc.ID+".md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected content file to be deleted")
	}
}

func TestDeleteWrongSecret(t *testing.T) {
	s := testStore(t)
	doc, _, _ := s.Create("Protected", "html", []byte("<p>safe</p>"), "public")

	err := s.Delete(doc.ID, "wrong-secret")
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateVisibility(t *testing.T) {
	s := testStore(t)
	doc, secret, _ := s.Create("Vis Test", "html", []byte("<p>hi</p>"), "public")

	err := s.Update(doc.ID, secret, &UpdateParams{Visibility: strPtr("private")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := s.Get(doc.ID)
	if got.Visibility != "private" {
		t.Fatalf("expected private, got %q", got.Visibility)
	}
}

func TestUpdateTitle(t *testing.T) {
	s := testStore(t)
	doc, secret, _ := s.Create("Old Title", "html", []byte("<p>hi</p>"), "public")

	err := s.Update(doc.ID, secret, &UpdateParams{Title: strPtr("New Title")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := s.Get(doc.ID)
	if got.Title != "New Title" {
		t.Fatalf("expected 'New Title', got %q", got.Title)
	}
}

func TestList(t *testing.T) {
	s := testStore(t)
	s.Create("Doc A", "html", []byte("<p>a</p>"), "public")
	s.Create("Doc B", "md", []byte("# b"), "public")
	s.Create("Doc C", "html", []byte("<p>c</p>"), "private")

	docs, total, err := s.List(1, 20, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 public docs, got %d", total)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	// Newest first
	if docs[0].Title != "Doc B" {
		t.Fatalf("expected Doc B first, got %q", docs[0].Title)
	}
}

func TestListSearch(t *testing.T) {
	s := testStore(t)
	s.Create("Kenya Report", "html", []byte("<p>kenya</p>"), "public")
	s.Create("Bhutan Analysis", "md", []byte("# bhutan"), "public")

	docs, total, err := s.List(1, 20, "kenya")
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if total != 1 || docs[0].Title != "Kenya Report" {
		t.Fatalf("expected Kenya Report, got %d results: %+v", total, docs)
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -v -count=1 ./...`
Expected: compilation errors — `Store`, `ErrNotFound`, etc. not defined

- [ ] **Step 4: Implement store.go**

Create `store.go`:

```go
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
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

type Document struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Format     string    `json:"format"`
	Visibility string    `json:"visibility"`
	SizeBytes  int       `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type UpdateParams struct {
	Title      *string
	Visibility *string
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
		CREATE TABLE IF NOT EXISTS documents (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			format      TEXT NOT NULL CHECK(format IN ('html', 'md')),
			visibility  TEXT NOT NULL DEFAULT 'public' CHECK(visibility IN ('public', 'private')),
			secret_hash TEXT NOT NULL,
			size_bytes  INTEGER NOT NULL,
			created_at  TEXT NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db, docsDir: docsDir}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Create(title, format string, content []byte, visibility string) (*Document, string, error) {
	id, err := gonanoid.New(10)
	if err != nil {
		return nil, "", fmt.Errorf("generate id: %w", err)
	}

	secret := generateSecret()
	hash := hashSecret(secret)
	now := time.Now().UTC()

	if _, err := s.db.Exec(
		`INSERT INTO documents (id, title, format, visibility, secret_hash, size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, title, format, visibility, hash, len(content), now.Format(time.RFC3339),
	); err != nil {
		return nil, "", fmt.Errorf("insert: %w", err)
	}

	path := filepath.Join(s.docsDir, id+"."+format)
	if err := os.WriteFile(path, content, 0644); err != nil {
		s.db.Exec(`DELETE FROM documents WHERE id = ?`, id)
		return nil, "", fmt.Errorf("write file: %w", err)
	}

	doc := &Document{
		ID:         id,
		Title:      title,
		Format:     format,
		Visibility: visibility,
		SizeBytes:  len(content),
		CreatedAt:  now,
	}
	return doc, secret, nil
}

func (s *Store) Get(id string) (*Document, error) {
	doc := &Document{}
	var createdAt string
	err := s.db.QueryRow(
		`SELECT id, title, format, visibility, size_bytes, created_at FROM documents WHERE id = ?`, id,
	).Scan(&doc.ID, &doc.Title, &doc.Format, &doc.Visibility, &doc.SizeBytes, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	doc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
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

func (s *Store) Delete(id, secret string) error {
	if err := s.verifySecret(id, secret); err != nil {
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
	return nil
}

func (s *Store) Update(id, secret string, params *UpdateParams) error {
	if err := s.verifySecret(id, secret); err != nil {
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
	return nil
}

func (s *Store) List(page, limit int, query string) ([]Document, int, error) {
	offset := (page - 1) * limit

	var countQuery, listQuery string
	var args []any

	if query != "" {
		pattern := "%" + query + "%"
		countQuery = `SELECT COUNT(*) FROM documents WHERE visibility = 'public' AND title LIKE ?`
		listQuery = `SELECT id, title, format, visibility, size_bytes, created_at FROM documents WHERE visibility = 'public' AND title LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []any{pattern}
	} else {
		countQuery = `SELECT COUNT(*) FROM documents WHERE visibility = 'public'`
		listQuery = `SELECT id, title, format, visibility, size_bytes, created_at FROM documents WHERE visibility = 'public' ORDER BY created_at DESC LIMIT ? OFFSET ?`
	}

	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	args = append(args, limit, offset)
	rows, err := s.db.Query(listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		var createdAt string
		if err := rows.Scan(&d.ID, &d.Title, &d.Format, &d.Visibility, &d.SizeBytes, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		docs = append(docs, d)
	}
	return docs, total, nil
}

func (s *Store) verifySecret(id, secret string) error {
	var storedHash string
	err := s.db.QueryRow(`SELECT secret_hash FROM documents WHERE id = ?`, id).Scan(&storedHash)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query secret: %w", err)
	}
	if hashSecret(secret) != storedHash {
		return ErrForbidden
	}
	return nil
}

func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "sk_" + hex.EncodeToString(b)
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v -count=1 ./...`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add store.go store_test.go go.mod go.sum
git commit -m "feat: storage layer — SQLite metadata + flat file content"
```

---

### Task 3: Rate Limiter

**Files:**
- Create: `ratelimit.go`, `ratelimit_test.go`

- [ ] **Step 1: Write failing tests**

Create `ratelimit_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("1.2.3.4") {
		t.Fatal("6th request should be denied")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	rl.Allow("1.1.1.1")
	rl.Allow("1.1.1.1")

	if rl.Allow("1.1.1.1") {
		t.Fatal("1.1.1.1 should be rate limited")
	}
	if !rl.Allow("2.2.2.2") {
		t.Fatal("2.2.2.2 should be allowed")
	}
}

func TestRateLimiterExpiry(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)

	rl.Allow("1.1.1.1")
	if rl.Allow("1.1.1.1") {
		t.Fatal("should be denied immediately")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("1.1.1.1") {
		t.Fatal("should be allowed after window expires")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestRateLimiter -count=1 ./...`
Expected: compilation error — `NewRateLimiter` not defined

- [ ] **Step 3: Implement rate limiter**

Create `ratelimit.go`:

```go
package main

import (
	"sync"
	"time"
)

type RateLimiter struct {
	max    int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		max:    max,
		window: window,
		hits:   make(map[string][]time.Time),
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter expired hits
	valid := rl.hits[ip][:0]
	for _, t := range rl.hits[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.max {
		rl.hits[ip] = valid
		return false
	}

	rl.hits[ip] = append(valid, now)
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v -run TestRateLimiter -count=1 ./...`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add ratelimit.go ratelimit_test.go
git commit -m "feat: IP-based rate limiter with sliding window"
```

---

### Task 4: API Handlers

**Files:**
- Create: `handlers.go`, `handlers_test.go`
- Modify: `main.go` (wire handlers)

- [ ] **Step 1: Write failing tests for API handlers**

Create `handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	store := testStore(t)
	rl := NewRateLimiter(100, time.Minute)
	h := NewHandlers(store, rl, "http://test.local")
	mux := http.NewServeMux()
	h.RegisterAPI(mux)
	return httptest.NewServer(mux), store
}

func TestAPIPublish(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := `{"title":"Test Doc","format":"html","content":"<h1>Hello</h1>"}`
	resp, err := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	var result struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Secret string `json:"secret"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.ID == "" || result.URL == "" || result.Secret == "" {
		t.Fatalf("missing fields: %+v", result)
	}
	if !strings.HasPrefix(result.URL, "http://test.local/d/") {
		t.Fatalf("unexpected URL: %s", result.URL)
	}
}

func TestAPIPublishValidation(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"missing title", `{"format":"html","content":"x"}`, 400},
		{"missing format", `{"title":"t","content":"x"}`, 400},
		{"invalid format", `{"title":"t","format":"pdf","content":"x"}`, 400},
		{"missing content", `{"title":"t","format":"html"}`, 400},
		{"title too long", `{"title":"` + strings.Repeat("a", 201) + `","format":"html","content":"x"}`, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(tt.body))
			defer resp.Body.Close()
			if resp.StatusCode != tt.code {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected %d, got %d: %s", tt.code, resp.StatusCode, b)
			}
		})
	}
}

func TestAPIGetDocument(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	doc, _, _ := store.Create("Get Test", "html", []byte("<p>hi</p>"), "public")

	resp, _ := http.Get(srv.URL + "/api/documents/" + doc.ID)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result Document
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Title != "Get Test" {
		t.Fatalf("expected 'Get Test', got %q", result.Title)
	}
}

func TestAPIGetNotFound(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/api/documents/nonexistent")
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPIGetPrivateDoc(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	doc, _, _ := store.Create("Private", "html", []byte("<p>secret</p>"), "private")

	resp, _ := http.Get(srv.URL + "/api/documents/" + doc.ID)
	defer resp.Body.Close()

	// Private docs return 404, not 403
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for private doc, got %d", resp.StatusCode)
	}
}

func TestAPIDelete(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	doc, secret, _ := store.Create("Delete Me", "html", []byte("<p>bye</p>"), "public")

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/documents/"+doc.ID, nil)
	req.Header.Set("X-Secret", secret)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp2, _ := http.Get(srv.URL + "/api/documents/" + doc.ID)
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", resp2.StatusCode)
	}
}

func TestAPIDeleteWrongSecret(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	doc, _, _ := store.Create("Protected", "html", []byte("<p>safe</p>"), "public")

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/documents/"+doc.ID, nil)
	req.Header.Set("X-Secret", "wrong")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAPIPatch(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	doc, secret, _ := store.Create("Original", "html", []byte("<p>hi</p>"), "public")

	body := `{"title":"Updated","visibility":"private"}`
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/documents/"+doc.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Secret", secret)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	got, _ := store.Get(doc.ID)
	if got.Title != "Updated" || got.Visibility != "private" {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestAPIList(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	store.Create("Doc A", "html", []byte("<p>a</p>"), "public")
	store.Create("Doc B", "md", []byte("# b"), "public")
	store.Create("Private C", "html", []byte("<p>c</p>"), "private")

	resp, _ := http.Get(srv.URL + "/api/documents?page=1&limit=20")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Documents []Document `json:"documents"`
		Total     int        `json:"total"`
		Page      int        `json:"page"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Total != 2 {
		t.Fatalf("expected total 2, got %d", result.Total)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(result.Documents))
	}
}

func TestAPIListSearch(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	store.Create("Kenya Report", "html", []byte("<p>k</p>"), "public")
	store.Create("Bhutan Analysis", "md", []byte("# b"), "public")

	resp, _ := http.Get(srv.URL + "/api/documents?q=kenya")
	defer resp.Body.Close()

	var result struct {
		Documents []Document `json:"documents"`
		Total     int        `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Total != 1 || result.Documents[0].Title != "Kenya Report" {
		t.Fatalf("search failed: %+v", result)
	}
}

func TestAPIContentTooLarge(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	huge := strings.Repeat("x", 5*1024*1024+1)
	body := `{"title":"Big","format":"html","content":"` + huge + `"}`
	resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != 413 {
		t.Fatalf("expected 413, got %d", resp.StatusCode)
	}
}

func TestAPIRateLimit(t *testing.T) {
	store := testStore(t)
	rl := NewRateLimiter(2, time.Minute)
	h := NewHandlers(store, rl, "http://test.local")
	mux := http.NewServeMux()
	h.RegisterAPI(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"title":"RL Test","format":"html","content":"<p>x</p>"}`
	for i := 0; i < 2; i++ {
		resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
		resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("request %d: expected 201, got %d", i+1, resp.StatusCode)
		}
	}

	resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestAPIHealth(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/api/health")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestAPI -count=1 ./...`
Expected: compilation error — `NewHandlers`, `RegisterAPI` not defined

- [ ] **Step 3: Implement handlers.go**

Create `handlers.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxContentSize = 5 * 1024 * 1024 // 5 MB
const maxTitleLen = 200

type Handlers struct {
	store   *Store
	limiter *RateLimiter
	baseURL string
}

func NewHandlers(store *Store, limiter *RateLimiter, baseURL string) *Handlers {
	return &Handlers{store: store, limiter: limiter, baseURL: strings.TrimRight(baseURL, "/")}
}

func (h *Handlers) RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.handleHealth)
	mux.HandleFunc("POST /api/documents", h.handlePublish)
	mux.HandleFunc("GET /api/documents/{id}", h.handleGetDoc)
	mux.HandleFunc("DELETE /api/documents/{id}", h.handleDeleteDoc)
	mux.HandleFunc("PATCH /api/documents/{id}", h.handlePatchDoc)
	mux.HandleFunc("GET /api/documents", h.handleListDocs)
}

func (h *Handlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handlers) handlePublish(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !h.limiter.Allow(ip) {
		w.Header().Set("Retry-After", "60")
		jsonError(w, "rate limit exceeded", 429)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxContentSize+4096) // content + JSON overhead

	var req struct {
		Title      string `json:"title"`
		Format     string `json:"format"`
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			jsonError(w, "content too large (max 5 MB)", 413)
			return
		}
		jsonError(w, "invalid JSON", 400)
		return
	}

	if req.Title == "" {
		jsonError(w, "title is required", 400)
		return
	}
	if len(req.Title) > maxTitleLen {
		jsonError(w, fmt.Sprintf("title too long (max %d chars)", maxTitleLen), 400)
		return
	}
	if req.Format != "html" && req.Format != "md" {
		jsonError(w, "format must be 'html' or 'md'", 400)
		return
	}
	if req.Content == "" {
		jsonError(w, "content is required", 400)
		return
	}
	if len(req.Content) > maxContentSize {
		jsonError(w, "content too large (max 5 MB)", 413)
		return
	}
	if req.Visibility == "" {
		req.Visibility = "public"
	}
	if req.Visibility != "public" && req.Visibility != "private" {
		jsonError(w, "visibility must be 'public' or 'private'", 400)
		return
	}

	doc, secret, err := h.store.Create(req.Title, req.Format, []byte(req.Content), req.Visibility)
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         doc.ID,
		"url":        fmt.Sprintf("%s/d/%s", h.baseURL, doc.ID),
		"secret":     secret,
		"created_at": doc.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h *Handlers) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := h.store.Get(id)
	if err == ErrNotFound {
		jsonError(w, "not found", 404)
		return
	}
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}
	if doc.Visibility == "private" {
		jsonError(w, "not found", 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func (h *Handlers) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	secret := r.Header.Get("X-Secret")

	err := h.store.Delete(id, secret)
	if err == ErrNotFound {
		jsonError(w, "not found", 404)
		return
	}
	if err == ErrForbidden {
		jsonError(w, "forbidden", 403)
		return
	}
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}

	w.WriteHeader(204)
}

func (h *Handlers) handlePatchDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	secret := r.Header.Get("X-Secret")

	var req struct {
		Title      *string `json:"title"`
		Visibility *string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", 400)
		return
	}

	if req.Title != nil && len(*req.Title) > maxTitleLen {
		jsonError(w, fmt.Sprintf("title too long (max %d chars)", maxTitleLen), 400)
		return
	}
	if req.Visibility != nil && *req.Visibility != "public" && *req.Visibility != "private" {
		jsonError(w, "visibility must be 'public' or 'private'", 400)
		return
	}

	err := h.store.Update(id, secret, &UpdateParams{Title: req.Title, Visibility: req.Visibility})
	if err == ErrNotFound {
		jsonError(w, "not found", 404)
		return
	}
	if err == ErrForbidden {
		jsonError(w, "forbidden", 403)
		return
	}
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}

	doc, _ := h.store.Get(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func (h *Handlers) handleListDocs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := r.URL.Query().Get("q")

	docs, total, err := h.store.List(page, limit, query)
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}

	if docs == nil {
		docs = []Document{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"documents": docs,
		"total":     total,
		"page":      page,
	})
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}
```

- [ ] **Step 4: Update main.go to wire handlers**

Replace `main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	port := flag.Int("port", 80, "server port")
	dataDir := flag.String("data", "./data", "data directory")
	baseURL := flag.String("base-url", "http://localhost", "public base URL for generated links")
	flag.Parse()

	store, err := NewStore(*dataDir)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer store.Close()

	limiter := NewRateLimiter(10, time.Minute)
	handlers := NewHandlers(store, limiter, *baseURL)

	mux := http.NewServeMux()
	handlers.RegisterAPI(mux)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("share-service starting on %s (data=%s, base-url=%s)", addr, *dataDir, *baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

- [ ] **Step 5: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add handlers.go handlers_test.go main.go
git commit -m "feat: REST API handlers with validation and rate limiting"
```

---

### Task 5: HTML Templates & Web Routes

**Files:**
- Create: `templates/base.html`, `templates/home.html`, `templates/view.html`, `templates/created.html`, `templates/upload.html`, `static/style.css`
- Modify: `handlers.go` (add web routes), `main.go` (embed FS)

- [ ] **Step 1: Create static/style.css**

Create `static/style.css`:

```css
* { margin: 0; padding: 0; box-sizing: border-box; }

:root {
  --purple: #402765;
  --purple-light: #7c3aed;
  --gray-50: #fafafa;
  --gray-100: #f5f5f5;
  --gray-200: #e5e5e5;
  --gray-400: #a3a3a3;
  --gray-600: #525252;
  --gray-800: #262626;
  --green: #10b981;
  --amber: #f59e0b;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  color: var(--gray-800);
  background: var(--gray-50);
  line-height: 1.5;
}

/* Nav bar */
.nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 20px;
  border-bottom: 1px solid var(--gray-200);
  background: white;
}
.nav-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.nav-logo {
  width: 24px;
  height: 24px;
  background: var(--purple-light);
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-weight: 700;
  font-size: 13px;
  text-decoration: none;
}
.nav-brand {
  font-weight: 700;
  font-size: 16px;
  color: var(--gray-800);
  text-decoration: none;
}
.nav-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--gray-800);
  margin-left: 8px;
  padding-left: 12px;
  border-left: 1px solid var(--gray-200);
}
.nav-right {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: var(--gray-400);
}
.nav-link {
  color: var(--purple-light);
  text-decoration: none;
  font-size: 13px;
}

/* Homepage */
.container {
  max-width: 640px;
  margin: 0 auto;
  padding: 24px 20px;
}
.search-box {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--gray-200);
  border-radius: 8px;
  font-size: 14px;
  margin-bottom: 16px;
  outline: none;
}
.search-box:focus { border-color: var(--purple-light); }
.doc-list { display: flex; flex-direction: column; gap: 8px; }
.doc-item {
  display: flex;
  align-items: center;
  padding: 12px 14px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 8px;
  text-decoration: none;
  color: inherit;
  gap: 10px;
  transition: border-color 0.15s;
}
.doc-item:hover { border-color: var(--purple-light); }
.doc-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.doc-dot.html { background: var(--purple-light); }
.doc-dot.md { background: var(--green); }
.doc-info { flex: 1; }
.doc-title { font-weight: 600; font-size: 14px; }
.doc-meta { font-size: 12px; color: var(--gray-400); }
.pagination { display: flex; justify-content: center; gap: 12px; margin-top: 20px; }
.pagination a {
  color: var(--purple-light);
  text-decoration: none;
  font-size: 14px;
}
.empty { text-align: center; color: var(--gray-400); padding: 40px 0; font-size: 14px; }

/* Upload button */
.btn {
  background: var(--purple-light);
  color: white;
  padding: 6px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 600;
  text-decoration: none;
  border: none;
  cursor: pointer;
}
.btn:hover { background: var(--purple); }

/* Upload form */
.form-group { margin-bottom: 16px; }
.form-group label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 4px; }
.form-input {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--gray-200);
  border-radius: 8px;
  font-size: 14px;
  outline: none;
}
.form-input:focus { border-color: var(--purple-light); }
.form-textarea { min-height: 200px; font-family: monospace; resize: vertical; }
.form-select {
  padding: 10px 14px;
  border: 1px solid var(--gray-200);
  border-radius: 8px;
  font-size: 14px;
}
.form-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}

/* Viewer */
.viewer-frame {
  width: 100%;
  height: calc(100vh - 45px);
  border: none;
}

/* Created confirmation */
.created-box {
  max-width: 480px;
  margin: 60px auto;
  padding: 24px;
  background: white;
  border: 1px solid var(--gray-200);
  border-radius: 12px;
  text-align: center;
}
.created-box h2 { font-size: 18px; margin-bottom: 8px; }
.created-box .url { font-size: 14px; color: var(--purple-light); word-break: break-all; }
.secret-box {
  margin: 16px 0;
  padding: 12px;
  background: var(--gray-100);
  border-radius: 8px;
  font-family: monospace;
  font-size: 12px;
  word-break: break-all;
  text-align: left;
}
.secret-warning {
  font-size: 12px;
  color: var(--amber);
  margin-bottom: 16px;
}
.copy-btn {
  background: var(--gray-200);
  border: none;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  margin-top: 8px;
}
```

- [ ] **Step 2: Create templates/base.html**

Create `templates/base.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{block "title" .}}share.{{end}}</title>
  <link rel="stylesheet" href="/static/style.css">
</head>
<body>
  {{block "body" .}}{{end}}
  {{block "scripts" .}}{{end}}
</body>
</html>
```

- [ ] **Step 3: Create templates/home.html**

Create `templates/home.html`:

```html
{{template "base.html" .}}

{{define "title"}}share. — recent documents{{end}}

{{define "body"}}
<nav class="nav">
  <div class="nav-left">
    <a href="/" class="nav-logo">S</a>
    <a href="/" class="nav-brand">share.</a>
  </div>
  <a href="/upload" class="btn">Upload</a>
</nav>
<div class="container">
  <form method="get" action="/">
    <input type="text" name="q" class="search-box" placeholder="Search documents..." value="{{.Query}}">
  </form>
  {{if .Documents}}
  <div class="doc-list">
    {{range .Documents}}
    <a href="/d/{{.ID}}" class="doc-item">
      <span class="doc-dot {{.Format}}"></span>
      <div class="doc-info">
        <div class="doc-title">{{.Title}}</div>
        <div class="doc-meta">{{.Format | upper}} · {{.SizeBytes | filesize}} · {{.CreatedAt | timeago}}</div>
      </div>
    </a>
    {{end}}
  </div>
  {{if or .HasPrev .HasNext}}
  <div class="pagination">
    {{if .HasPrev}}<a href="/?page={{.PrevPage}}{{if .Query}}&q={{.Query}}{{end}}">← Newer</a>{{end}}
    {{if .HasNext}}<a href="/?page={{.NextPage}}{{if .Query}}&q={{.Query}}{{end}}">Older →</a>{{end}}
  </div>
  {{end}}
  {{else}}
  <div class="empty">No documents yet.</div>
  {{end}}
</div>
{{end}}
```

- [ ] **Step 4: Create templates/view.html**

Create `templates/view.html`:

```html
{{template "base.html" .}}

{{define "title"}}{{.Doc.Title}} — share.{{end}}

{{define "body"}}
<nav class="nav">
  <div class="nav-left">
    <a href="/" class="nav-logo">S</a>
    <span class="nav-title">{{.Doc.Title}}</span>
  </div>
  <div class="nav-right">
    <span>{{.Doc.CreatedAt | formatdate}}</span>
    <a href="/d/{{.Doc.ID}}/raw" class="nav-link">Raw</a>
  </div>
</nav>
<iframe class="viewer-frame" sandbox="allow-scripts" srcdoc="{{.Content}}"></iframe>
{{end}}
```

- [ ] **Step 5: Create templates/created.html**

Create `templates/created.html`:

```html
{{template "base.html" .}}

{{define "title"}}Published — share.{{end}}

{{define "body"}}
<nav class="nav">
  <div class="nav-left">
    <a href="/" class="nav-logo">S</a>
    <a href="/" class="nav-brand">share.</a>
  </div>
</nav>
<div class="created-box">
  <h2>Published!</h2>
  <p class="url"><a href="{{.URL}}">{{.URL}}</a></p>
  <div class="secret-box" id="secret">{{.Secret}}</div>
  <button class="copy-btn" onclick="navigator.clipboard.writeText(document.getElementById('secret').textContent).then(()=>this.textContent='Copied!')">Copy secret</button>
  <p class="secret-warning">Save this secret — it won't be shown again. You need it to delete or update this document.</p>
  <a href="{{.URL}}" class="btn">View document</a>
</div>
{{end}}
```

- [ ] **Step 6: Create templates/upload.html**

Create `templates/upload.html`:

```html
{{template "base.html" .}}

{{define "title"}}Upload — share.{{end}}

{{define "body"}}
<nav class="nav">
  <div class="nav-left">
    <a href="/" class="nav-logo">S</a>
    <a href="/" class="nav-brand">share.</a>
  </div>
</nav>
<div class="container">
  <h2 style="margin-bottom:20px;">Upload a document</h2>
  {{if .Error}}<p style="color:red;margin-bottom:12px;">{{.Error}}</p>{{end}}
  <form method="post" action="/upload" enctype="multipart/form-data">
    <div class="form-group">
      <label for="title">Title</label>
      <input type="text" id="title" name="title" class="form-input" required maxlength="200">
    </div>
    <div class="form-group">
      <label for="format">Format</label>
      <select id="format" name="format" class="form-select">
        <option value="html">HTML</option>
        <option value="md">Markdown</option>
      </select>
    </div>
    <div class="form-group">
      <label for="content">Content</label>
      <textarea id="content" name="content" class="form-input form-textarea" required></textarea>
    </div>
    <div class="form-group">
      <label class="form-toggle">
        <input type="checkbox" name="private" value="1">
        Make private
      </label>
    </div>
    <button type="submit" class="btn">Publish</button>
  </form>
</div>
{{end}}
```

- [ ] **Step 7: Add web routes and template rendering to handlers.go**

The template functions, embed directives, and markdown renderer go in a dedicated file:

Create `templates.go`:

```go
package main

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var md = goldmark.New(
	goldmark.WithRendererOptions(
		goldmarkhtml.WithUnsafe(false),
	),
)

func loadTemplates() *template.Template {
	funcMap := template.FuncMap{
		"upper": strings.ToUpper,
		"filesize": func(b int) string {
			if b < 1024 {
				return fmt.Sprintf("%d B", b)
			}
			kb := float64(b) / 1024
			if kb < 1024 {
				return fmt.Sprintf("%.0f KB", math.Round(kb))
			}
			mb := kb / 1024
			return fmt.Sprintf("%.1f MB", mb)
		},
		"timeago": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				m := int(d.Minutes())
				if m == 1 {
					return "1m ago"
				}
				return fmt.Sprintf("%dm ago", m)
			case d < 24*time.Hour:
				h := int(d.Hours())
				if h == 1 {
					return "1h ago"
				}
				return fmt.Sprintf("%dh ago", h)
			default:
				days := int(d.Hours() / 24)
				if days == 1 {
					return "1d ago"
				}
				return fmt.Sprintf("%dd ago", days)
			}
		},
		"formatdate": func(t time.Time) string {
			return t.Format("Jan 2, 2006")
		},
	}

	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"))
}

func renderMarkdown(source []byte) (string, error) {
	var buf strings.Builder
	if err := md.Convert(source, &buf); err != nil {
		return "", err
	}
	// Wrap in a basic readable stylesheet
	return `<!DOCTYPE html><html><head><style>
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:48rem;margin:2rem auto;padding:0 1rem;line-height:1.6;color:#262626}
h1,h2,h3{margin:1.5em 0 0.5em}
pre{background:#f5f5f5;padding:1rem;border-radius:8px;overflow-x:auto}
code{font-size:0.9em;background:#f5f5f5;padding:2px 6px;border-radius:4px}
pre code{background:none;padding:0}
table{border-collapse:collapse;width:100%}
th,td{border:1px solid #e5e5e5;padding:8px 12px;text-align:left}
img{max-width:100%}
blockquote{border-left:3px solid #e5e5e5;margin:1em 0;padding:0.5em 1em;color:#525252}
</style></head><body>` + buf.String() + `</body></html>`, nil
}
```

- [ ] **Step 8: Add web handler methods to handlers.go**

Add these imports to the top of `handlers.go`: `"html"`, `"html/template"`, `"io/fs"`, `"math"`.

Append to `handlers.go`:

```go
func (h *Handlers) RegisterWeb(mux *http.ServeMux, tmpl *template.Template) {
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		query := r.URL.Query().Get("q")
		limit := 20

		docs, total, err := h.store.List(page, limit, query)
		if err != nil {
			http.Error(w, "internal error", 500)
			return
		}
		if docs == nil {
			docs = []Document{}
		}

		totalPages := int(math.Ceil(float64(total) / float64(limit)))

		tmpl.ExecuteTemplate(w, "home.html", map[string]any{
			"Documents": docs,
			"Query":     query,
			"Page":      page,
			"HasPrev":   page > 1,
			"HasNext":   page < totalPages,
			"PrevPage":  page - 1,
			"NextPage":  page + 1,
		})
	})

	mux.HandleFunc("GET /d/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		doc, err := h.store.Get(id)
		if err == ErrNotFound || (err == nil && doc.Visibility == "private") {
			http.NotFound(w, r)
			return
		}

		content, err := h.store.ReadContent(doc.ID, doc.Format)
		if err != nil {
			http.Error(w, "internal error", 500)
			return
		}

		var htmlContent string
		if doc.Format == "md" {
			htmlContent, err = renderMarkdown(content)
			if err != nil {
				http.Error(w, "render error", 500)
				return
			}
		} else {
			htmlContent = string(content)
		}

		tmpl.ExecuteTemplate(w, "view.html", map[string]any{
			"Doc":     doc,
			"Content": template.HTMLAttr(html.EscapeString(htmlContent)),
		})
	})

	mux.HandleFunc("GET /d/{id}/raw", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		doc, err := h.store.Get(id)
		if err == ErrNotFound || (err == nil && doc.Visibility == "private") {
			http.NotFound(w, r)
			return
		}

		content, err := h.store.ReadContent(doc.ID, doc.Format)
		if err != nil {
			http.Error(w, "internal error", 500)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(content)
	})

	mux.HandleFunc("GET /upload", func(w http.ResponseWriter, r *http.Request) {
		tmpl.ExecuteTemplate(w, "upload.html", nil)
	})

	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !h.limiter.Allow(ip) {
			tmpl.ExecuteTemplate(w, "upload.html", map[string]any{"Error": "Rate limit exceeded. Try again in a minute."})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxContentSize+4096)
		if err := r.ParseMultipartForm(maxContentSize); err != nil {
			tmpl.ExecuteTemplate(w, "upload.html", map[string]any{"Error": "Content too large (max 5 MB)."})
			return
		}

		title := strings.TrimSpace(r.FormValue("title"))
		format := r.FormValue("format")
		content := r.FormValue("content")
		visibility := "public"
		if r.FormValue("private") == "1" {
			visibility = "private"
		}

		if title == "" || (format != "html" && format != "md") || content == "" {
			tmpl.ExecuteTemplate(w, "upload.html", map[string]any{"Error": "Title, format, and content are required."})
			return
		}
		if len(title) > maxTitleLen {
			tmpl.ExecuteTemplate(w, "upload.html", map[string]any{"Error": "Title too long (max 200 chars)."})
			return
		}

		doc, secret, err := h.store.Create(title, format, []byte(content), visibility)
		if err != nil {
			tmpl.ExecuteTemplate(w, "upload.html", map[string]any{"Error": "Failed to save document."})
			return
		}

		url := fmt.Sprintf("%s/d/%s", h.baseURL, doc.ID)
		tmpl.ExecuteTemplate(w, "created.html", map[string]any{
			"URL":    url,
			"Secret": secret,
		})
	})
}
```

- [ ] **Step 9: Update main.go to wire web routes**

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	port := flag.Int("port", 80, "server port")
	dataDir := flag.String("data", "./data", "data directory")
	baseURL := flag.String("base-url", "http://localhost", "public base URL for generated links")
	flag.Parse()

	store, err := NewStore(*dataDir)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer store.Close()

	limiter := NewRateLimiter(10, time.Minute)
	handlers := NewHandlers(store, limiter, *baseURL)
	tmpl := loadTemplates()

	mux := http.NewServeMux()
	handlers.RegisterAPI(mux)
	handlers.RegisterWeb(mux, tmpl)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("share-service starting on %s (data=%s, base-url=%s)", addr, *dataDir, *baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

- [ ] **Step 10: Add goldmark dependency**

```bash
go get github.com/yuin/goldmark
```

- [ ] **Step 11: Build and smoke test locally**

```bash
go build -o share-service && ./share-service -port 8080 -base-url http://localhost:8080 &
# Test health
curl -s http://localhost:8080/api/health
# Test publish via API
curl -s -X POST http://localhost:8080/api/documents \
  -H 'Content-Type: application/json' \
  -d '{"title":"Test","format":"html","content":"<h1>Hello World</h1>"}'
# Open in browser: http://localhost:8080
kill %1
```

Expected: health returns `{"status":"ok"}`, publish returns JSON with id/url/secret, homepage shows the doc.

- [ ] **Step 12: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: all PASS

- [ ] **Step 13: Commit**

```bash
git add templates.go templates/ static/ handlers.go main.go go.mod go.sum
git commit -m "feat: web UI — homepage feed, document viewer, upload form, confirmation page"
```

---

### Task 6: Dockerfile & Docker Compose

**Files:**
- Create: `Dockerfile`, `docker-compose.yml`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o share-service .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/share-service /usr/local/bin/share-service
EXPOSE 80
ENTRYPOINT ["share-service"]
CMD ["-data", "/data", "-base-url", "https://share.eregistrations.dev"]
```

- [ ] **Step 2: Create docker-compose.yml**

```yaml
services:
  app:
    build: .
    ports:
      - "80:80"
    volumes:
      - share-data:/data
    restart: unless-stopped

volumes:
  share-data:
```

- [ ] **Step 3: Test Docker build**

```bash
docker build -t share-service:test .
docker run --rm -p 8080:80 -e share-service:test -data /data -base-url http://localhost:8080 &
curl -s http://localhost:8080/api/health
docker stop $(docker ps -q --filter ancestor=share-service:test)
```

Expected: health returns `{"status":"ok"}`

- [ ] **Step 4: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "feat: Dockerfile and docker-compose for Coolify deployment"
```

---

### Task 7: Coolify Deployment Config

**Files:**
- Create: `singlewindow-deployments/projects/share.yml`

This task is in the `singlewindow-deployments` repo, not `share-service`.

- [ ] **Step 1: Create projects/share.yml**

In `/Users/moulaymehdi/PROJECTS/figma/singlewindow-deployments`:

```yaml
name: share
repo: unctad-ai/share-service
branch: main
domain: share.eregistrations.dev
description: Document sharing service for AI-generated HTML/MD
```

- [ ] **Step 2: Commit in singlewindow-deployments**

```bash
cd /Users/moulaymehdi/PROJECTS/figma/singlewindow-deployments
git add projects/share.yml
git commit -m "feat: add share-service Coolify deployment config"
```

- [ ] **Step 3: DNS setup**

Add A record for `share.eregistrations.dev` pointing to the singlewindow server IP. Verify:

```bash
dig +short share.eregistrations.dev
```

Expected: the singlewindow server IP

- [ ] **Step 4: Deploy via onboard script**

```bash
./scripts/onboard-project.sh projects/share.yml
```

- [ ] **Step 5: Verify deployment**

```bash
curl -sI https://share.eregistrations.dev/api/health
curl -s https://share.eregistrations.dev/api/health | jq .
```

Expected: HTTPS 200, `{"status":"ok"}`

---

### Task 8: Claude Skill in Plugin Marketplace

**Files:**
- Create: `plugins/share-service/.claude-plugin/plugin.json`, `plugins/share-service/skills/share/share.md`

This task is in the `UNCTAD-eRegistrations/plugin-marketplace` repo.

- [ ] **Step 1: Create plugin.json**

At `~/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service/.claude-plugin/plugin.json`:

```json
{
  "name": "share-service",
  "description": "Publish HTML and Markdown documents to share.eregistrations.dev for permanent sharing links",
  "author": {
    "name": "UNCTAD Trade Facilitation Section"
  }
}
```

- [ ] **Step 2: Create the skill**

At `~/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service/skills/share/share.md`:

```markdown
---
name: share
description: Publish HTML or Markdown documents to share.eregistrations.dev. Use when the user says "share this", "publish this", "upload this", or asks for a shareable link for a generated document.
---

# Share Document

Publish an HTML or Markdown document to share.eregistrations.dev and return a permanent URL.

## When to trigger

- User explicitly says "share this", "publish this", "upload this"
- User asks for a shareable link to a generated document
- Do NOT auto-trigger on file creation

## Steps

1. **Find the content.** Look for the most recently written `.html` or `.md` file in this conversation. If ambiguous, ask the user which file to share. Accept a file path argument if provided.

2. **Determine title.** Infer from the filename (strip extension, convert hyphens/underscores to spaces, title-case) or from the first `<title>` tag or `# heading` in the content. Show the inferred title and ask the user to confirm or change it.

3. **Determine format.** Use the file extension: `.html` → `html`, `.md` → `md`.

4. **Publish.** Read the file content and call the API:

```bash
curl -s -X POST https://share.eregistrations.dev/api/documents \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg title "$TITLE" --arg format "$FORMAT" --arg content "$(cat "$FILEPATH")" \
    '{title: $title, format: $format, content: $content}')"
```

For files larger than 1 MB, use multipart upload instead:

```bash
curl -s -X POST https://share.eregistrations.dev/upload \
  -F "title=$TITLE" \
  -F "format=$FORMAT" \
  -F "content=<$FILEPATH"
```

5. **Report the URL.** Parse the JSON response and present:
   - The permanent URL: `https://share.eregistrations.dev/d/{id}`
   - The raw URL: `https://share.eregistrations.dev/d/{id}/raw`

6. **Save history (optional).** Append `{id, secret, url, title, created_at}` to `.share-history.json` in the project root. Create the file if it doesn't exist. Add `.share-history.json` to `.gitignore` if not already there.

## API Reference

**Base URL:** `https://share.eregistrations.dev`

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | /api/documents | none | Publish (JSON body: title, format, content, visibility) |
| GET | /api/documents/{id} | none | Get metadata |
| PATCH | /api/documents/{id} | X-Secret header | Update title/visibility |
| DELETE | /api/documents/{id} | X-Secret header | Delete document |
| GET | /api/documents | none | List public docs (?page=&limit=&q=) |
| POST | /upload | none | Multipart upload (form fields: title, format, content) |

**Limits:** 5 MB max content, 200 char max title, 10 publishes/min/IP.

**Publish response:**
```json
{
  "id": "a3xK9mPq2v",
  "url": "https://share.eregistrations.dev/d/a3xK9mPq2v",
  "secret": "sk_...",
  "created_at": "2026-03-27T10:00:00Z"
}
```
```

- [ ] **Step 3: Commit in plugin-marketplace**

```bash
cd ~/.claude/plugins/marketplaces/unctad-digital-government
git add plugins/share-service/
git commit -m "feat: add share-service plugin for document publishing"
git push origin main
```

---

### Task 9: End-to-End Verification

- [ ] **Step 1: Verify API publish**

```bash
curl -s -X POST https://share.eregistrations.dev/api/documents \
  -H 'Content-Type: application/json' \
  -d '{"title":"E2E Test","format":"html","content":"<h1>End to End</h1><p>This is a test document.</p>"}' | jq .
```

Expected: 201 with id, url, secret

- [ ] **Step 2: Verify document viewer**

Open the returned URL in a browser. Verify:
- Minimal top bar shows title and date
- Content renders in sandboxed iframe
- Raw link serves `text/plain`

- [ ] **Step 3: Verify homepage**

Open `https://share.eregistrations.dev/`. Verify:
- Document appears in feed
- Search box filters by title
- Upload button links to upload form

- [ ] **Step 4: Verify manual upload**

Go to `https://share.eregistrations.dev/upload`. Submit a document. Verify:
- Confirmation page shows secret with copy button
- "View document" link works

- [ ] **Step 5: Verify management**

```bash
# Patch visibility
curl -s -X PATCH https://share.eregistrations.dev/api/documents/$ID \
  -H 'Content-Type: application/json' \
  -H "X-Secret: $SECRET" \
  -d '{"visibility":"private"}' | jq .

# Verify it's gone from public view
curl -s https://share.eregistrations.dev/api/documents/$ID
# Expected: 404

# Delete
curl -s -X DELETE https://share.eregistrations.dev/api/documents/$ID \
  -H "X-Secret: $SECRET" -w "%{http_code}"
# Expected: 204
```

- [ ] **Step 6: Verify skill**

In a new Claude Code session, generate an HTML file and say "share this". Verify the skill publishes and returns a URL.

- [ ] **Step 7: Clean up test documents**

Delete any test documents created during verification using their secrets.
