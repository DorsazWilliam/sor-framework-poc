# Model

Description canonique et unique du model, du framework et du stack. MODEL([CASES](./CASES.md)) = implémentation.

Cible : n'importe quelle web app ; optimisé pour les systems of record : données métier stockées puis analysées, workflows, views et fields custom, extensions.

## Règle de changement

1. Un problème trouvé = le model est faux quelque part. Pas de patch, pas de couche en plus
2. Repenser le concept fautif jusqu'à ce que le cas disparaisse
3. Réécrire ce document en entier là où le concept vit
4. Évaluer les conséquences de premier et de second ordre sur chaque section
5. Revérifier la cohérence interne : chaque règle écrite une seule fois, aucune section ne contredit une autre
6. Rejouer tous les cas de [CASES.md](./CASES.md) ; un cas qui demande une exception = retour à 2
7. Noter le changement dans le journal en bas

## 1. Données

- Une database Postgres 18 par tenant
- `schema.sql` par module = source of truth de la structure ; migrations par diff déclaratif (pgschema), vérifiées par squawk
- Les tables sont la vérité ; une row change par une command
- Stocké : les inputs, ce qu'un humain, un système ou le temps a décidé ou observé. Calculé : tout le reste, dans la projection
- Table `append_only` : update et delete refusés par la DB ; pour les registres (paiements, mouvements de stock, deliveries)
- La DB porte les invariants structurels : FK, unique, CHECK sur une row, NOT NULL, exclusion
- Custom field : colonne typée dans la table `<table>_x`, en 1:1 avec sa table
- Colonne `personal` : déclarée dans le schema, avec la personne qu'elle concerne

## 2. Projection

- Root : la row autour de laquelle une projection se calcule et qu'une command lock
- Projection : une SQL view par table root, une row par root ; colonnes de la table + valeurs calculées (montants, quantités restantes, retard)
- Condition : colonne booléenne nommée de la projection (`not_shipped`, `under_limit`)
- Action : nom + ensemble de conditions ; colonne d'action = AND de ses conditions ; une par command
- Table root sans projection écrite : projection = la table, avec `editable` et `deletable` à true
- Le temps entre par le paramètre `@now`

## 3. Access

- Principal : identité authentifiée par un IdP OIDC externe (, passkeys ; client credentials pour agents et services) ; humain, agent ou service
- Capability : role → commands et views permises
- Reach : les rows reliées au principal par des données (lui-même, management, membership) ; c'est une projection
- Chaque view et command déclare son anchor (une colonne) ou aucun
- `@reach` est résolu une fois par request ; une query anchorée sans `@reach` échoue au build
- Hors reach → `not_found`
- Données sensibles : view séparée, capability séparée
- Agent pour un humain : reach = intersection des deux ; l'event enregistre les deux

## 4. View et command

- View : SQL statique dans `queries.sql`, compilé par sqlc ; params + `@reach`
- Le kernel enveloppe une view avec filtre, tri, group, agrégat et page, sur ses colonnes de sortie ; `@reach` reste dans la view
- List, grid, pivot, dashboard, saved view = une view enveloppée ; une saved view est une row (view + params)
- Command : action + effet SQL ; inputs validés contre la projection, dans la transaction (`@qty <= remaining_qty`)
- Toute table a create, update (`editable`) et delete (`deletable`), générés depuis le schema

```go
var Cancel = kernel.Command("order.cancel", kernel.When("cancellable"), db.Cancel)
var Ship   = kernel.Command("order.ship",   kernel.When("shippable"),   db.Ship)
```

## 5. Exécution

Même ordre pour humain, agent, reaction et job :

1. principal → capability → reach
2. transaction (pgx v5)
3. lock des roots (`for update`), ordre d'id stable
4. lecture de la projection pour ces ids, `@now` fixé
5. ids où l'action est fausse → Rejection, avec les conditions fausses
6. validation des inputs contre la projection
7. effet
8. event + jobs insérés dans la transaction
9. commit
10. live

## 6. Event log

- Une table, partitionnée par mois : command, principal, pour qui, ids, colonnes changées (avant, après)
- Historique d'une row, audit, debug des agents
- Mois chauds dans Postgres ; mois plus vieux exportés en Parquet dans l'object storage, partition droppée ; rétention par tenant
- Effacement d'une personne : command qui anonymise ses colonnes `personal` dans les tables, l'event log et les fichiers Parquet concernés (réécrits) ; son id entre dans la liste d'effacement du control plane, rejouée à chaque restore de backup

## 7. Asynchrone

- Job : command différée (`After`, `Every`), dans River, inséré dans la transaction
- Reaction : event → command, avec son propre principal ; at-least-once, idempotente par event id
- Run : command sur un set de roots, exécutée en job ; progress live ; cancel = rollback ; undo = une autre command

## 8. Fichiers

- File : blob content-addressed (hash) dans l'object storage du tenant (S3, ou disque on-prem) + row `file` (hash, taille, type, nom)
- Une row métier référence un file par FK ; le file est atteignable si une row qui le référence est dans le reach
- Upload : URL signée courte → blob ; puis command qui attache le file à une row
- Download : view sur la row → URL signée courte
- Traitement (scan, miniature, OCR, extraction) : reaction sur l'attachement → job → files ou rows liés au file source
- Généré (PDF, export) : job ; view + template Typst → file attaché par command ; même input = même hash
- Blob sans référence : supprimé par un job de GC

## 9. Systèmes externes

- Connection : une row par système externe (endpoint, config) ; son secret dans le vault, référencé par nom ; son propre principal
- Sortant (email, eBill, API, SFTP) : la command écrit une row `delivery` dans sa transaction ; un job l'envoie, idempotency key = delivery id, retry avec backoff ; résultat ou échec = row liée ; relance par command
- Entrant push (webhook) : un endpoint par connection, signature vérifiée → command ; dédup par id externe
- Entrant pull : job `Every` par connection, curseur stocké en row ; chaque item → command ; dédup par id externe

## 10. Live

- Un stream SSE par session
- Event commité → bus in-process ; entre instances, Postgres LISTEN/NOTIFY
- Dependency key = (table root, id) ; une command invalide les roots qu'elle a lockés
- Les views touchées sont relancées, le diff est poussé
- Projection dépendant du temps : expose son prochain changement ; le live la relance à cette heure

## 11. Surfaces

- Un registry de views et commands → HTTP JSON + SSE (Huma, OpenAPI), MCP (Go SDK officiel), CLI `sor`, client TypeScript ; tous générés
- UI : SPA Svelte 5 + TypeScript ; un écran = views live + commands
- Bouton actif = colonne d'action ∧ reach ∧ capability
- Écrans génériques (list, form, grid, pivot, run) générés depuis le registry ; écrans denses écrits à la main ; grid et planning maison, virtualisés
- Chaque écran expose son arbre de views et commands, avec ids stables, lisible par un agent via MCP

## 12. Extensions

- Une extension = un module : schema `ext_x`, son propre principal, même pipeline
- Elle ajoute tables, colonnes `_x`, conditions sur les actions existantes, actions, commands, views, reactions
- Workflow custom = colonnes + conditions + commands
- Désinstaller = retirer ses ajouts
- Runtime : WASM sur wazero

## 13. Stack et layout

```
modules/<m>/  schema.sql, queries.sql, *.go (déclarations), ui/
gen/          généré : Go, TS, OpenAPI, MCP
kernel/       command, view, access, event, job, live, file, connection, tenant
ext/          runtime WASM
ui/           shell SPA, composants génériques
cmd/sor/      binaire : serve, migrate, tenant, introspect, run <command>
```

- Go 1.26, net/http, pgx v5 ; un pool pgx par tenant, ouvert à la demande
- Tenant = database (ce POC : une seule database)
- Observability : OpenTelemetry (traces, métriques), slog, rrweb pour le session replay ; export OTLP
- Packaging : un binaire statique (UI et migrations embarquées) ; image OCI
- DX : `go generate` + build en secondes ; la CLI `sor` est l'interface des agents

## Ouvert

- Live : multiplexage, debounce, coût de relance des views qui agrègent beaucoup de roots
- Projection : coût par root, et pour les runs sur beaucoup de roots
- Custom fields en colonnes `_x` : à valider en pratique ; repli jsonb validé
- Extensions : WASM, ou Go compilé par tenant
- UI : Svelte 5 ou React

## Journal

- 2026-11-23 : guards SQL dans les commands → colonnes d'action dans la projection, `When` + effet, lock des roots
  - 1er ordre : plus de dry-run ; Rejection = row de projection ; boutons sans query ; write skew supprimé par le lock du root ; inputs validés contre la projection
  - 2e ordre : trigger « couvert » supprimé, devient `editable` ; tout commitment référence un root ; dependency key du live = (projection, root id) ; `@now` en paramètre ; runs lockent dans un ordre stable
- 2026-11-23 : commitments partout = event sourcing généralisé, anti-pattern connu (Greg Young). Remplacé par : tables = vérité, changées par commands, on stocke les inputs, append_only pour les registres ; cible élargie à toute web app
  - 1er ordre : cycles, effacement, données de référence redeviennent des updates ; event log = historique
  - 2e ordre : action = AND de conditions nommées ; Rejection = conditions fausses ; extensions ajoutent des conditions ; CRUD par défaut ; view enveloppable → saved views ; workflow custom = colonnes + conditions + commands ; live keyé par table root
- 2026-11-23 : effacement, fichiers, systèmes externes
  - 1er ordre : effacement = command qui anonymise les colonnes `personal` + liste d'effacement rejouée au restore ; crypto-shredding écarté (statut légal incertain, gestion des clés ; l'event log se corrige directement) ; file = blob content-addressed + row ; document généré = file ; connection, outbox `delivery`, webhook, pull à curseur
  - 2e ordre : colonnes `personal` déclarées dans le schema ; GC des blobs par références ; secrets dans le vault ; integrations dans le même pipeline
- 2026-11-23 : event log archivé
  - 1er ordre : colonnes changées seulement ; partitions mensuelles ; vieux mois en Parquet dans l'object storage
  - 2e ordre : l'effacement réécrit les Parquet concernés ; l'historique ancien se lit dans les archives
- 2026-11-23 : réécriture complète ; stack et layout déplacés de VISION vers ce document ; event log en section propre ; arbre d'écran pour les agents précisé
