package sor

import (
	"embed"
	"io/fs"
)

//go:embed labels/*.json
var labels embed.FS

func init() { Labels, _ = fs.Sub(labels, "labels") }
