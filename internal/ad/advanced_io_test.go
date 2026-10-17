package ad

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Filtre et source LDAP ────────────────────────────────────────────────

func TestValiderFiltreLDAP(t *testing.T) {
	if errs := ValiderFiltreLDAP("(&(objectClass=user)(objectCategory=person))"); len(errs) != 0 {
		t.Errorf("filtre valide rejeté : %v", errs)
	}
	if errs := ValiderFiltreLDAP(""); len(errs) == 0 {
		t.Error("filtre vide accepté")
	}
	if errs := ValiderFiltreLDAP("objectClass=user"); len(errs) == 0 {
		t.Error("filtre sans parenthèses accepté")
	}
	if errs := ValiderFiltreLDAP("(&(a=1)"); len(errs) == 0 {
		t.Error("parenthèses déséquilibrées acceptées")
	}
	if errs := ValiderFiltreLDAP("(a=\x00b)"); len(errs) == 0 {
		t.Error("octet nul accepté")
	}
	long := "(" + strings.Repeat("a", 2048) + ")"
	if errs := ValiderFiltreLDAP(long); len(errs) == 0 {
		t.Error("filtre trop long accepté")
	}
}

func TestSourceLDAPValidate(t *testing.T) {
	src := NouvelleSourceLDAP()
	src.OUDN = "OU=Eleves,DC=l"
	if errs := src.Validate(); len(errs) != 0 {
		t.Errorf("source OU valide rejetée : %v", errs)
	}

	src.Mode = SourceLDAPOU
	src.OUDN = ""
	if errs := src.Validate(); len(errs) == 0 {
		t.Error("OU vide acceptée")
	}

	src.Mode = SourceLDAPFiltre
	src.BaseDN = "DC=l"
	src.Filtre = "(objectClass=user)"
	if errs := src.Validate(); len(errs) != 0 {
		t.Errorf("source filtre valide rejetée : %v", errs)
	}
	src.Filtre = "objectClass=user"
	if errs := src.Validate(); len(errs) == 0 {
		t.Error("filtre invalide accepté")
	}

	src.Mode = "bogus"
	if errs := src.Validate(); len(errs) == 0 {
		t.Error("mode inconnu accepté")
	}
}

func TestSourceLDAPRequete(t *testing.T) {
	src := NouvelleSourceLDAP()
	src.Mode = SourceLDAPOU
	src.OUDN = "OU=Eleves,DC=l"
	base, filtre := src.RequeteLDAP()
	if base != "OU=Eleves,DC=l" || filtre != FiltreUtilisateurs {
		t.Errorf("requête OU = %q / %q", base, filtre)
	}

	src.Mode = SourceLDAPGroupe
	src.BaseDN = "DC=l"
	src.GroupDN = "CN=6A,DC=l"
	base, filtre = src.RequeteLDAP()
	if base != "DC=l" || !strings.Contains(filtre, "(memberOf=CN=6A,DC=l)") {
		t.Errorf("requête groupe = %q / %q", base, filtre)
	}

	// Les caractères spéciaux du DN doivent être échappés dans le filtre.
	src.GroupDN = "CN=a(b),DC=l"
	_, filtre = src.RequeteLDAP()
	if strings.Contains(filtre, "(memberOf=CN=a(b)") {
		t.Errorf("DN non échappé : %q", filtre)
	}
}

// ─── Export CSV ───────────────────────────────────────────────────────────

func TestExportEntreesCSV(t *testing.T) {
	entrees := []EntreeLDAP{
		{"dn": "CN=A", "cn": "A", "memberOf": []string{"x", "y"}, "photo": []byte{0}},
		{"dn": "CN=B", "cn": "B", "mail": "b@x.fr"},
	}
	dest := filepath.Join(t.TempDir(), "e.csv")
	if _, err := ExportEntreesCSV(entrees, dest); err != nil {
		t.Fatalf("ExportEntreesCSV: %v", err)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if !strings.HasPrefix(string(donnees), "\xef\xbb\xbf") {
		t.Error("BOM UTF-8 absent")
	}
	contenu := strings.TrimPrefix(string(donnees), "\xef\xbb\xbf")
	lignes := strings.Split(strings.TrimRight(contenu, "\r\n"), "\r\n")
	if lignes[0] != "dn;cn;mail;memberOf;photo" {
		t.Errorf("en-tête = %q", lignes[0])
	}
	if !strings.Contains(lignes[1], "x | y") {
		t.Errorf("liste non jointe : %q", lignes[1])
	}
	if !strings.Contains(lignes[2], "b@x.fr") {
		t.Errorf("valeur mail absente : %q", lignes[2])
	}
}

// ─── LDIF ─────────────────────────────────────────────────────────────────

func TestLDIFNeedsBase64(t *testing.T) {
	cas := []struct {
		donnees []byte
		attendu bool
	}{
		{[]byte("simple"), false},
		{nil, false},
		{[]byte(" leading"), true},
		{[]byte(":colon"), true},
		{[]byte("<chevron"), true},
		{[]byte("Müller"), true},
		{[]byte("tab\there"), false},
		{[]byte("line\nbreak"), true},
		{[]byte("del\x7f"), true},
	}
	for _, c := range cas {
		if got := LDIFNeedsBase64(c.donnees); got != c.attendu {
			t.Errorf("LDIFNeedsBase64(%q) = %v, attendu %v", c.donnees, got, c.attendu)
		}
	}
}

func TestLigneLDIF(t *testing.T) {
	if got := LigneLDIF("mail", "j@x.fr"); got != "mail: j@x.fr" {
		t.Errorf("ligne simple = %q", got)
	}
	attendu := "cn:: " + base64.StdEncoding.EncodeToString([]byte("Müller"))
	if got := LigneLDIF("cn", "Müller"); got != attendu {
		t.Errorf("ligne base64 = %q", got)
	}
	if got := LigneLDIF("description", ""); got != "description:" {
		t.Errorf("ligne vide = %q", got)
	}
}

func TestLigneLDIFPliage(t *testing.T) {
	pliees := strings.Split(LigneLDIF("description", strings.Repeat("x", 200)), "\n")
	for _, l := range pliees {
		if len(l) > LDIFLigneMax {
			t.Errorf("ligne trop longue (%d) : %q", len(l), l)
		}
	}
	if !strings.HasPrefix(pliees[1], " ") {
		t.Error("suite non préfixée par une espace")
	}
	deplie := pliees[0]
	for _, l := range pliees[1:] {
		deplie += l[1:]
	}
	if deplie != "description: "+strings.Repeat("x", 200) {
		t.Errorf("dépliage incorrect : %q", deplie)
	}
}

func TestEntreeLDIF(t *testing.T) {
	entree := EntreeLDAP{
		"dn": "CN=Müller,DC=l", "cn": "Müller",
		"mail": []string{"a@x.fr", "b@x.fr"}, "description": "", "absent": nil,
	}
	bloc := EntreeLDIF(entree, []string{"cn", "mail", "description", "absent"}, "add")
	lignes := strings.Split(bloc, "\n")
	if !strings.HasPrefix(lignes[0], "dn:: ") {
		t.Errorf("DN accentué non encodé : %q", lignes[0])
	}
	if lignes[1] != "changetype: add" {
		t.Errorf("changetype = %q", lignes[1])
	}
	joint := strings.Join(lignes, "\n")
	if !strings.Contains(joint, "mail: a@x.fr") || !strings.Contains(joint, "mail: b@x.fr") {
		t.Errorf("multi-valué absent : %q", joint)
	}
	for _, l := range lignes {
		if strings.HasPrefix(l, "description") || strings.HasPrefix(l, "absent") {
			t.Errorf("ligne inattendue : %q", l)
		}
	}
}

func TestEntreeLDIFSansAttributs(t *testing.T) {
	entree := EntreeLDAP{"dn": "CN=A,DC=l", "cn": "A", "mail": "a@x.fr", "empty": ""}
	bloc := EntreeLDIF(entree, nil, "")
	if !strings.Contains(bloc, "cn: A") || !strings.Contains(bloc, "mail: a@x.fr") {
		t.Errorf("attributs manquants : %q", bloc)
	}
	if strings.Contains(bloc, "empty") {
		t.Errorf("attribut vide exporté : %q", bloc)
	}
}

func TestExportLDIF(t *testing.T) {
	entrees := []EntreeLDAP{{"dn": "CN=A,DC=l", "cn": "A"}, {"dn": "CN=B,DC=l", "cn": "B"}}
	dest := filepath.Join(t.TempDir(), "out.ldif")
	if _, err := ExportLDIF(entrees, dest, nil, "add"); err != nil {
		t.Fatalf("ExportLDIF: %v", err)
	}
	contenu, _ := os.ReadFile(dest)
	texte := string(contenu)
	if !strings.HasSuffix(texte, "\n") || !strings.Contains(texte, "\n\n") {
		t.Errorf("format LDIF incorrect : %q", texte)
	}
	if strings.Count(texte, "changetype: add") != 2 {
		t.Errorf("changetype manquant : %q", texte)
	}

	vide := filepath.Join(t.TempDir(), "empty.ldif")
	if _, err := ExportLDIF(nil, vide, nil, ""); err != nil {
		t.Fatalf("ExportLDIF vide: %v", err)
	}
	if donnees, _ := os.ReadFile(vide); len(donnees) != 0 {
		t.Errorf("fichier vide attendu, obtenu %q", donnees)
	}
}

// ─── vCard ────────────────────────────────────────────────────────────────

func TestVCardEscape(t *testing.T) {
	if got := VCardEscape("a,b;c\nd\\e"); got != "a\\,b\\;c\\nd\\\\e" {
		t.Errorf("VCardEscape = %q", got)
	}
}

func TestVCardPour(t *testing.T) {
	carte := VCardPour(EntreeLDAP{
		"sn": "Durand, Jr", "givenName": "Jean", "displayName": "Jean Durand",
		"department": "SVT", "title": "Professeur", "mail": "j@x.fr",
		"telephoneNumber": "0123", "mobile": "0612",
		"streetAddress": "1 rue des Écoles", "l": "Paris", "postalCode": "75000",
		"co": "France", "description": "L1\nL2",
	}, false)

	if !strings.HasPrefix(carte, "BEGIN:VCARD\r\nVERSION:3.0\r\n") || !strings.HasSuffix(carte, "END:VCARD") {
		t.Errorf("bornes vCard invalides : %q", carte)
	}
	for _, attendu := range []string{
		"FN:Jean Durand",
		"N:Durand\\, Jr;Jean;;;",
		"ORG:SVT", "TITLE:Professeur",
		"EMAIL;TYPE=INTERNET,WORK:j@x.fr",
		"TEL;TYPE=WORK,VOICE:0123",
		"TEL;TYPE=CELL:0612",
		"NOTE:L1\\nL2",
	} {
		if !strings.Contains(carte, attendu) {
			t.Errorf("vCard sans %q :\n%s", attendu, carte)
		}
	}
	if !strings.Contains(carte, "ADR;TYPE=WORK:;;1 rue des Écoles;Paris;;75000;France") {
		t.Errorf("ADR manquante :\n%s", carte)
	}
}

func TestVCardFallbackEtChampsVides(t *testing.T) {
	if carte := VCardPour(EntreeLDAP{"sn": "Martin", "givenName": "Alice"}, false); !strings.Contains(carte, "FN:Alice Martin") {
		t.Errorf("FN fallback = %q", carte)
	}
	if carte := VCardPour(EntreeLDAP{"cn": "Seul CN"}, false); !strings.Contains(carte, "FN:Seul CN") {
		t.Errorf("FN CN = %q", carte)
	}
	carte := VCardPour(EntreeLDAP{"cn": "X"}, false)
	if strings.Contains(carte, "ORG:") || strings.Contains(carte, "TEL;") || strings.Contains(carte, "ADR;") {
		t.Errorf("champs vides exportés :\n%s", carte)
	}
	if !strings.Contains(carte, "N:;;;") {
		t.Errorf("N vide = %q", carte)
	}
}

func TestVCardPhoto(t *testing.T) {
	entree := EntreeLDAP{"cn": "X", "jpegPhoto": []byte{0xff, 0xd8, 'B', 'I', 'N'}}
	avec := VCardPour(entree, true)
	attendu := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 'B', 'I', 'N'})
	if !strings.Contains(avec, "PHOTO;ENCODING=b;TYPE=JPEG:"+attendu) {
		t.Errorf("photo absente :\n%s", avec)
	}
	if strings.Contains(VCardPour(entree, false), "PHOTO") {
		t.Error("photo exportée sans option")
	}
	if strings.Contains(VCardPour(EntreeLDAP{"cn": "X"}, true), "PHOTO") {
		t.Error("PHOTO émise sans photo")
	}
	thumb := VCardPour(EntreeLDAP{"cn": "X", "thumbnailPhoto": []byte{0xff, 0xd8, 'T', 'H'}}, true)
	if !strings.Contains(thumb, base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 'T', 'H'})) {
		t.Error("repli thumbnailPhoto absent")
	}
}

func TestVCardPliage(t *testing.T) {
	carte := VCardPour(EntreeLDAP{"cn": "X", "description": strings.Repeat("y", 300)}, false)
	for _, l := range strings.Split(carte, "\r\n") {
		if len(l) > VCardLigneMax {
			t.Errorf("ligne vCard trop longue (%d)", len(l))
		}
	}
}

func TestExportVCards(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "c.vcf")
	if _, err := ExportVCards([]EntreeLDAP{{"cn": "A"}, {"cn": "B"}}, dest, false); err != nil {
		t.Fatalf("ExportVCards: %v", err)
	}
	donnees, _ := os.ReadFile(dest)
	if strings.Count(string(donnees), "BEGIN:VCARD") != 2 {
		t.Errorf("cartes = %d", strings.Count(string(donnees), "BEGIN:VCARD"))
	}
	if !strings.Contains(string(donnees), "\r\n") {
		t.Error("CRLF absent")
	}
}

// ─── Publipostage HTML ────────────────────────────────────────────────────

func TestTrouverPlaceholders(t *testing.T) {
	got := TrouverPlaceholders("{{b}} {{a}} {{b}}")
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("placeholders = %v", got)
	}
	if len(TrouverPlaceholders("aucun")) != 0 {
		t.Error("placeholders trouvés à tort")
	}
}

func TestRendererTemplate(t *testing.T) {
	if got := RendererTemplate("{{sn}} {{mail}}", EntreeLDAP{"sn": "<b>&", "mail": "a@b.fr"}); got != "&lt;b&gt;&amp; a@b.fr" {
		t.Errorf("rendu = %q", got)
	}
	if got := RendererTemplate("{{inconnu}}", EntreeLDAP{"cn": "x"}); got != "{{inconnu}}" {
		t.Errorf("placeholder inconnu = %q", got)
	}
	if got := RendererTemplate("{{memberOf}}|{{photo}}", EntreeLDAP{"memberOf": []string{"a", "b"}, "photo": []byte{0}}); got != "a, b|" {
		t.Errorf("listes/octets = %q", got)
	}
	for _, champ := range []string{"sn", "sAMAccountName", "mail"} {
		if !contient(TrouverPlaceholders(GabaritHTMLDefaut), champ) {
			t.Errorf("gabarit par défaut sans %q", champ)
		}
	}
}

func TestNomFichierSur(t *testing.T) {
	if got := NomFichierSur("a/b\\c d"); got != "a_b_c_d" {
		t.Errorf("NomFichierSur = %q", got)
	}
	if got := NomFichierSur("###"); got != "document" {
		t.Errorf("NomFichierSur(###) = %q", got)
	}
	if got := NomFichierSur(".hidden"); got != "hidden" {
		t.Errorf("NomFichierSur(.hidden) = %q", got)
	}
}

func TestMergeHTML(t *testing.T) {
	rows := []EntreeLDAP{
		{"sAMAccountName": "jdurand", "cn": "Jean Durand"},
		{"sAMAccountName": "a martin", "cn": "Alice Martin"},
	}
	dossier := t.TempDir()
	chemins, err := MergeHTML("{{cn}} — {{sAMAccountName}}", rows, dossier, "{sAMAccountName}_conv.html", true)
	if err != nil {
		t.Fatalf("MergeHTML: %v", err)
	}
	if len(chemins) != 2 {
		t.Fatalf("fichiers = %d", len(chemins))
	}
	noms := []string{filepath.Base(chemins[0]), filepath.Base(chemins[1])}
	if !contient(noms, "jdurand_conv.html") || !contient(noms, "a_martin_conv.html") {
		t.Errorf("noms = %v", noms)
	}
	contenu, _ := os.ReadFile(chemins[0])
	if !strings.Contains(string(contenu), "Jean Durand") {
		t.Errorf("contenu = %q", contenu)
	}
	index, err := os.ReadFile(filepath.Join(dossier, "index.html"))
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if !strings.Contains(string(index), "jdurand_conv.html") || !strings.Contains(string(index), "2 document") {
		t.Errorf("index = %q", index)
	}
}

func TestMergeHTMLRepliEtSuffixe(t *testing.T) {
	rows := []EntreeLDAP{{"sAMAccountName": "a/b\\c", "cn": "X"}, {"cn": "Y"}}
	dossier := t.TempDir()
	chemins, err := MergeHTML("{{cn}}", rows, dossier, "{sAMAccountName}.html", false)
	if err != nil {
		t.Fatalf("MergeHTML: %v", err)
	}
	if filepath.Base(chemins[0]) != "a_b_c.html" {
		t.Errorf("nom assaini = %q", filepath.Base(chemins[0]))
	}
	if _, err := os.Stat(filepath.Join(dossier, "index.html")); !os.IsNotExist(err) {
		t.Error("index.html généré sans option")
	}

	dossier2 := filepath.Join(t.TempDir(), "sub")
	chemins2, err := MergeHTML("{{cn}}", rows, dossier2, "{inconnu}.html", true)
	if err != nil {
		t.Fatalf("MergeHTML repli: %v", err)
	}
	if filepath.Base(chemins2[0]) != "document_1.html" || filepath.Base(chemins2[1]) != "document_2.html" {
		t.Errorf("repli = %q, %q", filepath.Base(chemins2[0]), filepath.Base(chemins2[1]))
	}

	chemins3, err := MergeHTML("{{cn}}", []EntreeLDAP{{"cn": "X", "sAMAccountName": "x"}}, t.TempDir(), "{sAMAccountName}", false)
	if err != nil {
		t.Fatalf("MergeHTML suffixe: %v", err)
	}
	if filepath.Base(chemins3[0]) != "x.html" {
		t.Errorf("suffixe .html = %q", filepath.Base(chemins3[0]))
	}
}

func TestMergeHTMLInvalide(t *testing.T) {
	rows := []EntreeLDAP{{"cn": "X"}}
	cas := []struct {
		gabarit string
		lignes  []EntreeLDAP
	}{
		{"", rows},
		{"{{cn}}", nil},
		{"sans champ", rows},
		{"{{ }}", rows},
	}
	for _, c := range cas {
		if _, err := MergeHTML(c.gabarit, c.lignes, t.TempDir(), "", true); err == nil {
			t.Errorf("gabarit %q : erreur attendue", c.gabarit)
		}
	}
}

// ─── GEP ──────────────────────────────────────────────────────────────────

func TestSuggererMappingGEPExact(t *testing.T) {
	mapping := SuggererMappingGEP([]string{"NOM DE FAMILLE", "PRENOM", "CLASSE", "MAIL", "DATE DE NAISSANCE", "MATRICULE"})
	attendu := map[string]string{
		"nom":            "NOM DE FAMILLE",
		"prenom":         "PRENOM",
		"email":          "MAIL",
		"date_naissance": "DATE DE NAISSANCE",
		"numero":         "MATRICULE",
	}
	for champ, valeur := range attendu {
		if mapping[champ] != valeur {
			t.Errorf("mapping[%s] = %q, attendu %q", champ, mapping[champ], valeur)
		}
	}
}

func TestSuggererMappingGEPSubstring(t *testing.T) {
	mapping := SuggererMappingGEP([]string{"Nom", "Prenom", "Division", "Etablissement"})
	if mapping["classe"] != "Division" {
		t.Errorf("classe = %q", mapping["classe"])
	}
	if mapping["ou"] != "Etablissement" {
		t.Errorf("ou = %q", mapping["ou"])
	}
	if got := SuggererMappingGEP([]string{"Prenom", "Nom", "Cours"})["ou"]; got != "" {
		t.Errorf("« ou » ne doit pas matcher « Cours » (obtenu %q)", got)
	}
	pm := SuggererMappingGEP([]string{"eleve_prenom", "eleve_nom"})
	if pm["prenom"] != "eleve_prenom" || pm["nom"] != "eleve_nom" {
		t.Errorf("cross-match prénom/nom : %v", pm)
	}
}

func TestParseGEPCSV(t *testing.T) {
	texte := "NOM;PRENOM;CLASSE\nDurand;Jean;6A\n;SansNom;6A\nDubois;;6A\n"
	resultat := ParseGEPCSV(texte, nil)
	if len(resultat.Lignes) != 1 || resultat.Lignes[0]["nom"] != "Durand" {
		t.Fatalf("lignes = %+v", resultat.Lignes)
	}
	if len(resultat.LignesIgnorees) != 2 || resultat.LignesIgnorees[0] != 2 || resultat.LignesIgnorees[1] != 3 {
		t.Errorf("lignes ignorées = %v", resultat.LignesIgnorees)
	}
	if resultat.Separateur != ";" {
		t.Errorf("séparateur = %q", resultat.Separateur)
	}
	if len(resultat.Lignes[0]) != len(ColonnesImportEduSync) {
		t.Errorf("colonnes normalisées = %v", resultat.Lignes[0])
	}
}

func TestGEPToEduSyncCSV(t *testing.T) {
	lignes := []EntreeLDAP{{"prenom": "Jean", "nom": "Durand", "classe": "6A"}}
	dest := filepath.Join(t.TempDir(), "import.csv")
	if _, err := GEPToEduSyncCSV(lignes, dest); err != nil {
		t.Fatalf("GEPToEduSyncCSV: %v", err)
	}
	donnees, _ := os.ReadFile(dest)
	contenu := strings.TrimPrefix(string(donnees), "\xef\xbb\xbf")
	lignesFichier := strings.Split(strings.ReplaceAll(contenu, "\r\n", "\n"), "\n")
	if lignesFichier[0] != strings.Join(ColonnesImportEduSync, ";") {
		t.Errorf("en-tête = %q", lignesFichier[0])
	}
	if !strings.HasPrefix(lignesFichier[1], "Jean;Durand;6A;") {
		t.Errorf("ligne = %q", lignesFichier[1])
	}
}

// ─── Configuration ────────────────────────────────────────────────────────

func TestConfigAdvIO(t *testing.T) {
	dir := t.TempDir()
	if cfg := ChargerConfigAdvIO(dir); cfg.CheminOutilGEP != "" {
		t.Errorf("config par défaut non vide : %+v", cfg)
	}
	if err := EnregistrerConfigAdvIO(dir, ConfigAdvIO{CheminOutilGEP: "/usr/bin/outil"}); err != nil {
		t.Fatalf("EnregistrerConfigAdvIO: %v", err)
	}
	if cfg := ChargerConfigAdvIO(dir); cfg.CheminOutilGEP != "/usr/bin/outil" {
		t.Errorf("relecture = %+v", cfg)
	}
}

func TestLancerOutilExterne(t *testing.T) {
	if err := LancerOutilExterne(ConfigAdvIO{}); err == nil {
		t.Error("chemin vide accepté")
	}
	if err := LancerOutilExterne(ConfigAdvIO{CheminOutilGEP: "/inexistant/outil"}); err == nil {
		t.Error("outil introuvable accepté")
	}
}

// contient indique si un slice de chaînes contient une valeur.
func contient(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if v == cible {
			return true
		}
	}
	return false
}
