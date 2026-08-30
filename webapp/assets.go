// Package webapp embeds the universal web client in the Go binary.
package webapp

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var embedded embed.FS

func Files() fs.FS {
	root, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	return root
}
