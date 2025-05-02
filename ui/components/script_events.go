package components

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// ScriptStatus renders a simple status span. Style tweaks can come later.
func ScriptStatus(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, "<span class=\"script-status\">%s</span>", text)
		return err
	})
}

// ScriptOutput renders a line of stdout/stderr.
func ScriptOutput(line string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, "<pre class=\"script-output\">%s</pre>", line)
		return err
	})
}
