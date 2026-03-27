package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math"
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
	mux.HandleFunc("POST /api/register", h.handleRegister)
	mux.HandleFunc("GET /api/me", h.handleMe)
	mux.HandleFunc("GET /api/me/documents", h.handleMyDocs)
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

func (h *Handlers) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !h.limiter.Allow(ip) {
		w.Header().Set("Retry-After", "60")
		jsonError(w, "rate limit exceeded", 429)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	pub, token, err := h.store.Register(req.Name)
	if err != nil {
		jsonError(w, "internal error", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]any{
		"publisher_id": pub.ID,
		"token":        token,
		"name":         pub.Name,
		"created_at":   pub.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h *Handlers) handleMe(w http.ResponseWriter, r *http.Request) {
	pub := h.authenticatePublisher(r)
	if pub == nil {
		jsonError(w, "unauthorized — provide Authorization: Bearer <token>", 401)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pub)
}

func (h *Handlers) handleMyDocs(w http.ResponseWriter, r *http.Request) {
	pub := h.authenticatePublisher(r)
	if pub == nil {
		jsonError(w, "unauthorized — provide Authorization: Bearer <token>", 401)
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	docs, total, err := h.store.ListByPublisher(pub.ID, page, limit)
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

	var publisherID string
	if pub := h.authenticatePublisher(r); pub != nil {
		publisherID = pub.ID
	}

	doc, secret, err := h.store.CreateWithPublisher(req.Title, req.Format, []byte(req.Content), req.Visibility, publisherID)
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

	var publisherID string
	if pub := h.authenticatePublisher(r); pub != nil {
		publisherID = pub.ID
	}

	err := h.store.Delete(id, secret, publisherID)
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

	var publisherID string
	if pub := h.authenticatePublisher(r); pub != nil {
		publisherID = pub.ID
	}

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

	err := h.store.Update(id, secret, publisherID, &UpdateParams{Title: req.Title, Visibility: req.Visibility})
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

func (h *Handlers) RegisterWeb(mux *http.ServeMux, tmpl *Templates) {
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

		tmpl.Render(w, "home.html", map[string]any{
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

		tmpl.Render(w, "view.html", map[string]any{
			"Doc":     doc,
			"Content": template.HTMLAttr(srcdocEscape(htmlContent)),
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
		tmpl.Render(w, "upload.html", nil)
	})

	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !h.limiter.Allow(ip) {
			tmpl.Render(w, "upload.html", map[string]any{"Error": "Rate limit exceeded. Try again in a minute."})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxContentSize+4096)
		if err := r.ParseMultipartForm(maxContentSize); err != nil {
			tmpl.Render(w, "upload.html", map[string]any{"Error": "Content too large (max 5 MB)."})
			return
		}

		title := strings.TrimSpace(r.FormValue("title"))
		format := r.FormValue("format")
		visibility := "public"
		if r.FormValue("private") == "1" {
			visibility = "private"
		}

		// Try file upload first, fall back to pasted content
		var content string
		file, header, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxContentSize+1))
			if err != nil {
				tmpl.Render(w, "upload.html", map[string]any{"Error": "Failed to read file."})
				return
			}
			if len(data) > maxContentSize {
				tmpl.Render(w, "upload.html", map[string]any{"Error": "File too large (max 5 MB)."})
				return
			}
			content = string(data)

			// Auto-detect format from file extension
			name := strings.ToLower(header.Filename)
			if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") {
				format = "md"
			} else {
				format = "html"
			}

			// Use filename as title if not provided
			if title == "" {
				title = strings.TrimSuffix(strings.TrimSuffix(header.Filename, ".html"), ".md")
			}
		} else {
			content = r.FormValue("content")
		}

		if title == "" || (format != "html" && format != "md") || content == "" {
			tmpl.Render(w, "upload.html", map[string]any{"Error": "Provide a file or paste content, and include a title."})
			return
		}
		if len(title) > maxTitleLen {
			tmpl.Render(w, "upload.html", map[string]any{"Error": "Title too long (max 200 chars)."})
			return
		}

		doc, secret, err := h.store.Create(title, format, []byte(content), visibility)
		if err != nil {
			tmpl.Render(w, "upload.html", map[string]any{"Error": "Failed to save document."})
			return
		}

		url := fmt.Sprintf("%s/d/%s", h.baseURL, doc.ID)
		tmpl.Render(w, "created.html", map[string]any{
			"URL":    url,
			"Secret": secret,
		})
	})
}

// srcdocEscape escapes content for use in an iframe srcdoc attribute.
// Only quotes and ampersands need escaping; < and > must remain literal
// so the iframe interprets them as HTML.
func srcdocEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func (h *Handlers) authenticatePublisher(r *http.Request) *Publisher {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	pub, err := h.store.GetPublisher(token)
	if err != nil {
		return nil
	}
	return pub
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
