package main

import (
	"strings"
	"testing"
)

func TestRenderMarkdownFeatures(t *testing.T) {
	md := []byte("# Hello\n\n- [ ] task\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n```python\nprint('hi')\n```\n")
	out, err := RenderMarkdown(md)
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)

	checks := []struct {
		name, substr string
	}{
		{"GFM table", "<table"},
		{"task list checkbox", "checkbox"},
		{"syntax highlight", "chroma"},
		{"heading ID", `id="hello"`},
	}
	for _, c := range checks {
		if !strings.Contains(html, c.substr) {
			t.Errorf("missing %s (looking for %q in %d bytes)", c.name, c.substr, len(out))
		}
	}
	t.Logf("rendered %d bytes", len(out))
}

func TestHighlightCSSGenerated(t *testing.T) {
	if highlightCSS == "" {
		t.Error("highlightCSS is empty")
	}
	if !strings.Contains(highlightCSS, ".chroma") {
		t.Error("highlightCSS missing .chroma selector")
	}
}

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // substring that should be present
		block string // substring that should NOT be present
	}{
		{
			name:  "strips script tags",
			input: `<h1>Hello</h1><script>alert('xss')</script><p>World</p>`,
			want:  "<h1>Hello</h1>",
			block: "<script",
		},
		{
			name:  "strips event handlers",
			input: `<p onclick="alert('xss')">Click me</p>`,
			want:  "<p>Click me</p>",
			block: "onclick",
		},
		{
			name:  "preserves safe content",
			input: `<h1 id="title">Hello</h1><p class="intro" style="color:red">World</p>`,
			want:  `id="title"`,
		},
		{
			name:  "preserves tables",
			input: `<table><tr><td>A</td></tr></table>`,
			want:  "<table>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := string(SanitizeHTML([]byte(tt.input)))
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Errorf("expected %q in output, got: %s", tt.want, out)
			}
			if tt.block != "" && strings.Contains(out, tt.block) {
				t.Errorf("expected %q to be stripped, got: %s", tt.block, out)
			}
		})
	}
}
