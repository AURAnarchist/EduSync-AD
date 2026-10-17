package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// adGroupeTest est un groupe AD de référence pour les conversions.
var adGroupeTest = ad.Group{
	DN: "CN=Eleves,OU=Groupes,DC=edusync,DC=local", CN: "Eleves",
	SAMAccountName: "Eleves", Description: "Tous les élèves",
	Members: []string{"CN=Léa Martin,OU=Eleves,DC=edusync,DC=local"},
}

// urlValues analyse une chaîne de requête (raccourci de test).
func urlValues(brut string) (url.Values, error) {
	return url.ParseQuery(brut)
}

// TestFiltresLDAP vérifie l'échappement et la forme des filtres de recherche.
func TestFiltresLDAP(t *testing.T) {
	// Aucune recherche : tous les comptes du domaine.
	obtenu := filtreUtilisateurs("")
	if obtenu != "(&(objectClass=user)(objectCategory=person))" {
		t.Errorf("filtre vide = %q", obtenu)
	}
	// Recherche textuelle : six attributs, motif encadré d'étoiles.
	obtenu = filtreUtilisateurs("Martin")
	if !strings.Contains(obtenu, "(sAMAccountName=*Martin*)") ||
		!strings.Contains(obtenu, "(mail=*Martin*)") ||
		!strings.Contains(obtenu, "(displayName=*Martin*)") ||
		!strings.HasSuffix(obtenu, "))") {
		t.Errorf("filtre = %q", obtenu)
	}
	// Les caractères spéciaux LDAP sont neutralisés (injection refusée).
	obtenu = filtreUtilisateurs("a)(objectClass=*)")
	if strings.Contains(obtenu, ")(objectClass=*)") && !strings.Contains(obtenu, `\29`) {
		t.Errorf("filtre non échappé = %q", obtenu)
	}
	if !strings.Contains(obtenu, `\28`) || !strings.Contains(obtenu, `\29`) {
		t.Errorf("filtre non échappé = %q", obtenu)
	}

	if obtenu := filtreGroupes(""); obtenu != "(objectClass=group)" {
		t.Errorf("filtre groupes vide = %q", obtenu)
	}
	obtenu = filtreGroupes("Eleves")
	if !strings.Contains(obtenu, "(cn=*Eleves*)") ||
		!strings.Contains(obtenu, "(sAMAccountName=*Eleves*)") {
		t.Errorf("filtre groupes = %q", obtenu)
	}

	if obtenu := filtreOUs(""); obtenu != "(objectClass=organizationalUnit)" {
		t.Errorf("filtre OU vide = %q", obtenu)
	}
	if obtenu := filtreOUs("Eleves"); !strings.Contains(obtenu, "(ou=*Eleves*)") {
		t.Errorf("filtre OU = %q", obtenu)
	}
}

// TestDomaineDNS vérifie la conversion DN → nom DNS.
func TestDomaineDNS(t *testing.T) {
	cas := map[string]string{
		"DC=edusync,DC=local":      "edusync.local",
		"dc=EDUSYNC,dc=local":      "EDUSYNC.local",
		"OU=Eleves,DC=lycee,DC=fr": "lycee.fr",
		"CN=x":                     "",
		"":                         "",
		" DC=edusync , DC=local ":  "edusync.local",
	}
	for entree, attendu := range cas {
		if obtenu := domaineDNS(entree); obtenu != attendu {
			t.Errorf("domaineDNS(%q) = %q, attendu %q", entree, obtenu, attendu)
		}
	}
}

// TestTraduireErreur vérifie la conversion des erreurs d'annuaire.
func TestTraduireErreur(t *testing.T) {
	if obtenu := traduireErreur(nil); obtenu != nil {
		t.Errorf("nil = %v", obtenu)
	}
	// Une erreur d'API est transmise telle quelle.
	origine := ErrIntrouvable("Utilisateur", "x")
	if obtenu := traduireErreur(origine); obtenu != origine {
		t.Errorf("erreur d'API = %v", obtenu)
	}

	cas := []struct {
		message string
		statut  int
		code    string
	}{
		{"LDAP Result Code 32 \"No Such Object\": ", http.StatusNotFound, "not_found"},
		{"ad: utilisateur introuvable", http.StatusNotFound, "not_found"},
		{"dial tcp 127.0.0.1:389: connect: connection refused", http.StatusServiceUnavailable, "annuaire_indisponible"},
		{"annuaire indisponible", http.StatusServiceUnavailable, "annuaire_indisponible"},
		{"i/o timeout", http.StatusServiceUnavailable, "annuaire_indisponible"},
		{"LDAP Result Code 49 Invalid Credentials", http.StatusInternalServerError, "annuaire_error"},
	}
	for _, test := range cas {
		obtenu := traduireErreur(errors.New(test.message))
		var apiErr *ErreurAPI
		if !errors.As(obtenu, &apiErr) {
			t.Fatalf("%q : type = %T", test.message, obtenu)
		}
		if apiErr.Statut != test.statut || apiErr.Code != test.code {
			t.Errorf("%q : statut = %d, code = %q (attendu %d / %q)",
				test.message, apiErr.Statut, apiErr.Code, test.statut, test.code)
		}
	}
}

// TestAnnuaireADSansPool vérifie qu'un adaptateur sans pool refuse proprement.
func TestAnnuaireADSansPool(t *testing.T) {
	adaptateur := NewAnnuaireAD(nil)
	if obtenu := adaptateur.BaseDN(); obtenu != "" {
		t.Errorf("BaseDN() = %q", obtenu)
	}
	ctx := context.Background()
	appels := []func() error{
		func() error { _, err := adaptateur.ChercherUtilisateurs(ctx, "", 0); return err },
		func() error { _, err := adaptateur.ObtenirUtilisateur(ctx, "x"); return err },
		func() error { _, err := adaptateur.CreerUtilisateur(ctx, CreationUtilisateur{}); return err },
		func() error { _, err := adaptateur.ModifierUtilisateur(ctx, "x", nil); return err },
		func() error { return adaptateur.SupprimerUtilisateur(ctx, "x") },
		func() error { return adaptateur.ReinitialiserMotDePasse(ctx, "x", "y") },
		func() error { _, err := adaptateur.ChercherGroupes(ctx, "", 0); return err },
		func() error { _, err := adaptateur.ObtenirGroupe(ctx, "x"); return err },
		func() error { _, err := adaptateur.CreerGroupe(ctx, CreationGroupe{}); return err },
		func() error { _, err := adaptateur.ChercherOUs(ctx, "", 0); return err },
		func() error { _, err := adaptateur.ObtenirOU(ctx, "x"); return err },
		func() error { _, err := adaptateur.CreerOU(ctx, "x", "", ""); return err },
	}
	for index, appel := range appels {
		err := appel()
		var apiErr *ErreurAPI
		if !errors.As(err, &apiErr) || apiErr.Statut != http.StatusServiceUnavailable {
			t.Errorf("appel %d : erreur = %v", index, err)
		}
	}
}

// TestVersUtilisateurGroupeUnite vérifie les conversions et la tolérance à nil.
func TestVersUtilisateurGroupeUnite(t *testing.T) {
	if obtenu := versUtilisateur(nil); obtenu.SAM != "" {
		t.Errorf("versUtilisateur(nil) = %+v", obtenu)
	}
	if obtenu := versGroupe(nil, false); obtenu.CN != "" {
		t.Errorf("versGroupe(nil) = %+v", obtenu)
	}
	if obtenu := versUnite(nil); obtenu.DN != "" {
		t.Errorf("versUnite(nil) = %+v", obtenu)
	}
	// Un groupe sans membres (liste) ne doit pas produire de champ vide.
	groupe := versGroupe(&adGroupeTest, false)
	if groupe.Membres != nil {
		t.Errorf("membres = %v", groupe.Membres)
	}
	groupe = versGroupe(&adGroupeTest, true)
	if len(groupe.Membres) != 1 {
		t.Errorf("membres = %v", groupe.Membres)
	}
}

// TestPaginer vérifie la normalisation des paramètres de pagination.
func TestPaginer(t *testing.T) {
	elements := []int{1, 2, 3, 4, 5}

	tranche, total, page, taille := paginer(elements, 1, 2)
	if total != 5 || page != 1 || taille != 2 || fmt.Sprint(tranche) != "[1 2]" {
		t.Errorf("page 1 = %v %d %d %d", tranche, total, page, taille)
	}
	tranche, _, page, _ = paginer(elements, 3, 2)
	if page != 3 || fmt.Sprint(tranche) != "[5]" {
		t.Errorf("page 3 = %v (page %d)", tranche, page)
	}
	tranche, _, page, taille = paginer(elements, -1, 0)
	if page != 1 || taille != 50 || len(tranche) != 5 {
		t.Errorf("normalisation = %v %d %d", tranche, page, taille)
	}
	tranche, _, _, taille = paginer(elements, 1, 5000)
	if taille != 500 || len(tranche) != 5 {
		t.Errorf("taille bornée = %d (%v)", taille, tranche)
	}
	tranche, _, _, _ = paginer(elements, 10, 2)
	if len(tranche) != 0 {
		t.Errorf("page hors bornes = %v", tranche)
	}
}

// TestAccesAuxChampsRequete vérifie la lecture des paramètres et du corps.
func TestAccesAuxChampsRequete(t *testing.T) {
	requete := &Requete{Requete: nil}
	if obtenu := requete.ValeurRequete("absent", "defaut"); obtenu != "defaut" {
		t.Errorf("valeur = %q", obtenu)
	}
	if obtenu := requete.EntierRequete("absent", 7); obtenu != 7 {
		t.Errorf("entier = %d", obtenu)
	}

	valeurs, _ := urlValues("page=3&page_size=abc&q=&vide=")
	requete = &Requete{Requete: valeurs}
	if obtenu := requete.EntierRequete("page", 1); obtenu != 3 {
		t.Errorf("page = %d", obtenu)
	}
	if obtenu := requete.EntierRequete("page_size", 50); obtenu != 50 {
		t.Errorf("page_size = %d", obtenu)
	}
	if obtenu := requete.ValeurRequete("q", "defaut"); obtenu != "defaut" {
		t.Errorf("valeur vide = %q", obtenu)
	}
	if obtenu := requete.ValeurRequete("vide", "defaut"); obtenu != "defaut" {
		t.Errorf("valeur vide = %q", obtenu)
	}

	// Corps JSON.
	requete = &Requete{Corps: []byte(`{"a":1}`)}
	objet, err := requete.ObjetJSON()
	if err != nil || objet["a"].(float64) != 1 {
		t.Fatalf("objet = %v (%v)", objet, err)
	}
	// Le corps est mémorisé : une seconde lecture n'échoue pas.
	if _, err := requete.JSON(); err != nil {
		t.Errorf("seconde lecture: %v", err)
	}
	// Corps vide = objet vide.
	requete = &Requete{}
	if objet, err := requete.ObjetJSON(); err != nil || len(objet) != 0 {
		t.Errorf("corps vide = %v (%v)", objet, err)
	}
	// Corps non-objet.
	requete = &Requete{Corps: []byte(`[1,2]`)}
	if _, err := requete.ObjetJSON(); err == nil {
		t.Error("une liste doit être refusée")
	}
	// Corps illisible.
	requete = &Requete{Corps: []byte("pas du json")}
	if _, err := requete.ObjetJSON(); err == nil {
		t.Error("un corps invalide doit être refusé")
	}

	// Lecture de chaînes et de booléens.
	objet = map[string]any{"texte": "  valeur  ", "vide": "", "drapeau": true, "nombre": 1}
	if obtenu := Texte(objet, "texte"); obtenu != "valeur" {
		t.Errorf("Texte = %q", obtenu)
	}
	if obtenu := Texte(objet, "absent"); obtenu != "" {
		t.Errorf("Texte absent = %q", obtenu)
	}
	if obtenu := Texte(objet, "nombre"); obtenu != "" {
		t.Errorf("Texte non-chaîne = %q", obtenu)
	}
	if obtenu := TexteOu(objet, "vide", "defaut"); obtenu != "defaut" {
		t.Errorf("TexteOu = %q", obtenu)
	}
	if !Booleen(objet, "drapeau", false) || Booleen(objet, "absent", true) != true ||
		Booleen(objet, "texte", false) {
		t.Error("Booleen incorrect")
	}
}

// TestReponsesJSON vérifie la forme des réponses.
func TestReponsesJSON(t *testing.T) {
	statut, charge := ReponseOK(map[string]any{"a": 1})
	if statut != http.StatusOK || charge.(map[string]any)["success"] != true {
		t.Errorf("ReponseOK = %d %v", statut, charge)
	}
	if _, charge := ReponseOK(nil); charge.(map[string]any)["data"] != nil {
		t.Errorf("ReponseOK(nil) = %v", charge)
	}
	statut, charge = ReponseOKMeta("x", map[string]any{"page": 1})
	if statut != http.StatusOK || charge.(map[string]any)["meta"] == nil {
		t.Errorf("ReponseOKMeta = %d %v", statut, charge)
	}
	statut, charge = ReponseCree("x", "/api/v1/users/x")
	if statut != http.StatusCreated ||
		charge.(map[string]any)["meta"].(map[string]any)["location"] != "/api/v1/users/x" {
		t.Errorf("ReponseCree = %d %v", statut, charge)
	}
	corps := ReponseErreur("code", "message", nil).(map[string]any)
	erreur := corps["error"].(map[string]any)
	if corps["success"] != false || erreur["code"] != "code" ||
		erreur["message"] != "message" || erreur["details"] == nil {
		t.Errorf("ReponseErreur = %v", corps)
	}
	if obtenu := ErrNonAutorise(""); obtenu.Message != "Clé API invalide ou absente" ||
		obtenu.Statut != http.StatusUnauthorized {
		t.Errorf("ErrNonAutorise = %+v", obtenu)
	}
	if obtenu := ErrInterdit(""); obtenu.Message != "Accès refusé (permissions insuffisantes)" ||
		obtenu.Statut != http.StatusForbidden {
		t.Errorf("ErrInterdit = %+v", obtenu)
	}
	if obtenu := ErrValidation("m", map[string]any{"k": 1}); obtenu.Code != "validation_error" ||
		obtenu.Details["k"] == nil {
		t.Errorf("ErrValidation = %+v", obtenu)
	}
	if obtenu := ErrIntrouvable("Utilisateur", "x"); obtenu.Code != "not_found" ||
		obtenu.Details["identifier"] != "x" || obtenu.Message != "Utilisateur 'x' introuvable" {
		t.Errorf("ErrIntrouvable = %+v", obtenu)
	}
	if obtenu := nouvelleErreurAPI(0, "c", "m", nil); obtenu.Error() != "m" {
		t.Errorf("Error() = %q", obtenu.Error())
	}
}

// TestChainesJSON vérifie la lecture des listes JSON.
func TestChainesJSON(t *testing.T) {
	objet := map[string]any{
		"events":   []any{"a", "b", 3, nil},
		"absent":   nil,
		"scalaire": "x",
	}
	if obtenu := chainesJSON(objet, "events"); len(obtenu) != 2 || obtenu[0] != "a" {
		t.Errorf("events = %v", obtenu)
	}
	if obtenu := chainesJSON(objet, "absent"); obtenu != nil {
		t.Errorf("absent = %v", obtenu)
	}
	if obtenu := chainesJSON(objet, "scalaire"); obtenu != nil {
		t.Errorf("scalaire = %v", obtenu)
	}
	if !contientEvenement([]string{"*"}, "user.created") ||
		!contientEvenement([]string{"user.created"}, "user.created") ||
		contientEvenement([]string{"user.deleted"}, "user.created") {
		t.Error("contientEvenement incorrect")
	}
}

// TestPointsEntree vérifie la cohérence de la table des points d'entrée.
func TestPointsEntree(t *testing.T) {
	points := PointsEntree()
	if len(points) < 15 {
		t.Fatalf("points = %d", len(points))
	}
	for nom, chemin := range points {
		if !strings.HasPrefix(chemin, PrefixeAPI) {
			t.Errorf("%s = %q", nom, chemin)
		}
	}
}

// TestEvenementsDisponibles vérifie l'exposition des événements.
func TestEvenementsDisponibles(t *testing.T) {
	evenements := EvenementsDisponibles()
	if len(evenements) != 24 {
		t.Fatalf("événements = %d", len(evenements))
	}
	for index := 1; index < len(evenements); index++ {
		if evenements[index-1] >= evenements[index] {
			t.Fatalf("liste non triée: %v", evenements)
		}
	}
}

// TestNomCleJamaisComplet vérifie que la clé complète n'est jamais journalisée.
func TestNomCleJamaisComplet(t *testing.T) {
	anonyme := &Requete{}
	if obtenu := anonyme.NomCle(); obtenu != "anonyme" {
		t.Errorf("NomCle() = %q", obtenu)
	}
	identifiee := &Requete{InfoCle: &CleAPIInfo{Nom: "Poste", Prefixe: "edusync_abcd"}}
	if obtenu := identifiee.NomCle(); strings.Contains(obtenu, "edusync_abcd") &&
		!strings.HasSuffix(obtenu, "…)") {
		t.Errorf("NomCle() = %q", obtenu)
	}
}
