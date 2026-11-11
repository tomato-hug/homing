// Package consoleweb embeds the built control panel. The source lives in
// console/; scripts/build-console.sh copies its dist/ output here so `go build`
// works without Node installed.
package consoleweb

import "embed"

// Files is the Vite build output: index.html, favicon.svg and assets/.
//
//go:embed index.html favicon.svg assets
var Files embed.FS
