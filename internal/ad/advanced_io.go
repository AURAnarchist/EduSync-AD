// Package ad : import/export avancés (M22).
//
//   - Import LDAP : depuis une OU, un groupe AD ou un filtre personnalisé.
//   - Export LDIF (RFC 2849) : attributs au choix, base64 automatique, pliage 76.
//   - Export vCard 3.0 : cartes de visite, photo JPEG optionnelle, CRLF, pliage 75.
//   - Publipostage HTML : un fichier par objet ({{champ}}) + index.html.
//   - Import GEP / base Éducation nationale : association automatique des
//     colonnes puis conversion au format d'import EduSync.
//
// La logique reprend fidèlement src/edusync_ad/core/advanced_io.py pour rester
// compatible avec les fichiers produits par la version Python.
package ad

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/go-ldap/ldap/v3"

	"github.com/estemobs/EduSync-AD/internal/config"
)

// ColonnesImportEduSync sont les colonnes attendues du CSV d'import (identiques
// à csv_io.EXPECTED_COLUMNS côté Python).
var ColonnesImportEduSync = []string{"prenom", "nom", "classe", "ou", "email", "date_naissance", "numero"}

// AttributsExportDefaut sont les attributs exportés par défaut.
var AttributsExportDefaut = []string{
	"cn", "givenName", "sn", "displayName",
	"sAMAccountName", "userPrincipalName", "mail",
	"telephoneNumber", "mobile", "department", "title",
	"physicalDeliveryOfficeName", "streetAddress", "l", "postalCode", "co",
	"description", "memberOf",
}

// Modes d'une source LDAP d'import.
const (
	SourceLDAPOU     = "ou"
	SourceLDAPGroupe = "groupe"
	SourceLDAPFiltre = "filtre"
)

// ModesSourceLDAP est le libellé affichable de chaque mode.
var ModesSourceLDAP = map[string]string{
	SourceLDAPOU:     "Depuis une OU",
	SourceLDAPGroupe: "Depuis un groupe AD",
	SourceLDAPFiltre: "Filtre LDAP personnalisé",
}

// FiltreUtilisateurs sélectionne les comptes utilisateurs.
const FiltreUtilisateurs = "(&(objectClass=user)(objectCategory=person))"

// LimiteTailleMax est la taille maximale d'un import (protection mémoire).
const LimiteTailleMax = 50000

// FichierConfigAdvIO est le nom du fichier de configuration du module.
const FichierConfigAdvIO = "adv_io.json"

// EntreeLDAP désigne un objet LDAP générique : attribut → valeur (string,
// []string pour multi-valué, []byte pour binaire). Les clés sont en minuscules.
type EntreeLDAP map[string]any

// valeurTexte retourne la première valeur textuelle d'un attribut.
func valeurTexte(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case []string:
		if len(t) == 0 {
			return ""
		}
		return t[0]
	default:
		return fmt.Sprint(t)
	}
}

// valeurOctets retourne la valeur brute (octets) d'un attribut.
func valeurOctets(v any) []byte {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return t
	case string:
		return []byte(t)
	case []string:
		if len(t) == 0 {
			return nil
		}
		return []byte(t[0])
	default:
		return []byte(fmt.Sprint(t))
	}
}

// premiereValeur retourne la première valeur non binaire et non vide parmi les
// attributs demandés.
func premiereValeur(e EntreeLDAP, attributs ...string) string {
	for _, attr := range attributs {
		var v any = e[attr]
		if liste, ok := v.([]string); ok {
			if len(liste) == 0 {
				v = ""
			} else {
				v = liste[0]
			}
		}
		if _, binaire := v.([]byte); binaire || v == nil {
			continue
		}
		if s := strings.TrimSpace(valeurTexte(v)); s != "" {
			return s
		}
	}
	return ""
}

// ─── Import LDAP ──────────────────────────────────────────────────────────

// ValiderFiltreLDAP contrôle la syntaxe d'un filtre avant envoi au serveur
// (parenthèses équilibrées, entre parenthèses, pas d'octet nul, longueur).
func ValiderFiltreLDAP(filtre string) []string {
	texte := strings.TrimSpace(filtre)
	if texte == "" {
		return []string{"Filtre LDAP vide."}
	}
	var erreurs []string
	if strings.ContainsRune(texte, '\x00') {
		erreurs = append(erreurs, "Le filtre contient un octet nul.")
	}
	if !(strings.HasPrefix(texte, "(") && strings.HasSuffix(texte, ")")) {
		erreurs = append(erreurs, "Le filtre doit être entre parenthèses (ex : (&(objectClass=user)(objectCategory=person))).")
	}
	profondeur := 0
	equilibre := true
	for _, c := range texte {
		switch c {
		case '(':
			profondeur++
		case ')':
			profondeur--
			if profondeur < 0 {
				equilibre = false
			}
		}
		if !equilibre {
			break
		}
	}
	if !equilibre || profondeur != 0 {
		erreurs = append(erreurs, "Parenthèses déséquilibrées dans le filtre.")
	}
	if len(texte) > 2048 {
		erreurs = append(erreurs, "Filtre trop long (2048 caractères maximum).")
	}
	return erreurs
}

// SourceLDAP décrit la source d'un import : OU, groupe ou filtre personnalisé.
type SourceLDAP struct {
	Mode      string   `json:"mode"`
	BaseDN    string   `json:"base_dn"`
	OUDN      string   `json:"ou_dn"`
	GroupDN   string   `json:"group_dn"`
	Filtre    string   `json:"ldap_filter"`
	Attributs []string `json:"attributes"`
	Limite    int      `json:"size_limit"`
}

// NouvelleSourceLDAP retourne une source vide avec les valeurs par défaut.
func NouvelleSourceLDAP() SourceLDAP {
	return SourceLDAP{
		Mode:      SourceLDAPOU,
		Filtre:    FiltreUtilisateurs,
		Attributs: append([]string(nil), AttributsExportDefaut...),
		Limite:    5000,
	}
}

// Validate vérifie la cohérence de la source.
func (s *SourceLDAP) Validate() []string {
	var erreurs []string
	if _, ok := ModesSourceLDAP[s.Mode]; !ok {
		erreurs = append(erreurs, "Source LDAP inconnue (ou / groupe / filtre).")
	}
	if len(s.Attributs) == 0 {
		erreurs = append(erreurs, "Aucun attribut sélectionné.")
	}
	if s.Limite <= 0 || s.Limite > LimiteTailleMax {
		erreurs = append(erreurs, fmt.Sprintf("Limite invalide (1 à %d).", LimiteTailleMax))
	}
	switch s.Mode {
	case SourceLDAPOU:
		if strings.TrimSpace(s.OUDN) == "" {
			erreurs = append(erreurs, "OU vide.")
		}
	case SourceLDAPGroupe:
		if strings.TrimSpace(s.GroupDN) == "" {
			erreurs = append(erreurs, "Groupe vide.")
		}
		if strings.TrimSpace(s.BaseDN) == "" {
			erreurs = append(erreurs, "Base DN vide (domaine non résolu).")
		}
	case SourceLDAPFiltre:
		if strings.TrimSpace(s.BaseDN) == "" {
			erreurs = append(erreurs, "Base DN vide (domaine non résolu).")
		}
		erreurs = append(erreurs, ValiderFiltreLDAP(s.Filtre)...)
	}
	return erreurs
}

// RequeteLDAP retourne le couple (base, filtre) correspondant à la source.
func (s *SourceLDAP) RequeteLDAP() (string, string) {
	switch s.Mode {
	case SourceLDAPOU:
		return s.OUDN, FiltreUtilisateurs
	case SourceLDAPGroupe:
		return s.BaseDN, fmt.Sprintf("(&(objectClass=user)(objectCategory=person)(memberOf=%s))", ldap.EscapeFilter(s.GroupDN))
	default:
		return s.BaseDN, strings.TrimSpace(s.Filtre)
	}
}

// ImporterDepuisAD charge les objets LDAP décrits par la source.
func (p *Pool) ImporterDepuisAD(ctx context.Context, source SourceLDAP) ([]EntreeLDAP, error) {
	if erreurs := source.Validate(); len(erreurs) > 0 {
		return nil, fmt.Errorf("ad: source LDAP invalide: %s", strings.Join(erreurs, "; "))
	}
	base, filtre := source.RequeteLDAP()
	entrees, err := p.rechercherLDAP(ctx, base, filtre, source.Attributs, source.Limite)
	if err != nil {
		return nil, err
	}
	resultat := make([]EntreeLDAP, 0, len(entrees))
	for _, e := range entrees {
		resultat = append(resultat, entreeDepuisLDAPEntry(e, source.Attributs))
	}
	return resultat, nil
}

// rechercherLDAP effectue une recherche LDAP paginée, tronquée à « limite »
// entrées (0 = illimité) — protection mémoire côté client.
func (p *Pool) rechercherLDAP(ctx context.Context, base, filtre string, attributs []string, limite int) ([]*ldap.Entry, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	var entrees []*ldap.Entry
	ctrl := ldap.NewControlPaging(500)
	for {
		req := ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filtre, attributs, []ldap.Control{ctrl})
		res, err := conn.Search(req)
		if err != nil {
			return nil, fmt.Errorf("ad: recherche LDAP sous %s: %w", base, err)
		}
		entrees = append(entrees, res.Entries...)
		if limite > 0 && len(entrees) >= limite {
			entrees = entrees[:limite]
			break
		}
		for _, c := range res.Controls {
			if pc, ok := c.(*ldap.ControlPaging); ok {
				ctrl.SetCookie(pc.Cookie)
			}
		}
		if len(ctrl.Cookie) == 0 {
			break
		}
	}
	return entrees, nil
}

// attributsBinaires liste les attributs dont la valeur doit rester brute.
var attributsBinaires = map[string]bool{
	"jpegphoto": true, "thumbnailphoto": true, "objectguid": true,
	"objectsid": true, "ntsecuritydescriptor": true, "logonhours": true,
	"cacertificate": true, "usercertificate": true,
}

// attributLDAP retrouve un attribut par son nom (insensible à la casse).
func attributLDAP(e *ldap.Entry, nom string) *ldap.EntryAttribute {
	for _, a := range e.Attributes {
		if strings.EqualFold(a.Name, nom) {
			return a
		}
	}
	return nil
}

// entreeDepuisLDAPEntry convertit une entrée LDAP en EntreeLDAP.
func entreeDepuisLDAPEntry(e *ldap.Entry, attributs []string) EntreeLDAP {
	entree := EntreeLDAP{"dn": e.DN}
	for _, attr := range attributs {
		a := attributLDAP(e, attr)
		if a == nil {
			continue
		}
		if attributsBinaires[strings.ToLower(attr)] {
			if len(a.ByteValues) > 0 {
				entree[attr] = a.ByteValues[0]
			}
			continue
		}
		switch len(a.Values) {
		case 0:
		case 1:
			entree[attr] = a.Values[0]
		default:
			entree[attr] = append([]string(nil), a.Values...)
		}
	}
	return entree
}

// ─── Export CSV ───────────────────────────────────────────────────────────

// ExportEntreesCSV écrit un CSV générique des entrées (';' UTF-8 BOM).
func ExportEntreesCSV(entrees []EntreeLDAP, dest string) (string, error) {
	colonnes := colonnesUnion(entrees)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("ad: création du dossier d'export: %w", err)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("ad: création de %s: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", err
	}
	w := csv.NewWriter(f)
	w.Comma = ';'
	w.UseCRLF = true // comme le csv.writer Python (fin de ligne \r\n)
	if err := w.Write(colonnes); err != nil {
		return "", err
	}
	for _, e := range entrees {
		ligne := make([]string, len(colonnes))
		for i, c := range colonnes {
			switch v := e[c].(type) {
			case nil, []byte:
				ligne[i] = ""
			case []string:
				ligne[i] = strings.Join(v, " | ")
			default:
				ligne[i] = fmt.Sprint(v)
			}
		}
		if err := w.Write(ligne); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return dest, nil
}

// colonnesUnion construit la liste des colonnes de toutes les entrées.
// « dn » en tête, puis les autres par ordre alphabétique (déterminisme : les
// maps Go n'ont pas d'ordre d'insertion, contrairement aux dicts Python).
func colonnesUnion(entrees []EntreeLDAP) []string {
	vus := map[string]bool{}
	var autres []string
	for _, e := range entrees {
		for k := range e {
			if vus[k] {
				continue
			}
			vus[k] = true
			if k != "dn" {
				autres = append(autres, k)
			}
		}
	}
	sort.Strings(autres)
	if vus["dn"] {
		return append([]string{"dn"}, autres...)
	}
	return autres
}

// ─── Export LDIF (RFC 2849) ───────────────────────────────────────────────

// LDIFLigneMax est la longueur maximale d'une ligne LDIF avant pliage.
const LDIFLigneMax = 76

// LDIFNeedsBase64 indique si une valeur doit être encodée en base64 (RFC 2849).
func LDIFNeedsBase64(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	switch data[0] {
	case 0x20, 0x3A, 0x3C:
		return true
	}
	for _, b := range data {
		if b == 0x00 || b == 0x0A || b == 0x0D || b > 0x7E {
			return true
		}
	}
	return false
}

// plierLigne plie une ligne à LDIFLigneMax caractères (RFC 2849).
func plierLigne(ligne string) []string {
	if len(ligne) <= LDIFLigneMax {
		return []string{ligne}
	}
	plie := []string{ligne[:LDIFLigneMax]}
	reste := ligne[LDIFLigneMax:]
	for len(reste) > 0 {
		fin := LDIFLigneMax - 1
		if fin > len(reste) {
			fin = len(reste)
		}
		plie = append(plie, " "+reste[:fin])
		reste = reste[fin:]
	}
	return plie
}

// LigneLDIF construit une ligne LDIF (pliée) : « attr: valeur » ou « attr:: base64 ».
func LigneLDIF(attribut string, valeur any) string {
	data := valeurOctets(valeur)
	if len(data) == 0 {
		return attribut + ":"
	}
	var brute string
	if LDIFNeedsBase64(data) {
		brute = attribut + ":: " + base64.StdEncoding.EncodeToString(data)
	} else {
		brute = attribut + ": " + string(data)
	}
	return strings.Join(plierLigne(brute), "\n")
}

// EntreeLDIF construit le bloc LDIF d'une entrée.
func EntreeLDIF(e EntreeLDAP, attributs []string, changetype string) string {
	lignes := []string{LigneLDIF("dn", e["dn"])}
	if changetype != "" {
		lignes = append(lignes, "changetype: "+changetype)
	}
	keys := attributs
	if keys == nil {
		for k := range e {
			if k != "dn" {
				keys = append(keys, k)
			}
		}
		// Ordre déterministe : les clés d'une map Go ne le sont pas.
		sort.Strings(keys)
	}
	for _, attr := range keys {
		if attr == "dn" {
			continue
		}
		valeur := e[attr]
		switch t := valeur.(type) {
		case nil:
			continue
		case []string:
			if len(t) == 0 {
				continue
			}
			for _, item := range t {
				lignes = append(lignes, LigneLDIF(attr, item))
			}
		default:
			if valeurTexte(valeur) == "" {
				continue
			}
			lignes = append(lignes, LigneLDIF(attr, valeur))
		}
	}
	return strings.Join(lignes, "\n")
}

// ExportLDIF écrit un fichier LDIF (UTF-8), entrées séparées par une ligne vide.
func ExportLDIF(entrees []EntreeLDAP, dest string, attributs []string, changetype string) (string, error) {
	blocs := make([]string, 0, len(entrees))
	for _, e := range entrees {
		blocs = append(blocs, EntreeLDIF(e, attributs, changetype))
	}
	contenu := ""
	if len(blocs) > 0 {
		contenu = strings.Join(blocs, "\n\n") + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("ad: création du dossier LDIF: %w", err)
	}
	if err := os.WriteFile(dest, []byte(contenu), 0o644); err != nil {
		return "", fmt.Errorf("ad: écriture LDIF %s: %w", dest, err)
	}
	return dest, nil
}

// ─── Export vCard 3.0 ─────────────────────────────────────────────────────

// VCardLigneMax est la longueur maximale d'une ligne vCard avant pliage.
const VCardLigneMax = 75

// VCardEscape échappe une valeur vCard (barre inverse, sauts, « ; » et « , »).
func VCardEscape(v any) string {
	texte := valeurTexte(v)
	texte = strings.ReplaceAll(texte, "\\", "\\\\")
	texte = strings.ReplaceAll(texte, "\r\n", "\\n")
	texte = strings.ReplaceAll(texte, "\r", "\\n")
	texte = strings.ReplaceAll(texte, "\n", "\\n")
	texte = strings.ReplaceAll(texte, ";", "\\;")
	texte = strings.ReplaceAll(texte, ",", "\\,")
	return texte
}

// plierVCard plie une ligne à VCardLigneMax caractères.
func plierVCard(ligne string) []string {
	if len(ligne) <= VCardLigneMax {
		return []string{ligne}
	}
	plie := []string{ligne[:VCardLigneMax]}
	reste := ligne[VCardLigneMax:]
	for len(reste) > 0 {
		fin := VCardLigneMax - 1
		if fin > len(reste) {
			fin = len(reste)
		}
		plie = append(plie, " "+reste[:fin])
		reste = reste[fin:]
	}
	return plie
}

// VCardPour construit une carte vCard 3.0 (lignes CRLF, pliées à 75).
func VCardPour(e EntreeLDAP, inclurePhoto bool) string {
	famille := premiereValeur(e, "sn")
	prenom := premiereValeur(e, "givenName")
	fn := premiereValeur(e, "displayName")
	if fn == "" {
		fn = strings.TrimSpace(prenom + " " + famille)
	}
	if fn == "" {
		fn = premiereValeur(e, "cn")
	}

	lignes := []string{
		"BEGIN:VCARD",
		"VERSION:3.0",
		"FN:" + VCardEscape(fn),
		fmt.Sprintf("N:%s;%s;;;", VCardEscape(famille), VCardEscape(prenom)),
	}
	societe := premiereValeur(e, "company")
	if societe == "" {
		societe = premiereValeur(e, "department")
	}
	if societe != "" {
		lignes = append(lignes, "ORG:"+VCardEscape(societe))
	}
	if titre := premiereValeur(e, "title"); titre != "" {
		lignes = append(lignes, "TITLE:"+VCardEscape(titre))
	}
	if mail := premiereValeur(e, "mail"); mail != "" {
		lignes = append(lignes, "EMAIL;TYPE=INTERNET,WORK:"+VCardEscape(mail))
	}
	if tel := premiereValeur(e, "telephoneNumber"); tel != "" {
		lignes = append(lignes, "TEL;TYPE=WORK,VOICE:"+VCardEscape(tel))
	}
	if mobile := premiereValeur(e, "mobile"); mobile != "" {
		lignes = append(lignes, "TEL;TYPE=CELL:"+VCardEscape(mobile))
	}
	rue := premiereValeur(e, "streetAddress")
	ville := premiereValeur(e, "l")
	region := premiereValeur(e, "st")
	codePostal := premiereValeur(e, "postalCode")
	pays := premiereValeur(e, "co")
	if rue != "" || ville != "" || region != "" || codePostal != "" || pays != "" {
		lignes = append(lignes, fmt.Sprintf("ADR;TYPE=WORK:;;%s;%s;%s;%s;%s",
			VCardEscape(rue), VCardEscape(ville), VCardEscape(region), VCardEscape(codePostal), VCardEscape(pays)))
	}
	if note := premiereValeur(e, "description"); note != "" {
		lignes = append(lignes, "NOTE:"+VCardEscape(note))
	}
	if inclurePhoto {
		var photo []byte
		if b, ok := e["jpegPhoto"].([]byte); ok && len(b) > 0 {
			photo = b
		} else if b, ok := e["thumbnailPhoto"].([]byte); ok && len(b) > 0 {
			photo = b
		}
		if len(photo) > 0 {
			lignes = append(lignes, "PHOTO;ENCODING=b;TYPE=JPEG:"+base64.StdEncoding.EncodeToString(photo))
		}
	}
	lignes = append(lignes, "END:VCARD")

	var pliees []string
	for _, ligne := range lignes {
		pliees = append(pliees, plierVCard(ligne)...)
	}
	return strings.Join(pliees, "\r\n")
}

// ExportVCards écrit toutes les cartes dans un unique fichier .vcf (UTF-8).
func ExportVCards(entrees []EntreeLDAP, dest string, inclurePhoto bool) (string, error) {
	cartes := make([]string, 0, len(entrees))
	for _, e := range entrees {
		cartes = append(cartes, VCardPour(e, inclurePhoto))
	}
	contenu := ""
	if len(cartes) > 0 {
		contenu = strings.Join(cartes, "\r\n") + "\r\n"
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("ad: création du dossier vCard: %w", err)
	}
	if err := os.WriteFile(dest, []byte(contenu), 0o644); err != nil {
		return "", fmt.Errorf("ad: écriture vCard %s: %w", dest, err)
	}
	return dest, nil
}

// ─── Publipostage HTML ────────────────────────────────────────────────────

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// GabaritHTMLDefaut est le gabarit de convention fourni par défaut.
const GabaritHTMLDefaut = `<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <title>Convention — {{cn}}</title>
  <style>
    body { font-family: Georgia, serif; margin: 3cm; line-height: 1.6; }
    h1 { border-bottom: 2px solid #1B4F91; padding-bottom: .3em; }
    table { border-collapse: collapse; margin-top: 1.5em; }
    td { border: 1px solid #999; padding: .5em 1em; }
  </style>
</head>
<body>
  <h1>Convention de poste</h1>
  <p>Entre l'établissement et :</p>
  <table>
    <tr><td>Nom</td><td>{{sn}} {{givenName}}</td></tr>
    <tr><td>Identifiant</td><td>{{sAMAccountName}}</td></tr>
    <tr><td>Adresse mail</td><td>{{mail}}</td></tr>
    <tr><td>Service</td><td>{{department}}</td></tr>
    <tr><td>Fonction</td><td>{{title}}</td></tr>
  </table>
  <p>Fait à __________________, le ____________</p>
</body>
</html>
`

// TrouverPlaceholders retourne les champs {{…}} du gabarit, sans doublon.
func TrouverPlaceholders(gabarit string) []string {
	var trouves []string
	vus := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(gabarit, -1) {
		cle := m[1]
		if !vus[cle] {
			vus[cle] = true
			trouves = append(trouves, cle)
		}
	}
	return trouves
}

// RendererTemplate remplace {{champ}} par la valeur HTML-échappée. Un champ
// absent de la ligne est laissé tel quel (visible = erreur signalée).
func RendererTemplate(gabarit string, ligne EntreeLDAP) string {
	return placeholderRe.ReplaceAllStringFunc(gabarit, func(match string) string {
		sous := placeholderRe.FindStringSubmatch(match)
		cle := sous[1]
		valeur, ok := ligne[cle]
		if !ok {
			return match
		}
		switch t := valeur.(type) {
		case []byte:
			return ""
		case []string:
			return html.EscapeString(strings.Join(t, ", "))
		default:
			return html.EscapeString(fmt.Sprint(valeur))
		}
	})
}

var nomFichierRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// NomFichierSur assainit un nom de fichier.
func NomFichierSur(nom string) string {
	nettoye := nomFichierRe.ReplaceAllString(nom, "_")
	nettoye = strings.Trim(nettoye, "._")
	if nettoye == "" {
		return "document"
	}
	return nettoye
}

var motifRe = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// formaterMotif remplace les {champ} du motif par les valeurs de la ligne.
// Retourne ok=false si un champ référencé est absent.
func formaterMotif(motif string, ligne EntreeLDAP) (string, bool) {
	ok := true
	resultat := motifRe.ReplaceAllStringFunc(motif, func(match string) string {
		cle := motifRe.FindStringSubmatch(match)[1]
		valeur, present := ligne[cle]
		if !present {
			ok = false
			return ""
		}
		return valeurTexte(valeur)
	})
	return resultat, ok
}

// MergeHTML génère un fichier HTML par objet + un index.html de synthèse.
// Retourne la liste des chemins générés.
func MergeHTML(gabarit string, lignes []EntreeLDAP, dossier string, motifNom string, faireIndex bool) ([]string, error) {
	if strings.TrimSpace(gabarit) == "" {
		return nil, errors.New("ad: gabarit HTML vide")
	}
	if len(lignes) == 0 {
		return nil, errors.New("ad: aucun objet à publiposter")
	}
	if len(TrouverPlaceholders(gabarit)) == 0 {
		return nil, errors.New("ad: gabarit sans champ valide (syntaxe attendue : {{champ}})")
	}
	if motifNom == "" {
		motifNom = "{sAMAccountName}_document.html"
	}

	if err := os.MkdirAll(dossier, 0o755); err != nil {
		return nil, fmt.Errorf("ad: création du dossier de publipostage: %w", err)
	}

	type genere struct{ fichier, libelle string }
	var generes []genere
	var chemins []string

	for index, ligne := range lignes {
		nom, ok := formaterMotif(motifNom, ligne)
		if !ok {
			nom = fmt.Sprintf("document_%d.html", index+1)
		}
		nom = NomFichierSur(nom)
		if !strings.HasSuffix(strings.ToLower(nom), ".html") {
			nom += ".html"
		}
		chemin := filepath.Join(dossier, nom)
		libelle := premiereValeur(ligne, "cn", "displayName", "sAMAccountName")
		if libelle == "" {
			libelle = nom
		}
		if err := os.WriteFile(chemin, []byte(RendererTemplate(gabarit, ligne)), 0o644); err != nil {
			return nil, fmt.Errorf("ad: écriture %s: %w", chemin, err)
		}
		generes = append(generes, genere{fichier: nom, libelle: libelle})
		chemins = append(chemins, chemin)
	}

	if faireIndex {
		var items strings.Builder
		for _, g := range generes {
			fmt.Fprintf(&items, "<li><a href=\"%s\">%s</a></li>\n", html.EscapeString(g.fichier), html.EscapeString(g.libelle))
		}
		index := fmt.Sprintf("<!doctype html>\n<html lang=\"fr\">\n<head><meta charset=\"utf-8\"><title>Publipostage — %d document(s)</title></head>\n<body><h1>Publipostage (%d document(s))</h1>\n<ul>\n%s</ul>\n</body>\n</html>\n",
			len(generes), len(generes), items.String())
		if err := os.WriteFile(filepath.Join(dossier, "index.html"), []byte(index), 0o644); err != nil {
			return nil, fmt.Errorf("ad: écriture index.html: %w", err)
		}
	}
	return chemins, nil
}

// ─── Import GEP / base Éducation nationale ────────────────────────────────

// GEPFieldAliases associe chaque colonne attendue à ses en-têtes possibles.
var GEPFieldAliases = map[string][]string{
	"prenom":         {"prenom", "prénom", "first name", "firstname", "given name"},
	"nom":            {"nom", "lastname", "last name", "surname", "family name", "nom de famille"},
	"classe":         {"classe", "class", "division", "groupe", "niveau", "level"},
	"ou":             {"ou", "unite", "unité", "organizationalunit", "etablissement", "établissement"},
	"email":          {"email", "e-mail", "mail", "adresse mail", "courriel"},
	"date_naissance": {"date de naissance", "date_naissance", "ddn", "naissance", "birthdate", "birth date"},
	"numero":         {"numero", "numéro", "matricule", "numero etudiant", "num etudiant", "id eleve"},
}

// stripAccents minuscule la chaîne et retire les accents (NFKD simplifié).
func stripAccents(texte string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(texte) {
		switch r {
		case 'à', 'â', 'ä', 'á', 'ã', 'å':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'î', 'ï', 'í':
			b.WriteRune('i')
		case 'ô', 'ö', 'ó', 'õ':
			b.WriteRune('o')
		case 'û', 'ü', 'ú', 'ù':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		case 'ñ':
			b.WriteRune('n')
		default:
			// Comme Python (encode ascii/ignore) : les caractères hors ASCII
			// non repliés sont supprimés.
			if r > unicode.MaxASCII {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SuggererMappingGEP associe chaque colonne attendue à l'en-tête réel :
// correspondance exacte d'abord (accents/casse ignorés), puis sous-chaîne.
// L'ordre des en-têtes est préservé (déterminisme, parité Python).
func SuggererMappingGEP(headers []string) map[string]string {
	type enteteNormalise struct {
		normalise string
		original  string
	}
	var ordonnees []enteteNormalise
	vus := map[string]bool{}
	for _, h := range headers {
		cle := stripAccents(strings.TrimSpace(h))
		if vus[cle] {
			continue
		}
		vus[cle] = true
		ordonnees = append(ordonnees, enteteNormalise{normalise: cle, original: h})
	}
	normalise := make(map[string]string, len(ordonnees))
	for _, en := range ordonnees {
		normalise[en.normalise] = en.original
	}

	mapping := make(map[string]string, len(ColonnesImportEduSync))
	for _, col := range ColonnesImportEduSync {
		mapping[col] = ""
	}

	// 1. Correspondance exacte.
	for _, attendu := range ColonnesImportEduSync {
		for _, alias := range GEPFieldAliases[attendu] {
			if original, ok := normalise[stripAccents(alias)]; ok {
				mapping[attendu] = original
				break
			}
		}
	}

	// 2. Correspondance par sous-chaîne (en-têtes déjà attribués exclus).
	utilises := map[string]bool{}
	for _, v := range mapping {
		if v != "" {
			utilises[v] = true
		}
	}
	for _, attendu := range ColonnesImportEduSync {
		if mapping[attendu] != "" {
			continue
		}
		for _, alias := range GEPFieldAliases[attendu] {
			needle := stripAccents(alias)
			if len(needle) < 3 {
				continue
			}
			for _, en := range ordonnees {
				if utilises[en.original] {
					continue
				}
				if strings.Contains(en.normalise, needle) {
					mapping[attendu] = en.original
					utilises[en.original] = true
					break
				}
			}
			if mapping[attendu] != "" {
				break
			}
		}
	}
	return mapping
}

// ResultatGEP décrit le résultat d'un import GEP.
type ResultatGEP struct {
	Lignes         []EntreeLDAP
	LignesIgnorees []int
	Mapping        map[string]string
	Separateur     string
}

// DetecterSeparateur devine le séparateur CSV d'un échantillon.
func DetecterSeparateur(echantillon string) string {
	candidats := []string{";", ",", "\t"}
	meilleur := ";"
	max := -1
	for _, sep := range candidats {
		if n := strings.Count(echantillon, sep); n > max {
			max = n
			meilleur = sep
		}
	}
	return meilleur
}

// ParseGEPCSV parse le CSV libre produit par l'outil tiers GEP.
func ParseGEPCSV(texte string, mapping map[string]string) ResultatGEP {
	separateur := DetecterSeparateur(tronque(texte, 4096))
	reader := csv.NewReader(strings.NewReader(texte))
	reader.Comma = rune(separateur[0])
	reader.FieldsPerRecord = -1

	enregistrements, err := reader.ReadAll()
	if err != nil || len(enregistrements) == 0 {
		return ResultatGEP{Mapping: map[string]string{}, Separateur: separateur}
	}
	headers := enregistrements[0]
	if mapping == nil {
		mapping = SuggererMappingGEP(headers)
	}
	indexEntete := make(map[string]int, len(headers))
	for i, h := range headers {
		indexEntete[h] = i
	}

	var lignes []EntreeLDAP
	var ignorees []int
	for numero, enr := range enregistrements[1:] {
		lire := func(champ string) string {
			entete, ok := mapping[champ]
			if !ok || entete == "" {
				return ""
			}
			i, present := indexEntete[entete]
			if !present || i >= len(enr) {
				return ""
			}
			return strings.TrimSpace(enr[i])
		}
		if lire("prenom") == "" || lire("nom") == "" {
			ignorees = append(ignorees, numero+1)
			continue
		}
		ligne := EntreeLDAP{}
		for _, col := range ColonnesImportEduSync {
			ligne[col] = lire(col)
		}
		lignes = append(lignes, ligne)
	}
	return ResultatGEP{Lignes: lignes, LignesIgnorees: ignorees, Mapping: mapping, Separateur: separateur}
}

// tronque limite une chaîne à n octets (sans casser l'encodage au mieux).
func tronque(texte string, n int) string {
	if len(texte) <= n {
		return texte
	}
	return texte[:n]
}

// GEPToEduSyncCSV convertit les lignes GEP au format d'import EduSync (';' BOM).
func GEPToEduSyncCSV(lignes []EntreeLDAP, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("ad: création du dossier d'import: %w", err)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("ad: création de %s: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", err
	}
	w := csv.NewWriter(f)
	w.Comma = ';'
	w.UseCRLF = true // comme le csv.writer Python (fin de ligne \r\n)
	if err := w.Write(ColonnesImportEduSync); err != nil {
		return "", err
	}
	for _, ligne := range lignes {
		enr := make([]string, len(ColonnesImportEduSync))
		for i, col := range ColonnesImportEduSync {
			enr[i] = valeurTexte(ligne[col])
		}
		if err := w.Write(enr); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return dest, nil
}

// ─── Configuration (outil tiers GEP) ──────────────────────────────────────

// ConfigAdvIO est la configuration du module (outil tiers GEP).
type ConfigAdvIO struct {
	CheminOutilGEP string `json:"gep_tool_path"`
}

// ChargerConfigAdvIO lit la configuration depuis un dossier (défaut : vide).
func ChargerConfigAdvIO(dir string) ConfigAdvIO {
	donnees, err := os.ReadFile(filepath.Join(dir, FichierConfigAdvIO))
	if err != nil {
		return ConfigAdvIO{}
	}
	var cfg ConfigAdvIO
	if err := json.Unmarshal(donnees, &cfg); err != nil {
		return ConfigAdvIO{}
	}
	return cfg
}

// EnregistrerConfigAdvIO écrit la configuration dans un dossier.
func EnregistrerConfigAdvIO(dir string, cfg ConfigAdvIO) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ad: création dossier config adv_io: %w", err)
	}
	donnees, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("ad: encodage adv_io: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, FichierConfigAdvIO), donnees, 0o600); err != nil {
		return fmt.Errorf("ad: écriture adv_io.json: %w", err)
	}
	return nil
}

// DossierConfigAdvIO retourne le dossier de configuration partagé avec Python.
func DossierConfigAdvIO() (string, error) {
	return config.ConfigDir()
}

// LancerOutilExterne lance l'outil tiers GEP. Retourne nil si le lancement est
// parti, sinon une erreur explicite.
func LancerOutilExterne(cfg ConfigAdvIO) error {
	chemin := strings.TrimSpace(cfg.CheminOutilGEP)
	if chemin == "" {
		return errors.New("aucun outil tiers configuré")
	}
	info, err := os.Stat(chemin)
	if err != nil || info.IsDir() {
		return fmt.Errorf("outil introuvable : %s", chemin)
	}
	absolu, err := filepath.Abs(chemin)
	if err != nil {
		absolu = chemin
	}
	cmd := exec.Command(absolu)
	cmd.Dir = filepath.Dir(absolu)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("lancement impossible : %w", err)
	}
	return nil
}
