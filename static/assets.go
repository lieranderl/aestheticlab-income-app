// Package static embeds the production frontend assets in the Go binary.
package static

import "embed"

// Files holds the compiled CSS, the Datastar client, draft.js, the service
// worker, fonts, icons and the web app manifest. The list is explicit so the
// frontend tests beside them (*.test.js) never reach production.
//
//go:embed app.css datastar.js draft.js sw.js manifest.webmanifest
//go:embed fonts/*.woff2 icons/*.svg icons/*.png
var Files embed.FS
