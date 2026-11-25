package kernel

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema is the concatenated schema of all modules, kernel first.
func (s *Static) Schema() string {
	var b strings.Builder
	for _, m := range s.Modules {
		b.WriteString(m.Schema)
		b.WriteString("\n")
	}
	return b.String()
}

// DomainEmpty reports whether every table declared outside the kernel is empty.
func (s *Static) DomainEmpty(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	core := newRegistry()
	Core.Register(core)
	var parts []string
	for _, t := range s.tableSeq {
		if core.Tables[t] == nil {
			parts = append(parts, "not exists (select from "+ident(t)+")")
		}
	}
	if len(parts) == 0 {
		return true, nil
	}
	var empty bool
	return empty, pool.QueryRow(ctx, "select "+strings.Join(parts, " and ")).Scan(&empty)
}

// EnsurePartitions creates the event partitions of the months −1 to +2 in schema event_parts.
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `do $$
declare m timestamptz;
begin
  create schema if not exists event_parts;
  for i in -1..2 loop
    m := date_trunc('month', sor_now()) + make_interval(months => i);
    execute format('create table if not exists event_parts.%I partition of public.event for values from (%L) to (%L)',
                   'event_' || to_char(m, 'YYYYMM'), m, m + interval '1 month');
  end loop;
end $$`)
	return err
}
