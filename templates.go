package main

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

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

