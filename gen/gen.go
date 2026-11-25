// Package gen holds the generated manifests.
package gen

import _ "embed"

// SQLC is the sqlc manifest of all modules.
//
//go:embed sqlc.json
var SQLC []byte
