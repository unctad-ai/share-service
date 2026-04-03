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
