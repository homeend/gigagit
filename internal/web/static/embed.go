// Package static is gg web's embedded page: the SPA gg web serves and the
// shared painters (markdown.js, splitdiff.js, style.css) the overview HTML
// export inlines. A package of its own so a package outside internal/web can
// embed the same bytes (go:embed cannot reach a parent directory). DAG leaf.
package static

import "embed"

//go:embed *.js *.css *.html
var FS embed.FS
