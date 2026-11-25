# Cas de référence : une boutique

Une seule application cohérente : une boutique en ligne B2C et B2B, avec entrepôts, livraison, facturation et support.

MODEL(CASES) = implémentation. Le model est une transformation : il prend ces cas et produit le schema, les projections, les commands, les views, les roles et les écrans, sans décision ad hoc. Un cas qui demande une exception = défaut du model.

Ids stables : on ajoute, on ne renumérote pas.

## Acteurs

- Client B2C ; client B2B (une entreprise, plusieurs acheteurs, un responsable achats)
- Magasinier (rattaché à un entrepôt) ; chef d'entrepôt
- Livreur (tablette)
- Commercial (ses comptes B2B) ; chef des ventes (ses commerciaux)
- Support ; finance ; admin
- Agent IA support ; agent IA réassort (autonome) ; transporteur, provider de paiement, comptabilité (systèmes)

## Catalogue et stock

- C1 L'admin crée et modifie des produits ; un produit archivé n'est plus commandable mais reste dans l'historique
- C2 L'admin ajoute un champ « origine » aux produits, filtrable et visible en grille
- C3 Le prix de revient et la marge ne sont visibles que par la finance, y compris dans les totaux
- C4 Le magasinier réceptionne une livraison fournisseur au clavier, ligne par ligne, sans latence perçue
- C5 Le stock disponible = reçu − réservé − expédié, par entrepôt, toujours exact
- C6 L'agent réassort propose des commandes fournisseur quand le stock passe sous un seuil ; le chef d'entrepôt les approuve

## Commande

- O1 Le client remplit un panier, change les quantités, retire une ligne
- O2 Il passe commande ; le panier n'est plus modifiable ; le stock est réservé
- O3 Une commande B2B au-dessus de 10'000 doit être approuvée par le responsable achats du client avant d'être placée
- O4 Il paie en deux fois ; la commande affiche « partiellement payée », puis « payée »
- O5 Une commande B2C impayée 30 minutes après placement est annulée automatiquement, et le stock libéré
- O6 Il annule une commande placée non expédiée ; les paiements sont remboursés chez le provider
- O7 Annulation et expédition lancées au même instant sur la même commande : une seule réussit
- O8 Commande récurrente B2B : chaque premier lundi du mois, avec des mois sautés

## Expédition et livraison

- S1 Le magasinier de Genève voit et expédie les commandes de Genève seulement
- S2 Il expédie 3 lignes sur 5 ; la commande affiche « partiellement expédiée »
- S3 Expédier plus que la quantité restante est refusé, avec la quantité restante comme raison
- S4 La réservation du transporteur échoue pendant 2 heures ; rien n'est perdu ni doublé
- S5 Le transporteur notifie la livraison par webhook, parfois deux fois
- S6 Le livreur réserve un créneau de chargement au quai ; deux livreurs sur le même créneau : un seul réussit
- S7 Le livreur scanne les colis et prend une photo à la remise, sur tablette, avec un réseau instable
- S8 Une commande non expédiée 3 jours après placement apparaît « en retard », sans aucun write

## Retour et remboursement

- R1 Retour possible 30 jours après livraison, plus après
- R2 Le support accepte un retour ; le remboursement part au provider ; le stock revient à réception

## Facturation et comptabilité

- F1 Les clients B2B sont facturés une fois par mois pour toutes leurs commandes livrées, en un run : progress visible, annulable, défaisable
- F2 Une facture génère un PDF identique à chaque régénération
- F3 Les factures sont exportées en comptabilité ; une erreur côté compta ne bloque rien d'autre
- F4 Défacturer une facture exportée est refusé ; non exportée, accepté
- F5 La finance clôture le mois : plus aucune facture ni paiement modifiable dans la période

## Accès et agents

- A1 Le commercial voit ses comptes B2B et leurs commandes ; le chef des ventes voit ceux de tous ses commerciaux
- A2 Le support voit le state de paiement, pas les montants ni les moyens de paiement ; la finance voit tout
- A3 Un acheteur B2B voit les commandes de son entreprise ; le client B2C ne voit que les siennes
- A4 L'agent support traite les demandes d'un client : il ne voit que ce client ; l'historique dit agent + pour qui
- A5 L'agent réassort agit en son nom propre, avec ses propres droits

## Analytics et live

- L1 Dashboard des ventes par mois et par entrepôt, à jour en live
- L2 Tableau de marge par produit pour la finance
- L3 Classement des commerciaux visible par toute l'équipe ventes, sans voir les comptes des autres
- L4 Le statut d'une commande change chez le client au moment où le magasinier expédie

## Extensions

- X1 Un client installe un plugin qui ajoute une table, une view et une command ; il ne lit rien hors de son périmètre
- X2 Un plugin réagit à la facturation pour déposer un fichier chez l'ERP du client
- X3 Désinstaller le plugin ne casse rien
