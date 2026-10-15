// Package static embeds the production frontend assets in the Go binary.
package static

import "embed"

// Files holds the compiled CSS, the Datastar client, draft.js, fonts,
// icons and the web app manifest.
//
//go:embed *.css *.js *.webmanifest fonts/*.woff2 icons/*.svg icons/*.png
var Files embed.FS
