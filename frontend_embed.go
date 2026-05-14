package main

import "embed"

// embeddedFrontend contains the production web UI. The build expects web/dist
// to exist; run `cd web && npm run build` before compiling release binaries.
//
//go:embed web/dist/* web/dist/assets/*
var embeddedFrontend embed.FS
