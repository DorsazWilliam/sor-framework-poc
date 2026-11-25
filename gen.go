// Package sor holds the generation steps (sqlc manifest, then the static check) and the embedded labels.
package sor

import (
	"embed"
	"io/fs"
)

//go:generate docker run --rm -v $PWD:/src -w /src sqlc/sqlc:1.31.1 generate
//go:generate go run ./cmd/sor check --static

//go:embed labels/*.json
var labels embed.FS

// Labels holds labels/<lang>.json.
var Labels, _ = fs.Sub(labels, "labels")
