// Package sor holds the generation steps (sqlc manifest, then the static check) and the embedded labels.
package sor

import "io/fs"

//go:generate docker run --rm -v $PWD:/src -w /src sqlc/sqlc:1.31.1 generate
//go:generate go run ./cmd/sor check --static

// Labels holds labels/<lang>.json when embedded; nil otherwise.
var Labels fs.FS
