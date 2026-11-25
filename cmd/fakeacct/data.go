package main

import "fmt"

var (
	namePrefixes = []string{"Alpenblick", "Lac Bleu", "Rive Droite", "Bergwerk", "Sonnhalde", "Pierre Blanche", "Rheintal", "Vallon", "Seeland", "Grand Pré", "Mittelland", "Clair Ruisseau", "Talwind", "Haute Route", "Lindenhof", "Belle Rive", "Juraweg"}
	nameSectors  = []string{"Bau", "Ingénierie", "Architectes", "Immobilier", "Logistik", "Conseil", "Treuhand", "Énergie", "Informatik", "Géomètres", "Transports", "Holz", "Paysage", "Medizintechnik", "Agro", "Design", "Hydraulique", "Elektro"}
	legalForms   = []string{"SA", "AG", "Sàrl", "GmbH"}
	streets      = []string{"Rue des Tilleuls", "Bahnhofstrasse", "Chemin du Verger", "Seestrasse", "Avenue de la Gare", "Kirchweg", "Route de la Forêt", "Industriestrasse", "Rue du Moulin", "Dorfstrasse", "Chemin des Vignes", "Lindenweg", "Rue du Lac"}
	towns        = []string{"1003 Lausanne", "1205 Genève", "1700 Fribourg", "1950 Sion", "2000 Neuchâtel", "2502 Biel/Bienne", "3011 Bern", "4051 Basel", "5000 Aarau", "6003 Luzern", "7000 Chur", "8001 Zürich", "9000 St. Gallen", "1820 Montreux", "3900 Brig"}
	mandates     = []string{"Étude de faisabilité", "Mandat annuel", "Révision", "Direction des travaux", "Expertise", "Avant-projet", "Projet d'exécution", "Accompagnement", "Audit", "Mise en service"}
	subjects     = []string{"Bâtiment Nord", "Siège", "Halle logistique", "Réseau", "Comptes", "Rénovation façade", "Parc", "Atelier", "Site Est", "Infrastructure", "Entrepôt", "Pont", "Annexe", "Système qualité", "Toiture"}
)

const (
	baseAccounts  = 300
	extraAccounts = 5
	perAccount    = 5
	extraProjects = 10
)

func accountID(i int) string { return fmt.Sprintf("A%04d", i+1) }

func accountName(i int) string {
	return namePrefixes[i%len(namePrefixes)] + " " + nameSectors[(i/len(namePrefixes))%len(nameSectors)] + " " + legalForms[i%len(legalForms)]
}

func accountAddress(i int) string {
	return fmt.Sprintf("%s %d, %s", streets[i%len(streets)], 1+(i*7)%60, towns[(i/len(streets)+i)%len(towns)])
}

func projectName(j int) string {
	return mandates[j%len(mandates)] + " – " + subjects[(j/len(mandates)+j)%len(subjects)]
}

// seed loads the dataset in a fixed order, so revisions are deterministic.
func (s *store) seed() {
	for i := range baseAccounts + extraAccounts {
		s.putAccount(account{ExternalID: accountID(i), Name: accountName(i), Address: accountAddress(i)})
	}
	for j := range baseAccounts*perAccount + extraProjects {
		owner := j / perAccount
		if j >= baseAccounts*perAccount {
			owner = baseAccounts + (j-baseAccounts*perAccount)/2
		}
		s.putProject(project{ExternalID: fmt.Sprintf("P%05d", j+1), AccountExternalID: accountID(owner), Name: projectName(j)})
	}
}
