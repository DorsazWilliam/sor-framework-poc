// Package seed loads the synthetic deterministic dataset.
package seed

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed seed.sql
var script string

// Sizes are the settings seed.sql reads with current_setting('seed.<name>').
var Sizes = map[string]map[string]string{
	"demo": {"size": "demo", "users": "20", "activities": "40000"},
	"perf": {"size": "perf", "users": "200", "activities": "600000"},
}

// Run sets the size settings and executes seed.sql in one transaction.
func Run(ctx context.Context, pool *pgxpool.Pool, size string) error {
	settings, ok := Sizes[size]
	if !ok {
		return fmt.Errorf("unknown seed size %q (demo, perf)", size)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for k, v := range settings {
		if _, err := tx.Exec(ctx, "select set_config($1, $2, true)", "seed."+k, v); err != nil {
			return err
		}
	}
	if script != "" {
		if _, err := tx.Exec(ctx, script); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
