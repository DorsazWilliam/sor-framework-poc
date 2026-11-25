# Décisions

Journal des choix ouverts tranchés pour ce POC. Chaque entrée : la décision, ce qu'elle remplace, ses conséquences de 1er et de 2e ordre. Les changements du model sont aussi dans le journal de [MODEL.md](./MODEL.md).

## Model

### D1. Le temps entre par `sor_now()`

- Décision : une fonction SQL, l'instant de la transaction, épinglable par le setting `sor.now` (test, replay)
- Remplace : le paramètre `@now`. Une SQL view n'a pas de paramètre ; `@now` aurait demandé un second mécanisme pour les projections
- 1er ordre : un seul mécanisme pour projections, views et effets ; fixé pour toute la request sans code ; Go ne lit jamais l'horloge
- 2e ordre : une condition temporelle est une colonne ordinaire de la projection ; un test de délai épingle `sor.now`

### D2. Tables child et roots `under`

- Décision : une table est root ou child d'un root (ses commands lockent le parent). Un root peut être `under` un autre : ses commands tiennent le root supérieur en `for share`, une command sur le root supérieur les exclut toutes
- Cas : écriture d'une activity pendant qu'un `period_lock` se pose. Sans row commune, la transaction qui lit « pas de verrou » peut commiter après le verrou (le verrou inséré est un fantôme pour elle)
- Appliqué : `period_lock` est child de `company` (`for update`) ; `activity` est `under company` (`for share`) ; `expense` est child de `activity`
- Écarté : `SERIALIZABLE` (ne garantit pas l'ordre temps réel et impose des retries) ; lock de table (bloqué par autovacuum, toxique pour les transactions longues) ; advisory lock (un root qui n'est pas une row) ; lock de toutes les rows `user` (fantôme sur un user créé pendant le verrou)
- 1er ordre : deux modes de lock, dérivés des déclarations de tables ; ordre global : roots `under` puis (table, id)
- 2e ordre : la clé de live d'une row child est son root ; les conditions d'une row child sont celles de son root ; une clôture de période (CASES F5) suit le même schéma

### D3. Command = `When` + effet + `Ensure`

- Décision : l'action `When` tient sur l'état avant l'effet, l'action `Ensure` sur l'état après ; les deux lues dans la projection
- Remplace : « inputs validés contre la projection » (`@qty <= remaining_qty`), qui demandait un langage d'expressions
- Cas : déplacer une activity dans une période verrouillée ; créer une activity (aucun état avant) ; facturer une invoice dont la dernière activity vient de partir
- 1er ordre : create = `Ensure(editable)`, update = `When` + `Ensure`, delete = `When(deletable)` ; une Rejection après l'effet porte la row de projection (CASES S3 : la quantité restante est dans la row)
- 2e ordre : le reach se vérifie aussi sur l'état après (on ne déplace pas une row hors de son reach) ; aucune validation en Go

### D4. L'effet écrit ses events

- Décision : le kernel exécute chaque statement d'effet dans `with eff as (<dml> returning old, new) insert into event …` (PG18)
- Remplace : un diff avant / après en Go, ou des triggers d'audit
- 1er ordre : un event par row réellement changée, rows child comprises, en un statement ; impossible à oublier ; aucun trigger, aucun setting de session
- 2e ordre : la liste retournée sert de réponse, de clés de live, de roots à relire et de déclencheur des deliveries ; un statement d'effet est un DML simple

### D5. Une action est un ensemble de conditions dans le registry

- Décision : la projection porte les conditions ; l'action (nom + conditions) est déclarée ; le kernel et l'UI calculent le AND
- Remplace : une colonne d'action écrite dans la view, que le kernel aurait dû décomposer pour nommer les conditions fausses
- 1er ordre : Rejection et tooltip d'un bouton inactif nomment les conditions sans parser de SQL
- 2e ordre : une extension ajoute une condition à une action par une déclaration

### D6. Live = clés d'invalidation, le client relance

- Décision : le stream SSE porte (table root, ids) au commit, via `NOTIFY` ; le client relance ses views touchées
- Remplace : views relancées et diff poussé par le serveur
- 1er ordre : serveur sans état d'abonnement ; un seul chemin pour une ou plusieurs instances ; reconnexion = tout relancer
- 2e ordre : les roots d'une view sont dérivés du plan de sa query ; le filtrage par reach se fait sur les anchors portés par la clé

### D7. Le registry est une donnée

- Décision : manifest sqlc + catalog Postgres + déclarations Go ; aucune génération de Go ; HTTP, OpenAPI, MCP, CLI et UI générique lisent le même registry
- Remplace : du code généré par surface
- Constat : sqlc type mal les expressions de views (`any`, `sum(numeric)` en bigint) ; Postgres (`PREPARE`) est exact
- 1er ordre : sqlc sert à compiler le SQL, nommer les params et porter les commentaires ; les types viennent de Postgres ; les effets s'exécutent par nom, leur lien avec les commands est vérifié par `sor check`
- 2e ordre : `gen/` ne contient que deux JSON ; le client TypeScript vient d'OpenAPI

### D8. Reach nommés

- Décision : un principal a plusieurs reach nommés (`self`, `team`, `user`) ; un anchor = colonne + nom de reach
- Cas : valider concerne les autres (`team`) ; saisir concerne soi et son scope (`user`) ; une saved view n'appartient qu'à soi (`self`)
- 1er ordre : le module d'accès rend les reach en une query ; l'agent pour un humain = intersection nom par nom
- 2e ordre : CASES (entrepôt, comptes, entreprise) = d'autres noms, même mécanisme

### D9. Donnée sensible = table ou view à part

- Décision : le prix de revient horaire vit dans `cost_rate` (user, date d'effet), pas dans `work_period` ; coûts et marges dans `activity_cost` et `profitability_cost`
- Écart avec SLICE : `work_period` ne porte plus le prix de revient
- 1er ordre : la capability se pose sur une table et une view entières, jamais sur une colonne
- 2e ordre : un changement de coût ne crée pas de work_period

## Domaine

### D10. Classes d'heures en créneaux exclusifs

- `hour_class_slot (day_type, span_tod)` avec contrainte d'exclusion ; `day_type` 1..7 = jour ISO, 8 = férié journée entière
- 1er ordre : une minute appartient à une classe au plus, par construction ; pas de priorité entre classes
- 2e ordre : « à valider » par classe = un test de chevauchement sans sous-requête ; la répartition n'est calculée que pour les activities qui touchent un créneau

### D11. Folder : valeurs propres, estimé sur son total

- Le tarif d'un folder est sa colonne (pas d'héritage dans l'arbre) ; estimé dépassé = total du folder > estimé
- 1er ordre : toutes les activities non validées d'un folder dépassé sont à valider

### D12. Valeurs figées = colonnes de l'activity et de l'expense

- Remplacé par D34 : les valeurs figées sont des lignes de facture, pas des colonnes de l'activity
- 1er ordre : un changement de tarif suit sur le non facturé, rien ne bouge sur le facturé

### D13. `period_lock` ne gate que la saisie

- create, update, delete d'activity et d'expense ; valider et facturer restent possibles dans une période verrouillée

### D14. Une activity validée ne se modifie plus

- `editable` et `deletable` incluent `not_validated` ; `unvalidate` la rouvre

### D15. OCR : suggestion stockée

- `ocr_amount` = ce que le système a observé ; `amount` rempli par l'OCR seulement s'il est vide

### D16. `accounting_export` = view sur `delivery`

- L'export d'une invoice est une `delivery` ; « créé ou exporté » se calcule depuis ses `delivery_result`
- 1er ordre : une seule outbox pour tout sortant ; une invoice exportée ne revient pas en cours (`not_exported`)

### D17. Run = une transaction, un savepoint par root

- Annuler = rollback : rien n'est écrit ; un root refusé est noté et le run continue ; le progress s'écrit hors de la transaction

## Stack

### D18. Session navigateur par cookie

- Le serveur fait le flow authorization code + PKCE et pose le JWT en cookie HttpOnly ; agents et CLI envoient un bearer ; un seul chemin de vérification
- 1er ordre : `EventSource` natif, pas de token dans le JavaScript

### D19. Upload direct signé avec checksum

- Le navigateur envoie au storage par URL signée ; le sha256 signé fait vérifier le contenu par le storage ; l'enregistrement de la row `file` ne fait qu'un `HEAD`

### D20. UI : Svelte 5 + bits-ui + tokens CSS

- bits-ui : primitives headless natives Svelte 5 (Combobox, Select, DatePicker, Dialog, Popover, Menu, Tabs, Tooltip) ; style par nos tokens ; grid et toast maison
- Écarté : web components Fluent (ni combobox, ni date picker, ni grid) ; bibliothèques à Tailwind

### D21. MCP : un tool par view et par command

- Liste filtrée par capability ; mêmes schemas que HTTP

### D22. Hors slice

- Archive Parquet, effacement, extensions WASM, webhooks, multi-tenant, OpenTelemetry, session replay : voir SPEC §25

### D24. Formulation des projections, validée avant le code

Expérience sur Postgres 18, 600k activities, avant d'écrire le domaine. Une projection sert la lecture d'une row et l'agrégat de masse avec la même définition si elle respecte deux règles :

- Élagable : `left join` sur clé unique ou `left join … on true` vers une view d'une row ; le planner retire ce que la query n'utilise pas (`sum(duration)` par la view = la table seule)
- Parallel-safe : aucune sous-requête corrélée dans une colonne. Mesuré : rentabilité sur tout l'historique 2.5 s avec `exists` et sous-requêtes scalaires, 0.6 s avec joins sur clé, containment de multirange et une fonction SQL `parallel safe`
- 1er ordre : verrous de période lus comme multiranges ; classes d'heures par une fonction appelée seulement quand la durée touche un créneau ; `counted_min` entier
- 2e ordre : `plan_cache_mode = force_custom_plan` (le plan générique d'une view paramétrée est 4× plus lent et sans parallélisme)

### D25. Pagination par id

- Constat : sauter 5000 rows par `offset` à travers la projection coûte 250 ms, les joins s'exécutent pour les rows sautées
- Décision : la colonne `id` d'une view est l'identité de sa row ; le kernel pagine sur les ids (seules les colonnes du tri et du filtre sont calculées), puis lit les rows de la page
- 1er ordre : une page profonde coûte comme la première ; règle générale du wrap, aucune view n'est traitée à part

### D26. Lock d'autres roots : un statement à part

- Constat : en READ COMMITTED, un statement qui locke des rows et calcule depuis la projection lit les valeurs d'avant l'attente du lock
- Décision : l'effet commence par un `select … for update` par id ; les statements suivants ont un snapshot postérieur. `mark_invoiced` : lock, rattachement, puis gel des valeurs lues dans la projection
- 1er ordre : le check sur les colonnes figées devient `frozen ⇒ invoice_id`

### D27. Jobs : un aller-retour de plus

- Constat : l'insertion transactionnelle de River (`InsertManyTx`) est un aller-retour à elle seule ; elle ne se met pas dans le batch de commit
- Décision : ≤ 4 allers-retours pour une command, ≤ 5 pour une command qui enfile des jobs
- Écarté : écrire dans les tables de River par notre propre SQL (couplage à son schema interne) ; une outbox maison que River viderait (deux files)
- 1er ordre : le chemin chaud (saisie) n'a pas de reaction et reste à 4

### D28. `Under` sans FK = singleton

- `Under(root)` tient toutes les rows du root supérieur ; exact pour `company`. La forme générale par FK (CASES F5 : invoices sous leur période) est du model, hors slice

### D29. Plans : views avec leurs valeurs, commands préparées, joins dans l'ordre écrit

- Constat (modules réels, 600k activities) : planifier une view sur `activity_p` (16 relations) coûte 6 à 120 ms ; avec `force_custom_plan` (D24) chaque request le repaie, plus que l'exécution
- Décision : remplace le réglage global de D24
  - une view est un statement non nommé : planifiée à chaque exécution avec ses valeurs
  - les statements d'une command sont préparés : leur forme (lookup par ids) ne dépend pas des valeurs
  - `join_collapse_limit = 1`, `from_collapse_limit = 1` : l'ordre des joins est celui du SQL (root, puis lookups) ; la recherche d'ordre, qui fait le coût de planification, disparaît
- 1er ordre : l'auteur d'une projection écrit ses joins dans l'ordre d'exécution ; les plans sont déterministes
- 2e ordre : les réglages dont le model dépend sont posés sur la database par `sor migrate`, pas par l'environnement

Suite, mesurée après application :

- Préparer ne suffit pas : en mode `auto` Postgres replanifiait les statements d'une command à chaque appel. Une command commence par `set local plan_cache_mode = force_generic_plan` (même aller-retour) : 256 appels, 2 planifications ; `activity.update` p95 7 ms
- Avec l'ordre écrit, une view de plusieurs relations placée en `join` interne est planifiée en bloc et calculée en entier (160 à 730 ms au lieu de 0.2). Règle : une projection se lit par ses clés (`= any(array(…))` ou `left join`) ; une view de prédicat porte sur une seule relation. La pagination (D30) et la relecture des roots suivent déjà cette forme
- Planification après changement : 1 à 5 ms sur toutes les views

### D30. Pagination par id : lecture par tableau d'ids

- Constat : `k join (<view>) using (id)` est planifié sans `k` (view calculée en entier, 170 ms) ; `where id = any(array(select id from k))` pousse le filtre dans la view (12 ms)
- Décision : D25 garde son principe, sa forme SQL change

### D31. Une invoice prend les activities facturables

- Constat : le project interne (absences, vacances) recevait une invoice à 0 de 400 lignes à chaque run
- Décision : une activity dont la task n'est « jamais » facturée n'est candidate d'aucune invoice
- 1er ordre : `invoice_p`, les candidats du run et les effets de `mark_invoiced` partagent ce prédicat, écrit une fois

### D32. `self` est le reach du kernel

- Constat : intersection agent ∩ humain sur `self` = vide ({agent} ∩ {humain})
- Décision : `self` n'est pas une donnée du module d'accès ; c'est l'identité effective, posée par le kernel
- 1er ordre : un agent qui agit pour un humain voit ses rows anchorées `self` (saved views, runs)

### D33. Écho du live : le client retient, le serveur nomme

- Première idée, retirée : un id d'invocation choisi par le client (en-tête). Il servait aussi de clé de corrélation interne ; un id réutilisé mêlait deux invocations
- Décision : l'id est toujours celui du serveur ; tant qu'une de ses commands est en vol, le client retient les clés du live, puis écarte celles de son `call`
- 1er ordre : un mécanisme de moins, aucun en-tête ; un job a son propre id, donc le progress d'un run arrive à celui qui l'a lancé

## Revue du code contre le model

Une relecture indépendante du code (2026-11-25) a confirmé le cœur : un seul chemin de command pour humain, agent, job et run ; un seul wrap ; aucun nom du domaine dans le kernel. Elle a montré des coutures là où un concept avait été ajouté sans être décidé. Décisions :

### D34. Facturer = écrire des lignes

- Constat : `frozen_*` sur activity et expense mêlaient inputs et instantané d'une décision ; colonnes gardées, check, `coalesce` partout
- Décision : `invoice_line` et `invoice_expense`, tables child de l'invoice ; l'activity et l'expense ne portent que des inputs
- 1er ordre : « facturée » = une ligne existe ; une activity sur une seule invoice = contrainte unique ; revenir en cours = supprimer les lignes ; les totaux d'une invoice facturée se lisent dans ses lignes
- 2e ordre : l'historique d'une invoice montre ses lignes ; l'activity n'est pas modifiée par la facturation
- Écart avec SLICE (« stocké sur l'activity : facture ») : la facture d'une activity se lit par sa ligne

### D35. Le statement de lock rend `@locked`

- Les ids rendus par le `select … for update` d'un effet sont le param `@locked` des statements suivants : ils n'agissent que sur ce qui est locké. Une row apparue entre-temps reste hors de l'effet (elle sera candidate la prochaine fois)
- Remplace le rattachement en deux temps de D26

### D36. Rien n'est ouvert par défaut

- Constat : une table sans `Read` était lisible par tout principal authentifié
- Décision : `Read`, `Write` et `@needs` sont obligatoires ; `any` se dit, comme `@anchor none`
- 1er ordre : le registry servi à un principal (`/api/meta`, MCP) passe par un seul filtre de capability

### D37. Liste de travail = view + filtre par action

- Constat : la view « à valider » réécrivait en SQL les conditions de l'action `validatable`
- Décision : le wrap filtre par action ; les conditions vivent dans la projection et la déclaration, nulle part ailleurs
- 1er ordre : toute liste « ce sur quoi je peux faire X » s'obtient sans query

### D38. Une seule définition, un seul mécanisme

- `activity_class` lit `activity_p` au lieu d'en réécrire les formules
- La reaction sur insertion (`OnInsert`) remplace le cas `delivery` écrit dans le pipeline ; le chemin d'une delivery (`path`) est une donnée, pas une convention
- Un seul résolveur de reach ; `owner_id` = identité effective pour `run` comme pour `saved_view`
- `Me` et `Meta` sont dans OpenAPI : le contrat TypeScript est généré, plus recopié ; `ids` est un champ de la command ; l'icône d'un écran est déclarée avec lui, l'UI ne connaît aucun nom du domaine hors de l'écran de saisie
- Supprimé : options, hooks et branches que seuls les tests utilisaient

### D39. Registres du kernel

- `event`, `run`, `file` sont écrits par le kernel hors pipeline ; toute autre row change par une command. La règle sur les sous-requêtes corrélées (D24) vaut pour une projection agrégée en masse, pas pour un root lu par pages

### D23. Budgets

- Fixés depuis la perception (immédiat ≤ 100 ms, frappe ≤ 50 ms), avant mesure : SPEC §23
