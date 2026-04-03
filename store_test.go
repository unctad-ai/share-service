package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	err := s.Delete(doc.ID, secret, "")
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

	err := s.Delete(doc.ID, "wrong-secret", "")
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateVisibility(t *testing.T) {
	s := testStore(t)
	doc, secret, _ := s.Create("Vis Test", "html", []byte("<p>hi</p>"), "public")

	err := s.Update(doc.ID, secret, "", &UpdateParams{Visibility: strPtr("private")})
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

	err := s.Update(doc.ID, secret, "", &UpdateParams{Title: strPtr("New Title")})
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

	docs, total, err := s.List(1, 20, ListFilter{})
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

	docs, total, err := s.List(1, 20, ListFilter{Query: "kenya"})
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if total != 1 || docs[0].Title != "Kenya Report" {
		t.Fatalf("expected Kenya Report, got %d results: %+v", total, docs)
	}
}

// Publisher tests

func TestRegister(t *testing.T) {
	s := testStore(t)

	pub, token, err := s.Register("Test Bot")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if pub.ID == "" || pub.Name != "Test Bot" || token == "" {
		t.Fatalf("unexpected publisher: %+v, token=%q", pub, token)
	}
	if !startsWith(token, "tok_") {
		t.Fatalf("expected tok_ prefix, got %q", token)
	}
	if !startsWith(pub.ID, "pub_") {
		t.Fatalf("expected pub_ prefix, got %q", pub.ID)
	}
}

func TestGetPublisher(t *testing.T) {
	s := testStore(t)

	pub, token, _ := s.Register("Lookup Bot")

	got, err := s.GetPublisher(token)
	if err != nil {
		t.Fatalf("GetPublisher: %v", err)
	}
	if got.ID != pub.ID || got.Name != "Lookup Bot" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestGetPublisherInvalidToken(t *testing.T) {
	s := testStore(t)

	_, err := s.GetPublisher("tok_invalid")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateWithPublisher(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("Pub Bot")
	doc, _, err := s.CreateWithPublisher(CreateParams{Title: "Pub Doc", Format: "html", Content: []byte("<p>hi</p>"), Visibility: "public", PublisherID: pub.ID})
	if err != nil {
		t.Fatalf("CreateWithPublisher: %v", err)
	}
	if doc.PublisherID != pub.ID {
		t.Fatalf("expected publisher_id %q, got %q", pub.ID, doc.PublisherID)
	}

	got, _ := s.Get(doc.ID)
	if got.PublisherID != pub.ID {
		t.Fatalf("Get: expected publisher_id %q, got %q", pub.ID, got.PublisherID)
	}
}

func TestListByPublisher(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("List Bot")
	s.CreateWithPublisher(CreateParams{Title: "My Doc A", Format: "html", Content: []byte("<p>a</p>"), Visibility: "public", PublisherID: pub.ID})
	s.CreateWithPublisher(CreateParams{Title: "My Doc B", Format: "md", Content: []byte("# b"), Visibility: "private", PublisherID: pub.ID})
	s.Create("Other Doc", "html", []byte("<p>other</p>"), "public") // anonymous

	docs, total, err := s.ListByPublisher(pub.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListByPublisher: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 docs, got %d", total)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	// Includes private docs
	if docs[0].Title != "My Doc B" {
		t.Fatalf("expected My Doc B first, got %q", docs[0].Title)
	}
}

func TestDeleteWithPublisherToken(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("Del Bot")
	doc, _, _ := s.CreateWithPublisher(CreateParams{Title: "To Delete", Format: "html", Content: []byte("<p>bye</p>"), Visibility: "public", PublisherID: pub.ID})

	// Delete using publisher ID (not per-doc secret)
	err := s.Delete(doc.ID, "", pub.ID)
	if err != nil {
		t.Fatalf("Delete with publisher: %v", err)
	}

	_, err = s.Get(doc.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteWrongPublisher(t *testing.T) {
	s := testStore(t)

	pub1, _, _ := s.Register("Owner")
	pub2, _, _ := s.Register("Other")
	doc, _, _ := s.CreateWithPublisher(CreateParams{Title: "Protected", Format: "html", Content: []byte("<p>x</p>"), Visibility: "public", PublisherID: pub1.ID})

	err := s.Delete(doc.ID, "", pub2.ID)
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateWithPublisherToken(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("Update Bot")
	doc, _, _ := s.CreateWithPublisher(CreateParams{Title: "Original", Format: "html", Content: []byte("<p>hi</p>"), Visibility: "public", PublisherID: pub.ID})

	err := s.Update(doc.ID, "", pub.ID, &UpdateParams{Title: strPtr("Updated")})
	if err != nil {
		t.Fatalf("Update with publisher: %v", err)
	}

	got, _ := s.Get(doc.ID)
	if got.Title != "Updated" {
		t.Fatalf("expected 'Updated', got %q", got.Title)
	}
}

func TestMetadataFieldsStoredAndRetrieved(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("Meta Bot")
	doc, _, err := s.CreateWithPublisher(CreateParams{
		Title:        "Meta Doc",
		Format:       "html",
		Content:      []byte("<p>meta</p>"),
		Visibility:   "public",
		PublisherID:  pub.ID,
		Project:      "rw",
		DocType:      "report",
		AgentSession: "sess_abc123",
		Tags:         "migration,audit",
	})
	if err != nil {
		t.Fatalf("CreateWithPublisher: %v", err)
	}
	if doc.Project != "rw" || doc.DocType != "report" || doc.AgentSession != "sess_abc123" || doc.Tags != "migration,audit" {
		t.Fatalf("metadata not set on returned doc: %+v", doc)
	}

	got, err := s.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Project != "rw" {
		t.Fatalf("expected project 'rw', got %q", got.Project)
	}
	if got.DocType != "report" {
		t.Fatalf("expected doc_type 'report', got %q", got.DocType)
	}
	if got.AgentSession != "sess_abc123" {
		t.Fatalf("expected agent_session 'sess_abc123', got %q", got.AgentSession)
	}
	if got.Tags != "migration,audit" {
		t.Fatalf("expected tags 'migration,audit', got %q", got.Tags)
	}
	if got.Pinned != false {
		t.Fatal("expected pinned false by default")
	}
}

func TestCreateConvenienceMethodEmptyMetadata(t *testing.T) {
	s := testStore(t)

	doc, _, err := s.Create("Simple", "html", []byte("<p>simple</p>"), "public")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Project != "" || got.DocType != "" || got.AgentSession != "" || got.Tags != "" {
		t.Fatalf("expected empty metadata, got project=%q doc_type=%q agent_session=%q tags=%q", got.Project, got.DocType, got.AgentSession, got.Tags)
	}
	if got.Pinned != false {
		t.Fatal("expected pinned false")
	}
}

func TestUpdatePinned(t *testing.T) {
	s := testStore(t)
	doc, secret, _ := s.Create("Pin Test", "html", []byte("<p>pin</p>"), "public")

	pinTrue := true
	err := s.Update(doc.ID, secret, "", &UpdateParams{Pinned: &pinTrue})
	if err != nil {
		t.Fatalf("Update pinned: %v", err)
	}

	got, _ := s.Get(doc.ID)
	if !got.Pinned {
		t.Fatal("expected pinned true after update")
	}

	pinFalse := false
	err = s.Update(doc.ID, secret, "", &UpdateParams{Pinned: &pinFalse})
	if err != nil {
		t.Fatalf("Update pinned false: %v", err)
	}

	got, _ = s.Get(doc.ID)
	if got.Pinned {
		t.Fatal("expected pinned false after second update")
	}
}

func TestMetadataInList(t *testing.T) {
	s := testStore(t)

	s.CreateWithPublisher(CreateParams{Title: "RW Report", Format: "html", Content: []byte("<p>rw</p>"), Visibility: "public", Project: "rw", DocType: "report", Tags: "migration"})
	s.Create("Plain Doc", "html", []byte("<p>plain</p>"), "public")

	docs, total, err := s.List(1, 20, ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2, got %d", total)
	}

	// Find the RW Report
	var found bool
	for _, d := range docs {
		if d.Title == "RW Report" {
			found = true
			if d.Project != "rw" || d.DocType != "report" || d.Tags != "migration" {
				t.Fatalf("metadata missing in list: %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("RW Report not found in list")
	}
}

func TestMetadataInListByPublisher(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("List Meta Bot")
	s.CreateWithPublisher(CreateParams{Title: "Pub Meta Doc", Format: "html", Content: []byte("<p>pm</p>"), Visibility: "public", PublisherID: pub.ID, Project: "tz", Tags: "audit"})

	docs, total, err := s.ListByPublisher(pub.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListByPublisher: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1, got %d", total)
	}
	if docs[0].Project != "tz" || docs[0].Tags != "audit" {
		t.Fatalf("metadata missing in ListByPublisher: %+v", docs[0])
	}
}

func strPtr(s string) *string { return &s }

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestExpiredDocsExcludedFromList(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	store.Create("Old Doc", "html", []byte("<p>old</p>"), "public")
	old := time.Now().UTC().Add(-91 * 24 * time.Hour).Format(time.RFC3339Nano)
	store.db.Exec(`UPDATE documents SET created_at = ? WHERE title = 'Old Doc'`, old)

	store.Create("New Doc", "html", []byte("<p>new</p>"), "public")

	docs, total, _ := store.List(1, 20, ListFilter{})
	if total != 1 {
		t.Fatalf("expected 1 non-expired doc, got %d", total)
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
		t.Fatalf("expected 1 pinned doc, got %d", total)
	}
	if docs[0].Title != "Pinned Old" {
		t.Errorf("expected 'Pinned Old', got %q", docs[0].Title)
	}
}
