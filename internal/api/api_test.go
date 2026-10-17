package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// ─── Doublure d'annuaire ───────────────────────────────────────────────────

// annuaireFactice implémente à la fois `api.Annuaire` et `portal.Annuaire` :
// les routes AD et la validation d'une demande de portail peuvent ainsi être
// exercées sans contrôleur de domaine.
type annuaireFactice struct {
	mu           sync.Mutex
	base         string
	comptes      map[string]Utilisateur
	groupes      map[string]Groupe
	unites       map[string]Unite
	motsDePasse  map[string]string
	creations    []CreationUtilisateur
	comptesCrees []portal.CompteCreation
}

// nouvelAnnuaireFactice construit un annuaire peuplé de deux comptes.
func nouvelAnnuaireFactice() *annuaireFactice {
	faux := &annuaireFactice{
		base:        "DC=edusync,DC=local",
		comptes:     map[string]Utilisateur{},
		groupes:     map[string]Groupe{},
		unites:      map[string]Unite{},
		motsDePasse: map[string]string{},
	}
	faux.comptes["l.martin"] = Utilisateur{
		SAM: "l.martin", CN: "Léa Martin", Mail: "lea.martin@edusync.local",
		DN: "CN=Léa Martin,OU=Eleves,DC=edusync,DC=local", UPN: "l.martin@edusync.local",
		OU: "OU=Eleves,DC=edusync,DC=local", Actif: true,
	}
	faux.comptes["p.dupont"] = Utilisateur{
		SAM: "p.dupont", CN: "Paul Dupont", Mail: "paul.dupont@edusync.local",
		DN: "CN=Paul Dupont,OU=Personnels,DC=edusync,DC=local", UPN: "p.dupont@edusync.local",
		OU: "OU=Personnels,DC=edusync,DC=local", Actif: false,
	}
	faux.groupes["Eleves"] = Groupe{
		CN: "Eleves", SAM: "Eleves", DN: "CN=Eleves,OU=Groupes,DC=edusync,DC=local",
		Description: "Tous les élèves", OU: "OU=Groupes,DC=edusync,DC=local",
		Membres: []string{"CN=Léa Martin,OU=Eleves,DC=edusync,DC=local"},
	}
	faux.unites["OU=Eleves,DC=edusync,DC=local"] = Unite{
		DN: "OU=Eleves,DC=edusync,DC=local", Nom: "Eleves", Description: "Élèves",
	}
	return faux
}

// BaseDN retourne la racine du domaine.
func (a *annuaireFactice) BaseDN() string { return a.base }

// ChercherUtilisateurs filtre les comptes sur le SAM, le CN ou l'adresse.
func (a *annuaireFactice) ChercherUtilisateurs(_ context.Context,
	requete string, limite int) ([]Utilisateur, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	recherche := strings.ToLower(requete)
	var resultats []Utilisateur
	for _, compte := range a.comptes {
		if recherche != "" && !strings.Contains(strings.ToLower(compte.SAM), recherche) &&
			!strings.Contains(strings.ToLower(compte.CN), recherche) &&
			!strings.Contains(strings.ToLower(compte.Mail), recherche) {
			continue
		}
		resultats = append(resultats, compte)
	}
	sort.Slice(resultats, func(i, j int) bool { return resultats[i].SAM < resultats[j].SAM })
	if limite > 0 && len(resultats) > limite {
		resultats = resultats[:limite]
	}
	return resultats, nil
}

func (a *annuaireFactice) ObtenirUtilisateur(_ context.Context, sam string) (*Utilisateur, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	compte, ok := a.comptes[strings.ToLower(sam)]
	if !ok {
		return nil, nil
	}
	return &compte, nil
}

func (a *annuaireFactice) CreerUtilisateur(_ context.Context, params CreationUtilisateur) (*Utilisateur, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, existe := a.comptes[strings.ToLower(params.SAM)]; existe {
		return nil, nouvelleErreurAPI(409, "conflict", "Le compte existe déjà", nil)
	}
	compte := Utilisateur{
		SAM: params.SAM, CN: params.CN, Mail: params.Mail, OU: params.OU,
		DN: "CN=" + params.CN + "," + params.OU, Actif: true,
	}
	a.comptes[strings.ToLower(params.SAM)] = compte
	a.creations = append(a.creations, params)
	return &compte, nil
}

func (a *annuaireFactice) ModifierUtilisateur(_ context.Context, sam string, champs map[string]string) (*Utilisateur, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	compte, ok := a.comptes[strings.ToLower(sam)]
	if !ok {
		return nil, ErrIntrouvable("Utilisateur", sam)
	}
	if valeur, ok := champs["cn"]; ok {
		compte.CN = valeur
	}
	if valeur, ok := champs["mail"]; ok {
		compte.Mail = valeur
	}
	if valeur, ok := champs["ou"]; ok {
		compte.OU = valeur
	}
	if valeur, ok := champs["active"]; ok {
		compte.Actif = valeur == "true"
	}
	a.comptes[strings.ToLower(sam)] = compte
	return &compte, nil
}

func (a *annuaireFactice) SupprimerUtilisateur(_ context.Context, sam string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.comptes[strings.ToLower(sam)]; !ok {
		return ErrIntrouvable("Utilisateur", sam)
	}
	delete(a.comptes, strings.ToLower(sam))
	return nil
}

func (a *annuaireFactice) ReinitialiserMotDePasse(_ context.Context, sam, motDePasse string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.comptes[strings.ToLower(sam)]; !ok {
		return ErrIntrouvable("Utilisateur", sam)
	}
	a.motsDePasse[strings.ToLower(sam)] = motDePasse
	return nil
}

func (a *annuaireFactice) ChercherGroupes(_ context.Context, requete string, limite int) ([]Groupe, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var resultats []Groupe
	for _, groupe := range a.groupes {
		if requete != "" && !strings.Contains(strings.ToLower(groupe.CN), strings.ToLower(requete)) {
			continue
		}
		// La liste est allégée : les membres ne sont résolus qu'au détail,
		// comme le fait l'adaptateur de `*ad.Pool`.
		groupe.Membres = nil
		resultats = append(resultats, groupe)
	}
	sort.Slice(resultats, func(i, j int) bool { return resultats[i].CN < resultats[j].CN })
	if limite > 0 && len(resultats) > limite {
		resultats = resultats[:limite]
	}
	return resultats, nil
}

func (a *annuaireFactice) ObtenirGroupe(_ context.Context, cn string) (*Groupe, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, groupe := range a.groupes {
		if strings.EqualFold(groupe.CN, cn) {
			copie := groupe
			return &copie, nil
		}
	}
	return nil, nil
}

func (a *annuaireFactice) CreerGroupe(_ context.Context, params CreationGroupe) (*Groupe, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, existant := range a.groupes {
		if strings.EqualFold(existant.CN, params.CN) {
			return nil, nouvelleErreurAPI(409, "conflict", "Le groupe existe déjà", nil)
		}
	}
	groupe := Groupe{
		CN: params.CN, SAM: params.SAM, Description: params.Description, OU: params.OU,
		DN: "CN=" + params.CN + "," + params.OU,
	}
	a.groupes[params.CN] = groupe
	return &groupe, nil
}

func (a *annuaireFactice) ChercherOUs(_ context.Context, requete string, limite int) ([]Unite, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var resultats []Unite
	for _, unite := range a.unites {
		if requete != "" && !strings.Contains(strings.ToLower(unite.Nom), strings.ToLower(requete)) {
			continue
		}
		resultats = append(resultats, unite)
	}
	sort.Slice(resultats, func(i, j int) bool { return resultats[i].DN < resultats[j].DN })
	if limite > 0 && len(resultats) > limite {
		resultats = resultats[:limite]
	}
	return resultats, nil
}

func (a *annuaireFactice) ObtenirOU(_ context.Context, dn string) (*Unite, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	unite, ok := a.unites[dn]
	if !ok {
		return nil, nil
	}
	return &unite, nil
}

func (a *annuaireFactice) CreerOU(_ context.Context, nom, description, parentDN string) (*Unite, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	unite := Unite{DN: "OU=" + nom + "," + parentDN, Nom: nom, Description: description}
	a.unites[unite.DN] = unite
	return &unite, nil
}

// ─── Portal.Annuaire ───────────────────────────────────────────────────────

// TrouverUtilisateur résout un identifiant ou une adresse (portail).
func (a *annuaireFactice) TrouverUtilisateur(requete string) (*portal.Utilisateur, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, compte := range a.comptes {
		if strings.EqualFold(compte.SAM, requete) || strings.EqualFold(compte.Mail, requete) {
			return &portal.Utilisateur{DN: compte.DN, SAM: compte.SAM, CN: compte.CN,
				Mail: compte.Mail, Desactive: !compte.Actif}, nil
		}
	}
	return nil, nil
}

// DefinirMotDePasse enregistre le mot de passe (portail).
func (a *annuaireFactice) DefinirMotDePasse(dn, motDePasse string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.motsDePasse[dn] = motDePasse
	return nil
}

// IdentifiantsExistants retourne les SAM déjà pris (portail).
func (a *annuaireFactice) IdentifiantsExistants() (map[string]bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	existants := map[string]bool{}
	for sam := range a.comptes {
		existants[sam] = true
	}
	return existants, nil
}

// CreerCompte crée le compte demandé par une pré-inscription validée.
func (a *annuaireFactice) CreerCompte(compte portal.CompteCreation, motDePasse string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.comptesCrees = append(a.comptesCrees, compte)
	a.comptes[strings.ToLower(compte.Identifiant)] = Utilisateur{
		SAM: compte.Identifiant, CN: compte.Nom, Mail: compte.Mail, OU: compte.OU,
		DN: "CN=" + compte.Nom + "," + compte.OU, Actif: true,
	}
	a.motsDePasse[compte.Identifiant] = motDePasse
	return nil
}

// ─── Montage ───────────────────────────────────────────────────────────────

// banc regroupe un serveur prêt à l'emploi et ses dépendances.
type banc struct {
	serveur  *Serveur
	deps     Dependances
	faux     *annuaireFactice
	magasin  *MagasinCles
	cle      string
	dossier  string
	webhooks *webhooks.Store
}

// nouveauBanc monte un serveur complet (routes, clés, portail, webhooks).
func nouveauBanc(t *testing.T) *banc {
	t.Helper()
	dossier := t.TempDir()
	faux := nouvelAnnuaireFactice()

	magasin, err := OuvrirMagasinCles(filepath.Join(dossier, NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture du magasin de clés: %v", err)
	}
	cle, _, err := magasin.Creer("Tests", []string{"*"}, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la clé: %v", err)
	}

	storeWebhooks, err := webhooks.OuvrirStore(filepath.Join(dossier, webhooks.NomFichier),
		filepath.Join(dossier, webhooks.NomFichierLivraisons))
	if err != nil {
		t.Fatalf("ouverture du magasin de webhooks: %v", err)
	}
	t.Cleanup(func() { _ = storeWebhooks.Fermer() })

	dispatcher := webhooks.NouveauDispatcher(storeWebhooks)
	dispatcher.CheminCle = filepath.Join(dossier, "secret.key")

	storePortail, err := portal.OuvrirStore(filepath.Join(dossier, portal.FichierBase))
	if err != nil {
		t.Fatalf("ouverture du magasin du portail: %v", err)
	}
	t.Cleanup(func() { _ = storePortail.Fermer() })
	configPortail := portal.ConfigDefaut()
	configPortail.OUAccueil = "OU=Eleves,DC=edusync,DC=local"
	// L'envoi de la notification d'identifiants est délégué : aucun serveur
	// SMTP n'est requis dans un test.
	service := portal.NouveauService(configPortail, config.DefaultAppConfig(),
		faux, storePortail, "edusync.local", filepath.Join(dossier, "journal.db"))
	service.EnvoiDirect = func(_, _, _ string) error { return nil }

	deps := Dependances{
		Annuaire:   faux,
		Portail:    service,
		Webhooks:   storeWebhooks,
		Dispatcher: dispatcher,
		Config:     config.DefaultAppConfig(),
		Version:    "4.0.0",
	}

	routeur := NouveauRouteur()
	EnregistrerRoutes(routeur, deps)
	serveur := NouveauServeur(ConfigDefaut(), routeur, magasin)
	return &banc{serveur: serveur, deps: deps, faux: faux, magasin: magasin,
		cle: cle, dossier: dossier, webhooks: storeWebhooks}
}

// requete construit une requête HTTP avec la clé de test.
func (b *banc) requete(t *testing.T, methode, chemin, corps string) *http.Request {
	t.Helper()
	var lecteur *strings.Reader
	if corps == "" {
		lecteur = strings.NewReader("")
	} else {
		lecteur = strings.NewReader(corps)
	}
	requete := httptest.NewRequest(methode, chemin, lecteur)
	requete.Header.Set("X-API-Key", b.cle)
	if corps != "" {
		requete.Header.Set("Content-Type", "application/json")
	}
	return requete
}

// appeler exécute une requête et retourne la réponse.
func (b *banc) appeler(t *testing.T, methode, chemin, corps string) *httptest.ResponseRecorder {
	t.Helper()
	enregistreur := httptest.NewRecorder()
	b.serveur.servir(enregistreur, b.requete(t, methode, chemin, corps))
	return enregistreur
}

// charge décode le corps JSON d'une réponse.
func charge(t *testing.T, enregistreur *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var valeur map[string]any
	if err := json.Unmarshal(enregistreur.Body.Bytes(), &valeur); err != nil {
		t.Fatalf("corps JSON invalide: %v (%s)", err, enregistreur.Body.String())
	}
	return valeur
}

// donnees retourne le champ `data` d'une réponse.
func donnees(t *testing.T, enregistreur *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	charge := charge(t, enregistreur)
	bloc, ok := charge["data"].(map[string]any)
	if !ok {
		t.Fatalf("champ data absent: %s", enregistreur.Body.String())
	}
	return bloc
}

// ─── Configuration ─────────────────────────────────────────────────────────

// TestConfigValider vérifie les messages de validation, identiques à Python.
func TestConfigValider(t *testing.T) {
	cfg := ConfigDefaut()
	if erreurs := cfg.Valider(); len(erreurs) != 0 {
		t.Fatalf("configuration par défaut invalide: %v", erreurs)
	}
	cas := []struct {
		nom      string
		config   Config
		attendre string
	}{
		{"hôte vide", Config{Port: 8080}, "Interface d'écoute requise (ex. 127.0.0.1)."},
		{"port nul", Config{Hote: "127.0.0.1", Port: 0}, "Port invalide (1-65535)."},
		{"port haut", Config{Hote: "127.0.0.1", Port: 70000}, "Port invalide (1-65535)."},
		{"limite", Config{Hote: "127.0.0.1", Port: 80, LimiteRPM: 10001},
			"Limite de taux invalide (0-10000)."},
	}
	for _, test := range cas {
		if erreurs := test.config.Valider(); !contientMessage(erreurs, test.attendre) {
			t.Errorf("%s : erreurs = %v, attendu %q", test.nom, erreurs, test.attendre)
		}
	}
	if obtenu := cfg.BaseURL(); obtenu != "http://127.0.0.1:8788" {
		t.Errorf("BaseURL() = %q", obtenu)
	}
	partage := ConfigDefaut()
	partage.Hote = "0.0.0.0"
	if obtenu := partage.BaseURL(); obtenu != "http://127.0.0.1:8788" {
		t.Errorf("BaseURL() sur 0.0.0.0 = %q", obtenu)
	}
}

// TestConfigAllerRetour vérifie que `api.json` est relu avec les clés Python.
func TestConfigAllerRetour(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichier)
	cfg := ConfigDefaut()
	cfg.Actif = true
	cfg.Port = 9099
	cfg.CORSOrigines = []string{"https://admin.edusync.local"}
	if _, err := SauvegarderConfig(cfg, chemin); err != nil {
		t.Fatalf("sauvegarde: %v", err)
	}

	donnees, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	var brut map[string]any
	if err := json.Unmarshal(donnees, &brut); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	clesPython := []string{"actif", "hote", "port", "cors_origins", "rate_limit_rpm", "titre"}
	if len(brut) != len(clesPython) {
		t.Fatalf("clés = %v", brut)
	}
	for _, cle := range clesPython {
		if _, ok := brut[cle]; !ok {
			t.Errorf("clé %q absente", cle)
		}
	}

	relu := ChargerConfig(chemin)
	if !relu.Actif || relu.Port != 9099 || relu.Hote != "127.0.0.1" ||
		len(relu.CORSOrigines) != 1 || relu.LimiteRPM != 60 || relu.Titre != "EduSync AD API" {
		t.Fatalf("relu = %+v", relu)
	}
	if _, err := SauvegarderConfig(Config{Hote: "127.0.0.1", Port: 0}, chemin); err == nil {
		t.Error("une configuration invalide ne doit pas être écrite")
	}
	if obtenu := ChargerConfig(filepath.Join(t.TempDir(), "absent.json")); obtenu.Port != 8788 {
		t.Errorf("fichier absent = %+v", obtenu)
	}
	if obtenu := ChargerConfig(chemin + ".absent"); obtenu.Hote != "127.0.0.1" {
		t.Errorf("chemin absent = %+v", obtenu)
	}
}

func contientMessage(messages []string, attendu string) bool {
	for _, message := range messages {
		if message == attendu {
			return true
		}
	}
	return false
}
