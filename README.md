# sor-framework-poc

Toy project. A proof of concept for a super lightweight, performant internal framework for systems of record (Go + Postgres), built to see whether such a framework is vibe-codable.

The slice built on it: time entry, expenses, validation, hour and vacation balances, invoicing and profitability for a services firm.

## Big ideas

- **Tables are the truth, commands change them.** Only inputs are stored. Everything else is computed in one SQL view per root table (its *projection*): amounts, remaining quantities, and named boolean *conditions*.
- **A command is `When` + SQL effect + `Ensure`.** An *action* is a named set of conditions. The command locks its roots, reads the projection, checks `When` before the effect and `Ensure` after it. A refusal names the false conditions; the same data greys out the button in the UI.
- **Locks between roots are structural.** A table is a root or a child of a root; a root can live *under* another root. Writers of the lower root hold the upper one shared, so a period lock and a time entry cannot interleave.
- **The effect writes its own event.** Each effect statement runs as `with eff as (… returning old, new) insert into event …`: one history row per changed row, impossible to forget.
- **Access is data.** Capability = role → commands and views. Reach = rows related to the principal by data, resolved once per request. An anchored query without `@reach` fails the build. An agent acting for a human gets the intersection.
- **The registry is data, not generated code.** Static SQL compiled by sqlc, types read from Postgres, a few Go declarations per module. HTTP + OpenAPI, MCP, CLI, live updates and the generic UI all derive from that one registry.
- **Live is invalidation.** A commit notifies (root table, ids); clients re-run the views that read those roots.

The canonical description is [docs/MODEL.md](docs/MODEL.md); the build spec is [docs/SPEC.md](docs/SPEC.md); choices are in [docs/DECISIONS.md](docs/DECISIONS.md); measurements in [docs/PERF.md](docs/PERF.md).

## Run

Needs Docker.

```sh
docker compose up
```

The first run builds the images (about a minute). Then open http://localhost:8080. The login page of the mock identity provider asks for a username: `admin`, `manager`, `finance` or `user`.

| Service | URL |
|---|---|
| App (UI, API, MCP at `/mcp`) | http://localhost:8080 |
| OpenAPI | http://localhost:8080/openapi.json |
| Mock OIDC provider | http://localhost:8090 |
| Object storage (S3) | http://localhost:8333 |
| Fake accounting server | http://localhost:8070 |

Load the performance dataset (200 users, 600k activities):

```sh
docker compose exec app sor seed perf
```

## Status

Works end to end on a fresh `docker compose up`: keyboard time entry, expenses with receipt OCR, validation, balances, profitability, invoicing (run, PDF, CSV export to the fake accounting server with retry), period locks, master data, history, live updates, MCP.

Also: a dashboard made of tiles (a tile is a wrapped view drawn as a number, bars, a donut, progress bars or a calendar heatmap; hand-made SVG), and a client-side view cache invalidated by the live stream, so navigation and page reloads paint at once.

Known limits:

- Analytic screens over the whole history are slow (0.8 to 1.9 s on 600k activities): rows and totals are two full computations, and balances carry over from the first entry. See [docs/PERF.md](docs/PERF.md).
- Declarative migration of stacked views failed in place a few times during development; a fresh database always applies.
- Not built: event archive, erasure of a person, extensions, webhooks, multi-tenancy.
- No human has reviewed the code; no security review; no load test.

## Layout

```
kernel/     the framework: registry, command pipeline, views, access, events, live, jobs, files, connections
modules/    the domain: schema.sql, queries.sql and a few declarations per module
ui/         Svelte 5 SPA: design system, generic screens, the hand-made time entry screen
cmd/        the `sor` binary and the fake accounting server
```

## License

[The Unlicense](LICENSE).
