package grouptemplates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonTexte sérialise une valeur pour comparer deux structures JSON de façon
// indépendante du type entier/nombre flottant.
func jsonTexte(t *testing.T, valeur any) string {
	t.Helper()
	donnees, err := json.Marshal(valeur)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(donnees)
}

func modeleIntegre(t *testing.T, id string) ModeleGroupe {
	t.Helper()
	for _, modele := range ModelesIntegres() {
		if modele.ID == id {
			return modele
		}
	}
	t.Fatalf("modèle intégré introuvable : %s", id)
	return ModeleGroupe{}
}

func TestSpecGroupeValidate(t *testing.T) {
	if erreurs := (SpecGroupe{MotifNom: "{classe}", Portee: "global"}).Validate(); len(erreurs) != 0 {
		t.Fatalf("spécification valide rejetée : %v", erreurs)
	}
	if erreurs := (SpecGroupe{}).Validate(); !contientFragment(erreurs, "vide") {
		t.Fatalf("nom vide non détecté : %v", erreurs)
	}
	if erreurs := (SpecGroupe{MotifNom: "g", Portee: "galactique"}).Validate(); !contientFragment(erreurs, "Portée") {
		t.Fatalf("portée inconnue non détectée : %v", erreurs)
	}
	if erreurs := (SpecGroupe{MotifNom: "g-{classe"}).Validate(); !contientFragment(erreurs, "accolades") {
		t.Fatalf("accolades non détectées : %v", erreurs)
	}
	if erreurs := (SpecGroupe{MotifNom: "{classe.1}"}).Validate(); !contientFragment(erreurs, "champ") {
		t.Fatalf("champ invalide non détecté : %v", erreurs)
	}
	// Un champ vide `{}` est refusé comme par `string.Formatter`.
	if erreurs := (SpecGroupe{MotifNom: "{}"}).Validate(); !contientFragment(erreurs, "champ") {
		t.Fatalf("champ vide non détecté : %v", erreurs)
	}
}

func TestSpecGroupeRoundTrip(t *testing.T) {
	spec := SpecGroupe{MotifNom: "{classe}-Profs", Portee: "universal", Description: "desc"}
	if jsonTexte(t, SpecGroupeDepuisDict(spec.ToDict())) != jsonTexte(t, spec) {
		t.Fatal("aller-retour SpecGroupe non équivalent")
	}
}

func TestCatalogues(t *testing.T) {
	if len(PorteesGroupe) != 3 {
		t.Fatalf("portées attendues : 3, obtenu %d", len(PorteesGroupe))
	}
	for portee := range PorteesGroupe {
		if _, ok := ValeursTypeGroupe[portee]; !ok {
			t.Fatalf("valeur groupType manquante pour %s", portee)
		}
	}
}

func TestModelesIntegres(t *testing.T) {
	modeles := ModelesIntegres()
	if len(modeles) != 4 {
		t.Fatalf("4 modèles attendus, obtenu %d", len(modeles))
	}
	for _, modele := range modeles {
		if erreurs := modele.Validate(); len(erreurs) != 0 {
			t.Fatalf("modèle %s invalide : %v", modele.ID, erreurs)
		}
	}
	kinds := map[string]bool{}
	for _, modele := range modeles {
		kinds[modele.Type] = true
	}
	for _, attendu := range []string{"classe_eleve", "classe_prof", "personnel_admin", "service_technique"} {
		if !kinds[attendu] {
			t.Fatalf("type %s absent des modèles intégrés", attendu)
		}
	}

	classe := modeleIntegre(t, "classe_eleve")
	if classe.MotifOU != "Eleves-{classe}" {
		t.Fatalf("motif d'OU inattendu : %s", classe.MotifOU)
	}
	if classe.Quota == nil || classe.Quota.TailleGo != 5.0 {
		t.Fatal("quota 5 Go attendu pour classe_eleve")
	}
	if classe.Dossier == nil || classe.Espace == nil || classe.Profil == nil {
		t.Fatal("dossier, espace et profil attendus pour classe_eleve")
	}
	if classe.PrereglageHeure != "Heures cours" {
		t.Fatalf("préréglage inattendu : %s", classe.PrereglageHeure)
	}
	if classe.Politique == nil {
		t.Fatal("politique de mot de passe attendue")
	}

	if got := classe.Placeholders(); len(got) != 1 || got[0] != "classe" {
		t.Fatalf("placeholders classe_eleve : %v", got)
	}
	if got := modeleIntegre(t, "classe_prof").Placeholders(); len(got) != 1 || got[0] != "classe" {
		t.Fatalf("placeholders classe_prof : %v", got)
	}
	if got := modeleIntegre(t, "personnel_admin").Placeholders(); len(got) != 0 {
		t.Fatalf("placeholders personnel_admin : %v", got)
	}
	if got := modeleIntegre(t, "service_technique").Placeholders(); len(got) != 0 {
		t.Fatalf("placeholders service_technique : %v", got)
	}
	if modeleIntegre(t, "service_technique").PrereglageHeure != "" {
		t.Fatal("service_technique ne doit pas avoir de filtrage horaire")
	}

	admin := modeleIntegre(t, "personnel_admin")
	portees := map[string]string{}
	for _, groupe := range admin.Groupes {
		portees[groupe.MotifNom] = groupe.Portee
	}
	if portees["Administrateurs"] != "universal" || portees["Personnel-Admin"] != "global" {
		t.Fatalf("portées personnel_admin inattendues : %v", portees)
	}
}

func TestValidationModele(t *testing.T) {
	base := modeleIntegre(t, "classe_eleve")

	mauvaisID := base.Clone()
	mauvaisID.ID = "Mauvais ID"
	if !contientFragment(mauvaisID.Validate(), "Identifiant") {
		t.Fatal("identifiant invalide non détecté")
	}

	nomVide := base.Clone()
	nomVide.Nom = " "
	if !contientFragment(nomVide.Validate(), "vide") {
		t.Fatal("nom vide non détecté")
	}

	typeInconnu := base.Clone()
	typeInconnu.Type = "alien"
	if !contientFragment(typeInconnu.Validate(), "inconnu") {
		t.Fatal("type inconnu non détecté")
	}

	sansGroupe := base.Clone()
	sansGroupe.Groupes = []SpecGroupe{}
	if !contientFragment(sansGroupe.Validate(), "Aucun groupe") {
		t.Fatal("absence de groupe non détectée")
	}

	duplique := base.Clone()
	duplique.Groupes = append(duplique.Groupes, SpecGroupe{MotifNom: "{classe}", Portee: "global"})
	if !contientFragment(duplique.Validate(), "dupliqué") {
		t.Fatal("motif dupliqué non détecté")
	}

	heuresInconnues := base.Clone()
	heuresInconnues.PrereglageHeure = "Heures folles"
	if !contientFragment(heuresInconnues.Validate(), "Préréglage") {
		t.Fatal("préréglage inconnu non détecté")
	}

	template := base.Clone()
	politique := *template.Politique
	politique.Longueur = 3
	template.Politique = &politique
	if !contientFragment(template.Validate(), "6 à 128") {
		t.Fatal("bornes de longueur du mot de passe non détectées")
	}

	licenceVide := base.Clone()
	licenceVide.Licences = []string{"sku-ok", "  "}
	if !contientFragment(licenceVide.Validate(), "Licence") {
		t.Fatal("licence vide non détectée")
	}

	scriptInvalide := base.Clone()
	script := *scriptInvalide.ScriptLogon
	script.Contenu = ""
	scriptInvalide.ScriptLogon = &script
	if !contientFragment(scriptInvalide.Validate(), "contenu du script") {
		t.Fatal("script vide non détecté")
	}

	quotaNul := base.Clone()
	quota := *quotaNul.Quota
	quota.TailleGo = 0
	quotaNul.Quota = &quota
	if !contientFragment(quotaNul.Validate(), "strictement positive") {
		t.Fatal("quota nul non détecté")
	}

	if base.OUParenteDN != "" {
		t.Fatal("l'OU parente d'un modèle intégré doit rester vide")
	}
}

func TestMotifs(t *testing.T) {
	if rendu, err := RendreMotif("Eleves-{classe}", map[string]string{"classe": "3emeA"}); err != nil || rendu != "Eleves-3emeA" {
		t.Fatalf("rendu inattendu : %q (%v)", rendu, err)
	}
	if rendu, err := RendreMotif("{classe}-Profs", map[string]string{"classe": "6B"}); err != nil || rendu != "6B-Profs" {
		t.Fatalf("rendu inattendu : %q (%v)", rendu, err)
	}
	if _, err := RendreMotif("{inconnu}", map[string]string{}); err == nil || !strings.Contains(err.Error(), "inconnu") {
		t.Fatalf("valeur manquante non signalée : %v", err)
	}
	if rendu, err := RendreMotif("{{litteral}}", map[string]string{}); err != nil || rendu != "{litteral}" {
		t.Fatalf("accollades littérales : %q (%v)", rendu, err)
	}

	classe := modeleIntegre(t, "classe_eleve")
	if erreurs := ValiderValeurs(classe, map[string]string{"classe": "3emeA"}); len(erreurs) != 0 {
		t.Fatalf("valeurs valides rejetées : %v", erreurs)
	}
	if !contientFragment(ValiderValeurs(classe, map[string]string{}), "classe") {
		t.Fatal("valeur absente non détectée")
	}
	if !contientFragment(ValiderValeurs(classe, map[string]string{"classe": " "}), "classe") {
		t.Fatal("valeur vide non détectée")
	}

	if SamGroupe("6B-Profs") != "6B-Profs" {
		t.Fatal("sAMAccountName inattendu pour 6B-Profs")
	}
	if SamGroupe(`a/b:c*d?"e`) != "a-b-c-d--e" {
		t.Fatalf("sAMAccountName inattendu : %s", SamGroupe(`a/b:c*d?"e`))
	}
	if SamGroupe("...") != "groupe" {
		t.Fatalf("sAMAccountName inattendu : %s", SamGroupe("..."))
	}
}

func TestPersistance(t *testing.T) {
	dossier := t.TempDir()

	// Fichier absent → modèles intégrés.
	absents := ChargerModeles(filepath.Join(dossier, "absent.json"))
	if len(absents) != 4 {
		t.Fatalf("4 modèles intégrés attendus, obtenu %d", len(absents))
	}

	// Aller-retour.
	if _, err := SauvegarderModeles(dossier, ModelesIntegres()); err != nil {
		t.Fatalf("sauvegarde : %v", err)
	}
	recharges := ChargerModeles(CheminParDefaut(dossier))
	if len(recharges) != 4 {
		t.Fatalf("4 modèles attendus après rechargement, obtenu %d", len(recharges))
	}
	for index, modele := range recharges {
		if jsonTexte(t, modele.ToDict()) != jsonTexte(t, ModelesIntegres()[index].ToDict()) {
			t.Fatalf("modèle %s altéré par l'aller-retour", modele.ID)
		}
	}

	// Fichier corrompu → modèles intégrés.
	corrompu := filepath.Join(dossier, "corrompu.json")
	if err := os.WriteFile(corrompu, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(ChargerModeles(corrompu)) != 4 {
		t.Fatal("un fichier corrompu doit retourner les modèles intégrés")
	}

	// Objet sans clé « modeles » (ancien format) → modèles intégrés.
	sansCle := filepath.Join(dossier, "sanscle.json")
	if err := os.WriteFile(sansCle, []byte(`{"autre": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(ChargerModeles(sansCle)) != 4 {
		t.Fatal("un objet sans clé « modeles » doit retourner les modèles intégrés")
	}
}

func TestDuplication(t *testing.T) {
	modeles := ModelesIntegres()
	if got := IDModeleUnique(modeles, "classe_eleve"); got != "classe_eleve-2" {
		t.Fatalf("identifiant unique inattendu : %s", got)
	}
	if got := IDModeleUnique(modeles, "Nouveau Modèle!"); got != "nouveau-modele" {
		t.Fatalf("identifiant normalisé inattendu : %s", got)
	}
	if got := IDModeleUnique(modeles, "!!!"); got != "modele" {
		t.Fatalf("identifiant de repli inattendu : %s", got)
	}

	clone := DupliquerModele(modeles, modeles[0], "")
	if clone.ID != "classe_eleve-2" {
		t.Fatalf("identifiant de copie inattendu : %s", clone.ID)
	}
	if clone.Nom != "Classe élève (copie)" {
		t.Fatalf("nom de copie inattendu : %s", clone.Nom)
	}
	if jsonTexte(t, clone.ToDict()) == jsonTexte(t, modeles[0].ToDict()) {
		t.Fatal("la copie doit différer de la source")
	}
	if len(modeles) != 4 {
		t.Fatal("la liste d'origine ne doit pas être modifiée")
	}

	nomme := DupliquerModele(ModelesIntegres(), modeleIntegre(t, "classe_eleve"), "Classe 6e")
	if nomme.Nom != "Classe 6e" {
		t.Fatalf("nom de copie inattendu : %s", nomme.Nom)
	}
}

func TestJSONXML(t *testing.T) {
	modele := modeleIntegre(t, "classe_eleve")
	dossier := t.TempDir()

	// Aller-retour dictionnaire.
	if jsonTexte(t, ModeleDepuisDict(modele.ToDict()).ToDict()) != jsonTexte(t, modele.ToDict()) {
		t.Fatal("aller-retour dictionnaire non équivalent")
	}

	// Clone profond.
	clone := modele.Clone()
	clone.Groupes[0].MotifNom = "autre"
	if modele.Groupes[0].MotifNom != "{classe}" {
		t.Fatal("le clone doit être indépendant de la source")
	}

	// JSON.
	cheminJSON, err := ExporterModele(modele, filepath.Join(dossier, "m.json"))
	if err != nil {
		t.Fatalf("export JSON : %v", err)
	}
	importe, err := ImporterModele(cheminJSON)
	if err != nil {
		t.Fatalf("import JSON : %v", err)
	}
	if jsonTexte(t, importe.ToDict()) != jsonTexte(t, modele.ToDict()) {
		t.Fatal("aller-retour JSON non équivalent")
	}

	// XML.
	cheminXML, err := ExporterModeleXML(modele, filepath.Join(dossier, "m.xml"))
	if err != nil {
		t.Fatalf("export XML : %v", err)
	}
	depuisXML, err := ImporterModeleXML(cheminXML)
	if err != nil {
		t.Fatalf("import XML : %v", err)
	}
	if jsonTexte(t, depuisXML.ToDict()) != jsonTexte(t, modele.ToDict()) {
		t.Fatalf("aller-retour XML non équivalent:\n%s\n%s",
			jsonTexte(t, depuisXML.ToDict()), jsonTexte(t, modele.ToDict()))
	}

	// CRLF et nuls préservés par le XML.
	donnees := depuisXML.ToDict()
	script, _ := carteAny(donnees["logon_script"])
	if contenu, _ := script["content"].(string); !strings.Contains(contenu, "\r\n") {
		t.Fatal("le CRLF du script doit être préservé par le XML")
	}
	politique, _ := carteAny(donnees["password_policy"])
	if politique["pattern_fixe"] != nil {
		t.Fatal("pattern_fixe doit rester nul après l'aller-retour XML")
	}
	quota, _ := carteAny(donnees["quota"])
	if quota["hard_limit"] != false {
		t.Fatal("hard_limit doit rester faux après l'aller-retour XML")
	}
	if licences, _ := donnees["licenses"].([]any); len(licences) != 0 {
		t.Fatalf("les licences doivent rester une liste vide : %v", licences)
	}
}

func TestImportRejets(t *testing.T) {
	dossier := t.TempDir()

	corrompu := filepath.Join(dossier, "bad.json")
	if err := os.WriteFile(corrompu, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModele(corrompu); err == nil || !strings.Contains(err.Error(), "impossible") {
		t.Fatalf("JSON corrompu attendu en erreur : %v", err)
	}

	tableau := filepath.Join(dossier, "arr.json")
	if err := os.WriteFile(tableau, []byte("[1, 2]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModele(tableau); err == nil {
		t.Fatal("un tableau JSON doit être rejeté")
	}

	invalide := filepath.Join(dossier, "inval.json")
	contenu := `{"id": "ok", "name": "", "groups": [{"name_pattern": "g", "scope": "global"}]}`
	if err := os.WriteFile(invalide, []byte(contenu), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModele(invalide); err == nil || !strings.Contains(err.Error(), "Modèle invalide") {
		t.Fatalf("modèle invalide attendu en erreur : %v", err)
	}

	mauvaiseRacine := filepath.Join(dossier, "bad.xml")
	if err := os.WriteFile(mauvaiseRacine, []byte("<autre/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModeleXML(mauvaiseRacine); err == nil || !strings.Contains(err.Error(), "Racine") {
		t.Fatalf("racine XML inattendue attendue en erreur : %v", err)
	}

	casse := filepath.Join(dossier, "broken.xml")
	if err := os.WriteFile(casse, []byte("<modele"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImporterModeleXML(casse); err == nil {
		t.Fatal("un XML tronqué doit être rejeté")
	}
}

func TestEchapperRDN(t *testing.T) {
	cas := map[string]string{
		"3emeA":             "3emeA",
		`a,b+c"d\e;f<g>h=i`: `a\,b\+c\"d\\e\;f\<g\>h\=i`,
		" lead and trail ":  `\ lead and trail\ `,
		"#hash":             `\#hash`,
		"  a  ":             `\  a \ `,
	}
	for entree, attendu := range cas {
		if got := EchapperRDN(entree); got != attendu {
			t.Fatalf("EchapperRDN(%q) = %q, attendu %q", entree, got, attendu)
		}
	}
}

func TestResumeGrille(t *testing.T) {
	var cours PrereglageHeures
	for _, prereglage := range prereglagesHeures() {
		if prereglage.Nom == "Heures cours" {
			cours = prereglage
		}
	}
	attendu := "Lundi–Vendredi 08:00-18:00 ; Samedi 08:00-12:00 ; Dimanche fermé"
	if got := resumeGrille(construireGrille(cours)); got != attendu {
		t.Fatalf("résumé des heures cours inattendu :\n%s\nattendu :\n%s", got, attendu)
	}
	if got := resumeGrille([7][24]bool{}); got != "Aucune heure autorisée" {
		t.Fatalf("résumé d'une grille vide inattendu : %s", got)
	}
	var pleine [7][24]bool
	for jour := range pleine {
		for heure := range pleine[jour] {
			pleine[jour][heure] = true
		}
	}
	if got := resumeGrille(pleine); got != "Toutes heures (sans restriction)" {
		t.Fatalf("résumé d'une grille pleine inattendu : %s", got)
	}
}

func TestScriptFSRM(t *testing.T) {
	quota := ParametresQuota{TailleGo: 5, SeuilAlerte: 80, LimiteDure: false}
	script := ScriptFSRM(quota, []string{`\\srv\classes\3emeA`})
	for _, fragment := range []string{
		"New-FsrmQuotaTemplate", "5368709120", "EduSync-5Go-80pct-souple",
		`$path = '\\srv\classes\3emeA'`, "-SoftLimit:$true",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("fragment %q absent du script FSRM :\n%s", fragment, script)
		}
	}
	if quota.NomModele() != "EduSync-5Go-80pct-souple" {
		t.Fatalf("nom de modèle FSRM inattendu : %s", quota.NomModele())
	}
	if quota.TailleOctets() != 5368709120 {
		t.Fatalf("taille en octets inattendue : %d", quota.TailleOctets())
	}
}

func contientFragment(liste []string, fragment string) bool {
	for _, item := range liste {
		if strings.Contains(item, fragment) {
			return true
		}
	}
	return false
}
