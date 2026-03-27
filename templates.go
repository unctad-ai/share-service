package main

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/yuin/goldmark"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var md = goldmark.New()

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
	return `<!DOCTYPE html><html><head><style>
body{font-family:'Plus Jakarta Sans',system-ui,sans-serif;max-width:48rem;margin:2rem auto;padding:0 1rem;line-height:1.7;color:#1a1d26}
h1,h2,h3{margin:1.5em 0 0.5em;letter-spacing:-0.01em}
pre{background:#eef0f4;padding:1rem;border-radius:6px;overflow-x:auto}
code{font-size:0.9em;background:#eef0f4;padding:2px 6px;border-radius:4px}
pre code{background:none;padding:0}
table{border-collapse:collapse;width:100%}
th,td{border:1px solid #d8dae2;padding:8px 12px;text-align:left}
img{max-width:100%}
blockquote{border-left:3px solid #d8dae2;margin:1em 0;padding:0.5em 1em;color:#3d4155}
a{color:#2d5be3}
</style></head><body>` + buf.String() + `</body></html>`, nil
}
