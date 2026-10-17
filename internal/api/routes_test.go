package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// TestRoutesUtilisateursListe vérifie le listage, la pagination et le filtre.
func TestRoutesUtilisateursListe(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/users", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	items := bloc["items"].([]any)
	if len(items) != 2 || bloc["total"].(float64) != 2 {
		t.Fatalf("items = %v", items)
	}
	premier := items[0].(map[string]any)
	if premier["sam"] != "l.martin" || premier["cn"] != "Léa Martin" ||
		premier["mail"] != "lea.martin@edusync.local" || premier["actif"] != true {
		t.Errorf("premier = %v", premier)
	}
	if bloc["page"].(float64) != 1 || bloc["page_size"].(float64) != 50 {
		t.Errorf("pagination = %v", bloc)
	}

	// Filtre textuel insensible à la casse.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/users?q=DUPONT", "")
	bloc = donnees(t, enregistreur)
	if len(bloc["items"].([]any)) != 1 {
		t.Errorf("filtre = %v", bloc["items"])
	}

	// Pagination : une page de 1.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/users?page=2&page_size=1", "")
	bloc = donnees(t, enregistreur)
	if bloc["page"].(float64) != 2 || bloc["page_size"].(float64) != 1 ||
		len(bloc["items"].([]any)) != 1 {
		t.Errorf("pagination = %v", bloc)
	}

	// Le paramètre de requête invalide retombe sur la valeur par défaut.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/users?page=abc&page_size=-4", "")
	bloc = donnees(t, enregistreur)
	if bloc["page"].(float64) != 1 || bloc["page_size"].(float64) != 50 {
		t.Errorf("valeurs par défaut = %v", bloc)
	}
}

// TestRoutesUtilisateurDetail vérifie le détail et le 404.
func TestRoutesUtilisateurDetail(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/users/l.martin", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["sam"] != "l.martin" || bloc["dn"] == "" {
		t.Errorf("utilisateur = %v", bloc)
	}

	inconnu := b.appeler(t, http.MethodGet, "/api/v1/users/inconnu", "")
	if inconnu.Code != http.StatusNotFound {
		t.Fatalf("statut = %d", inconnu.Code)
	}
	if code := codeErreur(t, inconnu); code != "not_found" {
		t.Errorf("code = %q", code)
	}
}

// TestRoutesUtilisateurCreation vérifie la création (mot de passe généré).
func TestRoutesUtilisateurCreation(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/users",
		`{"sam":"n.bernard","cn":"Nina Bernard","mail":"nina.bernard@edusync.local",
		  "given_name":"Nina","sn":"Bernard","ou":"OU=Personnels,DC=edusync,DC=local"}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["utilisateur"].(map[string]any)["sam"] != "n.bernard" {
		t.Errorf("utilisateur = %v", bloc["utilisateur"])
	}
	genere, ok := bloc["mot_de_passe"].(string)
	if !ok || len(genere) < 12 {
		t.Fatalf("mot de passe généré = %v", bloc["mot_de_passe"])
	}
	if len(b.faux.creations) != 1 || b.faux.creations[0].MotDePasse != genere {
		t.Errorf("créations = %+v", b.faux.creations)
	}

	// Création avec mot de passe imposé.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users",
		`{"sam":"o.petit","mail":"o.petit@edusync.local","password":"MotDePasse1!"}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc = donnees(t, enregistreur)
	if _, present := bloc["mot_de_passe"]; present {
		t.Error("un mot de passe fourni ne doit pas être renvoyé")
	}
	if b.faux.creations[1].MotDePasse != "MotDePasse1!" {
		t.Errorf("mot de passe transmis = %q", b.faux.creations[1].MotDePasse)
	}

	// Champs obligatoires manquants.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users", `{"sam":"x"}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if code := codeErreur(t, enregistreur); code != "validation_error" {
		t.Errorf("code = %q", code)
	}

	// Compte existant : 409.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users",
		`{"sam":"l.martin","mail":"l.martin@edusync.local"}`)
	if enregistreur.Code != http.StatusConflict {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
}

// TestRoutesUtilisateurModification vérifie la modification (PATCH).
func TestRoutesUtilisateurModification(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodPatch, "/api/v1/users/p.dupont",
		`{"mail":"paul.dupont@nouveau.local","active":true}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["mail"] != "paul.dupont@nouveau.local" || bloc["actif"] != true {
		t.Errorf("utilisateur = %v", bloc)
	}

	// Aucun champ reconnu.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/users/p.dupont", `{"bidon":1}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	bloc = charge(t, enregistreur)
	if !strings.Contains(bloc["error"].(map[string]any)["message"].(string), "Aucun champ") {
		t.Errorf("erreur = %v", bloc["error"])
	}

	// Type inattendu.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/users/p.dupont", `{"cn":42}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut = %d", enregistreur.Code)
	}
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/users/p.dupont", `{"active":"oui"}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut = %d", enregistreur.Code)
	}

	// Utilisateur inconnu.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/users/inconnu", `{"cn":"X"}`)
	if enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut = %d", enregistreur.Code)
	}
}

// TestRoutesUtilisateurSuppression vérifie la suppression.
func TestRoutesUtilisateurSuppression(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodDelete, "/api/v1/users/p.dupont", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["deleted"] != true || bloc["sam"] != "p.dupont" {
		t.Errorf("données = %v", bloc)
	}
	if enregistreur := b.appeler(t, http.MethodDelete, "/api/v1/users/p.dupont", ""); enregistreur.Code != http.StatusNotFound {
		t.Errorf("seconde suppression = %d", enregistreur.Code)
	}
}

// TestRoutesMotDePasse vérifie la réinitialisation et sa permission.
func TestRoutesMotDePasse(t *testing.T) {
	b := nouveauBanc(t)

	// Avec la permission `users:password` (clé `*`).
	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/users/l.martin/password", `{}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	genere, ok := bloc["mot_de_passe"].(string)
	if !ok || bloc["reset"] != true {
		t.Fatalf("données = %v", bloc)
	}
	if b.faux.motsDePasse["l.martin"] != genere {
		t.Errorf("mot de passe appliqué = %q", b.faux.motsDePasse["l.martin"])
	}

	// Mot de passe imposé : non renvoyé.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users/l.martin/password",
		`{"password":"NouveauMdp1!"}`)
	bloc = donnees(t, enregistreur)
	if _, present := bloc["mot_de_passe"]; present {
		t.Error("un mot de passe fourni ne doit pas être renvoyé")
	}
	if b.faux.motsDePasse["l.martin"] != "NouveauMdp1!" {
		t.Errorf("mot de passe appliqué = %q", b.faux.motsDePasse["l.martin"])
	}

	// Sans la permission : 403.
	cle, _, err := b.magasin.Creer("Sans mot de passe", []string{"users:read"}, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la clé: %v", err)
	}
	b.cle = cle
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users/l.martin/password", `{}`)
	if enregistreur.Code != http.StatusForbidden {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if !strings.Contains(charge(t, enregistreur)["error"].(map[string]any)["message"].(string),
		"users:password") {
		t.Errorf("message = %v", charge(t, enregistreur)["error"])
	}

	// Utilisateur inconnu, avec une clé disposant de la permission.
	admin, _, err := b.magasin.Creer("Admin", []string{"*"}, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la clé admin: %v", err)
	}
	b.cle = admin
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/users/inconnu/password", `{}`)
	if enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
}

// TestRoutesGroupes vérifie le listage, le détail et la création des groupes.
func TestRoutesGroupes(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/groups", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	items := bloc["items"].([]any)
	if len(items) != 1 || bloc["total"].(float64) != 1 {
		t.Fatalf("items = %v", items)
	}
	// La liste est allégée : les membres ne sont pas transmis.
	if _, present := items[0].(map[string]any)["members"]; present {
		t.Error("la liste ne doit pas inclure les membres")
	}

	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/groups/Eleves", "")
	bloc = donnees(t, enregistreur)
	if bloc["cn"] != "Eleves" || len(bloc["members"].([]any)) != 1 {
		t.Errorf("groupe = %v", bloc)
	}
	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/groups/Inconnu", ""); enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut groupe inconnu = %d", enregistreur.Code)
	}

	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/groups",
		`{"cn":"Professeurs","description":"Équipe pédagogique","ou":"OU=Groupes,DC=edusync,DC=local"}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if donnees(t, enregistreur)["cn"] != "Professeurs" {
		t.Errorf("groupe créé = %v", donnees(t, enregistreur))
	}
	// Sans CN : 422.
	if enregistreur := b.appeler(t, http.MethodPost, "/api/v1/groups", `{}`); enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut sans cn = %d", enregistreur.Code)
	}
	// Groupe existant : 409.
	if enregistreur := b.appeler(t, http.MethodPost, "/api/v1/groups", `{"cn":"Eleves"}`); enregistreur.Code != http.StatusConflict {
		t.Errorf("statut conflit = %d", enregistreur.Code)
	}
}

// TestRoutesOUs vérifie le listage, le détail et la création des OU.
func TestRoutesOUs(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/ous", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if len(donnees(t, enregistreur)["items"].([]any)) != 1 {
		t.Errorf("items = %v", donnees(t, enregistreur)["items"])
	}

	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/ous/OU=Eleves,DC=edusync,DC=local", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if donnees(t, enregistreur)["name"] != "Eleves" {
		t.Errorf("OU = %v", donnees(t, enregistreur))
	}
	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/ous/OU=Inconnu,DC=x", ""); enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut OU inconnue = %d", enregistreur.Code)
	}

	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/ous",
		`{"name":"Stages","description":"Stagiaires"}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if donnees(t, enregistreur)["dn"] != "OU=Stages,DC=edusync,DC=local" {
		t.Errorf("OU créée = %v", donnees(t, enregistreur))
	}
	if enregistreur := b.appeler(t, http.MethodPost, "/api/v1/ous", `{}`); enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut sans nom = %d", enregistreur.Code)
	}
}

// TestRoutesPortail vérifie la liste, la validation et le refus des demandes.
func TestRoutesPortail(t *testing.T) {
	b := nouveauBanc(t)
	service := b.deps.Portail.(*portal.Service)

	// Aucune demande pour l'instant.
	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/portal/requests", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if donnees(t, enregistreur)["total"].(float64) != 0 {
		t.Errorf("total = %v", donnees(t, enregistreur)["total"])
	}

	// Pré-inscription d'un élève.
	demande, err := service.Store.CreerDemande(portal.ProfilEleve, "Nina", "Bernard",
		"nina.bernard@edusync.local", "5eB", "Arrivée en cours d'année", time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la demande: %v", err)
	}
	seconde, err := service.Store.CreerDemande(portal.ProfilPersonnel, "Omar", "Petit",
		"omar.petit@edusync.local", "", "", time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la demande: %v", err)
	}

	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/portal/requests?statut=en_attente", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	bloc := donnees(t, enregistreur)
	if bloc["total"].(float64) != 2 || bloc["statut"] != "en_attente" {
		t.Fatalf("bloc = %v", bloc)
	}
	premier := bloc["items"].([]any)[0].(map[string]any)
	if premier["id"] == "" || premier["profil"] == "" || premier["statut"] != "en_attente" {
		t.Errorf("demande = %v", premier)
	}

	// Refus : la demande change d'état.
	enregistreur = b.appeler(t, http.MethodPost,
		"/api/v1/portal/requests/"+seconde.ID+"/refuse", `{"note":"Dossier incomplet"}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut refus = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc = donnees(t, enregistreur)
	if bloc["refused"] != true || bloc["note"] != "Dossier incomplet" {
		t.Errorf("refus = %v", bloc)
	}

	// Validation : le compte est créé dans l'annuaire.
	enregistreur = b.appeler(t, http.MethodPost,
		"/api/v1/portal/requests/"+demande.ID+"/approve", `{}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut validation = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc = donnees(t, enregistreur)
	if bloc["approved"] != true || bloc["demande_id"] != demande.ID {
		t.Errorf("validation = %v", bloc)
	}
	if len(b.faux.comptesCrees) != 1 ||
		b.faux.comptesCrees[0].Nom != "Nina Bernard" ||
		b.faux.comptesCrees[0].Mail != "nina.bernard@edusync.local" {
		t.Fatalf("comptes créés = %+v", b.faux.comptesCrees)
	}
	infos, ok := bloc["infos"].(map[string]any)
	if !ok || infos["identifiant"] == "" || infos["mot_de_passe"] == "" {
		t.Errorf("infos = %v", bloc["infos"])
	}

	// Une demande déjà traitée n'est plus validable.
	enregistreur = b.appeler(t, http.MethodPost,
		"/api/v1/portal/requests/"+demande.ID+"/approve", `{}`)
	if enregistreur.Code == http.StatusOK {
		t.Error("une demande déjà validée ne doit plus l'être")
	}
	// Demande inconnue.
	enregistreur = b.appeler(t, http.MethodPost,
		"/api/v1/portal/requests/inconnue/approve", `{}`)
	if enregistreur.Code == http.StatusOK {
		t.Error("une demande inconnue ne doit pas être validée")
	}

	// Sans portail configuré : 503.
	deps := b.deps
	deps.Portail = nil
	routeur := NouveauRouteur()
	EnregistrerRoutes(routeur, deps)
	serveur := NouveauServeur(ConfigDefaut(), routeur, b.magasin)
	enregistreur = httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet, "/api/v1/portal/requests", nil)
	requete.Header.Set("X-API-Key", b.cle)
	serveur.servir(enregistreur, requete)
	if enregistreur.Code != http.StatusServiceUnavailable {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if code := codeErreur(t, enregistreur); code != "portail_indisponible" {
		t.Errorf("code = %q", code)
	}
}

// TestRoutesWebhooksCUD vérifie la création, la lecture, la modification et la
// suppression des webhooks via l'API.
func TestRoutesWebhooksCUD(t *testing.T) {
	b := nouveauBanc(t)

	// Liste vide.
	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/webhooks", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if len(listeDonnees(t, enregistreur)) != 0 {
		t.Errorf("liste = %v", listeDonnees(t, enregistreur))
	}

	// Création : le secret est chiffré au stockage.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/webhooks",
		`{"name":"Journalisation","url":"https://journal.edusync.local/hook",
		  "events":["user.created","user.deleted"],"secret":"topsecret",
		  "headers":{"X-Client":"edusync"},"retry_policy":"fixed","max_retries":3,
		  "timeout_seconds":5}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	identifiant, _ := bloc["id"].(string)
	if identifiant == "" || !strings.HasPrefix(identifiant, "w") {
		t.Fatalf("identifiant = %q", identifiant)
	}
	if bloc["secret"] == "topsecret" || bloc["secret"] == "" {
		t.Errorf("secret stocké en clair = %v", bloc["secret"])
	}
	if bloc["retry_policy"] != "fixed" || bloc["max_retries"].(float64) != 3 ||
		bloc["timeout_seconds"].(float64) != 5 {
		t.Errorf("webhook = %v", bloc)
	}

	// Détail.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/webhooks/"+identifiant, "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if donnees(t, enregistreur)["name"] != "Journalisation" {
		t.Errorf("webhook = %v", donnees(t, enregistreur))
	}

	// Modification.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/webhooks/"+identifiant,
		`{"name":"Journal","active":false,"events":["*"]}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc = donnees(t, enregistreur)
	if bloc["name"] != "Journal" || bloc["active"] != false {
		t.Errorf("webhook modifié = %v", bloc)
	}
	// Seuls les webhooks actifs sont listés sur demande.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/webhooks?active=true", "")
	if len(listeDonnees(t, enregistreur)) != 0 {
		t.Errorf("webhooks actifs = %v", listeDonnees(t, enregistreur))
	}
	// Modification invalide : 422.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/webhooks/"+identifiant,
		`{"url":"ftp://invalide"}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut = %d", enregistreur.Code)
	}
	// Webhook inconnu.
	enregistreur = b.appeler(t, http.MethodPatch, "/api/v1/webhooks/inconnu", `{"name":"X"}`)
	if enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut = %d", enregistreur.Code)
	}

	// Création sans événement.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/webhooks",
		`{"name":"Vide","url":"https://x.local"}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut = %d", enregistreur.Code)
	}

	// Événements disponibles.
	enregistreur = b.appeler(t, http.MethodGet, "/api/v1/webhooks/events", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if len(donnees(t, enregistreur)["items"].([]any)) != 24 {
		t.Errorf("événements = %v", donnees(t, enregistreur)["items"])
	}

	// Suppression.
	enregistreur = b.appeler(t, http.MethodDelete, "/api/v1/webhooks/"+identifiant, "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if enregistreur := b.appeler(t, http.MethodDelete, "/api/v1/webhooks/"+identifiant, ""); enregistreur.Code != http.StatusNotFound {
		t.Errorf("seconde suppression = %d", enregistreur.Code)
	}
}

// TestRoutesWebhookTest vérifie l'envoi d'un événement de test (signature HMAC).
func TestRoutesWebhookTest(t *testing.T) {
	b := nouveauBanc(t)

	var recu []byte
	serveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recu = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(recu)
		w.WriteHeader(http.StatusOK)
	}))
	defer serveur.Close()

	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/webhooks",
		`{"name":"Test","url":"`+serveur.URL+`","events":["system.startup"],"secret":"abc"}`)
	if enregistreur.Code != http.StatusCreated {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	identifiant := donnees(t, enregistreur)["id"].(string)

	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/webhooks/"+identifiant+"/test",
		`{"event":"system.startup"}`)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["status_code"].(float64) != 200 || bloc["event"] != "system.startup" {
		t.Fatalf("livraison = %v", bloc)
	}
	var charge map[string]any
	if err := json.Unmarshal(recu, &charge); err != nil {
		t.Fatalf("corps reçu: %v (%s)", err, recu)
	}
	if charge["event"] != "system.startup" {
		t.Errorf("charge = %v", charge)
	}

	// Événement non souscrit : 422.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/webhooks/"+identifiant+"/test",
		`{"event":"user.created"}`)
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Errorf("statut = %d", enregistreur.Code)
	}
	// Webhook inconnu : 404.
	enregistreur = b.appeler(t, http.MethodPost, "/api/v1/webhooks/inconnu/test", `{}`)
	if enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut = %d", enregistreur.Code)
	}
}

// TestRoutesSansWebhooks vérifie le message quand aucun magasin n'existe.
func TestRoutesSansWebhooks(t *testing.T) {
	b := nouveauBanc(t)
	deps := b.deps
	deps.Webhooks = nil
	deps.Dispatcher = nil
	routeur := NouveauRouteur()
	EnregistrerRoutes(routeur, deps)
	serveur := NouveauServeur(ConfigDefaut(), routeur, b.magasin)

	enregistreur := httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	requete.Header.Set("X-API-Key", b.cle)
	serveur.servir(enregistreur, requete)
	if enregistreur.Code != http.StatusServiceUnavailable {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if code := codeErreur(t, enregistreur); code != "webhooks_indisponibles" {
		t.Errorf("code = %q", code)
	}
}

// TestRoutesAudit vérifie la lecture du journal d'actions.
func TestRoutesAudit(t *testing.T) {
	b := nouveauBanc(t)

	// Sans journal configuré : 503.
	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/audit", "")
	if enregistreur.Code != http.StatusServiceUnavailable {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if code := codeErreur(t, enregistreur); code != "journal_indisponible" {
		t.Errorf("code = %q", code)
	}

	// Avec un journal : les entrées sont listées et les filtres transmis.
	journal := &journalFactice{}
	deps := b.deps
	deps.Journal = journal
	routeur := NouveauRouteur()
	EnregistrerRoutes(routeur, deps)
	serveur := NouveauServeur(ConfigDefaut(), routeur, b.magasin)

	enregistreur = httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet,
		"/api/v1/audit?action=create_user&resultat=success&date_debut=2026-01-01", nil)
	requete.Header.Set("X-API-Key", b.cle)
	serveur.servir(enregistreur, requete)
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := donnees(t, enregistreur)
	if bloc["total"].(float64) != 1 {
		t.Fatalf("total = %v", bloc["total"])
	}
	entree := bloc["items"].([]any)[0].(map[string]any)
	if entree["action"] != "create_user" || entree["utilisateur"] != "portail" ||
		entree["simulation"] != false {
		t.Errorf("entrée = %v", entree)
	}
	filtres := bloc["filtres"].(map[string]any)
	if filtres["action"] != "create_user" || filtres["date_debut"] != "2026-01-01" {
		t.Errorf("filtres = %v", filtres)
	}
	if journal.filtre.Limit != 50 {
		t.Errorf("limite transmise = %d", journal.filtre.Limit)
	}
}

// TestRoutesInfo vérifie la route `/info` et ses points d'entrée.
func TestRoutesInfo(t *testing.T) {
	b := nouveauBanc(t)
	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/info", "")
	if enregistreur.Code != http.StatusOK {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	bloc := donnees(t, enregistreur)
	if bloc["name"] != "EduSync AD" || bloc["api_version"] != VersionAPI {
		t.Errorf("bloc = %v", bloc)
	}
	points := bloc["endpoints"].(map[string]any)
	if points["users"] != PrefixeAPI+"/users" || points["openapi"] != PrefixeAPI+"/openapi.json" {
		t.Errorf("points = %v", points)
	}
}

// listeDonnees retourne le champ `data` d'une réponse de type liste.
func listeDonnees(t *testing.T, enregistreur *httptest.ResponseRecorder) []any {
	t.Helper()
	valeur, ok := charge(t, enregistreur)["data"].([]any)
	if !ok {
		t.Fatalf("data n'est pas une liste: %s", enregistreur.Body.String())
	}
	return valeur
}

// journalFactice est une doublure de lecture du journal d'actions.
type journalFactice struct {
	filtre audit.Filter
}

// List retourne une entrée et mémorise le filtre reçu.
func (j *journalFactice) List(filtre audit.Filter) ([]audit.Entry, error) {
	j.filtre = filtre
	return []audit.Entry{{
		ID: 1,
		ActionLogEntry: models.ActionLogEntry{
			Timestamp:   "2026-10-17T12:00:00+00:00",
			ActionType:  "create_user",
			Compte:      "n.bernard",
			Resultat:    "success",
			SessionID:   "abcdef123456",
			Simulation:  false,
			Detail:      "{}",
			Utilisateur: "portail",
			Domaine:     "edusync.local",
		},
	}}, nil
}
