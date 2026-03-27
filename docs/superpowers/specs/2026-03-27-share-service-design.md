# share.eregistrations.dev — Design Spec

A lightweight document sharing service for AI-generated HTML and Markdown. One API call to publish, one permanent URL to share.

## Problem

The team frequently generates HTML and MD documents with AI (reports, analyses, mockups). Sharing them currently requires Teams, which is painful: too many steps to get a link out, and documents don't persist as permanent URLs.

## Solution

A Go service at `share.eregistrations.dev` that:

1. Accepts HTML or MD content via REST API (or manual upload)
2. Stores it with a permanent short URL
3. Renders it in a clean, minimal viewer

Claude Code publishes documents automatically via a skill in the plugin marketplace.

## Repositories

| What | Where |
|------|-------|
| Go server | `unctad-ai/share-service` |
| Claude skill | `UNCTAD-eRegistrations/plugin-marketplace/plugins/share-service/` |
| Coolify config | `unctad-ai/singlewindow-deployments/projects/share.yml` |

## Data Model

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | nanoid, 10 chars (e.g. `a3xK9mPq2v`) |
| `title` | string | Required |
| `format` | enum | `html` or `md` |
| `visibility` | enum | `public` (default) or `private` |
| `secret` | string | Hashed in DB. Plaintext returned only in POST response. Required for DELETE/PATCH. |
| `created_at` | timestamp | Immutable |
| `size_bytes` | int | Content size |

Content is stored as flat files on disk: `data/docs/{id}.html` or `data/docs/{id}.md`. Metadata lives in SQLite (`data/share.db`).

No user accounts. Private documents require the `secret` token (returned at creation) for visibility toggling or deletion.

## API

Base URL: `https://share.eregistrations.dev`

### Publish a document

```
POST /api/documents
Content-Type: application/json

{
  "title": "Kenya Service Mapping",
  "format": "html",
  "content": "<html>...",
  "visibility": "public"
}
```

Response:

```json
{
  "id": "a3xK9mPq2v",
  "url": "https://share.eregistrations.dev/d/a3xK9mPq2v",
  "secret": "sk_8f2a...long-token",
  "created_at": "2026-03-27T10:00:00Z"
}
```

No API key required for publishing. The service is internal to the team.

### Limits

- **Max content size:** 5 MB per document. Requests exceeding this return `413 Payload Too Large`.
- **Rate limit:** 10 publishes per minute per IP. Exceeding returns `429 Too Many Requests` with `Retry-After` header.
- **Title length:** 200 characters max.

### Other endpoints

```
GET    /api/documents/{id}    — get document metadata
DELETE /api/documents/{id}    — delete (requires X-Secret header)
PATCH  /api/documents/{id}    — update title/visibility (requires X-Secret header)
GET    /api/documents         — list public documents (paginated, ?page=1&limit=20&q=search)
```

### Secret header

Management operations require the secret returned at creation:

```
X-Secret: sk_8f2a...
```

## Web Routes

```
GET  /                — homepage: recent public docs feed + upload button
GET  /d/{id}          — view document (rendered)
GET  /d/{id}/raw      — raw content (served as text/plain; charset=utf-8 for both formats)
GET  /upload          — manual upload form
POST /upload          — form submission (redirects to /d/{id})
GET  /api/health      — health check (returns {"status":"ok"})
```

### Document viewer (`/d/{id}`)

Minimal top bar design:

- Thin bar with: logo mark (purple "S" square), document title, date, "Raw" link
- Document content fills the viewport below
- HTML documents rendered in `<iframe sandbox="allow-scripts" srcdoc="...">` — no `allow-same-origin` (prevents cookie/storage access to the host). Scripts run but are fully isolated from the parent frame.
- MD documents converted to HTML server-side using `goldmark` with `html.WithUnsafe(false)` (raw HTML blocks in markdown are escaped, not rendered). Output is rendered in a sandboxed iframe identical to HTML docs — same isolation guarantees for both formats.
- Private documents return 404 (not 403 — don't leak existence)

### Homepage (`/`)

Clean feed layout:

- Header: "share." logo + Upload button
- List of recent public documents, each showing: format indicator dot, title, format/size/age
- Paginated, newest first
- Search box filters by title (SQLite `LIKE` — simple and sufficient)

### Upload form (`/upload`)

Simple form: title input, format selector (HTML/MD), content textarea (or file drop), visibility toggle. Submits as `multipart/form-data`.

After creation, redirects to a one-time confirmation page (`/d/{id}?created=1`) that displays the secret with a "copy" button and a warning that it won't be shown again. From there, a link continues to the document viewer.

The `POST /upload` endpoint accepts both browser form submissions and programmatic multipart uploads (useful for large files that exceed practical JSON payload sizes).

## Deployment

Runs on the existing Coolify-managed singlewindow server alongside voice agent projects.

### DNS

Add an A record for `share.eregistrations.dev` pointing to the singlewindow server IP. This is on the `eregistrations.dev` domain (not `singlewindow.dev`), so the DNS record must be created separately from the existing wildcard.

### Coolify project config

```yaml
name: share
repo: unctad-ai/share-service
branch: main
domain: share.eregistrations.dev
description: Document sharing service for AI-generated HTML/MD
```

### Dockerfile

Multi-stage build:

1. `golang:1.22-alpine` — build the binary
2. `alpine:3.19` — runtime with the single binary + embedded templates

### Persistent storage

Docker volume mounted at `/data`:

```
/data/
├── share.db       — SQLite database
└── docs/          — flat document files ({id}.html, {id}.md)
```

The volume survives container redeploys.

### Resource footprint

Minimal — ~10-20 MB RAM. No external services. Single port (80), Traefik handles TLS.

## Go Project Structure

```
unctad-ai/share-service/
├── main.go                — entry point, router setup
├── handlers.go            — HTTP handlers (API + web)
├── store.go               — SQLite + file storage layer
├── templates/             — Go html/template files (embedded)
│   ├── base.html          — shared layout
│   ├── home.html          — homepage feed
│   ├── view.html          — document viewer
│   ├── created.html       — one-time confirmation page (shows secret)
│   └── upload.html        — upload form
├── static/                — CSS, minimal JS (embedded)
│   └── style.css
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
└── README.md
```

Key dependencies:

- `modernc.org/sqlite` — pure Go SQLite (no CGO)
- `github.com/yuin/goldmark` — Markdown → HTML
- `github.com/matoous/go-nanoid/v2` — ID generation
- Standard library for HTTP, templates, embedding

## Claude Skill (Plugin Marketplace)

Published at `UNCTAD-eRegistrations/plugin-marketplace/plugins/share-service/`.

### Trigger conditions

- User explicitly says "share this", "publish this", "upload this", or asks for a shareable link
- The skill does NOT auto-trigger on file creation — it requires an explicit user request

### Behavior

1. Identifies the target content: reads the most recently written `.html` or `.md` file in the conversation, or accepts a file path argument
2. Infers title from the filename or first heading; prompts user to confirm
3. Calls `POST /api/documents` with title, format, content (content sent as JSON string; for files >1 MB, uses multipart form upload to `POST /upload` instead)
4. Returns the permanent URL to the user
5. Optionally saves `{id, secret, url, title}` to `.share-history.json` in the project root for later management

### Skill structure

```
plugins/share-service/
├── .claude-plugin/
│   └── plugin.json        — plugin metadata
└── skills/
    └── share.md           — skill definition (trigger, API reference, examples)
```

The skill teaches Claude the API contract and when to use it. No local dependencies — it just makes HTTP calls to `share.eregistrations.dev`.

## Operational

### Backup

The `/data` volume should be included in the existing `backup-coolify.sh` cycle. Add a cron job or extend the script to `tar` the share data directory alongside Coolify state exports.

### Retention

No automatic expiry in v1. Documents persist indefinitely. If disk usage becomes a concern, add an optional `expires_at` field and a cleanup goroutine later. At ~5 MB max per doc and dozens per week, storage growth is ~1-2 GB/year — manageable.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| No user accounts | Team tool, not a product. Simple secret-based ownership is sufficient. |
| Immutable content | AI-generated docs are snapshots. New version = new publish. |
| Public by default | Reduces friction — the whole point. Private is opt-in. |
| SQLite + flat files | Light usage (dozens/week). No need for Postgres. Single-file DB, zero ops. |
| Go single binary | Zero runtime deps, trivial Docker image, consistent with user preference. |
| Sandboxed iframe for HTML | Isolates document styles from the viewer shell. Prevents CSS/JS leaks. |
| nanoid IDs | Short, URL-safe, collision-resistant. No sequential IDs to enumerate. |
| No API key for publishing | Internal service. If needed later, add a shared key via env var. |
| Skill in plugin-marketplace | Decoupled from server repo. Installable independently by any team member. |
