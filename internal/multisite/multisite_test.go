package multisite

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/crypto"
)

// echantillon retourne un profil complet de test.
func echantillon() ProfilDomaine {
	return ProfilDomaine{
		ID:                "aaaa1111",
		Label:             "Lycée Victor Hugo",
		Domain:            "lycee-victor-hugo.local",
		Controller:        "10.0.0.5",
		Username:          "admin",
		Password:          "S3cret!",
		RememberPassword:  true,
		VerifyCertificate: false,
		CACertPath:        "/etc/ssl/ca.pem",
	}
}

// ─── Identifiants ────────────────────────────────────────────────────────

func TestUniqueSiteIDNeverCollides(t *testing.T) {
	existants := []ProfilDomaine{{ID: "deadbeef", Label: "a", Domain: "a.local"}}
	vus := map[string]bool{"deadbeef": true}
	for i := 0; i < 50; i++ {
		id := NewSiteID(existants)
		if id == "" || vus[id] {
			t.Fatalf("identifiant en collision : %q", id)
		}
		vus[id] = true
		existants = append(existants, ProfilDomaine{ID: id, Label: "x", Domain: "x.local"})
	}
}

func TestNouveauProfilDefauts(t *testing.T) {
	profil := NouveauProfil("Site", "college-a.local", AvecIdentifiant("admin"))
	if profil.ID == "" {
		t.Error("identifiant vide")
	}
	if !profil.VerifyCertificate {
		t.Error("verify_certificate devrait valoir true par défaut")
	}
	if profil.RememberPassword {
		t.Error("remember_password devrait valoir false par défaut")
	}
	if profil.StoredPassword() != "" {
		t.Error("stored_password devrait être vide")
	}
}

// ─── Validation ──────────────────────────────────────────────────────────

func TestValidateAccepteUnProfilValide(t *testing.T) {
	if errs := echantillon().Validate(); len(errs) != 0 {
		t.Errorf("profil valide rejeté : %v", errs)
	}
}

func TestValidateExigeLabelEtDomaine(t *testing.T) {
	profil := ProfilDomaine{ID: "x"}
	if errs := profil.Validate(); len(errs) != 2 {
		t.Errorf("erreurs = %v", errs)
	}
}

func TestValidateRejetteDomaineMalforme(t *testing.T) {
	profil := ProfilDomaine{ID: "x", Label: "Site", Domain: "domaine invalide!"}
	if !contientSousChaine(profil.Validate(), "invalide") {
		t.Errorf("erreurs = %v", profil.Validate())
	}
}

func TestValidateExigeMotDePasseSiMemorise(t *testing.T) {
	profil := ProfilDomaine{ID: "x", Label: "S", Domain: "a.local", RememberPassword: true}
	if !contientSousChaine(profil.Validate(), "mot de passe") {
		t.Errorf("erreurs = %v", profil.Validate())
	}
}

// ─── Persistance chiffrée ────────────────────────────────────────────────

func TestSitesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	cle := filepath.Join(dir, "secret.key")

	if err := SauvegarderSites([]ProfilDomaine{echantillon()}, chemin, cle); err != nil {
		t.Fatalf("SauvegarderSites: %v", err)
	}
	charges, err := ChargerSites(chemin, cle)
	if err != nil {
		t.Fatalf("ChargerSites: %v", err)
	}
	if len(charges) != 1 || charges[0] != echantillon() {
		t.Errorf("round-trip = %+v", charges)
	}
}

func TestSitesFichierChiffre(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	if err := SauvegarderSites([]ProfilDomaine{echantillon()}, chemin, filepath.Join(dir, "secret.key")); err != nil {
		t.Fatal(err)
	}
	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if contientOctets(brut, []byte("lycee-victor-hugo.local")) || contientOctets(brut, []byte("S3cret!")) {
		t.Error("données en clair dans le fichier")
	}
	var enveloppe map[string]any
	if err := json.Unmarshal(brut, &enveloppe); err != nil {
		t.Fatalf("enveloppe illisible: %v", err)
	}
	if len(enveloppe) != 2 || enveloppe["version"] == nil || enveloppe["payload"] == nil {
		t.Errorf("enveloppe = %v", enveloppe)
	}
}

func TestSitesFichierPrive(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	if err := SauvegarderSites([]ProfilDomaine{echantillon()}, chemin, filepath.Join(dir, "secret.key")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %o, attendu 600", info.Mode().Perm())
	}
}

func TestMotDePasseAbandonneSiNonMemorise(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	cle := filepath.Join(dir, "secret.key")
	profil := echantillon()
	profil.RememberPassword = false

	if err := SauvegarderSites([]ProfilDomaine{profil}, chemin, cle); err != nil {
		t.Fatal(err)
	}
	charges, err := ChargerSites(chemin, cle)
	if err != nil {
		t.Fatal(err)
	}
	if charges[0].Password != "" {
		t.Errorf("mot de passe conservé : %q", charges[0].Password)
	}
}

func TestChargerFichierAbsentRetourneVide(t *testing.T) {
	dir := t.TempDir()
	profils, err := ChargerSites(filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("erreur inattendue: %v", err)
	}
	if len(profils) != 0 {
		t.Errorf("profils = %v", profils)
	}
}

func TestChargerFichierCorrompuLeve(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	if err := os.WriteFile(chemin, []byte(`{"version": 1, "payload": "pas-du-base64!"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ChargerSites(chemin, filepath.Join(dir, "secret.key")); err == nil {
		t.Error("fichier corrompu accepté")
	} else if _, ok := err.(*MultisiteError); !ok {
		t.Errorf("type d'erreur = %T", err)
	}
}

func TestAssurerSitesNeLeveJamais(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	if err := os.WriteFile(chemin, []byte("{cassé"), 0o600); err != nil {
		t.Fatal(err)
	}
	profils := AssurerSites(chemin, filepath.Join(dir, "secret.key"), filepath.Join(dir, "absent.json"), nil)
	if len(profils) != 0 {
		t.Errorf("profils = %v", profils)
	}
}

func TestSauvegardeNormaliseLaCasseDuDomaine(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "domaines.json")
	cle := filepath.Join(dir, "secret.key")
	profil := ProfilDomaine{ID: "id1", Label: "S", Domain: "  LYCEE-VICTOR-HUGO.LOCAL ", VerifyCertificate: true}
	if err := SauvegarderSites([]ProfilDomaine{profil}, chemin, cle); err != nil {
		t.Fatal(err)
	}
	charges, err := ChargerSites(chemin, cle)
	if err != nil {
		t.Fatal(err)
	}
	if charges[0].Domain != "lycee-victor-hugo.local" {
		t.Errorf("domaine = %q", charges[0].Domain)
	}
}

// ─── Recherche ───────────────────────────────────────────────────────────

func TestTrouverProfilEtProfilPourDomaine(t *testing.T) {
	profils := []ProfilDomaine{echantillon(), {ID: "bbbb2222", Label: "B", Domain: "b.local"}}
	if p := TrouverProfil(profils, "bbbb2222"); p == nil || p.Label != "B" {
		t.Errorf("TrouverProfil = %+v", p)
	}
	if TrouverProfil(profils, "inconnu") != nil {
		t.Error("profil inconnu trouvé")
	}
	if TrouverProfil(profils, "") != nil {
		t.Error("identifiant vide accepté")
	}
	if p := ProfilPourDomaine(profils, "LYCEE-VICTOR-HUGO.local"); p == nil || p.ID != "aaaa1111" {
		t.Errorf("ProfilPourDomaine = %+v", p)
	}
	if ProfilPourDomaine(profils, "autre.local") != nil {
		t.Error("domaine inconnu trouvé")
	}
	if ProfilPourDomaine(profils, "") != nil {
		t.Error("domaine vide accepté")
	}
}

// ─── Migration connection.local.json (M9 → M25) ──────────────────────────

// ecrireConnexionHeritee écrit connection.local.json (champs chiffrés un par un).
func ecrireConnexionHeritee(t *testing.T, chemin, clePath, domaine, controleur, utilisateur string, motDePasse *string) {
	t.Helper()
	cle, err := crypto.LoadOrCreateKey(clePath)
	if err != nil {
		t.Fatal(err)
	}
	champs := map[string]string{"domaine": domaine, "controleur": controleur, "utilisateur": utilisateur}
	if motDePasse != nil {
		champs["mot_de_passe"] = *motDePasse
	}
	payload := map[string]string{}
	for champ, valeur := range champs {
		jeton, err := crypto.Encrypt(cle, valeur)
		if err != nil {
			t.Fatal(err)
		}
		payload[champ] = jeton
	}
	donnees, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportConnexionHeritee(t *testing.T) {
	dir := t.TempDir()
	motDePasse := "Bienvenue01"
	herite := filepath.Join(dir, "connection.local.json")
	ecrireConnexionHeritee(t, herite, filepath.Join(dir, "secret.key"),
		"lycee-victor-hugo.local", "dc01.lycee-victor-hugo.local", "admin", &motDePasse)

	profil, err := ImporterConnexionHeritee(filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key"), herite)
	if err != nil {
		t.Fatal(err)
	}
	if profil == nil {
		t.Fatal("profil nil")
	}
	if profil.Domain != "lycee-victor-hugo.local" || profil.Controller != "dc01.lycee-victor-hugo.local" ||
		profil.Username != "admin" || profil.Password != "Bienvenue01" || !profil.RememberPassword {
		t.Errorf("profil = %+v", profil)
	}
}

func TestImportConnexionHeriteeSansMotDePasse(t *testing.T) {
	dir := t.TempDir()
	herite := filepath.Join(dir, "connection.local.json")
	ecrireConnexionHeritee(t, herite, filepath.Join(dir, "secret.key"), "d.local", "c", "u", nil)

	profil, err := ImporterConnexionHeritee(filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key"), herite)
	if err != nil {
		t.Fatal(err)
	}
	if profil == nil {
		t.Fatal("profil nil")
	}
	if profil.Password != "" || profil.RememberPassword {
		t.Errorf("profil = %+v", profil)
	}
}

func TestImportConnexionHeriteeAbsente(t *testing.T) {
	dir := t.TempDir()
	profil, err := ImporterConnexionHeritee(filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key"), filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if profil != nil {
		t.Errorf("profil = %+v", profil)
	}
}

func TestAssurerSitesMigreUneFois(t *testing.T) {
	dir := t.TempDir()
	motDePasse := "Bienvenue01"
	herite := filepath.Join(dir, "connection.local.json")
	ecrireConnexionHeritee(t, herite, filepath.Join(dir, "secret.key"),
		"lycee-victor-hugo.local", "dc01", "admin", &motDePasse)
	chemin := filepath.Join(dir, "domaines.json")
	cle := filepath.Join(dir, "secret.key")

	premier := AssurerSites(chemin, cle, herite, nil)
	if len(premier) != 1 {
		t.Fatalf("premier = %v", premier)
	}
	if _, err := os.Stat(chemin); err != nil {
		t.Fatalf("fichier non créé: %v", err)
	}

	second := AssurerSites(chemin, cle, herite, nil)
	if len(second) != 1 || second[0].ID != premier[0].ID {
		t.Errorf("second = %v (premier = %v)", second, premier)
	}
}

func TestAssurerSitesVideSansSource(t *testing.T) {
	dir := t.TempDir()
	profils := AssurerSites(filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key"), filepath.Join(dir, "absent.json"), nil)
	if len(profils) != 0 {
		t.Errorf("profils = %v", profils)
	}
}

// contientSousChaine indique si une des chaînes contient « cible ».
func contientSousChaine(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if strings.Contains(v, cible) {
			return true
		}
	}
	return false
}

// contientOctets indique si « aiguille » apparaît dans « botte ».
func contientOctets(botte, aiguille []byte) bool {
	return bytes.Contains(botte, aiguille)
}
