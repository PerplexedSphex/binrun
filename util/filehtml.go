package util

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	hl "github.com/yuin/goldmark-highlighting"
	"github.com/yuin/goldmark/extension"
)

// -----------------------------------------------------------------------------
// tiny cache so we only parse each file once per process
// -----------------------------------------------------------------------------
var (
	cache sync.Map // map[string]string
)

// FileToHTML converts a Markdown or source-code file to ready-to-embed HTML.
//
//	path   – file path inside fsys
//	lang   – "" to auto-detect from extension, or override like "go", "js"
//
// The returned templ.Component is either safe HTML (templ.Raw) or an error.
func FileToHTML(path string, lang string, fsys fs.FS) templ.Component {
	// fast path: already cached
	if v, ok := cache.Load(path); ok {
		return templ.Raw(v.(string))
	}

	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error { return err })
	}

	// language detection / wrapping ------------------------------------------------
	if lang == "" {
		lang = strings.TrimPrefix(filepath.Ext(path), ".") // ".go" -> "go"
	}
	if lang != "" && lang != "md" && lang != "markdown" {
		// wrap in fenced code block so Goldmark + Chroma highlight it
		src = append([]byte("```"+lang+"\n"), append(src, []byte("\n```")...)...)
	}

	// markdown -> HTML -------------------------------------------------------------
	var buf bytes.Buffer
	goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			hl.NewHighlighting(hl.WithStyle("github")), // inline colours
		),
	).Convert(src, &buf)

	htmlStr := buf.String()
	cache.Store(path, htmlStr) // memoise for next call
	return templ.Raw(htmlStr)
}
