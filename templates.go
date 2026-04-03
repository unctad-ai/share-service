package main

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"

	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithFormatOptions(),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
	),
)

type Templates struct {
	pages map[string]*template.Template
}

func loadTemplates() *Templates {
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

	base := template.Must(template.New("base.html").Funcs(funcMap).ParseFS(templateFS, "templates/base.html"))

	pageNames := []string{"home.html", "view.html", "created.html", "upload.html"}
	pages := make(map[string]*template.Template, len(pageNames))

	for _, name := range pageNames {
		t := template.Must(base.Clone())
		template.Must(t.ParseFS(templateFS, "templates/"+name))
		pages[name] = t
	}

	return &Templates{pages: pages}
}

func (t *Templates) Render(w interface{ Write([]byte) (int, error) }, name string, data any) {
	t.pages[name].ExecuteTemplate(w, name, data)
}

func renderMarkdown(source []byte) (string, error) {
	var buf strings.Builder
	if err := md.Convert(source, &buf); err != nil {
		return "", err
	}
	return `<!DOCTYPE html><html><head>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
*{box-sizing:border-box}
body{font-family:'Inter',system-ui,-apple-system,sans-serif;max-width:52rem;margin:2.5rem auto;padding:0 1.5rem;line-height:1.75;color:#1a1d26;font-size:16px}
h1{font-size:2em;font-weight:700;margin:1.8em 0 0.6em;letter-spacing:-0.025em;line-height:1.2;border-bottom:1px solid #e5e7eb;padding-bottom:0.3em}
h2{font-size:1.5em;font-weight:600;margin:1.6em 0 0.5em;letter-spacing:-0.02em;line-height:1.3}
h3{font-size:1.2em;font-weight:600;margin:1.4em 0 0.4em;letter-spacing:-0.015em}
h4{font-size:1.05em;font-weight:600;margin:1.2em 0 0.3em}
p{margin:0.8em 0}
pre{background:#f6f8fa;padding:1rem 1.2rem;border-radius:8px;overflow-x:auto;border:1px solid #e5e7eb;line-height:1.5;margin:1.2em 0}
code{font-family:'JetBrains Mono','Fira Code',monospace;font-size:0.875em}
:not(pre)>code{background:#f0f2f5;padding:2px 6px;border-radius:4px;color:#c7254e}
pre code{background:none;padding:0;color:inherit}
table{border-collapse:collapse;width:100%;margin:1.2em 0;font-size:0.95em}
thead{background:#f6f8fa}
th{font-weight:600;text-align:left;border:1px solid #d0d7de;padding:8px 12px}
td{border:1px solid #d0d7de;padding:8px 12px}
tr:nth-child(even){background:#fafbfc}
img{max-width:100%;border-radius:6px}
blockquote{border-left:4px solid #3b82f6;margin:1.2em 0;padding:0.6em 1.2em;background:#f0f4ff;border-radius:0 6px 6px 0;color:#374151}
blockquote p{margin:0.3em 0}
a{color:#2563eb;text-decoration:none}
a:hover{text-decoration:underline}
hr{border:none;border-top:2px solid #e5e7eb;margin:2em 0}
ul,ol{padding-left:1.6em;margin:0.8em 0}
li{margin:0.3em 0}
li>ul,li>ol{margin:0.2em 0}
input[type="checkbox"]{margin-right:0.4em;transform:scale(1.15);position:relative;top:1px}
strong{font-weight:600}
del{color:#6b7280}
.highlight pre{margin:0;border:none}
</style></head><body>` + buf.String() + `</body></html>`, nil
}
