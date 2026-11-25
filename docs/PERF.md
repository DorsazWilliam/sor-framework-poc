# Performance

Mesure unique du 2026-11-25 sur le code `012bd60`, sans réglage après coup. Données brutes : [perf-run.json](./perf-run.json). Harness : `perf/` (Playwright, 20 passes par scénario, la première écartée).

- Seed perf : 200 users, ~600k activities sur 3 ans, 38 914 invoices
- Stack locale `docker compose` sur un poste Windows, pendant que d'autres conteneurs sans rapport tournaient (deux autres Postgres, un stack de 7 services) : les chiffres sont indicatifs, à ±20 %
- Budgets : repères fixés avant toute mesure ([SPEC](./SPEC.md) §23). 100 ms et 1 s sont des seuils de perception connus ; les autres sont un jugement

## Résultat en trois lignes

- La saisie et les écrans de travail tiennent : écran de saisie affiché en 57 ms, activity sauvée et confirmée en 37 à 49 ms, validation de 100 activities en 208 ms, live entre deux sessions en 158 ms (p95, navigateur)
- Le pipeline tient son compte : 2 allers-retours DB par view, 4 par command, 5 avec un job
- Les écrans d'analyse dépassent d'un facteur 2 : soldes ~0.85 s, rentabilité avec prix de revient 0.8 s sur 12 mois et 1.9 s sur tout l'historique

## Navigateur

| # | Scénario | Budget ms | p50 | p95 | |
|---|---|---:|---:|---:|---|
| B1 | clic → écran de saisie affiché | 100 | 37 | 58 | ok |
| B2a | clic → activities, première page | 120 | 135 | 178 | dépassé |
| B2b | clic → facturation, première page | 120 | 76 | 126 | limite |
| B2c | clic → périodes verrouillées | 120 | 24 | 39 | ok |
| B2d | clic → projects (données de base) | 120 | 31 | 51 | ok |
| B3 | Entrée → activity créée et confirmée | 50 | 23 | 49 | ok |
| B3u | Entrée → activity modifiée et confirmée | 50 | 21 | 37 | ok |
| B4 | validation de 100 activities, liste à jour | 250 | 143 | 208 | ok |
| B5 | soldes, équipe du manager, année en cours | 150 | 807 | 892 | dépassé |
| B6 | soldes, 200 users, tout l'historique | 500 | 839 | 966 | dépassé |
| B7 | rentabilité avec prix de revient, 12 mois | 400 | 707 | 816 | dépassé |
| B8 | rentabilité avec prix de revient, tout l'historique | 900 | 1568 | 1935 | dépassé |
| B9 | grid activities, 1 mois, tous les users, première page | 120 | 123 | 140 | limite |
| B9d | grid activities, défilement profond | 120 | 40 | 54 | ok |
| B10 | run de facturation, 1 mois : premier progress | 300 | 320 | 354 | limite |
| B10e | run de facturation : fin (1 122 invoices) | 5000 | 2179 | 4383 | ok |
| B10r | run annulé puis relancé : fin | 5000 | 2456 | 5149 | limite |
| B11 | impression → PDF affiché | 1500 | 240 | 321 | ok |
| B12 | commit en session A → row à jour en session B | 300 | 149 | 158 | ok |

## Serveur

| # | Mesure | Budget | Valeur | |
|---|---|---:|---:|---|
| S1 | allers-retours DB : view / command / command avec job | 2 / 4 / 5 | 2 / 4 / 5 | ok |
| S2 | `activity.create` / `activity.update`, p95 | 15 ms | 18 / 22 ms | limite |
| S3 | `timesheet`, p95 | 5 ms | 13 à 20 ms | dépassé |
| S3 | page de `activity_grid`, p95 | 30 ms | 25 ms | ok |

Le S2 du fichier brut (88 ms) mélange la validation de 100 activities aux commands de saisie ; le tableau donne les deux commands de saisie.

## Lecture des dépassements

**Écrans d'analyse (B5 à B8) : le coût est payé deux fois.** L'écran générique demande les rows puis les totaux : deux requêtes, et pour une view d'agrégat chacune recalcule tout (`balance` 400 + 450 ms ; `profitability_cost` 730 + 775 ms). Une view d'agrégat rend peu de rows : le client peut sommer celles qu'il a, ou le serveur rendre rows et totaux d'un seul calcul. Non fait.

**Soldes de l'équipe (B5) aussi chers que tous les users (B6).** Le report d'un solde se calcule depuis la première activity de chaque user ; l'intervalle affiché ne borne rien. C'est la conséquence directe de « soldes calculés, rien de stocké ». À 10 ans d'historique ce coût triple. Un solde clôturé en fin d'année serait un input (une décision), pas une valeur dérivée : la piste est dans le model, pas dans le SQL.

**Rentabilité avec prix de revient : 730 ms sur 12 mois, contre 315 ms mesurés plus tôt dans la journée sur le même seed.** Écart non expliqué (machine plus chargée, ou coût ajouté par une des dernières passes). Non investigué.

**Première page des activities (B2a, B9).** La page elle-même coûte 25 ms ; ce sont les totaux affichés par défaut (une somme sur les ~17 000 activities du mois, montants calculés) qui ajoutent ~95 ms.

**`timesheet` à 13–20 ms pour 5.** La planification de la projection (une quinzaine de relations) se repaie à chaque view. L'écran de saisie reste à 58 ms.

## Ce que la mesure dit du model

- Le chemin d'écriture est court et stable : lock, projection, effet avec ses events, relecture, commit, en 4 allers-retours, à 600k activities comme à 40k
- « Tout calculer dans la projection » tient pour lire une row, un jour, une page (0.3 à 25 ms) et pour un run de 1 100 invoices (2 s)
- Il coûte en agrégat sur l'historique : 2 à 3 µs par activity et par requête, quoi qu'on demande. C'est là que le model devra choisir ce qu'une clôture fige
- La planification a coûté plus que l'exécution jusqu'à ce que l'ordre des joins soit fixé ([DECISIONS](./DECISIONS.md) D29) : une projection large impose une discipline d'écriture du SQL
