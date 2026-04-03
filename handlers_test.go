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

func TestAPIPublishDefaultsToPrivate(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := `{"title":"Default Visibility","format":"html","content":"<p>test</p>"}`
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
		ID         string `json:"id"`
		Visibility string `json:"visibility"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Visibility != "private" {
		t.Fatalf("expected visibility 'private', got %q", result.Visibility)
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

	// Private (unlisted) docs are accessible via direct link, just not in listings
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for private doc via direct link, got %d", resp.StatusCode)
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

// Publisher API tests

func TestAPIRegister(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := `{"name":"Test Bot"}`
	resp, err := http.Post(srv.URL+"/api/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	var result struct {
		PublisherID string `json:"publisher_id"`
		Token       string `json:"token"`
		Name        string `json:"name"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.PublisherID == "" || result.Token == "" {
		t.Fatalf("missing fields: %+v", result)
	}
	if result.Name != "Test Bot" {
		t.Fatalf("expected name 'Test Bot', got %q", result.Name)
	}
	if !strings.HasPrefix(result.Token, "tok_") {
		t.Fatalf("expected tok_ prefix: %q", result.Token)
	}
}

func TestAPIRegisterEmptyName(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/register", "application/json", strings.NewReader(`{}`))
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201 with empty name, got %d", resp.StatusCode)
	}
}

func TestAPIMe(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	pub, token, _ := store.Register("Me Bot")

	req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result Publisher
	json.NewDecoder(resp.Body).Decode(&result)
	if result.ID != pub.ID {
		t.Fatalf("expected %q, got %q", pub.ID, result.ID)
	}
}

func TestAPIMeUnauthorized(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/api/me")
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAPIMyDocs(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	pub, token, _ := store.Register("Docs Bot")
	store.CreateWithPublisher(CreateParams{Title: "My Doc", Format: "html", Content: []byte("<p>mine</p>"), Visibility: "public", PublisherID: pub.ID})
	store.CreateWithPublisher(CreateParams{Title: "My Private", Format: "md", Content: []byte("# private"), Visibility: "private", PublisherID: pub.ID})
	store.Create("Other Doc", "html", []byte("<p>other</p>"), "public")

	req, _ := http.NewRequest("GET", srv.URL+"/api/me/documents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Documents []Document `json:"documents"`
		Total     int        `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Total != 2 {
		t.Fatalf("expected 2 docs (including private), got %d", result.Total)
	}
}

func TestAPIPublishWithToken(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	pub, token, _ := store.Register("Publish Bot")

	body := `{"title":"Token Doc","format":"html","content":"<p>hi</p>"}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/documents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	doc, _ := store.Get(result.ID)
	if doc.PublisherID != pub.ID {
		t.Fatalf("expected publisher_id %q, got %q", pub.ID, doc.PublisherID)
	}
}

func TestAPIDeleteWithToken(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	pub, token, _ := store.Register("Del Bot")
	doc, _, _ := store.CreateWithPublisher(CreateParams{Title: "To Delete", Format: "html", Content: []byte("<p>bye</p>"), Visibility: "public", PublisherID: pub.ID})

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/documents/"+doc.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestAPIPatchWithToken(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	pub, token, _ := store.Register("Patch Bot")
	doc, _, _ := store.CreateWithPublisher(CreateParams{Title: "Original", Format: "html", Content: []byte("<p>hi</p>"), Visibility: "public", PublisherID: pub.ID})

	body := `{"title":"Updated via Token"}`
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/documents/"+doc.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	got, _ := store.Get(doc.ID)
	if got.Title != "Updated via Token" {
		t.Fatalf("expected 'Updated via Token', got %q", got.Title)
	}
}

func TestAPIPublishRejectsSecrets(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	tests := []struct {
		name    string
		content string
	}{
		{"AWS key", "<p>key: AKIAIOSFODNN7EXAMPLE</p>"},
		{"private key", "<p>-----BEGIN RSA PRIVATE KEY-----</p>"},
		{"password", `<p>config: {"password": "hunter2"}</p>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"title":"` + tt.name + `","format":"html","content":"` + strings.ReplaceAll(tt.content, `"`, `\"`) + `"}`
			resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
			defer resp.Body.Close()

			if resp.StatusCode != 422 {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 422, got %d: %s", resp.StatusCode, b)
			}

			var result struct {
				Error    string   `json:"error"`
				Patterns []string `json:"patterns"`
			}
			json.NewDecoder(resp.Body).Decode(&result)
			if result.Error == "" || len(result.Patterns) == 0 {
				t.Fatalf("expected error with patterns, got: %+v", result)
			}
		})
	}
}

func TestAPIPublishAllowsCleanContent(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := `{"title":"Clean Doc","format":"html","content":"<h1>Hello World</h1><p>This is safe.</p>"}`
	resp, _ := http.Post(srv.URL+"/api/documents", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}
}

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

	// Fetch and verify metadata persisted
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
	if doc["tags"] != "migration,tanzania" {
		t.Errorf("expected tags 'migration,tanzania', got %v", doc["tags"])
	}
}

func TestAPIListFilterProject(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	store.Create("Global Doc", "html", []byte("<p>g</p>"), "public")
	store.CreateWithPublisher(CreateParams{Title: "TZ Report", Format: "html", Content: []byte("<p>tz</p>"), Visibility: "public", Project: "tz"})
	store.CreateWithPublisher(CreateParams{Title: "RW Analysis", Format: "md", Content: []byte("# rw"), Visibility: "public", Project: "rw"})

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
	if result.Documents[0].Title != "TZ Report" {
		t.Errorf("expected 'TZ Report', got %q", result.Documents[0].Title)
	}
}

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

	// Verify document ID returned
	id, ok := pub["id"].(string)
	if !ok || id == "" {
		t.Fatal("expected document ID in response")
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
	if list.Documents[0]["doc_type"] != "migration-analysis" {
		t.Errorf("expected doc_type 'migration-analysis', got %v", list.Documents[0]["doc_type"])
	}

	// Verify publisher's documents endpoint
	req2, _ := http.NewRequest("GET", srv.URL+"/api/me/documents", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp, _ = http.DefaultClient.Do(req2)
	var myDocs struct {
		Documents []map[string]any `json:"documents"`
		Total     int              `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&myDocs)
	resp.Body.Close()

	if myDocs.Total != 1 {
		t.Fatalf("expected 1 doc in my documents, got %d", myDocs.Total)
	}
}
