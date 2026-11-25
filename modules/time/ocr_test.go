package time

import "testing"

func TestReceiptAmount(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"fr restaurant", `CAFE DU MARCHE
Place du Marché 3, 1003 Lausanne
25.11.2026 12:41  Table 4
2 x Menu du jour     19,50   39,00
1 x Eau minérale      4,80
Sous-total           43,80
TVA 8,1 %             3,28
TOTAL CHF            43,80
Carte                43,80
Merci de votre visite`, "43.80"},
		{"fr à payer with thousands", `Garage des Alpes
Facture 2026-118 du 03.11.2026
Pneus hiver          4 x 245.00   980.00
Main d'oeuvre                     312.50
Sous total                      1'292.50
TVA 8.1%                          104.69
Montant à payer CHF             1'397.19`, "1397.19"},
		{"de Gesamt", `Bäckerei Sonnhalde
Bahnhofstrasse 12, 8001 Zürich
Datum: 24.11.2026
Gipfeli          3 x 1.90     5.70
Brot dunkel                   6.40
Zwischensumme                12.10
MwSt 2.6%                     0.31
Gesamtbetrag CHF             12.10
Bar                          20.00
Rückgeld                      7.90`, "12.10"},
		{"de zu zahlen with comma decimals", `Hotel Seeland
Rechnung Nr. 4471
2 Nächte à 1.150,00       2.300,00
Kurtaxe                       7,00
Summe netto               2.307,00
MwSt 3,8 %                   87,67
Zu zahlen EUR             2.394,67`, "2394.67"},
		{"en amount due", `CITY PARKING LTD
Entry 08:02  Exit 17:45
Duration 9h43
Subtotal                 22.00
VAT 20%                   4.40
Amount due GBP           26.40
Paid by card             26.40`, "26.40"},
		{"en total with spaces as thousands", `Conference Centre
Room hire          1 200.00
Catering             450.50
TOTAL              1 650.50`, "1650.50"},
		{"no total line: largest amount", `Kiosk
Journal  4.50
Chocolat 2.20
Carte    6.70`, "6.70"},
		{"date and times are not amounts", `Tankstelle 24.11.2026 07:15
Bleifrei 95  32.41 l x 1.89
Betrag CHF 61.25`, "61.25"},
		{"nothing", `Merci
A bientôt`, ""},
	} {
		got, ok := receiptAmount(tc.text)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: got %q (%v), want %q", tc.name, got, ok, tc.want)
		}
	}
}
