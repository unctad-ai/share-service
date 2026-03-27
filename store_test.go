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
	doc, _, err := s.CreateWithPublisher("Pub Doc", "html", []byte("<p>hi</p>"), "public", pub.ID)
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
	s.CreateWithPublisher("My Doc A", "html", []byte("<p>a</p>"), "public", pub.ID)
	s.CreateWithPublisher("My Doc B", "md", []byte("# b"), "private", pub.ID)
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
	doc, _, _ := s.CreateWithPublisher("To Delete", "html", []byte("<p>bye</p>"), "public", pub.ID)

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
	doc, _, _ := s.CreateWithPublisher("Protected", "html", []byte("<p>x</p>"), "public", pub1.ID)

	err := s.Delete(doc.ID, "", pub2.ID)
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateWithPublisherToken(t *testing.T) {
	s := testStore(t)

	pub, _, _ := s.Register("Update Bot")
	doc, _, _ := s.CreateWithPublisher("Original", "html", []byte("<p>hi</p>"), "public", pub.ID)

	err := s.Update(doc.ID, "", pub.ID, &UpdateParams{Title: strPtr("Updated")})
	if err != nil {
		t.Fatalf("Update with publisher: %v", err)
	}

	got, _ := s.Get(doc.ID)
	if got.Title != "Updated" {
		t.Fatalf("expected 'Updated', got %q", got.Title)
	}
}

func strPtr(s string) *string { return &s }

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
