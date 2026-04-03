# Phase 1: Governed Registry — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform share-service from a document host into a governed registry of AI work with structured metadata, an activity feed homepage, OpenGraph previews, and a retention policy.

**Architecture:** Extend the existing SQLite schema with 4 metadata columns (`project`, `doc_type`, `agent_session`, `tags`). Update the publish API to accept these fields. Replace the homepage with a filterable activity feed grouped by project. Add OpenGraph `<meta>` tags so shared links render rich previews in Slack/Jira. Add a retention system that marks stale documents for expiry unless pinned.

**Tech Stack:** Go 1.25, SQLite (WAL), html/template, existing design system CSS

**Spec:** `docs/council-verdict-share-service-strategy.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `store.go` | **Modify** | Schema migration (4 new columns + pinned flag), updated Create/List/Get queries |
| `handlers.go` | **Modify** | Accept metadata in publish API, filter params in list API, OpenGraph data in web handler |
| `templates/base.html` | **Modify** | Add OpenGraph meta tags block |
| `templates/view.html` | **Modify** | Populate OpenGraph tags for document pages |
| `templates/home.html` | **Rewrite** | Activity feed with project filter chips, publisher names, doc type badges |
| `static/style.css` | **Modify** | Activity feed styles, filter chips, type badges |
| `handlers_test.go` | **Modify** | Tests for new metadata fields, filtering, OpenGraph |
| `store_test.go` | **Modify** | Tests for schema migration, metadata storage, retention |

---

### Task 1: Schema migration — add metadata columns

**Files:**
- Modify: `store.go:61-85` (NewStore schema creation)
- Modify: `store.go:29-37` (Document struct)

- [ ] **Step 1: Write failing test — metadata fields stored and retrieved**

Add to `store_test.go`:

```go
func TestCreateWithMetadata(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	doc, _, err := store.CreateWithPublisher("Test", "md", []byte("# hi"), "public", "")
	if err != nil {
		t.Fatal(err)
	}

	// Default metadata should be empty strings
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Project != "" {
		t.Errorf("expected empty project, got %q", got.Project)
	}
	if got.DocType != "" {
		t.Errorf("expected empty doc_type, got %q", got.DocType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestCreateWithMetadata -v ./...`
Expected: FAIL — `got.Project undefined (type Document has no field or method Project)`

- [ ] **Step 3: Add metadata fields to Document struct**

In `store.go`, update the `Document` struct:

```go
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
```

- [ ] **Step 4: Add schema migration in NewStore**

After the existing `CREATE TABLE IF NOT EXISTS documents` block, add migration for existing databases:

```go
// Migrate: add metadata columns if missing
for _, col := range []struct{ name, def string }{
	{"project", "TEXT NOT NULL DEFAULT ''"},
	{"doc_type", "TEXT NOT NULL DEFAULT ''"},
	{"agent_session", "TEXT NOT NULL DEFAULT ''"},
	{"tags", "TEXT NOT NULL DEFAULT ''"},
	{"pinned", "INTEGER NOT NULL DEFAULT 0"},
} {
	s.db.Exec(fmt.Sprintf(`ALTER TABLE documents ADD COLUMN %s %s`, col.name, col.def))
	// ALTER TABLE ADD COLUMN is a no-op if column exists in SQLite (returns error, which we ignore)
}
```

- [ ] **Step 5: Update CreateWithPublisher to accept and store metadata**

Update `CreateWithPublisher` signature and body. Add a `CreateParams` struct:

```go
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
```

Update `CreateWithPublisher` to accept `CreateParams`:

```go
func (s *Store) CreateWithPublisher(p CreateParams) (*Document, string, error) {
```

Update the INSERT query:

```go
if _, err := s.db.Exec(
	`INSERT INTO documents (id, title, format, visibility, secret_hash, size_bytes, publisher_id, project, doc_type, agent_session, tags, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	id, p.Title, p.Format, p.Visibility, hash, len(p.Content), pubID, p.Project, p.DocType, p.AgentSession, p.Tags, now.Format(time.RFC3339Nano),
); err != nil {
```

Keep the `Create` convenience method:

```go
func (s *Store) Create(title, format string, content []byte, visibility string) (*Document, string, error) {
	return s.CreateWithPublisher(CreateParams{
		Title: title, Format: format, Content: content, Visibility: visibility,
	})
}
```

- [ ] **Step 6: Update Get to read metadata columns**

```go
func (s *Store) Get(id string) (*Document, error) {
	doc := &Document{}
	var createdAt string
	var pubID sql.NullString
	err := s.db.QueryRow(
		`SELECT id, title, format, visibility, size_bytes, publisher_id, project, doc_type, agent_session, tags, pinned, created_at FROM documents WHERE id = ?`, id,
	).Scan(&doc.ID, &doc.Title, &doc.Format, &doc.Visibility, &doc.SizeBytes, &pubID, &doc.Project, &doc.DocType, &doc.AgentSession, &doc.Tags, &doc.Pinned, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	doc.PublisherID = pubID.String
	doc.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return doc, nil
}
```

- [ ] **Step 7: Update List and ListByPublisher to read metadata columns**

Update the row scan in both `List` and `ListByPublisher` to include new columns. The `List` SELECT becomes:

```go
listQuery = `SELECT id, title, format, visibility, size_bytes, project, doc_type, tags, created_at FROM documents WHERE visibility = 'public' ORDER BY created_at DESC LIMIT ? OFFSET ?`
```

And the scan:

```go
if err := rows.Scan(&d.ID, &d.Title, &d.Format, &d.Visibility, &d.SizeBytes, &d.Project, &d.DocType, &d.Tags, &createdAt); err != nil {
```

Apply the same pattern to `ListByPublisher`.

- [ ] **Step 8: Fix all callers of CreateWithPublisher**

Search for all calls to `CreateWithPublisher` in `handlers.go` and `store.go` and update them to use `CreateParams`. The publish handler becomes:

```go
doc, secret, err := h.store.CreateWithPublisher(CreateParams{
	Title:        req.Title,
	Format:       req.Format,
	Content:      []byte(req.Content),
	Visibility:   req.Visibility,
	PublisherID:  publisherID,
	Project:      req.Project,
	DocType:      req.DocType,
	AgentSession: req.AgentSession,
	Tags:         req.Tags,
})
```

The upload handler uses `Create` (unchanged signature).

- [ ] **Step 9: Run all tests**

Run: `go test ./... -v`
Expected: All tests PASS (existing tests use `Create` convenience method which wraps `CreateParams`)

- [ ] **Step 10: Commit**

```bash
git add store.go handlers.go store_test.go handlers_test.go
git commit -m "feat: add metadata columns (project, doc_type, agent_session, tags, pinned)"
```

---

### Task 2: Accept metadata in publish API

**Files:**
- Modify: `handlers.go:128-133` (publish request struct)
- Modify: `handlers_test.go`

- [ ] **Step 1: Write failing test — metadata accepted and returned**

```go
func TestPublishWithMetadata(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := `{"title":"Test","format":"html","content":"<p>hi</p>","visibility":"public","project":"tz","doc_type":"migration-analysis","tags":"migration,tanzania"}`
	resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	id := result["id"].(string)

	// Fetch and verify metadata
	resp2, _ := http.Get(srv.URL + "/api/documents/" + id)
	defer resp2.Body.Close()
	var doc map[string]any
	json.NewDecoder(resp2.Body).Decode(&doc)

	if doc["project"] != "tz" {
		t.Errorf("expected project 'tz', got %v", doc["project"])
	}
	if doc["doc_type"] != "migration-analysis" {
		t.Errorf("expected doc_type 'migration-analysis', got %v", doc["doc_type"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestPublishWithMetadata -v ./...`
Expected: FAIL — metadata fields not in response

- [ ] **Step 3: Update publish handler request struct**

In `handlers.go`, update the request struct in `handlePublish`:

```go
var req struct {
	Title        string `json:"title"`
	Format       string `json:"format"`
	Content      string `json:"content"`
	Visibility   string `json:"visibility"`
	Project      string `json:"project"`
	DocType      string `json:"doc_type"`
	AgentSession string `json:"agent_session"`
	Tags         string `json:"tags"`
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add handlers.go handlers_test.go
git commit -m "feat: accept project, doc_type, agent_session, tags in publish API"
```

---

### Task 3: Filter API by project and doc_type

**Files:**
- Modify: `store.go` (List method)
- Modify: `handlers.go` (handleListDocs, homepage handler)
- Modify: `handlers_test.go`

- [ ] **Step 1: Write failing test — filter by project**

```go
func TestAPIListFilterProject(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	store.Create("TZ Report", "html", []byte("<p>tz</p>"), "public")
	store.CreateWithPublisher(CreateParams{Title: "RW Report", Format: "html", Content: []byte("<p>rw</p>"), Visibility: "public", Project: "rw"})
	store.CreateWithPublisher(CreateParams{Title: "TZ Analysis", Format: "md", Content: []byte("# tz"), Visibility: "public", Project: "tz"})

	resp, _ := http.Get(srv.URL + "/api/documents?project=tz")
	defer resp.Body.Close()

	var result struct {
		Documents []Document `json:"documents"`
		Total     int        `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Total != 1 {
		t.Fatalf("expected 1 doc for project=tz, got %d", result.Total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestAPIListFilterProject -v ./...`
Expected: FAIL — returns all documents, not filtered

- [ ] **Step 3: Update List method to accept filter params**

Add a `ListFilter` struct and update `List`:

```go
type ListFilter struct {
	Query   string
	Project string
	DocType string
}

func (s *Store) List(page, limit int, filter ListFilter) ([]Document, int, error) {
	offset := (page - 1) * limit

	var where []string
	var args []any
	where = append(where, "visibility = 'public'")

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
	countQuery := `SELECT COUNT(*) FROM documents WHERE ` + whereClause
	listQuery := `SELECT id, title, format, visibility, size_bytes, project, doc_type, tags, created_at FROM documents WHERE ` + whereClause + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`

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
		if err := rows.Scan(&d.ID, &d.Title, &d.Format, &d.Visibility, &d.SizeBytes, &d.Project, &d.DocType, &d.Tags, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		docs = append(docs, d)
	}
	return docs, total, nil
}
```

- [ ] **Step 4: Update all callers of List**

In `handlers.go`, update `handleListDocs`:

```go
query := r.URL.Query().Get("q")
project := r.URL.Query().Get("project")
docType := r.URL.Query().Get("type")

docs, total, err := h.store.List(page, limit, ListFilter{Query: query, Project: project, DocType: docType})
```

Update the homepage handler similarly:

```go
query := r.URL.Query().Get("q")
project := r.URL.Query().Get("project")

docs, total, err := h.store.List(page, limit, ListFilter{Query: query, Project: project})
```

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add store.go handlers.go handlers_test.go store_test.go
git commit -m "feat: filter documents by project and doc_type in list API"
```

---

### Task 4: OpenGraph meta tags for rich Slack/Jira previews

**Files:**
- Modify: `templates/base.html`
- Modify: `templates/view.html`
- Modify: `handlers.go` (pass OG data to template)

- [ ] **Step 1: Add OpenGraph block to base.html**

In `templates/base.html`, add inside `<head>` after the title:

```html
{{block "opengraph" .}}{{end}}
```

- [ ] **Step 2: Populate OpenGraph in view.html**

Add at the top of `view.html`, after the title define:

```html
{{define "opengraph"}}
<meta property="og:title" content="{{.Doc.Title}}">
<meta property="og:description" content="{{.Doc.Format | upper}} document · {{.Doc.SizeBytes | filesize}}{{if .Doc.Project}} · Project: {{.Doc.Project}}{{end}}">
<meta property="og:type" content="article">
<meta property="og:site_name" content="share.">
<meta name="twitter:card" content="summary">
{{end}}
```

- [ ] **Step 3: Verify by fetching headers**

Run: `go build && curl -s https://share.eregistrations.dev/d/08gFHwR2Uq | grep 'og:'`

Expected: `<meta property="og:title"` tags present in response (after deploy)

- [ ] **Step 4: Commit**

```bash
git add templates/base.html templates/view.html
git commit -m "feat: add OpenGraph meta tags for rich link previews in Slack/Jira"
```

---

### Task 5: Activity feed homepage

**Files:**
- Rewrite: `templates/home.html`
- Modify: `static/style.css`
- Modify: `handlers.go` (pass distinct projects for filter chips)

- [ ] **Step 1: Add store method to get distinct projects**

In `store.go`:

```go
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
```

- [ ] **Step 2: Update homepage handler to pass projects**

In `handlers.go`, the `GET /{$}` handler:

```go
projects, _ := h.store.DistinctProjects()

tmpl.Render(w, "home.html", map[string]any{
	"Documents": docs,
	"Projects":  projects,
	"Query":     query,
	"Project":   project,
	"Page":      page,
	"HasPrev":   page > 1,
	"HasNext":   page < totalPages,
	"PrevPage":  page - 1,
	"NextPage":  page + 1,
})
```

- [ ] **Step 3: Rewrite home.html as activity feed**

```html
{{template "base.html" .}}

{{define "title"}}share. — team activity{{end}}

{{define "body"}}
<nav class="nav">
  <div class="nav-left">
    <a href="/" class="logo"><span class="logo-text">share<span class="logo-dot">.</span></span></a>
    <span class="nav-subtitle">team activity</span>
  </div>
  <div class="nav-right">
    <a href="/upload" class="btn">Upload</a>
  </div>
</nav>
<div class="container">
  <form method="get" action="/">
    <input type="text" name="q" class="search-box" placeholder="Search documents..." value="{{.Query}}">
  </form>
  {{if .Projects}}
  <div class="filter-chips">
    <a href="/" class="chip{{if not .Project}} chip-active{{end}}">All</a>
    {{range .Projects}}
    <a href="/?project={{.}}" class="chip{{if eq . $.Project}} chip-active{{end}}">{{.}}</a>
    {{end}}
  </div>
  {{end}}
  {{if .Documents}}
  <div class="doc-list">
    {{range .Documents}}
    <a href="/d/{{.ID}}" class="doc-item">
      <span class="doc-format {{.Format}}">{{.Format | upper}}</span>
      <div class="doc-info">
        <div class="doc-title">{{.Title}}</div>
        <div class="doc-meta">
          {{.SizeBytes | filesize}} · {{.CreatedAt | timeago}}{{if .Project}} · <span class="doc-project">{{.Project}}</span>{{end}}{{if .DocType}} · <span class="doc-type">{{.DocType}}</span>{{end}}
        </div>
      </div>
    </a>
    {{end}}
  </div>
  {{if or .HasPrev .HasNext}}
  <div class="pagination">
    {{if .HasPrev}}<a href="/?page={{.PrevPage}}{{if .Query}}&q={{.Query}}{{end}}{{if .Project}}&project={{.Project}}{{end}}">← Newer</a>{{end}}
    {{if .HasNext}}<a href="/?page={{.NextPage}}{{if .Query}}&q={{.Query}}{{end}}{{if .Project}}&project={{.Project}}{{end}}">Older →</a>{{end}}
  </div>
  {{end}}
  {{else}}
  <div class="empty">
    <div class="empty-heading">No documents yet</div>
    <div class="empty-text">AI agent outputs, reports, and analyses will appear here as they're published.</div>
  </div>
  {{end}}
</div>
{{end}}
```

- [ ] **Step 4: Add filter chip and activity feed CSS**

Append to `static/style.css`:

```css
/* ---- Filter chips ---- */
.filter-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 16px;
}
.chip {
  padding: 4px 12px;
  border: 1px solid var(--stone-200);
  border-radius: 20px;
  font-size: 12px;
  font-weight: 500;
  color: var(--stone-600);
  text-decoration: none;
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.chip:hover { background: var(--stone-50); border-color: var(--stone-400); }
.chip-active { background: var(--accent-subtle); border-color: var(--accent); color: var(--accent); }

/* ---- Activity feed meta ---- */
.doc-project {
  font-weight: 600;
  color: var(--accent);
}
.doc-type {
  color: var(--stone-600);
  font-style: italic;
}
.nav-subtitle {
  font-size: 13px;
  color: var(--stone-600);
  font-weight: 400;
}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./... && go test ./...`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add store.go handlers.go templates/home.html static/style.css
git commit -m "feat: activity feed homepage with project filter chips"
```

---

### Task 6: Retention policy — pinned and expires_at

**Files:**
- Modify: `store.go`
- Modify: `handlers.go`
- Modify: `store_test.go`

- [ ] **Step 1: Write failing test — unpinned docs expire after 90 days**

```go
func TestExpiredDocsExcludedFromList(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	// Create a doc with old timestamp
	store.Create("Old Doc", "html", []byte("<p>old</p>"), "public")
	// Manually set created_at to 91 days ago
	old := time.Now().UTC().Add(-91 * 24 * time.Hour).Format(time.RFC3339Nano)
	store.db.Exec(`UPDATE documents SET created_at = ? WHERE title = 'Old Doc'`, old)

	store.Create("New Doc", "html", []byte("<p>new</p>"), "public")

	docs, total, _ := store.List(1, 20, ListFilter{})
	if total != 1 {
		t.Fatalf("expected 1 (non-expired), got %d", total)
	}
	if docs[0].Title != "New Doc" {
		t.Errorf("expected 'New Doc', got %q", docs[0].Title)
	}
}

func TestPinnedDocsNeverExpire(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	store.Create("Pinned Old", "html", []byte("<p>pinned</p>"), "public")
	old := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339Nano)
	store.db.Exec(`UPDATE documents SET created_at = ?, pinned = 1 WHERE title = 'Pinned Old'`, old)

	docs, total, _ := store.List(1, 20, ListFilter{})
	if total != 1 {
		t.Fatalf("expected 1 (pinned), got %d", total)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestExpired -v ./... && go test -run TestPinned -v ./...`
Expected: FAIL — expired docs still returned

- [ ] **Step 3: Add retention filter to List queries**

In `store.go` `List` method, add to the where clauses:

```go
// Retention: exclude unpinned docs older than 90 days
where = append(where, "(pinned = 1 OR created_at > ?)")
args = append(args, time.Now().UTC().Add(-90*24*time.Hour).Format(time.RFC3339Nano))
```

- [ ] **Step 4: Add pin/unpin to PATCH endpoint**

In `handlers.go` `handlePatchDoc`, extend the request struct:

```go
var req struct {
	Title      *string `json:"title"`
	Visibility *string `json:"visibility"`
	Pinned     *bool   `json:"pinned"`
}
```

And in `UpdateParams` in `store.go`:

```go
type UpdateParams struct {
	Title      *string
	Visibility *string
	Pinned     *bool
}
```

Add to `Update`:

```go
if params.Pinned != nil {
	pinVal := 0
	if *params.Pinned { pinVal = 1 }
	if _, err := s.db.Exec(`UPDATE documents SET pinned = ? WHERE id = ?`, pinVal, id); err != nil {
		return fmt.Errorf("update pinned: %w", err)
	}
}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add store.go handlers.go store_test.go
git commit -m "feat: retention policy — 90-day expiry for unpinned docs, pin/unpin via PATCH"
```

---

### Task 7: Update /share skill to send metadata

**Files:**
- Modify: `/Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service/skills/share/SKILL.md`

- [ ] **Step 1: Update the skill's API call template**

In the SKILL.md, update the curl command in `/share <file-path>`:

```bash
curl -s -X POST https://share.eregistrations.dev/api/documents \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $(cat .share-token)" \
  -d '{
    "title": "<title>",
    "format": "<html|md>",
    "content": "<file-contents>",
    "visibility": "private",
    "project": "<project-tag-if-known>",
    "doc_type": "<type: migration-analysis|service-audit|debug-report|implementation-plan|documentation|research>",
    "tags": "<comma-separated-tags>"
  }'
```

- [ ] **Step 2: Update the skill instructions**

Add to the skill's command instructions (after step 3, before confirmation):

```markdown
4. Detect metadata from context:
   - **project**: Use the current git repo name or directory name as a project tag (e.g., `tz` for Tanzania, `rw` for Rwanda). If unsure, leave empty.
   - **doc_type**: Classify the content: `migration-analysis`, `service-audit`, `debug-report`, `implementation-plan`, `documentation`, `research`, or leave empty.
   - **tags**: Extract 2-3 relevant keywords from the content.
```

- [ ] **Step 3: Commit**

```bash
cd /Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government
git add plugins/share-service/skills/share/SKILL.md
git commit -m "feat: send project, doc_type, tags metadata when publishing"
```

---

### Task 8: Integration test — full publish-and-browse flow

**Files:**
- Modify: `handlers_test.go`

- [ ] **Step 1: Write integration test**

```go
func TestFullPublishBrowseFlow(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	// Register publisher
	resp, _ := http.Post(srv.URL+"/api/register", "application/json", strings.NewReader(`{"name":"test-agent"}`))
	var reg map[string]any
	json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	token := reg["token"].(string)

	// Publish with metadata
	req, _ := http.NewRequest("POST", srv.URL+"/api/documents", strings.NewReader(`{
		"title":"Tanzania Migration Report",
		"format":"md",
		"content":"# Migration\n\n- [ ] Step 1\n\n| Table | Works |\n|---|---|\n| Yes | Yes |",
		"visibility":"public",
		"project":"tz",
		"doc_type":"migration-analysis",
		"tags":"migration,tanzania"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ = http.DefaultClient.Do(req)
	var pub map[string]any
	json.NewDecoder(resp.Body).Decode(&pub)
	resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("publish failed: %d", resp.StatusCode)
	}

	// List filtered by project
	resp, _ = http.Get(srv.URL + "/api/documents?project=tz")
	var list struct {
		Documents []map[string]any `json:"documents"`
		Total     int              `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()

	if list.Total != 1 {
		t.Fatalf("expected 1 doc for project=tz, got %d", list.Total)
	}
	if list.Documents[0]["project"] != "tz" {
		t.Errorf("expected project 'tz', got %v", list.Documents[0]["project"])
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test -run TestFullPublishBrowseFlow -v ./...`
Expected: PASS

- [ ] **Step 3: Run full suite**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add handlers_test.go
git commit -m "test: integration test for full publish-browse flow with metadata"
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Schema migration + metadata columns | store.go, store_test.go |
| 2 | Accept metadata in publish API | handlers.go, handlers_test.go |
| 3 | Filter by project/doc_type | store.go, handlers.go, handlers_test.go |
| 4 | OpenGraph meta tags | templates/base.html, view.html |
| 5 | Activity feed homepage | templates/home.html, style.css, handlers.go, store.go |
| 6 | Retention policy (90-day expiry + pin) | store.go, handlers.go, store_test.go |
| 7 | Update /share skill with metadata | SKILL.md |
| 8 | Integration test | handlers_test.go |
