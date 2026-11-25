# Slice

La feature slice que ce POC construit sur [MODEL.md](./MODEL.md) : saisie des heures, frais, validation, soldes, facturation et rentabilité d'une société de services. Elle doit éprouver le model sur toutes ses aspérités, et prouver des temps de chargement et de sauvegarde très bas vus de l'UI.

## Environnement

- Un `docker compose up` : Postgres 18, SeaweedFS (S3), mock-oauth2-server (OIDC), l'app (image avec tesseract et Typst), un faux serveur de comptabilité
- Une seule database
- UI : SPA, design system posé dès le départ, écrans génériques depuis le registry ; écran de saisie fait main ; live minimal
- Textes : descriptions en anglais dans les commentaires SQL (MCP, OpenAPI) ; labels UI fr/de/en dans des fichiers embarqués ; labels des éléments créés par l'admin sur leur row ; français par défaut

## Concepts

- account : client ; importé de la comptabilité
- project : appartient à un account ; tarif ; saisie interdite ; archivé ; user référent
- folder : arbre sous un project (un folder peut avoir un folder parent) ; tarif ; durée estimée ; saisie interdite
- task : catalogue global de types de tâche, permis folder par folder
  - genre : prestation, absence, vacances
  - facturation : toujours (durée facturée = durée réelle), parfois (durée facturée saisie), jamais (0)
  - validation responsable requise ou non
  - tarif
- user : responsable (manager), groupes
- group, function : droits = fonctions cochées par groupe ; un user a l'union (OR) de ses groupes ; admin = tout
- work_period : par user, à partir d'une date : heures par jour de semaine, jours de vacances par an, taux d'activité, prix de revient horaire
- public_holiday : date, heures déduites (veille de fête partielle possible)
- hour_class : s'applique selon jours de semaine, heures, fériés ; coefficient sur les heures comptées, coefficient sur le tarif ; validation responsable requise ou non
- activity : user, date, heure de début, durée réelle (minutes), durée facturée (saisie seulement si facturation « parfois »), folder, task, description
- expense_type : par montant ou par quantité ; prix unitaire ; prix de facturation distinct ; mode : rembourser le user et facturer, user seulement, facturation seulement
- expense : ligne de frais sur une activity : type, quantité, montant, mode ; ticket (file) ; montant lu par OCR
- period_lock : de, à, un user ou tous ; aucune écriture d'activity ou d'expense dans la période
- invoice : project, account, période du / au ; en cours ou facturée
- accounting_export : export d'une facture facturée vers la comptabilité ; créé ou exporté

## Calculé sur une activity

- Tarif : celui du folder, sinon du project, sinon de la task ; un tarif par user × task le remplace
- Montant = tarif × durée facturée
- Durée comptée : 0 pour une task sans heures, sinon somme pondérée par les classes d'heures, sinon durée réelle
- Répartition par classe d'heures : durée, montant, prix de revient par classe
- Prix de revient = prix de revient horaire de la work_period en vigueur × durée comptée
- À valider : la task le demande, ou une classe d'heures qui le demande porte des heures, ou l'estimé du folder est dépassé
- Stocké : validée par, validée le ; facture

## Validation

- Par activity : le responsable valide une activity « à valider » ; une seule étape
- Scope du responsable : ses subordonnés directs, ou tous ses subordonnés (arbre), ou tout le monde, selon ses fonctions
- period_lock : posé et retiré par les fonctions dédiées

## Soldes heures et vacances

- Heures à faire = heures de la work_period par jour de semaine sur l'intervalle, moins les fériés, au prorata du taux d'activité
- Heures faites = somme des durées comptées
- Vacances disponibles = jours par an × heures par jour de vacances, au prorata de la part d'année couverte ; vacances prises = activities dont la task est du genre vacances
- Soldes reportés d'un intervalle et d'une année à l'autre ; rien au-delà d'aujourd'hui
- Sortie : reportées, à faire, faites, solde ; vacances reportées, disponibles, prises, solde

## Facturation

- Une invoice couvre un project et une période ; elle prend les activities non facturées de la période
- Passer à facturée : les activities sont rattachées à l'invoice et leurs valeurs de rentabilité (montant, tarif, durée) figées
- Revenir en cours : les activities sont libérées, leurs valeurs figées effacées
- Une invoice facturée ne se supprime pas ; une invoice sans activity ne se facture pas
- Changer l'état de facturation demande une fonction dédiée
- Run : crée les invoices en cours de tous les projects qui ont des activities non facturées sur une période (progress live, annulable)
- PDF : facture + récapitulatif de ses activities ; identique à chaque régénération

## Rentabilité d'un project (et d'un folder)

- Sur les activities d'un intervalle : prestations (montants), frais facturés, frais collaborateur, durées, prix de revient
- Réalisé = prestations + frais
- Solde = réalisé − facturé
- Marge = prestations − prix de revient ; solde − prix de revient
- Les activities facturées utilisent leurs valeurs figées
- Prix de revient visible seulement avec la fonction dédiée

## Comptabilité (faux serveur)

- Pull : job périodique qui importe accounts et projects ; manuel aussi
- Push : une command exporte une invoice facturée vers la comptabilité ; le serveur peut être mis en panne

## Roles et reach

- Fonctions : activities, frais, factures, état de facturation, period_lock, validation, prix de revient, données de base
- Scope sur les activities des autres : aucun, subordonnés directs, tous les subordonnés, tous
- Principals de service : comptabilité, ocr
- Agent MCP pour un user : reach = intersection

## Commands

- Données de base : account (lecture + pull), project, folder, task, expense_type, user, group et fonctions, work_period, public_holiday, hour_class : create, update, delete, archive
- activity : create, update, delete ; conditions : pas dans un period_lock, pas facturée, folder et project ouverts à la saisie, task permise dans le folder, owner ou dans le scope
- activity : validate, unvalidate
- expense : create, update, delete, attach_receipt ; apply_ocr par le principal ocr ; mêmes conditions que l'activity
- period_lock : create, delete
- invoice : create, mark_invoiced, back_to_draft, delete (en cours), export
- invoice run
- invoice.print
- account.upsert_from_accounting : principal comptabilité, depuis le job pull
- saved_view : save, delete

## Views

- Listes génériques de chaque entité
- Ma journée, ma semaine
- À valider, dans le scope
- Activities : grid dans le reach, filtre, group, saved views
- Soldes heures et vacances par user
- Rentabilité par project et par folder (cross-root)
- Historique d'une row

## Écrans

1. Saisie : journée et semaine au clavier ; fait main
2. Frais d'une activity : upload du ticket, montant pré-rempli par OCR, live
3. À valider : liste du responsable, validation en masse, live
4. Activities : grid générique, saved views
5. Soldes heures et vacances
6. Rentabilité des projects
7. Facturation : run, liste, mark_invoiced, back_to_draft, PDF affiché, export avec statut et relance
8. Périodes verrouillées
9. Données de base : écrans génériques
10. Comptabilité : connection, dernier pull, pull manuel
11. Historique : panneau sur chaque row

## Performance

- Seed synthétique : ≤ 600k activities, ≤ 200 users, distributions réalistes ; charge normale
- Budgets fixés avant de mesurer
- Navigateur (Playwright) : clic → écran affiché ; frappe → sauvé confirmé ; validation en masse ; ouverture des soldes et de la rentabilité ; run de facturation
- Serveur : allers-retours DB par request, durée de chaque view et command
- Un budget dépassé = défaut de conception

## À éprouver

- Écriture d'une activity pendant qu'une période se verrouille ou qu'une invoice la fige (lock entre roots)
- Tarif modifié après coup : les activities non facturées suivent, les facturées gardent leurs valeurs figées
- Répartition par classe d'heures et « à valider » entièrement calculés
- Soldes calculés en view, avec reports et fériés
- Refus avec les conditions fausses, en UI et en MCP
- Query anchorée sans `@reach` refusée au build ; hors reach → `not_found`
- Run annulé : rien n'est écrit
- Comptabilité en panne : rien de perdu ni doublé, relance par command
- Même PDF à chaque régénération
