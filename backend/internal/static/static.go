// Package static embeds the compiled frontend dist into the binary.
// The dist/ directory is populated at build time by the Docker frontend-builder stage
// (or by running `pnpm build` locally and copying the output here).
package static

import "embed"

// FS holds the embedded frontend dist directory.
// Files are served from the root of the embed FS (i.e., dist/index.html → "/index.html").
//
//go:embed dist
var FS embed.FS
