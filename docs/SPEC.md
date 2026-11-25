# Spec

Traduction de [MODEL.md](./MODEL.md) en code pour la slice de [SLICE.md](./SLICE.md). Ce document fait foi pour l'implémentation ; une divergence trouvée en codant = retour ici, pas de patch. Décisions et alternatives écartées : [DECISIONS.md](./DECISIONS.md). Budgets et mesures : [PERF.md](./PERF.md).

Règle du repo : public et générique. Aucun nom de produit, société, client ou personne ; aucun code, texte ou image propriétaire.

## 1. Idée centrale

Le registry est une donnée, pas du code généré :

- `schema.sql` (tables, projections) + `queries.sql` (views, effets) = tout le SQL, statique, compilé par sqlc
- sqlc produit un manifest JSON (`gen/sqlc.json`) : texte des queries, params nommés, commentaires
- Postgres est l'autorité des types : au démarrage le kernel prépare chaque query (types des params et des colonnes) et lit le catalog (tables, colonnes, FK, commentaires)
- Les déclarations Go d'un module (`module.go`) ajoutent ce que le SQL ne dit pas : roots, anchors, capabilities, actions, commands, jobs, écrans
- De ce registry sortent, sans code par opération : HTTP + OpenAPI (Huma), MCP, CLI, métadonnées de l'UI générique, client TypeScript

Aucun Go généré. Un module = SQL + ~100 lignes de déclarations.

## 2. Stack

| Rôle | Choix |
|---|---|
| Langage | Go 1.27, `net/http`, `log/slog` |
| DB | Postgres 18, pgx v5 (pool, pipeline/batch) |
| SQL compile | sqlc 1.31.1, codegen `json` seul, analyseur statique |
| Migrations | pgschema 2.0 (diff déclaratif, plan DB externe), squawk 2.74 (lint du plan) |
| HTTP / OpenAPI | Huma v2.41, adapter `humago`, opérations enregistrées dynamiquement |
| MCP | `modelcontextprotocol/go-sdk` v1.8, Streamable HTTP stateless |
| Jobs | River v0.51, driver `riverpgxv5`, schema `river` |
| Object storage | SeaweedFS 4.55 (S3), AWS SDK Go v2 |
| OIDC | `navikt/mock-oauth2-server` 7.0 ; `coreos/go-oidc` v3 + `x/oauth2` |
| PDF / OCR | Typst 0.15 (CLI), tesseract 5 (CLI, fra+deu+eng) |
| UI | Svelte 5 (runes) + TypeScript + Vite 8, bits-ui 2 (primitives headless), CSS tokens maison, `unplugin-icons` + Fluent System Icons |
| Client TS | `openapi-typescript` 7 (types) + `fetch` |
| Perf | Playwright 1.65 |

## 3. Layout

```
cmd/sor/            binaire : up, serve, migrate, seed, check, openapi, call, view, meta
cmd/fakeacct/       faux serveur de comptabilité
kernel/             package kernel + schema.sql + queries.sql
modules/base/       schema.sql, queries.sql, module.go      (company, users, groupes, données de base)
modules/time/       idem                                     (activities, frais, validation, locks, soldes)
modules/billing/    idem + invoice.typ                       (invoices, rentabilité)
modules/accounting/ idem                                     (connection, pull, push)
seed/               seed synthétique déterministe
labels/             fr.json, de.json, en.json
gen/                sqlc.json, openapi.json (commités)
ui/                 SPA
deploy/             Dockerfile, idp.json, s3.json
perf/               harness Playwright
compose.yaml, sqlc.yaml, pgschema.toml, .squawk.toml
```

Ordre des schemas (sqlc et pgschema) : kernel, base, time, billing, accounting.

## 4. Conventions SQL

- snake_case, singulier ; identifiants toujours quotés par le kernel (`"user"`)
- Toute table : `id bigint generated always as identity primary key` ; unicité composite par contrainte `unique`
- Une colonne FK porte le même nom partout où elle cible la même table (`user_id`, `manager_id`, `validated_by` → `user`) ; le kernel résout « colonne → table référencée » par nom, depuis le catalog ; un nom ambigu échoue au check
- Suffixes = format : `_min` durée en minutes (h:mm), `_tod` minute du jour (HH:MM), `_pct`, `_coef`, `_at` instant ; `numeric(…,2)` = montant ; `date` = date
- `archived boolean not null default false` sur les données de base ; archiver = update
- Description anglaise de chaque table, colonne et view par `comment on` ; de chaque query par ses lignes de commentaire
- Enum Postgres pour toute liste fermée
- Projection de `t` : view `t_p` = colonnes de `t` + valeurs calculées + conditions (booléens) ; `left join` sur clés uniques et sous-requêtes scalaires gardées par `case`, pour que le planner retire ce qu'une query n'utilise pas
- Aucune syntaxe propre à PG18 dans `schema.sql` et `queries.sql` (le parser de sqlc suit PG17) ; le SQL construit par le kernel l'utilise (`returning old, new`)
- Le temps : `sor_now()` uniquement, jamais `now()` ni l'horloge de Go
- Fuseau de la database : `Europe/Zurich`
- Invariants structurels dans la DB : FK, unique, check, not null, exclusion ; trigger seulement pour `append_only`

## 5. Kernel : schema

```sql
create function sor_now() returns timestamptz language sql stable as
$$ select coalesce(nullif(current_setting('sor.now', true), '')::timestamptz, transaction_timestamp()) $$;
create function sor_today() returns date language sql stable as $$ select sor_now()::date $$;
create function sor_diff(o jsonb, n jsonb) returns jsonb language sql immutable as
$$ select coalesce(jsonb_object_agg(k, jsonb_build_array(o->k, n->k)), '{}'::jsonb)
   from jsonb_object_keys(coalesce(n, o)) k where (o->k) is distinct from (n->k) $$;
create function sor_append_only() returns trigger ...  -- raise, errcode 23514
```

| Table | Colonnes | Notes |
|---|---|---|
| `file` | hash (sha256 hex, unique), size, type, name, created_at | blob = clé `hash` dans le bucket |
| `event` | at, call uuid, command, principal_id, for_id, tbl, row_id, root_table, root_id, changes jsonb | partitionnée par mois sur `at` ; partitions dans le schema `event_parts` ; index (root_table, root_id, at) |
| `run` | name, input jsonb, principal_id, for_id, state (queued, running, done, cancelled, failed), total, done, rejected jsonb, error, cancel_requested, created_at, finished_at | root ; anchor principal_id, reach `self` |
| `saved_view` | owner_id, screen, name, state jsonb ; unique (owner_id, screen, name) | root ; anchor owner_id, reach `self` |
| `connection` | kind, name, endpoint, secret_name, principal_id, cursor, pulled_at, pull_error | root |
| `delivery` | connection_id, kind, root_table, root_id, payload jsonb, principal_id, created_at | root, append_only |
| `delivery_result` | delivery_id, at, ok, status, detail | child de delivery, append_only |

`principal_id`, `for_id`, `owner_id` : ids de la table des principals du module base (`user`), sans FK (tampons de log).

## 6. Kernel : déclarations

```go
type Module struct { Name string; Register func(r *Registry) }

r.Table(name, opts...)
    Root()                       // défaut
    ChildOf(root, fkColumn)      // ses commands lockent le root parent
    Under(root)                  // ses commands tiennent ce root en for share
    Anchor(column, reach)        // rows visibles et modifiables si column ∈ reach du principal
    Read(fn) / Write(fn)         // capability ; "" = tout principal authentifié
    Guarded(columns...)          // colonnes hors CRUD générique (posées par des commands dédiées)
    Label(column)                // défaut "name"
    AppendOnly() / NoCRUD()
    Screen(group)                // écran générique de données de base

r.Action(table, name, conditions...)          // action = AND de conditions de t_p
r.Command(name, opts...)
    On(table)                    // table root ou child ; input `ids` sauf Rootless
    Rootless()                   // pas de root existant (upsert, create dédié)
    When(action)                 // tenue par l'état avant l'effet
    Ensure(action)               // tenue par l'état après l'effet ; "table.action" = sur les roots touchés d'une autre table
    Effect(queries...)           // statements :exec du manifest, dans l'ordre
    Needs(fn)                    // capability
    Reach(name)                  // reach autre que celui de la table (ex. "team")
r.Job(kind, fn, opts...)         // As(subject), Every(d), Unique()
r.Reaction(command, jobKind)     // au commit de la command, le job est inséré dans la transaction
r.Run(name, candidatesView, perRootCommand, Needs(fn))
r.Screen(spec)
```

Dérivé, jamais déclaré :

- Les views : toute query `:many`/`:one` du manifest ; annotations dans son commentaire : `@anchor <colonne> <reach>` ou `@anchor none` (obligatoire), `@needs <fn>` (optionnel)
- L'input d'une view ou d'une command : les params de ses queries moins les params du kernel (`@reach`, `@self`, `@ids`), typés par Postgres
- Les colonnes de sortie et leurs types : `PREPARE`
- Les roots dont dépend une view (live) : `EXPLAIN (GENERIC_PLAN)` de la query complète → tables lues → roots
- CRUD de toute table non `NoCRUD` : `t.create`, `t.update`, `t.delete` ; view liste `t` (sur `t_p` si elle existe) et `t.lookup` (id, label) ; un module remplace `t.lookup` en écrivant la query `TLookup`
- Actions par défaut : `editable` et `deletable`, sans condition, si le module ne les déclare pas

Nom d'une view dans le registry = nom sqlc en snake_case (`ActivityGrid` → `activity_grid`). Command = `table.verbe`.

## 7. Exécution

### View

1. principal → capability → reach (1 aller-retour)
2. `select coalesce(json_agg(t), '[]') from (<wrap>) t` : Postgres produit le JSON, le kernel le transmet tel quel (1 aller-retour)

Wrap = `select <cols | group, agg> from (<query>) v where <filter> group by … order by … limit … offset …`. Colonnes validées contre les colonnes de sortie, valeurs en params. `@reach` reste dans la query.

```json
{"params": {...}, "filter": [{"col","op","value"}], "sort": [{"col","dir"}],
 "group": ["col"], "agg": [{"col","fn"}], "limit": 200, "offset": 0}
```

`op` : eq, ne, lt, le, gt, ge, in, contains, null, notnull. `fn` : sum, count, min, max, avg. `limit` ≤ 5000, défaut 200.

### Command

Même ordre pour humain, agent, reaction et job.

1. principal → capability → reach (aller-retour 1)
2. `begin` ; roots `Under` en `for share` ; roots en `for update`, ordre (table, id) ; lecture de la projection des roots (aller-retour 2)
3. Root absent ou anchor hors reach → `not_found`
4. `When` fausse sur un root → Rejection : pour chaque root, les conditions fausses et sa row de projection
5. Effets, puis relecture de la projection des roots touchés (aller-retour 3)
6. Anchor hors reach après l'effet → `not_found` ; `Ensure` fausse → Rejection ; rollback
7. Jobs des reactions (River `InsertTx`), `pg_notify`, `commit` (aller-retour 4)

Règles :

- Root d'une command sur une table child = la row parente ; `When`/`Ensure` sont des actions du root
- CRUD générique : create = `Ensure(editable)` ; update = `When(editable)` + `Ensure(editable)` ; delete = `When(deletable)` et supprime d'abord les rows des tables child
- CRUD d'une table child : `When(editable)` + `Ensure(editable)` du root
- Chaque statement d'effet est un DML simple (pas de `with` en tête, pas de `returning`). Le kernel l'exécute enveloppé : l'effet et ses events sont un seul statement

```sql
with eff as (<dml> returning to_jsonb(old) as o, to_jsonb(new) as n)
insert into event(call, command, principal_id, for_id, tbl, row_id, root_table, root_id, changes)
select $c, $cmd, $p, $f, '<tbl>', (coalesce(n,o)->>'id')::bigint,
       '<root>', (coalesce(n,o)->>'<fk du root | id>')::bigint, sor_diff(o, n)
from eff where o is distinct from n
returning tbl, row_id, root_table, root_id
```

- Les rows retournées = ce que la command a changé : réponse, clés de live, roots à relire, deliveries à envoyer
- Un effet qui change d'autres roots que les siens les locke lui-même : sous-requête `… order by id for update`
- Erreur de contrainte Postgres (23xxx) → `constraint` avec le nom de la contrainte
- Budget : ≤ 4 allers-retours DB par command, ≤ 2 par view ; aucun cache de principal

### Résultats

| HTTP | code | Corps |
|---|---|---|
| 200 | | `{"changed":[{"table","ids"}], "run": id?}` ou les rows d'une view |
| 401 | `unauthenticated` | |
| 403 | `forbidden` | `needs` |
| 404 | `not_found` | |
| 409 | `rejected` | `action`, `rejected: [{id, conditions: [...], row}]` |
| 409 | `constraint` | `constraint` |
| 422 | `invalid` | erreurs de schema (RFC 9457) |

## 8. Access

- Authentification : JWT de l'IdP, vérifié par JWKS ; cookie de session HttpOnly SameSite=Strict pour le navigateur (flow authorization code + PKCE côté serveur : `/auth/login`, `/auth/callback`, `/auth/logout`), `Authorization: Bearer` pour agents, CLI et services (client credentials)
- `sub` → `user.subject` ; sujet inconnu → 403 ; claim `act_for` = sujet de l'humain pour qui l'agent agit
- Contrat du module base : la query `Principal(@subjects)` rend par sujet `id, kind, name, lang, functions fn[], reach jsonb` (`{"self":[…],"team":[…],"user":[…]}`)
- Agent pour un humain : fonctions et reach = intersection des deux rows ; `@self` = l'humain ; l'event porte `principal_id` (agent) et `for_id` (humain)
- Reach, dans le module base : `self` = lui-même ; `team` = ses subordonnés directs (`scope_direct`), tout son arbre (`scope_tree`) ou tous les humains sauf lui (`scope_all`) ; `user` = `self` ∪ `team`
- Fonctions (enum `fn`) : activity, expense, invoice, invoice_state, period_lock, validation, cost, master_data, scope_direct, scope_tree, scope_all, accounting_sync, ocr ; un groupe `admin` les a toutes ; un principal a l'union de ses groupes
- Build : `sor check --static` (dans `go generate` et dans le Dockerfile) refuse une query sans `@anchor`, et une query anchorée sans param `reach` ; il refuse aussi un `@needs` hors enum, une query `:exec` sans command, un label fr manquant
- Donnée sensible = table ou view séparée avec sa capability : `cost_rate`, `activity_cost`, `profitability_cost`
- L'émetteur OIDC est une seule URL (`http://localhost:8090/default`) pour le navigateur et l'app ; dans compose, le client HTTP OIDC de l'app compose l'adresse `localhost:8090` vers `idp:8080`

## 9. Event log et live

- Une row `event` par row changée ; `call` regroupe une invocation ; seed = chargement en masse, sans events
- Historique d'une row : view kernel `history(root_table, root_id)`, servie après lecture du root par sa view liste (capability, reach)
- Partitions : créées par `sor migrate` (mois courant −1 à +2) et par un job mensuel, dans `event_parts`
- Live : au commit, `pg_notify('sor_live', {"t": root_table, "ids": […], "a": [anchors], "c": command, "e": call})` ; plus de 200 ids → `ids` omis (toute la table)
- Chaque instance a une connexion `LISTEN` et diffuse aux streams SSE (`GET /api/live`) ; un stream reçoit une clé si la table n'a pas d'anchor ou si `a` ∩ son reach ≠ ∅
- Le client relance ses views dont les roots contiennent `t` (debounce 50 ms) ; reconnexion = tout relancer ; le serveur ne garde aucun état d'abonnement
- Progress d'un run : update de sa row `run` hors transaction du run, même canal

## 10. Jobs, reactions, runs

- Un seul kind River (`sor`) : `{kind, args, subjects}` → `JobFunc(ctx, k, job)` ; un job appelle views et commands par le kernel, avec ses principals
- Reaction : au commit de la command, un job par invocation, args `{ids, call}`, unique par (kind, call)
- Jobs périodiques : `Every`
- Run : `POST /api/c/<run>` insère la row `run` et son job dans une transaction → `{"run": id}`. Le job : candidats = rows de la view (chaque row = input de la command par root) ; une seule transaction ; un savepoint par root ; Rejection d'un root → noté dans `run.rejected`, on continue ; `done` mis à jour toutes les 200 ms ; `run.cancel` pose `cancel_requested`, le job le lit entre deux roots → rollback, state `cancelled`
- Jobs du kernel : `run.execute`, `delivery.send`, `event.partition` (mensuel), `file.gc` (quotidien : rows `file` sans référence FK, puis leur blob)

## 11. Fichiers

- Upload : le client calcule le sha256 ; `POST /api/files/presign {hash, size, type}` → URL PUT signée 5 min, checksum sha256 signé (le storage refuse un contenu différent), ou `exists` ; `POST /api/files {hash, name, type}` → `HEAD` du blob → row `file` → `{id}` ; puis une command attache l'id à une row
- Download : `POST /api/file {view, params, id, column}` → le kernel exécute la view (capability, reach) filtrée sur `id`, lit la colonne (id de file) → URL GET signée 5 min `{url, name, type}`
- URLs signées pour l'endpoint public du storage (`http://localhost:8333`) ; l'app parle au storage par son nom interne
- Généré : job → view (JSON) + template Typst → PDF → blob par son hash → row `file` → command d'attache ; `typst compile --ignore-system-fonts --creation-timestamp <epoch de la donnée>` ; même donnée = mêmes octets = même hash

## 12. Systèmes externes

- Secret d'une connection : variable d'environnement `SECRET_<secret_name>` (le vault du POC)
- Sortant : un effet insère dans `delivery` ; au commit le kernel enfile `delivery.send` ; le job poste `payload` à `endpoint` avec `Idempotency-Key: <delivery id>`, écrit une `delivery_result` ; échec → retry River (5 tentatives, backoff) ; `delivery.retry` (command) renfile le job
- Entrant pull : job périodique par connection ; chaque item → command d'upsert (dédup par `external_id` unique) ; puis `connection.advance` enregistre curseur et instant
- Entrant push (webhook) : hors slice

## 13. Surfaces

### HTTP

| Route | |
|---|---|
| `POST /api/v/{view}` | view enveloppée |
| `POST /api/c/{command}` | command ou run |
| `GET /api/live` | SSE |
| `GET /api/meta` | registry pour l'UI et les agents |
| `GET /api/me` | principal, fonctions, reach |
| `GET /api/labels/{lang}` | labels |
| `POST /api/files/presign`, `POST /api/files`, `POST /api/file` | fichiers |
| `GET /openapi.json` | une opération par view et par command, schemas exacts |
| `/mcp` | MCP |
| `/auth/*` | session navigateur |
| `/` | SPA embarquée |

Opérations Huma construites à la main (`huma.Schema` depuis les types Postgres), handler brut, validation `huma.Validate`, erreurs RFC 9457. En-tête `Server-Timing: db;dur=<ms>;desc="<n> rt", app;dur=<ms>` sur chaque réponse ; une ligne slog par request (name, principal, ms, rt).

### Contrat JSON

Le serveur et l'UI s'accordent sur ces formes, exactement.

```ts
// GET /api/me
type Me = { id: number; name: string; kind: string; lang: "fr"|"de"|"en";
            functions: string[]; reach: Record<string, number[]>; for?: { id: number; name: string } }

// GET /api/meta
type Col = { name: string;
             type: string;      // nom du type Postgres : int8, int4, int2, numeric, text, bool, date, timestamptz, jsonb, int4range, _int4, ou le nom d'un enum
             format: "" | "min" | "tod" | "pct" | "coef" | "money";
             nullable: boolean;
             ref?: string;      // table référencée (colonne FK, résolue par nom)
             enum?: string;     // nom de l'enum
             description?: string }
type Table = { name: string; root: string /* elle-même si root */; child_fk?: string; under?: string;
               anchor?: { column: string; reach: string }; read: string; write: string; label: string; crud: boolean;
               columns: (Col & { guarded: boolean; has_default: boolean })[];
               conditions: string[]; actions: Record<string, string[]>; description?: string }
type View = { name: string; params: Col[]; columns: Col[]; roots: string[]; needs: string;
              anchor?: { column: string; reach: string }; description?: string }
type Command = { name: string; table: string; kind: "create" | "update" | "delete" | "custom" | "run";
                 rootless: boolean; input: Col[] /* hors ids */; when?: string; ensure?: string;
                 reach?: string; needs: string; description?: string }
type Wrap = { filter?: { col: string; op: string; value?: unknown }[]; sort?: { col: string; dir: "asc" | "desc" }[];
              group?: string[]; agg?: { col: string; fn: string }[]; limit?: number; offset?: number }
type Screen = { id: string; group: string; views: string[]; defaults: Record<string, string>; table?: string;
                commands: string[]; wrap?: Wrap; children: Screen[]; link?: Record<string, string>; component?: string }
type Meta = { tables: Table[]; views: View[]; commands: Command[]; screens: Screen[];
              enums: Record<string, string[]>; functions: string[]; groups: string[] }

// POST /api/v/{view}       body: { params?: Record<string, unknown> } & Wrap
//   200: tableau JSON des rows, tel que produit par Postgres ; avec group/agg : colonnes de group + "<fn>_<col>" ("count" pour count)
// POST /api/c/{command}    body: create {…colonnes} | update {ids, …colonnes} | delete {ids} | custom {ids, …input} | rootless {…input} | run {…params}
//   200: { changed: { table: string; ids: number[] }[]; ids: number[] /* roots de la command, créés compris */; run?: number }
// erreurs: application/problem+json { status, title, code, needs?, action?, rejected?: { id: number; conditions: string[]; row: object }[], constraint?, errors? }
// GET /api/live            SSE, data: { t: string; ids?: number[]; c: string; e: string }
// GET /api/labels/{lang}   { [key: string]: string }
// POST /api/files/presign  { hash, size, type } → { exists: boolean; url?: string; headers?: Record<string, string> }
// POST /api/files          { hash, name, type } → { id: number }
// POST /api/file           { view, params, id, column } → { url, name, type }
// view history             params { root_table, root_id } → { at, command, principal, for, tbl, row_id, changes }[]
```

### MCP

- Un tool par command et par view permises au principal (`tools/list` filtré par capability) ; nom = nom du registry ; input schema = le même que HTTP ; views : `params` + champs du wrap
- Rejection → `isError: true` + `structuredContent` identique au corps HTTP
- Resource `screen://{id}` : l'arbre de l'écran (views, commands, enfants), ids stables

### CLI

`sor call <command> '<json>'`, `sor view <view> '<json>'`, `sor meta` : client HTTP avec token client credentials (`SOR_URL`, `SOR_TOKEN_URL`, `SOR_CLIENT_ID`).

### TypeScript

`gen/openapi.json` → `openapi-typescript` → `ui/src/gen/api.d.ts` ; `api.view(name, body)` et `api.call(name, input)` typés par nom.

## 14. Build, migration, check

- `go generate ./...` : sqlc (image Docker) → `gen/sqlc.json` ; puis `sor check --static`
- `sor migrate` : crée l'extension `btree_gist`, le schema `river` (rivermigrate), puis `pgschema apply` du schema concaténé sur `public` (plan DB = database `sorplan` du même serveur) ; puis partitions d'events
- `sor check` : démarre le registry contre la DB (prépare toutes les queries, vérifie déclarations ↔ catalog ↔ labels) et sort
- `sor openapi` : écrit `gen/openapi.json`
- Lint des migrations : `pgschema plan --output-sql stdout | squawk` (script `deploy/lint-migration`)
- `sor up` (compose) : migrate, seed `demo` si la DB est vide, serve

## 15. Domaine : schema

Module base :

| Table | Colonnes | Déclaration |
|---|---|---|
| `company` | name, address, vat_no, currency ; une seule row (`id = 1`) | root ; Write master_data |
| `user` | kind (human, agent, service), subject unique, name, manager_id, lang, archived | root ; Write master_data |
| `user_group` | name unique, admin | root ; Write master_data |
| `user_group_member` | user_group_id, user_id | child de user_group |
| `user_group_fn` | user_group_id, fn | child de user_group |
| `account` | external_id unique, name, address, archived | root ; Write master_data |
| `project` | external_id unique, account_id, name, rate, entry_closed, archived, lead_user_id | root ; Write master_data |
| `folder` | project_id, parent_id, name, rate, estimated_min, entry_closed | root ; Write master_data |
| `folder_task` | folder_id, task_id ; unique | child de folder |
| `task` | name, kind (service, absence, vacation), billing (always, sometimes, never), requires_validation, counts_hours, rate, archived | root ; Write master_data |
| `user_task_rate` | user_id, task_id, rate ; unique | root ; Write master_data |
| `work_period` | user_id, valid_from, week_min int[7], vacation_days, activity_pct ; unique (user_id, valid_from) | root ; anchor user_id/user ; Write master_data |
| `cost_rate` | user_id, valid_from, hourly_cost ; unique (user_id, valid_from) | root ; Read cost, Write cost |
| `public_holiday` | date unique, label, deducted_min (null = journée entière) | root ; Write master_data |
| `hour_class` | label, hours_coef, rate_coef, requires_validation | root ; Write master_data |
| `hour_class_slot` | hour_class_id, day_type 1..8, span_tod int4range ; exclusion (day_type =, span_tod &&) | child de hour_class |

Module time :

| Table | Colonnes | Déclaration |
|---|---|---|
| `activity` | user_id, date, start_tod, duration_min (> 0, start + durée ≤ 1440), billed_min, folder_id, task_id, description, validated_by, validated_at, invoice_id, frozen_rate, frozen_billed_min, frozen_amount | root ; Under company ; anchor user_id/user ; Write activity ; Guarded validated_*, invoice_id, frozen_* |
| `expense_type` | name, by_quantity, unit_price, billing_unit_price, mode (both, user_only, billing_only), archived | root ; Write master_data |
| `expense` | activity_id, expense_type_id, quantity, amount, mode (null = celui du type), receipt_file_id, ocr_amount, frozen_user_amount, frozen_billed_amount | child de activity ; Write expense ; Guarded receipt_file_id, ocr_amount, frozen_* |
| `period_lock` | company_id (défaut 1), date_from, date_to, user_id (null = tous) | child de company ; Write period_lock |

Module billing :

| Table | Colonnes | Déclaration |
|---|---|---|
| `invoice` | project_id, date_from, date_to, invoiced_at, invoiced_by, pdf_file_id | root ; Write invoice ; Guarded invoiced_*, pdf_file_id |

Sémantique :

- `day_type` d'une date : 8 si férié journée entière, sinon son jour ISO (1..7)
- Une classe d'heures = des créneaux (day_type, minutes du jour) ; l'exclusion garantit qu'une minute appartient à une classe au plus
- Tarif du folder = sa propre colonne ; estimé dépassé = somme des durées réelles du folder > `estimated_min`
- Valeurs figées : posées par `invoice.mark_invoiced`, effacées par `invoice.back_to_draft` ; check `(invoice_id is null) = (frozen_amount is null)`
- Le compte d'une invoice = celui de son project

## 16. Domaine : projections

`activity_p` (colonnes de `activity` +) :

| Colonne | Définition |
|---|---|
| project_id | du folder |
| day_type, kind | de la date ; de la task |
| billed_min | figé, sinon : always → durée ; sometimes → `billed_min` saisi, à défaut durée ; never → 0 |
| rate | figé, sinon : user × task, sinon folder, sinon project, sinon task, sinon 0 |
| amount | figé, sinon `rate × billed_min / 60 × (minutes pondérées par rate_coef) / durée`, arrondi à 2 |
| counted_min | 0 si la task ne compte pas d'heures ; sinon durée + Σ chevauchement × (hours_coef − 1) |
| to_validate | la task le demande, ou la durée touche un créneau d'une classe qui le demande, ou l'estimé du folder est dépassé |
| conditions | `not_locked`, `not_invoiced`, `not_validated`, `is_validated`, `entry_open` (folder et project ouverts, project non archivé), `task_allowed` |

- `day_profile` (view) : par day_type, multirange des créneaux (`classed`) et de ceux à valider (`validating`) ; le chevauchement n'est calculé que si `span && classed`
- `activity_class` (view) : une row par activity et par classe touchée, plus la part hors classe : minutes, montant ; `activity_cost` : coût par activity = `counted_min × hourly_cost / 60` du `cost_rate` en vigueur à la date
- `expense_p` : mode effectif ; `user_amount` (figé, sinon 0 si billing_only, quantité × unit_price, ou amount) ; `billed_amount` (figé, sinon 0 si user_only, quantité × billing_unit_price, ou amount)
- `invoice_p` : account_id, `state` (draft, invoiced), activity_count, services_amount, expenses_amount, total_amount — en draft sur les activities non facturées du project dans la période, facturée sur ses activities rattachées ; `export_state` (none, created, exported, failed) ; conditions `is_draft`, `is_invoiced`, `has_activities`, `not_exported`

Actions :

| Table | Action | Conditions |
|---|---|---|
| activity | editable | not_locked, not_invoiced, not_validated, entry_open, task_allowed |
| activity | deletable | not_locked, not_invoiced, not_validated |
| activity | validatable | to_validate, not_validated |
| activity | unvalidatable | is_validated, not_invoiced |
| invoice | editable, deletable | is_draft |
| invoice | invoiceable | is_draft, has_activities |
| invoice | billed | is_invoiced, has_activities |
| invoice | revertible, exportable | is_invoiced, not_exported |

## 17. Domaine : commands

| Command | Déclaration | Effet |
|---|---|---|
| CRUD de chaque table | générique | |
| `activity.validate` | When validatable ; Needs validation ; Reach team | validated_by = @self, validated_at = sor_now() |
| `activity.unvalidate` | When unvalidatable ; Needs validation ; Reach team | remet à null |
| `expense.attach_receipt` | On expense ; When + Ensure editable ; Needs expense | receipt_file_id ; reaction → `ocr.read` |
| `expense.apply_ocr` | On expense ; When editable ; Needs ocr | ocr_amount ; amount s'il est vide |
| `invoice.mark_invoiced` | When invoiceable ; Ensure billed ; Needs invoice_state | fige activities (lock par id) et expenses, pose invoiced_at/by |
| `invoice.back_to_draft` | When revertible ; Needs invoice_state | libère expenses et activities, efface invoiced_at/by |
| `invoice.print` | Needs invoice | aucun ; reaction → `invoice.render` |
| `invoice.attach_pdf` | Needs invoice | pdf_file_id |
| `invoice.export` | When exportable ; Needs invoice | insère la `delivery` (payload = document de l'invoice) |
| `invoice.run` | Run ; candidats `invoice_run_candidates(date_from, date_to)` ; par root `invoice.create` ; Needs invoice | |
| `delivery.retry` | Needs invoice | aucun ; reaction → `delivery.send` |
| `delivery.result` | interne (principal de la connection) | insère `delivery_result` |
| `connection.pull` | Needs master_data | aucun ; reaction → `accounting.pull` |
| `connection.advance` | Needs accounting_sync | cursor, pulled_at, pull_error |
| `account.upsert_from_accounting`, `project.upsert_from_accounting` | Rootless ; Needs accounting_sync | `insert … on conflict (external_id) do update` |
| `run.cancel` | On run | cancel_requested |

Jobs : `ocr.read` (As svc-ocr : blob → tesseract → montant → `expense.apply_ocr`), `invoice.render` (principal de la command : `invoice_document` → Typst → file → `invoice.attach_pdf`), `accounting.pull` (As svc-accounting, Every 5 min et sur `connection.pull`).

Montant OCR : plus grand montant des lignes « total / montant / summe / betrag / à payer » hors sous-total et TVA ; à défaut le plus grand montant.

## 18. Domaine : views

| View | Params | Anchor | Needs | Contenu |
|---|---|---|---|---|
| `timesheet` | user_id, date_from, date_to | user_id / user | | activities (projection + labels), ordre date, start |
| `timesheet_day` | user_id, date_from, date_to | user_id / user | | par jour : due_min, done_min, férié, verrouillé |
| `folder_task_allowed` | folder_id | none | | tasks permises |
| `activity_grid` | date_from, date_to | user_id / user | | activities + labels |
| `activity_to_validate` | date_from, date_to | user_id / team | validation | `to_validate and not_validated` |
| `activity_class` | activity_id | user_id / user | | répartition par classe |
| `expense_by_activity` | activity_id | user_id / user | | lignes + montants |
| `balance` | date_from, date_to | user_id / user | | par user : carried, due, done, balance ; vac_carried, vac_available, vac_taken, vac_balance (minutes) |
| `profitability` | date_from, date_to | none | invoice | par project, folder : durées, prestations, frais facturés, frais collaborateur, réalisé, facturé, solde |
| `profitability_cost` | idem | none | cost | + prix de revient, marge sur prestations, marge sur solde |
| `invoice_activity` | invoice_id | none | invoice | activities de l'invoice (draft : candidates) |
| `invoice_document` | invoice_id | none | invoice | un JSON : company, account, project, lignes, frais, totaux |
| `invoice_run_candidates` | date_from, date_to | none | invoice | project_id, date_from, date_to des projects avec activities non facturées |
| `accounting_export` | invoice_id | none | invoice | deliveries de l'invoice : state, tentatives, dernier détail |
| `history` | root_table, root_id | — | celle du root | events |
| listes et lookups génériques | | celui de la table | Read | |

Soldes (pour un user, intervalle [a, b], rien après `sor_today()`) :

- due = Σ par work_period en vigueur : (Σ jours × `week_min[jour]` − Σ fériés `least(deducted_min ou week_min[jour], week_min[jour])`) × activity_pct / 100 ; le nombre de chaque jour de semaine dans un segment se calcule en forme close, sans dérouler les jours
- done = Σ `counted_min`
- vac_available = Σ jours du segment / jours de l'année × vacation_days × (Σ week_min / 5) × activity_pct / 100 ; vac_taken = Σ durée des activities de genre vacation
- carried = (done − due) depuis la première work_period jusqu'à a exclu ; balance = carried + done − due ; idem vacances

## 19. Écrans

```go
type Screen struct {
    ID, Group string
    Views     []string          // la première permise au principal
    Defaults  map[string]string // params : $today, $month_start, $year_start, $days(-90), $self
    Table     string            // CRUD, formulaire, historique ; "" = lecture seule
    Commands  []string          // barre de commands (row, sélection, global, run)
    Wrap      *Wrap             // tri, group, agrégats par défaut
    Children  []Screen          // onglets du drawer ; Link : param enfant ← colonne parente
    Link      map[string]string
    Component string            // écran fait main
}
```

| # | Écran | Déclaration |
|---|---|---|
| 1 | entry | Component `entry` ; views timesheet, timesheet_day ; enfants : frais, classes, historique |
| 2 | frais d'une activity | enfant `expense_by_activity`, Table expense, `expense.attach_receipt` |
| 3 | validation | `activity_to_validate`, `activity.validate` ; défaut : 90 derniers jours |
| 4 | activities | `activity_grid`, Table activity, validate/unvalidate, enfants frais + classes, saved views |
| 5 | balances | `balance` ; défaut : année en cours |
| 6 | profitability | `profitability_cost`, `profitability` ; group project_id ; défaut : année en cours |
| 7 | invoicing | liste `invoice`, Table invoice, `invoice.run`, mark_invoiced, back_to_draft, print, export ; enfants : activities, exports (`delivery.retry`), PDF |
| 8 | locks | liste `period_lock`, Table period_lock |
| 9 | données de base | un écran par table `Screen(group)` |
| 10 | accounting | liste `connection`, `connection.pull` ; enfant : deliveries |
| 11 | historique | onglet de tout drawer dont l'écran a une Table |

## 20. UI

### Design system

Sérieux, dense, plat ; proche de Fluent. Tokens CSS (`ui/src/ds/tokens.css`), thèmes clair et sombre par `data-theme`.

| Token | Clair | Sombre |
|---|---|---|
| bg-app / surface / surface-subtle | #F5F5F5 / #FFFFFF / #FAFAFA | #141414 / #1F1F1F / #292929 |
| header | #EDEDED | #2B2B2B |
| border / border-strong | #D1D1D1 / #A8A8A8 | #424242 / #666666 |
| text / text-muted | #242424 / #616161 | #F5F5F5 / #ADADAD |
| accent / hover / pressed | #0F6CBD / #115EA3 / #0F548C | #479EF5 / #62ABF5 / #2886DE |
| accent-subtle / selection | #EBF3FC / #E5F0FB | #12304D / #12304D |
| positive / warning / negative | #107C10 / #BC4B09 / #C50F1F | #54B054 / #F7630C / #E8484E |

- Police : `"Segoe UI Variable Text", "Segoe UI", system-ui, -apple-system, Roboto, "Noto Sans", Arial, sans-serif` ; `tabular-nums` dans les grids
- Tailles : 11 caption, 12 grid, 13 corps et formulaires, 14 titre de section semibold, 18 titre de page semibold
- Densité : row 24 px, header de grid 28, row de groupe et de total 26, contrôle 28, command bar 40, onglet 32, item de nav 32
- Espacement : 2, 4, 6, 8, 12, 16, 24, 32 ; cellule `0 8px` ; panneau 12
- Radius : 2 (inputs, cellules), 4 (boutons, popovers), 6 (dialogs) ; bordure 1 px ; ombre seulement sur overlays et drawer
- Grid : pas de zèbre, filet horizontal, nombres à droite, sélection `selection`, négatif en `negative`, totaux en semibold sous un filet fort
- Icônes : Fluent System Icons 16 px (20 en nav)
- Composants (`ui/src/ds/`) : Button, IconButton, Input, NumberInput, DurationInput, TimeInput, DateInput, Checkbox, Select, Combobox, Dialog, Drawer, Tabs, Menu, Tooltip, Toast, Badge, Field, CommandBar, Grid

### Shell

- Rail de navigation à gauche (240 px, repliable à 48) : groupes Temps, Facturation, Analyse, Données de base, Système ; écrans filtrés par capability
- Command bar (40 px) : titre, commands de l'écran, à droite utilisateur, langue, thème
- Contenu : barre de params et filtres (36 px), grid, totaux collants, barre d'état (24 px : rows, sélection, état du live)
- Drawer de détail à droite (420 px, redimensionnable) : formulaire, onglets enfants, historique
- Routage : `/#/<screen>` ; état d'un écran (params, wrap) dans l'URL

### Écran générique

Un composant `ViewScreen` rend toute `Screen` :

- Params : un champ par param de la view, du type du param
- Grid virtualisé maison (hauteur de row fixe, fenêtre par `translateY`, pages de 200 par offset) ; tri, filtre par colonne, group avec agrégats et drill-down, totaux = le wrap ; colonnes FK affichées par leur lookup ; formats par suffixe
- Saved views : état (params, wrap, colonnes) dans `saved_view`
- Commands : bouton actif = action vraie sur toutes les rows sélectionnées ∧ capability ; inactif → tooltip avec les labels des conditions fausses ; input restant → dialog généré ; run → dialog de progress avec annulation
- Formulaire généré depuis les colonnes de la Table : FK → Combobox sur lookup ; enum → Select ; bool → Checkbox ; date, `_min`, `_tod`, range `_tod`, tableau `_min`, numérique, texte ; colonne vers `file` → upload et aperçu
- Rejection → Toast et marquage des rows, avec les labels des conditions

### Écran de saisie (fait main)

- En haut : semaine (7 jours : date, fait / dû, férié, verrou), navigation, aujourd'hui, choix du user si le reach en contient plusieurs, bascule jour / semaine
- Grid d'édition : une ligne par activity, groupée par jour ; colonnes début, durée, fin, folder, task, durée facturée (si la task est « parfois »), description, état, montant ; une ligne vide en fin de jour
- Clavier : Tab / Maj+Tab entre cellules ; Entrée = sauver la ligne et descendre ; Échap = annuler la ligne ; Ctrl+D = dupliquer ; Ctrl+Suppr = supprimer ; Alt+← / → = jour ; Alt+↑ / ↓ = semaine
- Saisie : durée `1.5`, `1:30`, `90m` ; début `8`, `830`, `8:30`, défaut = fin de la ligne précédente ; folder par sous-chaînes de « project / folder » ; task parmi celles permises dans le folder
- Sauvegarde par ligne : create puis update des champs changés ; indicateur par ligne (en cours, sauvé, refusé + conditions)
- Pied : total du jour / dû, total de la semaine, solde
- Drawer de l'activity : frais (upload du ticket, montant pré-rempli par l'OCR en live), classes d'heures, historique

Repères de mesure : `data-ready` sur la racine d'un écran quand ses données sont affichées ; `data-saved="<n>"` sur une ligne à chaque confirmation.

### Labels

- `labels/<lang>.json`, clés plates : `t.<table>`, `c.<colonne>`, `c.<table>.<colonne>`, `v.<view>`, `cmd.<command>`, `cond.<condition>`, `fn.<fn>`, `e.<enum>.<valeur>`, `s.<écran>`, `g.<groupe>`, `constraint.<nom>`, `ui.<clé>`
- Français par défaut ; langue = `user.lang` ; nombres et dates par `Intl` (`de-CH` pour les nombres)
- Les labels des éléments créés par l'admin sont leurs colonnes (`name`, `label`)

## 21. Seed

- `seed/seed.sql` embarqué, en SQL ensembliste (`generate_series`, `setseed`), paramétré par settings ; `sor seed demo` : 20 users, ~40k activities ; `sor seed perf` : 200 users, 600k activities ; déterministe, relatif à aujourd'hui ; vide les tables du domaine avant de charger
- Arbre de responsables ; groupes admin, manager (validation, scope_tree), finance (invoice, invoice_state, cost, scope_all), collaborateur (activity, expense), agents, services
- ~300 accounts, ~1500 projects, ~6000 folders (20 % avec estimé), ~30 tasks, classes Nuit / Samedi / Dimanche et férié, fériés, work_periods, cost_rates
- Activities sur 3 ans, 97 % en semaine, 3 % soir, nuit ou week-end ; frais sur ~5 % ; les mois anciens facturés (valeurs figées prises dans `activity_p`), années passées verrouillées
- Accounts `A0001…` et projects `P00001…` (external_id) avec des noms provisoires ; le faux serveur de comptabilité sert les mêmes external_id avec leurs vrais noms, plus quelques accounts et projects en plus : le premier pull renomme et importe
- Sujets de démo : `admin`, `manager`, `finance`, `user`

## 22. Déploiement

`compose.yaml` : `db` (postgres:18, volume sur `/var/lib/postgresql`, timezone, `jit=off`), `s3` (SeaweedFS, `weed server -s3`), `idp` (mock-oauth2-server, `deploy/idp.json`), `acct` (`fakeacct`), `app` (`sor up`). Ports : app 8080, idp 8090, s3 8333, acct 8070.

Image de l'app : build UI (node) → build Go → debian slim + tesseract (fra, deu, eng) + typst + pgschema.

Faux serveur de comptabilité : `GET /accounts?since=`, `GET /projects?since=`, `POST /invoices` (dédup par `Idempotency-Key`), `GET /invoices`, `POST /admin/fail {on}`, `POST /admin/accounts`.

## 23. Budgets de performance

Fixés avant toute mesure, depuis ce qu'un utilisateur perçoit comme immédiat. Seed perf, stack locale, p95 côté navigateur sauf mention.

| # | Scénario | Budget |
|---|---|---|
| B1 | clic → écran de saisie affiché | 100 ms |
| B2 | clic → écran générique affiché (première page) | 120 ms |
| B3 | frappe → activity sauvée et confirmée | 50 ms |
| B4 | validation de 100 activities → confirmé, liste à jour | 250 ms |
| B5 | soldes, équipe de 10 | 150 ms |
| B6 | soldes, 200 users, tout l'historique | 500 ms |
| B7 | rentabilité, 12 mois, tous les projects | 400 ms |
| B8 | rentabilité, tout l'historique | 900 ms |
| B9 | grid activities, 1 mois, tous les users, page de 100 | 120 ms |
| B10 | run de facturation, 1 mois, tous les projects : premier progress / fin | 300 ms / 5 s |
| B11 | impression → PDF affiché | 1.5 s |
| B12 | commit → autre session à jour (live) | 300 ms |
| S1 | allers-retours DB : view / command | ≤ 2 / ≤ 4 |
| S2 | serveur : command activity (p95) | 15 ms |
| S3 | serveur : `timesheet` / page de grid (p95) | 5 ms / 30 ms |

Un budget dépassé = défaut de conception : retour à la spec.

## 24. Rejeu des cas

« À éprouver » de SLICE :

| Cas | Réponse du design |
|---|---|
| Activity écrite pendant qu'une période se verrouille | activity `under company` (`for share`), period_lock child de company (`for update`) : l'une attend l'autre, puis lit la projection à jour → `not_locked` fausse → Rejection |
| Activity écrite pendant qu'une invoice la fige | l'effet de `mark_invoiced` locke les activities par id ; il attend l'écriture en cours et fige la row à jour ; l'écriture suivante lit `not_invoiced` fausse |
| Tarif modifié après coup | `rate` et `amount` calculés pour le non facturé, `frozen_*` pour le facturé |
| Répartition par classe et « à valider » calculés | `activity_p`, `activity_class`, `day_profile` ; rien de stocké |
| Soldes en view, reports et fériés | `balance` |
| Refus avec les conditions fausses, UI et MCP | Rejection = conditions + row ; même corps en HTTP et en `structuredContent` |
| Query anchorée sans `@reach` refusée au build ; hors reach → `not_found` | `sor check --static` dans `go generate` et le Dockerfile ; étapes 3 et 6 du pipeline |
| Run annulé : rien n'est écrit | une transaction, rollback |
| Comptabilité en panne | `delivery` + job avec retry + `Idempotency-Key` ; `delivery.retry` |
| Même PDF à chaque régénération | timestamp de création pris dans la donnée, polices embarquées, blob par hash |

Écrans, commands et views de SLICE : §17 à §19 les couvrent un à un.

CASES (le model doit y tenir, sans les construire) :

- C5, O2, S3 : stock = projection d'un root ; l'effet locke les rows de stock ; `Ensure("stock.not_oversold")` refuse avec la row
- O5, R1, S8 : conditions temporelles par `sor_now()` ; O5 = job `After` → command
- O7 : même root, la seconde lit l'action fausse
- S6 : contrainte unique → `constraint`
- F5 : clôture = table child d'un root supérieur, invoices et paiements `under` ce root
- A1, A3, S1 : reach nommés ; A2, C3 : views séparées ; A4, A5 : intersection ou droits propres
- L1 à L4 : clés d'invalidation ; L3 = view sans anchor avec capability
- S7 : idempotence d'une command rejouée hors ligne, ouvert dans MODEL

## 25. Hors slice

Présents dans MODEL, non construits ici ; leur point d'ancrage :

- Archive Parquet et rétention des events : job sur les partitions
- Effacement d'une personne : command sur les colonnes `personal`
- Extensions WASM : un module de plus dans le registry
- Webhooks entrants : route par connection → command
- Multi-tenant : un pool par database
- OpenTelemetry, session replay : autour du pipeline
