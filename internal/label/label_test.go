package label

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ─── Utilitaires ─────────────────────────────────────────────────────────

func lignesTest(n int) []map[string]any {
	lignes := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		lignes = append(lignes, BuildLigneExport(map[string]any{
			"dn":        fmt.Sprintf("CN=Eleve %d,OU=3emeA,DC=lycee,DC=local", i),
			"sam":       fmt.Sprintf("eleve%d", i),
			"cn":        fmt.Sprintf("Eleve %d", i),
			"givenName": fmt.Sprintf("Prenom%d", i),
			"sn":        fmt.Sprintf("Nom%d", i),
			"mail":      fmt.Sprintf("eleve%d@lycee.fr", i),
		}))
	}
	return lignes
}

func jpegTest() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 60, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 60; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

func ids(modeles []ModeleLabel) []string {
	out := make([]string, 0, len(modeles))
	for _, m := range modeles {
		out = append(out, m.ID)
	}
	return out
}

// ─── Planches ────────────────────────────────────────────────────────────

func TestPlanchesIntegreesValides(t *testing.T) {
	for cle, planche := range Planches {
		if errs := planche.Validate(); len(errs) != 0 {
			t.Errorf("planche %s invalide: %v", cle, errs)
		}
	}
}

func TestGeometrieAveryRepriseDeLExport(t *testing.T) {
	for _, cle := range []string{"avery_l7160", "avery_l7163"} {
		f := FormatsAvery[cle]
		p := Planches[cle]
		if (p.LargeurMM != f.LargeurMM) || (p.HauteurMM != f.HauteurMM) {
			lw, lh := p.LabelSizeMM()
			t.Errorf("%s dimensions: (%v,%v) != format %v", cle, lw, lh, f)
		}
		if p.Colonnes != f.Colonnes || p.Lignes != f.Lignes {
			t.Errorf("%s grille: %d×%d", cle, p.Colonnes, p.Lignes)
		}
		if p.MargeGaucheMM != f.MargeGaucheMM || p.PasHorizontalMM != f.PasHorizontalMM {
			t.Errorf("%s marges/pas: %v", cle, p)
		}
		if p.ParPlanche() != f.ParPlanche() {
			t.Errorf("%s par planche: %d", cle, p.ParPlanche())
		}
	}
}

func TestBadgeEtCarteOfferts(t *testing.T) {
	badge, okBadge := Planches["badge_85x55"]
	carte, okCarte := Planches["carte_85x54"]
	if !okBadge || !okCarte {
		t.Fatal("planches badge/carte manquantes")
	}
	if badge.LargeurMM != 85.0 || carte.HauteurMM != 54.0 {
		t.Errorf("dimensions inattendues: %v / %v", badge, carte)
	}
}

func TestOriginLabelPlaceLaGrille(t *testing.T) {
	p := Planches["avery_l7160"]
	x, y := p.OriginLabel(0, 0)
	if x != p.MargeGaucheMM || y != p.MargeHautMM {
		t.Errorf("origine = (%v,%v)", x, y)
	}
	if _, y1 := p.OriginLabel(1, 0); y1 != p.MargeHautMM+p.PasVerticalMM {
		t.Errorf("ligne 1 = %v", y1)
	}
}

func TestPasQuiSeChevaucheRejete(t *testing.T) {
	p := Planche{Key: "bad", Nom: "Bad", Page: "A4", LargeurMM: 63.5, HauteurMM: 38.1, Colonnes: 3, Lignes: 7,
		MargeGaucheMM: 7.5, MargeHautMM: 15.5, PasHorizontalMM: 50.0, PasVerticalMM: 38.1}
	if !contientMot(p.Validate(), "chevauchement") {
		t.Errorf("attendu chevauchement: %v", p.Validate())
	}
}

func TestDebordementRejete(t *testing.T) {
	p := Planche{Key: "bad", Nom: "Bad", Page: "A4", LargeurMM: 63.5, HauteurMM: 38.1, Colonnes: 5, Lignes: 7,
		MargeGaucheMM: 7.5, MargeHautMM: 15.5, PasHorizontalMM: 66.0, PasVerticalMM: 38.1}
	if !contientMot(p.Validate(), "largeur") {
		t.Errorf("attendu débordement en largeur: %v", p.Validate())
	}
}

func TestPageInconnueRejetee(t *testing.T) {
	p := Planche{Key: "bad", Nom: "Bad", Page: "A5", LargeurMM: 63.5, HauteurMM: 38.1, Colonnes: 3, Lignes: 7,
		MargeGaucheMM: 7.5, MargeHautMM: 15.5, PasHorizontalMM: 66.0, PasVerticalMM: 38.1}
	if !contientMot(p.Validate(), "A4 ou A3") {
		t.Errorf("attendu page inconnue: %v", p.Validate())
	}
}

// ─── Éléments ────────────────────────────────────────────────────────────

func TestElementParDefautValide(t *testing.T) {
	if errs := ElementLabelDefaut().Validate(nil); len(errs) != 0 {
		t.Errorf("élément par défaut invalide: %v", errs)
	}
}

func TestCataloguesCoherents(t *testing.T) {
	if len(TypesElement) != 4 {
		t.Errorf("TypesElement = %v", TypesElement)
	}
	for _, cle := range []string{"texte", "statique", "photo", "qr"} {
		if _, ok := TypesElement[cle]; !ok {
			t.Errorf("type manquant: %s", cle)
		}
	}
	if len(Alignements) != 3 {
		t.Errorf("Alignements = %v", Alignements)
	}
}

func TestPolicesStandardUniquement(t *testing.T) {
	attendu := []string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Times-Roman", "Times-Bold", "Courier"}
	if !reflect.DeepEqual(Polices, attendu) {
		t.Errorf("Polices = %v", Polices)
	}
	for _, modele := range ModelesIntegres() {
		for _, el := range modele.Elements {
			if !contientChaine(Polices, el.Font) {
				t.Errorf("police non standard %q dans %s", el.Font, modele.ID)
			}
		}
	}
}

func TestElementTypeInconnu(t *testing.T) {
	el := ElementLabelDefaut()
	el.Kind = "logo"
	if !contientMot(el.Validate(nil), "inconnu") {
		t.Errorf("attendu inconnu: %v", el.Validate(nil))
	}
}

func TestElementChampInconnu(t *testing.T) {
	el := ElementLabelDefaut()
	el.Field = "numero_tele"
	if !contientMot(el.Validate(nil), "champ inconnu") {
		t.Errorf("attendu champ inconnu: %v", el.Validate(nil))
	}
}

func TestElementTexteFixeVide(t *testing.T) {
	el := ElementLabelDefaut()
	el.Kind = "statique"
	el.Text = "   "
	if !contientMot(el.Validate(nil), "Texte fixe vide") {
		t.Errorf("attendu texte vide: %v", el.Validate(nil))
	}
}

func TestElementCouleurInvalide(t *testing.T) {
	el := ElementLabelDefaut()
	el.ColorHex = "rouge"
	if !contientMot(el.Validate(nil), "#RRGGBB") {
		t.Errorf("attendu couleur: %v", el.Validate(nil))
	}
}

func TestElementAlignementInvalide(t *testing.T) {
	el := ElementLabelDefaut()
	el.Align = "milieu"
	if !contientMot(el.Validate(nil), "Alignement") {
		t.Errorf("attendu alignement: %v", el.Validate(nil))
	}
}

func TestElementPoliceInconnue(t *testing.T) {
	el := ElementLabelDefaut()
	el.Font = "Comic"
	if !contientMot(el.Validate(nil), "Police") {
		t.Errorf("attendu police: %v", el.Validate(nil))
	}
}

func TestElementDebordement(t *testing.T) {
	el := ElementLabelDefaut()
	el.XMM, el.WMM = 60, 40
	taille := [2]float64{63.5, 38.1}
	if !contientMot(el.Validate(&taille), "droite") {
		t.Errorf("attendu débordement droite: %v", el.Validate(&taille))
	}
	el = ElementLabelDefaut()
	el.YMM, el.HMM = 35, 10
	if !contientMot(el.Validate(&taille), "bas") {
		t.Errorf("attendu débordement bas: %v", el.Validate(&taille))
	}
}

func TestElementPositionNegative(t *testing.T) {
	el := ElementLabelDefaut()
	el.XMM = -1
	if !contientMot(el.Validate(nil), "négative") {
		t.Errorf("attendu négative: %v", el.Validate(nil))
	}
}

func TestElementValueLitLaLigne(t *testing.T) {
	row := lignesTest(1)[0]
	el := ElementLabelDefaut()
	if el.Value(row) != row["nom_complet"] {
		t.Errorf("value = %v", el.Value(row))
	}
	statique := ElementLabelDefaut()
	statique.Kind, statique.Text = "statique", "ECOLE"
	if statique.Value(row) != "ECOLE" {
		t.Errorf("value statique = %v", statique.Value(row))
	}
	champ := ElementLabelDefaut()
	champ.Field = "mail"
	if champ.Value(map[string]any{}) != "" {
		t.Errorf("value absente = %v", champ.Value(map[string]any{}))
	}
}

func TestElementAllerRetour(t *testing.T) {
	el := ElementLabelDefaut()
	el.Kind, el.Field, el.XMM, el.SizePT, el.Align = "qr", "identifiant", 12.5, 9.5, "droite"
	if got := ElementDepuisDict(el.ToDict()); !reflect.DeepEqual(got, el) {
		t.Errorf("aller-retour: %+v != %+v", got, el)
	}
}

func TestElementHitTest(t *testing.T) {
	modele := ModelesIntegres()[0]
	el := modele.Elements[0]
	if idx, ok := modele.ElementIndexAt(el.XMM+1, el.YMM+1); !ok || idx != 0 {
		t.Errorf("hit-test = %d, %v", idx, ok)
	}
	if _, ok := modele.ElementIndexAt(-5, -5); ok {
		t.Error("point hors étiquette détecté")
	}
}

// ─── Modèles ─────────────────────────────────────────────────────────────

func TestQuatreModelesIntegresValides(t *testing.T) {
	modeles := ModelesIntegres()
	if len(modeles) != 4 {
		t.Fatalf("modeles = %d", len(modeles))
	}
	for _, m := range modeles {
		if !m.Integre {
			t.Errorf("%s n'est pas marqué intégré", m.ID)
		}
		if errs := m.Validate(); len(errs) != 0 {
			t.Errorf("%s invalide: %v", m.ID, errs)
		}
	}
}

func TestModelesCouvrentLesFonctions(t *testing.T) {
	types := map[string]bool{}
	planches := map[string]bool{}
	for _, m := range ModelesIntegres() {
		planches[m.Planche] = true
		for _, el := range m.Elements {
			types[el.Kind] = true
		}
	}
	for _, typ := range []string{"texte", "statique", "photo", "qr"} {
		if !types[typ] {
			t.Errorf("type absent: %s", typ)
		}
	}
	for _, p := range []string{"avery_l7160", "avery_l7163", "badge_85x55", "carte_85x54"} {
		if !planches[p] {
			t.Errorf("planche absente: %s", p)
		}
	}
}

func TestModelePlancheInconnue(t *testing.T) {
	m := ModeleLabel{ID: "x", Nom: "X", Planche: "inconnue", Elements: []ElementLabel{ElementLabelDefaut()}}
	if !contientMot(m.Validate(), "Planche") {
		t.Errorf("attendu planche: %v", m.Validate())
	}
}

func TestModeleIDInvalide(t *testing.T) {
	m := ModeleLabel{ID: "Mauvais Id!", Nom: "X", Planche: "avery_l7160", Elements: []ElementLabel{ElementLabelDefaut()}}
	if !contientMot(m.Validate(), "Identifiant") {
		t.Errorf("attendu identifiant: %v", m.Validate())
	}
}

func TestModeleNomVide(t *testing.T) {
	m := ModeleLabel{ID: "x", Nom: "  ", Planche: "avery_l7160", Elements: []ElementLabel{ElementLabelDefaut()}}
	if !contientMot(m.Validate(), "Nom du modèle vide") {
		t.Errorf("attendu nom vide: %v", m.Validate())
	}
}

func TestModeleSansElement(t *testing.T) {
	m := ModeleLabel{ID: "x", Nom: "X", Planche: "avery_l7160"}
	if !contientMot(m.Validate(), "Aucun élément") {
		t.Errorf("attendu aucun élément: %v", m.Validate())
	}
}

func TestModeleFondInvalide(t *testing.T) {
	m := ModeleLabel{ID: "x", Nom: "X", Planche: "avery_l7160", FondHex: "bleu", Elements: []ElementLabel{ElementLabelDefaut()}}
	if !contientMot(m.Validate(), "fond") {
		t.Errorf("attendu fond: %v", m.Validate())
	}
}

func TestModeleAllerRetour(t *testing.T) {
	m := ModelesIntegres()[1]
	if got := ModeleDepuisDict(m.ToDict()); !reflect.DeepEqual(got, m) {
		t.Errorf("aller-retour modèle: %+v != %+v", got, m)
	}
}

func TestModeleCloneProfond(t *testing.T) {
	m := ModelesIntegres()[0]
	clone := m.Clone()
	clone.ID = "copie"
	clone.Elements[0].XMM = 42
	if m.Elements[0].XMM == 42 {
		t.Error("clone non profond")
	}
	if clone.Integre {
		t.Error("clone marqué intégré")
	}
}

// ─── Persistance ─────────────────────────────────────────────────────────

func TestSauvegardeEtChargement(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichier)
	modeles := ModelesIntegres()
	if _, err := SauvegarderModeles(modeles, chemin); err != nil {
		t.Fatalf("SauvegarderModeles: %v", err)
	}
	charges := ChargerModeles(chemin)
	if !reflect.DeepEqual(ids(charges), ids(modeles)) {
		t.Fatalf("ids = %v", ids(charges))
	}
	if !reflect.DeepEqual(charges[0].ToDict(), modeles[0].ToDict()) {
		t.Errorf("modèle 0 différent")
	}
}

func TestFichierAbsentRetourneIntegres(t *testing.T) {
	charges := ChargerModeles(filepath.Join(t.TempDir(), "absent.json"))
	if !reflect.DeepEqual(ids(charges), ids(ModelesIntegres())) {
		t.Errorf("ids = %v", ids(charges))
	}
}

func TestFichierCorrompuRetourneIntegres(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichier)
	if err := os.WriteFile(chemin, []byte("{ pas du json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(ChargerModeles(chemin)), ids(ModelesIntegres())) {
		t.Error("fichier corrompu mal géré")
	}
}

func TestIDModeleUnique(t *testing.T) {
	modeles := ModelesIntegres()
	cas := map[string]string{
		"nouveau":          "nouveau",
		"carte_visite":     "carte_visite-2",
		"Carte_Visite":     "carte_visite-2",
		"Étiquette classe": "etiquette-classe",
		"Étiquette_Classe": "etiquette_classe-2",
	}
	for base, attendu := range cas {
		if got := IDModeleUnique(modeles, base); got != attendu {
			t.Errorf("IDModeleUnique(%q) = %q, attendu %q", base, got, attendu)
		}
	}
}

func TestDupliquerModeleGardeLOriginal(t *testing.T) {
	modeles := ModelesIntegres()
	clone := DupliquerModele(modeles, modeles[0], "")
	if clone.ID == modeles[0].ID {
		t.Error("id du clone identique")
	}
	if clone.Integre {
		t.Error("clone marqué intégré")
	}
	if len(modeles) != 4 {
		t.Errorf("liste modifiée: %d", len(modeles))
	}
}

func TestExportPuisImport(t *testing.T) {
	source := ModelesIntegres()[2]
	dest := filepath.Join(t.TempDir(), "modele.json")
	if _, err := ExporterModele(source, dest); err != nil {
		t.Fatalf("ExporterModele: %v", err)
	}
	importe, err := ImporterModele(dest)
	if err != nil {
		t.Fatalf("ImporterModele: %v", err)
	}
	if !reflect.DeepEqual(importe.ToDict(), source.ToDict()) {
		t.Error("modèle importé différent")
	}
}

func TestImportRejetteModeleInvalide(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(chemin, []byte(`{"id":"x","name":"X","elements":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModele(chemin); err == nil {
		t.Error("modèle invalide accepté")
	}
}

func TestImportRejetteNonObjet(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(chemin, []byte("pas un objet"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModele(chemin); err == nil {
		t.Error("fichier non objet accepté")
	}
}

// ─── Rendu PDF ───────────────────────────────────────────────────────────

func TestRenduMultiPages(t *testing.T) {
	modele := ModelesIntegres()[0] // 21 étiquettes/planche
	donnees, pages, err := RendreEtiquettesPDF(lignesTest(25), modele, nil)
	if err != nil {
		t.Fatalf("RendreEtiquettesPDF: %v", err)
	}
	if pages != 2 {
		t.Errorf("pages = %d, attendu 2", pages)
	}
	if !bytes.HasPrefix(donnees, []byte("%PDF-")) {
		t.Error("pas un PDF")
	}
}

func TestBadgeEtCarteRendent(t *testing.T) {
	for _, modele := range ModelesIntegres() {
		donnees, pages, err := RendreEtiquettesPDF(lignesTest(3), modele, nil)
		if err != nil {
			t.Fatalf("%s: %v", modele.ID, err)
		}
		if pages < 1 || !bytes.HasPrefix(donnees, []byte("%PDF-")) {
			t.Errorf("%s: pages=%d", modele.ID, pages)
		}
	}
}

func TestModeleInvalideRefuseLeRendu(t *testing.T) {
	modele := ModeleLabel{ID: "x", Nom: "X", Planche: "inconnue", Elements: []ElementLabel{ElementLabelDefaut()}}
	if _, _, err := RendreEtiquettesPDF(lignesTest(1), modele, nil); err == nil {
		t.Error("modèle invalide accepté au rendu")
	}
}

func TestPhotoIntegreeSansErreur(t *testing.T) {
	var badge ModeleLabel
	for _, m := range ModelesIntegres() {
		if m.Planche == "badge_85x55" {
			badge = m
		}
	}
	lignes := lignesTest(2)
	photos := map[string][]byte{lignes[0]["identifiant"].(string): jpegTest()}
	donnees, pages, err := RendreEtiquettesPDF(lignes, badge, photos)
	if err != nil {
		t.Fatalf("RendreEtiquettesPDF: %v", err)
	}
	if pages != 1 {
		t.Errorf("pages = %d", pages)
	}
	if len(donnees) <= 1000 {
		t.Errorf("PDF trop petit sans photo intégrée: %d", len(donnees))
	}
}

func TestPhotoManquantePlaceholder(t *testing.T) {
	var badge ModeleLabel
	for _, m := range ModelesIntegres() {
		if m.Planche == "badge_85x55" {
			badge = m
		}
	}
	donnees, pages, err := RendreEtiquettesPDF(lignesTest(1), badge, map[string][]byte{})
	if err != nil || pages != 1 {
		t.Fatalf("pages=%d err=%v", pages, err)
	}
	if !bytes.HasPrefix(donnees, []byte("%PDF-")) {
		t.Error("pas un PDF")
	}
}

func TestQRSansIdentifiantIgnore(t *testing.T) {
	modele := ModelesIntegres()[0]
	lignes := []map[string]any{{"nom_complet": "Sans Id", "identifiant": ""}}
	donnees, pages, err := RendreEtiquettesPDF(lignes, modele, nil)
	if err != nil || pages != 1 {
		t.Fatalf("pages=%d err=%v", pages, err)
	}
	if !bytes.HasPrefix(donnees, []byte("%PDF-")) {
		t.Error("pas un PDF")
	}
}

func TestListeVideProduitUnePage(t *testing.T) {
	donnees, pages, err := RendreEtiquettesPDF(nil, ModelesIntegres()[0], nil)
	if err != nil {
		t.Fatalf("RendreEtiquettesPDF: %v", err)
	}
	if pages != 1 {
		t.Errorf("pages = %d", pages)
	}
	if !bytes.HasPrefix(donnees, []byte("%PDF-")) {
		t.Error("pas un PDF")
	}
}

// ─── Trombinoscope ───────────────────────────────────────────────────────

func TestTrombinoscopeA4EtA3(t *testing.T) {
	for _, page := range []string{"A4", "A3"} {
		donnees, pages, err := GenererTrombinoscopePDF(lignesTest(9), OptionsTrombinoscope{
			Page: page, Colonnes: 4, Titre: "Trombinoscope 3emeA", SousTitre: "Année 2026-2027",
		})
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		if pages < 1 || !bytes.HasPrefix(donnees, []byte("%PDF-")) {
			t.Errorf("%s: pages=%d", page, pages)
		}
	}
}

func TestTrombinoscopeColonnesBornees(t *testing.T) {
	if _, pages, err := GenererTrombinoscopePDF(lignesTest(3), OptionsTrombinoscope{Colonnes: 99}); err != nil || pages < 1 {
		t.Errorf("pages=%d err=%v", pages, err)
	}
}

func TestTrombinoscopePageInconnue(t *testing.T) {
	if _, _, err := GenererTrombinoscopePDF(lignesTest(1), OptionsTrombinoscope{Page: "A5"}); err == nil {
		t.Error("page inconnue acceptée")
	}
}

func TestTrombinoscopeVideRefuse(t *testing.T) {
	if _, _, err := GenererTrombinoscopePDF(nil, OptionsTrombinoscope{}); err == nil {
		t.Error("liste vide acceptée")
	}
}

func TestTrombinoscopeMultiPages(t *testing.T) {
	if _, pages, err := GenererTrombinoscopePDF(lignesTest(40), OptionsTrombinoscope{Colonnes: 4}); err != nil || pages < 2 {
		t.Errorf("pages=%d err=%v", pages, err)
	}
}

func TestTrombinoscopePhotosEtPlaceholders(t *testing.T) {
	lignes := lignesTest(3)
	photos := map[string][]byte{lignes[0]["identifiant"].(string): jpegTest()}
	donnees, pages, err := GenererTrombinoscopePDF(lignes, OptionsTrombinoscope{Photos: photos})
	if err != nil || pages != 1 {
		t.Fatalf("pages=%d err=%v", pages, err)
	}
	if !bytes.HasPrefix(donnees, []byte("%PDF-")) {
		t.Error("pas un PDF")
	}
}

func TestTaillePagesOfficielles(t *testing.T) {
	if TaillePagesMM["A4"] != [2]float64{210.0, 297.0} {
		t.Errorf("A4 = %v", TaillePagesMM["A4"])
	}
	if TaillePagesMM["A3"] != [2]float64{297.0, 420.0} {
		t.Errorf("A3 = %v", TaillePagesMM["A3"])
	}
}

func TestInitiales(t *testing.T) {
	cas := map[string]string{
		"Thomas Martin": "TM",
		"Eleve 10":      "EL",
		"":              "?",
		"A":             "A",
	}
	for entree, attendu := range cas {
		if got := Initiales(entree); got != attendu {
			t.Errorf("Initiales(%q) = %q, attendu %q", entree, got, attendu)
		}
	}
}

func TestBuildLigneExportDeriveLaClasse(t *testing.T) {
	row := BuildLigneExport(map[string]any{
		"dn": "CN=Eleve 3,OU=3emeA,DC=lycee,DC=local", "sam": "eleve3",
		"cn": "Eleve 3", "mail": "e3@lycee.fr", "disabled": true,
	})
	if row["classe"] != "3emeA" || row["identifiant"] != "eleve3" || row["etat"] != "Désactivé" {
		t.Errorf("ligne = %v", row)
	}
}

// contientMot indique si au moins un message contient le fragment donné.
func contientMot(messages []string, fragment string) bool {
	for _, m := range messages {
		if strings.Contains(m, fragment) {
			return true
		}
	}
	return false
}
