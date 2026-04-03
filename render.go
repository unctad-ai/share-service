package main

import (
	"bytes"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

var mdRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
			),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
	),
)

// highlightCSS holds the chroma theme CSS, generated once at init.
var highlightCSS string

func init() {
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf strings.Builder
	formatter.WriteCSS(&buf, style)
	highlightCSS = buf.String()
}

// RenderMarkdown converts markdown source to HTML.
func RenderMarkdown(source []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := mdRenderer.Convert(source, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
