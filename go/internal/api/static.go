package api

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticEmbed embed.FS

// StaticFiles returns the commander UI filesystem.
func StaticFiles() fs.FS {
	return staticEmbed
}
