# Share Service Guardrails Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four guardrails to prevent accidental document sharing: default-private visibility, secret scanning, confirmation in the skill, and safe no-args behavior.

**Architecture:** Server-side changes (Go) for default-private and secret scanning. Client-side changes (skill markdown) for confirmation flow and no-args safety. Secret scanning uses regex patterns in a dedicated module, called from the publish handler before persisting.

**Tech Stack:** Go 1.22+, net/http, regexp, existing test harness

---

### Task 1: Default Visibility to Private (Server)

**Files:**
- Modify: `handlers.go:162-163`
- Modify: `store.go:77` (DB default)
- Modify: `handlers_test.go:27-28` (update existing test)
- Modify: `store_test.go:27-28` (update existing test)

- [ ] **Step 1: Write a failing test — publish without visibility defaults to private**

Add to `handlers_test.go`:

```go
func TestAPIPublishDefaultsToPrivate(t *testing.T) {
	srv, store := testServer(t)
	defer srv.Close()

	body := `{"title":"No Vis","format":"html","content":"<p>hi</p>"}`
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
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	doc, _ := store.Get(result.ID)
	if doc.Visibility != "private" {
		t.Fatalf("expected default visibility 'private', got %q", doc.Visibility)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestAPIPublishDefaultsToPrivate -v`
Expected: FAIL — `expected default visibility 'private', got "public"`

- [ ] **Step 3: Change default visibility in handler**

In `handlers.go`, change line 163 from:
```go
	req.Visibility = "public"
```
to:
```go
	req.Visibility = "private"
```

- [ ] **Step 4: Change DB schema default**

In `store.go`, change the DEFAULT in the documents table DDL (line 77) from:
```sql
visibility   TEXT NOT NULL DEFAULT 'public' CHECK(visibility IN ('public', 'private')),
```
to:
```sql
visibility   TEXT NOT NULL DEFAULT 'private' CHECK(visibility IN ('public', 'private')),
```

- [ ] **Step 5: Fix existing tests that assumed public default**

In `handlers_test.go` `TestAPIPublish` (line 27), the test publishes without setting visibility. Since the default is now private, the document won't appear in public lists. The test itself doesn't check visibility, but confirm it still passes as-is — the 201 response and URL/secret/ID fields are unaffected.

In `store_test.go` `TestCreateAndGet` (line 23), the test explicitly passes `"public"`, so it's unaffected.

Run: `go test ./... -v`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add handlers.go store.go handlers_test.go
git commit -m "feat: default visibility to private to prevent accidental public sharing"
```

---

### Task 2: Secret Scanning (Server)

**Files:**
- Create: `secrets.go`
- Create: `secrets_test.go`
- Modify: `handlers.go` (call scanner before persisting)
- Modify: `handlers_test.go` (add rejection tests)

- [ ] **Step 1: Write failing tests for the secret scanner**

Create `secrets_test.go`:

```go
package main

import "testing"

func TestScanSecrets(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool // true = contains secret
	}{
		{"clean html", "<h1>Hello World</h1>", false},
		{"clean markdown", "# Report\nSome text", false},
		{"AWS access key", "AKIAIOSFODNN7EXAMPLE is my key", true},
		{"AWS secret key", "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", true},
		{"private key PEM", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...", true},
		{"private key generic", "-----BEGIN PRIVATE KEY-----\ndata", true},
		{"GitHub token", "ghp_1234567890abcdefghijklmnopqrstuvwxyz", true},
		{"GitHub PAT fine-grained", "github_pat_abcdefghij1234567890", true},
		{"Slack token", "xoxb-1234-5678-abcdefghijklmnop", true},
		{"Slack webhook", "hooks.slack.com/services/T00/B00/xxxx", true},
		{"generic API key", `"api_key": "sk_live_abc123def456"`, true},
		{"generic secret", `"secret_key": "mysupersecretvalue123"`, true},
		{"password in config", `password = "hunter2"`, true},
		{"password in JSON", `"password": "hunter2"`, true},
		{"bearer token", "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJ0ZXN0IjoidGVzdCJ9.abc123", true},
		{"base64 long string unlikely false pos", "data:image/png;base64,iVBORw0KGgo=", false},
		{"short password value", `"password": ""`, false},
		{"password mention in text", "reset your password in settings", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ScanSecrets(tt.content)
			got := len(findings) > 0
			if got != tt.want {
				t.Errorf("ScanSecrets(%q) found=%v, want=%v; findings=%v", tt.name, got, tt.want, findings)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestScanSecrets -v`
Expected: FAIL — `ScanSecrets` not defined

- [ ] **Step 3: Implement the secret scanner**

Create `secrets.go`:

```go
package main

import "regexp"

// SecretFinding describes a detected secret pattern.
type SecretFinding struct {
	Pattern string // human-readable label
}

var secretPatterns = []struct {
	label   string
	pattern *regexp.Regexp
}{
	{"AWS Access Key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"AWS Secret Key", regexp.MustCompile(`(?i)aws[_\-]?secret[_\-]?access[_\-]?key\s*[=:]\s*\S+`)},
	{"Private Key (PEM)", regexp.MustCompile(`-----BEGIN\s+[\w\s]*PRIVATE KEY-----`)},
	{"GitHub Token", regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36,}`)},
	{"GitHub PAT", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`)},
	{"Slack Token", regexp.MustCompile(`xox[bporas]-[0-9A-Za-z\-]+`)},
	{"Slack Webhook", regexp.MustCompile(`hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+`)},
	{"Generic API Key", regexp.MustCompile(`(?i)["']?(?:api[_\-]?key|apikey)["']?\s*[:=]\s*["']?\S{10,}["']?`)},
	{"Generic Secret", regexp.MustCompile(`(?i)["']?(?:secret[_\-]?key|client[_\-]?secret)["']?\s*[:=]\s*["']?\S{10,}["']?`)},
	{"Password", regexp.MustCompile(`(?i)["']?password["']?\s*[:=]\s*["'][^"']{1,}["']`)},
	{"Bearer Token (JWT)", regexp.MustCompile(`Bearer\s+eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`)},
}

// ScanSecrets checks content for common secret/credential patterns.
// Returns a list of findings (empty if clean).
func ScanSecrets(content string) []SecretFinding {
	var findings []SecretFinding
	for _, sp := range secretPatterns {
		if sp.pattern.MatchString(content) {
			findings = append(findings, SecretFinding{Pattern: sp.label})
		}
	}
	return findings
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestScanSecrets -v`
Expected: PASS

- [ ] **Step 5: Commit scanner module**

```bash
git add secrets.go secrets_test.go
git commit -m "feat: add secret scanner for credential detection in shared content"
```

- [ ] **Step 6: Write failing test — API rejects content with secrets**

Add to `handlers_test.go`:

```go
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
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test -run TestAPIPublishRejectsSecrets -v`
Expected: FAIL — 201 instead of 422

- [ ] **Step 8: Add secret scanning to the publish handler**

In `handlers.go`, in `handlePublish`, add after the content size check (after line 161) and before the visibility default (line 162):

```go
	// Scan for secrets/credentials
	if findings := ScanSecrets(req.Content); len(findings) > 0 {
		var patterns []string
		for _, f := range findings {
			patterns = append(patterns, f.Pattern)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{
			"error":    "content contains potentially sensitive data — review and remove secrets before sharing",
			"patterns": patterns,
		})
		return
	}
```

- [ ] **Step 9: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add handlers.go handlers_test.go
git commit -m "feat: reject documents containing detected secrets (422)"
```

---

### Task 3: Add Confirmation to Skill (Skill File)

**Files:**
- Modify: `/Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service/skills/share/skill.md`

- [ ] **Step 1: Update the `/share <file-path>` section**

Replace the current step 4 (the curl call) with a confirmation step. The updated section should be:

```markdown
### `/share <file-path>` — Publish a file

1. Read the file at the given path.
2. Detect format:
   - `.html` files → format `html`
   - `.md` files → format `md`
   - Other files → ask the user which format to use
3. Use the filename (without extension) as the title, or ask the user.
4. **Show a confirmation summary before publishing:**

   ```
   📋 About to share:
   • Title: <title>
   • Format: <format>
   • Visibility: private (unlisted)
   • Size: <file size in KB>
   • Preview: <first 200 characters of content>...

   Publish this document? (The URL will be accessible to anyone with the link)
   ```

   Wait for the user to confirm. If they say no, stop.

5. Call the API:

(rest of the curl call unchanged, but with `"visibility": "private"`)
```

- [ ] **Step 2: Update the curl command to default to private**

Change the curl `-d` JSON in the skill from `"visibility": "public"` to `"visibility": "private"`.

- [ ] **Step 3: Update the Important Notes section**

Change the visibility note from:
```
- **Visibility**: Default is `public`. Use `"visibility": "private"` for unlisted documents.
```
to:
```
- **Visibility**: Default is `private` (unlisted — accessible only via direct link). To make a document appear in the public listing, the user must explicitly request `"visibility": "public"`.
```

- [ ] **Step 4: Commit**

```bash
cd /Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service
git add skills/share/skill.md
git commit -m "feat: add confirmation step and default-private to share skill"
```

---

### Task 4: Fix `/share` No-Args Behavior (Skill File)

**Files:**
- Modify: `/Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service/skills/share/skill.md`

- [ ] **Step 1: Replace the `/share` (no arguments) section**

Replace the current section:

```markdown
### `/share` (no arguments) — Publish from context

If no file path is given:
1. Look at the most recent artifact, output, or content you generated in this conversation.
2. If it's HTML or Markdown, offer to publish it.
3. Ask for a title if one isn't obvious from context.
4. Publish using the same API call as above.
```

With:

```markdown
### `/share` (no arguments) — Publish from context

If no file path is given:
1. Ask the user: **"What would you like to share?"** Present options:
   - Any HTML or Markdown files generated in this conversation (list them by name)
   - "Or specify a file path"
2. **Do NOT auto-select content.** Wait for the user to explicitly choose.
3. Once the user picks content, ask for a title if one isn't obvious.
4. Show the same confirmation summary as `/share <file-path>` (title, format, visibility, size, preview).
5. Wait for confirmation before publishing.
6. Publish using the same API call as above.
```

- [ ] **Step 2: Commit**

```bash
cd /Users/moulaymehdi/.claude/plugins/marketplaces/unctad-digital-government/plugins/share-service
git add skills/share/skill.md
git commit -m "feat: require explicit user selection for no-args share"
```

---

### Task 5: Verify Everything Together

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/moulaymehdi/PROJECTS/software-factory/share-service && go test ./... -v`
Expected: All tests pass.

- [ ] **Step 2: Manual verification — default private**

```bash
curl -s -X POST http://localhost:80/api/documents \
  -H "Content-Type: application/json" \
  -d '{"title":"Test Default","format":"html","content":"<p>hi</p>"}' | jq .
```

Verify the document is created. Then check it's not in public listing:
```bash
curl -s http://localhost:80/api/documents | jq '.total'
```

- [ ] **Step 3: Manual verification — secret scanning**

```bash
curl -s -X POST http://localhost:80/api/documents \
  -H "Content-Type: application/json" \
  -d '{"title":"Bad Doc","format":"html","content":"key: AKIAIOSFODNN7EXAMPLE"}' | jq .
```

Expected: 422 response with `"error"` and `"patterns"` fields.

- [ ] **Step 4: Final commit if any fixups needed, then tag**

```bash
git log --oneline -5
```
