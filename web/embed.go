package web

import "embed"

// Assets is populated by npm run build before compiling the production binary.
//
//go:embed all:assets
var Assets embed.FS
