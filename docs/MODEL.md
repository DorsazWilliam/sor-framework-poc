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

## 2. Root et projection

- Root : la row autour de laquelle une projection se calcule et qu'une command lock
- Table child : ses rows appartiennent à un root par une FK ; ses commands lockent ce root et lisent ses conditions
- Root `under` un autre root : ses commands tiennent le root supérieur en `for share` ; une command sur le root supérieur (`for update`) les exclut toutes. Ce qui gouverne un ensemble de roots (verrou de période, clôture) est une table child du root supérieur
- Projection : une SQL view par table root, une row par root ; colonnes de la table + valeurs calculées (montants, quantités restantes, retard)
- Condition : colonne booléenne nommée de la projection (`not_shipped`, `under_limit`)
- Action : nom + ensemble de conditions, déclarée dans le registry ; vraie = AND de ses conditions
- Table root sans projection écrite : projection = la table ; `editable` et `deletable` sans condition
- Le temps entre par `sor_now()` : l'instant de la transaction, fixé pour toute la request, épinglable (test, replay)

## 3. Access

- Principal : identité authentifiée par un IdP OIDC externe (passkeys ; client credentials pour agents et services) ; humain, agent ou service
- Capability : role → commands et views permises
- Reach : les rows reliées au principal par des données (lui-même, management, membership) ; c'est une projection ; un principal a plusieurs reach nommés
- Chaque view et command déclare son anchor (une colonne et un reach) ou aucun
- `@reach` est résolu une fois par request ; une query anchorée sans `@reach` échoue au build
- Hors reach → `not_found`
- Données sensibles : table ou view séparée, capability séparée
- Agent pour un humain : capability et reach = intersection des deux ; l'event enregistre les deux

## 4. View et command

- View : SQL statique dans `queries.sql`, compilé par sqlc ; params + `@reach`
- Le kernel enveloppe une view avec filtre, tri, group, agrégat et page, sur ses colonnes de sortie ; `@reach` reste dans la view
- List, grid, pivot, dashboard, saved view = une view enveloppée ; une saved view est une row (view + params)
- Command : action avant (`When`) + effet SQL + action après (`Ensure`) ; les deux actions sont lues dans la projection, dans la transaction
- `Ensure` porte sur les roots touchés de sa table (`quantité restante ≥ 0` est une condition du stock)
- L'effet est du SQL statique ; chaque statement écrit ses events (§6)
- Toute table a create (`Ensure editable`), update (`When` et `Ensure editable`) et delete (`When deletable`), dérivés du schema

```go
r.Command("order.cancel", kernel.When("cancellable"), kernel.Effect("OrderCancel"))
r.Command("order.ship",   kernel.When("shippable"), kernel.Ensure("order_line.not_overshipped"), kernel.Effect("OrderShip"))
```

## 5. Exécution

Même ordre pour humain, agent, reaction et job :

1. principal → capability → reach
2. transaction (pgx v5)
3. lock : roots `under` en `for share`, puis roots en `for update`, ordre (table, id) stable
4. lecture de la projection pour ces ids
5. root absent ou hors reach → `not_found` ; ids où `When` est fausse → Rejection, avec les conditions fausses et la row de projection
6. effet ; un effet qui change d'autres roots les lock lui-même, par id
7. relecture de la projection des roots touchés : hors reach → `not_found` ; `Ensure` fausse → Rejection ; rollback
8. jobs insérés dans la transaction
9. commit
10. live

## 6. Event log

- Une table, partitionnée par mois : command, principal, pour qui, table et id de la row, son root, colonnes changées (avant, après)
- Une row par row changée, écrite par le statement d'effet lui-même (`returning old, new`) : l'effet et son event sont indissociables
- Historique d'une row, audit, debug des agents
- Mois chauds dans Postgres ; mois plus vieux exportés en Parquet dans l'object storage, partition droppée ; rétention par tenant
- Effacement d'une personne : command qui anonymise ses colonnes `personal` dans les tables, l'event log et les fichiers Parquet concernés (réécrits) ; son id entre dans la liste d'effacement du control plane, rejouée à chaque restore de backup

## 7. Asynchrone

- Job : command différée (`After`, `Every`), dans River, inséré dans la transaction
- Reaction : command → job, avec son propre principal ; inséré au commit de la command, unique par invocation ; at-least-once
- Run : command sur un set de roots, exécutée en job dans une transaction, un savepoint par root ; progress live ; cancel = rollback ; undo = une autre command

## 8. Fichiers

- File : blob content-addressed (hash) dans l'object storage du tenant (S3, ou disque on-prem) + row `file` (hash, taille, type, nom)
- Une row métier référence un file par FK ; le file est atteignable si une row qui le référence est dans le reach
- Upload : URL signée courte, hash signé → blob ; puis command qui attache le file à une row
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
- Event commité → Postgres `NOTIFY` → chaque instance → ses streams
- Dependency key = (table root, id) ; une command invalide les roots des rows qu'elle a changées
- Le stream porte les clés, filtrées par le reach de la session ; le client relance ses views touchées ; le serveur ne garde aucun état d'abonnement
- Les roots dont dépend une view sont dérivés du plan de sa query
- Projection dépendant du temps : expose son prochain changement ; le client la relance à cette heure

## 11. Surfaces

- Le registry est une donnée : manifest sqlc (queries, params, commentaires) + catalog Postgres (types, FK) + déclarations des modules
- Un registry de views et commands → HTTP JSON + SSE (Huma, OpenAPI), MCP (Go SDK officiel), CLI `sor`, client TypeScript ; tous dérivés, sans code par opération
- UI : SPA Svelte 5 + TypeScript ; un écran = views live + commands
- Bouton actif = action ∧ reach ∧ capability ; inactif, il nomme ses conditions fausses
- Écrans génériques (list, form, grid, pivot, run) rendus depuis le registry ; écrans denses écrits à la main ; grid et planning maison, virtualisés
- Chaque écran expose son arbre de views et commands, avec ids stables, lisible par un agent via MCP

## 12. Extensions

- Une extension = un module : schema `ext_x`, son propre principal, même pipeline
- Elle ajoute tables, colonnes `_x`, conditions sur les actions existantes, actions, commands, views, reactions
- Workflow custom = colonnes + conditions + commands
- Désinstaller = retirer ses ajouts
- Runtime : WASM sur wazero

## 13. Stack et layout

```
modules/<m>/  schema.sql, queries.sql, module.go (déclarations), templates
gen/          manifest sqlc, OpenAPI
kernel/       command, view, access, event, job, live, file, connection, tenant
ext/          runtime WASM
ui/           shell SPA, composants génériques
cmd/sor/      binaire : serve, migrate, check, call, view, meta
```

- Go 1.27, net/http, pgx v5 ; un pool pgx par tenant, ouvert à la demande
- Tenant = database (ce POC : une seule database)
- Observability : OpenTelemetry (traces, métriques), slog, rrweb pour le session replay ; export OTLP
- Packaging : un binaire statique (UI et migrations embarquées) ; image OCI
- DX : `go generate` + build en secondes ; la CLI `sor` est l'interface des agents

## Ouvert

- Live : coût de relance des views qui agrègent beaucoup de roots ; diff serveur si la relance client ne suffit plus
- Projection : coût par root, et pour les runs sur beaucoup de roots
- Custom fields en colonnes `_x` : à valider en pratique ; repli jsonb validé
- Extensions : WASM, ou Go compilé par tenant
- Idempotence d'une command rejouée par un client hors ligne (CASES S7)

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
- 2026-11-25 : spec du POC ([SPEC.md](./SPEC.md), [DECISIONS.md](./DECISIONS.md)) ; le model est faux sur six points, repensés
  - Lock entre roots : une row insérée (verrou de période) est un fantôme pour le lock de row → tables child et roots `under` (`for share` / `for update`)
    - 1er ordre : ordre de lock global ; la clé de live d'une row child est son root
    - 2e ordre : verrou et clôture sont des tables child d'un root supérieur
  - Validation des inputs : pas d'état avant pour un create, et un update peut déplacer une row vers un état interdit → `When` avant + `Ensure` après, plus de langage d'expressions
    - 1er ordre : le reach est aussi vérifié après l'effet
    - 2e ordre : CRUD dérivé du schema avec ces deux actions
  - `@now` : une SQL view n'a pas de paramètre → `sor_now()`
  - Colonne d'action : le kernel ne peut pas la décomposer pour nommer les conditions fausses → action déclarée dans le registry
  - Event : écrit par le statement d'effet (`returning old, new`), une row par row changée
  - Live et surfaces : diff serveur → clés d'invalidation et relance client ; code généré → registry donnée, types pris dans Postgres
  - Reaction : event → command devient command → job inséré au commit ; reach nommés ; donnée sensible en table séparée
