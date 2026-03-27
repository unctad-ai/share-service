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
