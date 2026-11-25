// Package sor holds the generation steps: sqlc manifest, then the static check.
package sor

//go:generate docker run --rm -v $PWD:/src -w /src sqlc/sqlc:1.31.1 generate
//go:generate go run ./cmd/sor check --static
