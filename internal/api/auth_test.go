package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPermissionsStandards vérifie que la liste des permissions correspond
// exactement à `STANDARD_PERMISSIONS` côté Python.
func TestPermissionsStandards(t *testing.T) {
	attendues := [][2]string{
		{"*", "Accès complet (admin API)"},
		{"users:read", "Lister/voir les utilisateurs"},
		{"users:write", "Créer/modifier/supprimer des utilisateurs"},
		{"users:password", "Réinitialiser les mots de passe"},
		{"groups:read", "Lister/voir les groupes"},
		{"groups:write", "Créer/modifier/supprimer des groupes"},
		{"ous:read", "Lister/voir les OUs"},
		{"ous:write", "Créer/modifier/supprimer des OUs"},
		{"audit:read", "Consulter le journal d'audit"},
		{"config:read", "Lire la configuration"},
		{"config:write", "Modifier la configuration"},
		{"portal:read", "Consulter le portail auto-service"},
		{"portal:write", "Gérer le portail (démarrer/arrêter, valider demandes)"},
		{"webhooks:read", "Lister les webhooks"},
		{"webhooks:write", "Créer/modifier/supprimer des webhooks"},
	}
	if len(PermissionsStandards) != len(attendues) {
		t.Fatalf("permissions = %d, attendu %d", len(PermissionsStandards), len(attendues))
	}
	for index, attendue := range attendues {
		if PermissionsStandards[index].Code != attendue[0] ||
			PermissionsStandards[index].Libelle != attendue[1] {
			t.Errorf("permission %d = %+v, attendu %v",
				index, PermissionsStandards[index], attendue)
		}
	}
}

// TestMagasinClesCycleDeVie couvre création, validation, révocation et
// suppression, ainsi que le format du fichier (clés Python).
func TestMagasinClesCycleDeVie(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichierCles)
	magasin, err := OuvrirMagasinCles(chemin)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	cle, info, err := magasin.Creer("Poste admin", []string{"users:read", "inexistante"}, 0, moment)
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if !strings.HasPrefix(cle, "edusync_") || len(cle) != len(PrefixeCle)+32 {
		t.Fatalf("clé = %q (%d caractères)", cle, len(cle))
	}
	if info.Prefixe != cle[:12] || info.ID[:3] != "ak_" || len(info.ID) != 15 {
		t.Fatalf("métadonnées = %+v", info)
	}
	if info.Empreinte == cle || len(info.Empreinte) != 64 {
		t.Fatalf("empreinte = %q", info.Empreinte)
	}
	if len(info.Permissions) != 1 || info.Permissions[0] != "users:read" {
		t.Errorf("permissions = %v", info.Permissions)
	}
	if info.CreeLe != "2026-10-17T12:00:00+00:00" || info.ExpireLe != nil || !info.Active {
		t.Errorf("info = %+v", info)
	}

	// Le fichier ne contient jamais la clé en clair.
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if strings.Contains(string(contenu), cle) {
		t.Fatal("la clé complète ne doit jamais être écrite sur disque")
	}
	var bruts []map[string]any
	if err := json.Unmarshal(contenu, &bruts); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	clesPython := []string{"id", "name", "key_hash", "prefix", "permissions",
		"created_at", "expires_at", "last_used_at", "active"}
	if len(bruts) != 1 || len(bruts[0]) != len(clesPython) {
		t.Fatalf("clés écrites = %v", bruts[0])
	}
	for _, clef := range clesPython {
		if _, ok := bruts[0][clef]; !ok {
			t.Errorf("clé %q absente", clef)
		}
	}
	infos, err := os.Stat(chemin)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if infos.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v", infos.Mode().Perm())
	}

	// Validation : la clé est reconnue, l'usage est daté, une variante non.
	if obtenu := magasin.Valider(cle, moment); obtenu == nil || obtenu.ID != info.ID {
		t.Fatalf("validation = %+v", obtenu)
	} else if obtenu.DernierUsage == nil || *obtenu.DernierUsage != "2026-10-17T12:00:00+00:00" {
		t.Errorf("dernier usage = %v", obtenu.DernierUsage)
	}
	if obtenu := magasin.Valider(cle+"x", moment); obtenu != nil {
		t.Error("une clé modifiée ne doit pas être acceptée")
	}
	if obtenu := magasin.Valider("pas-une-cle", moment); obtenu != nil {
		t.Error("une clé hors format ne doit pas être acceptée")
	}
	if obtenu := magasin.Valider(cle[:11], moment); obtenu != nil {
		t.Error("une clé trop courte ne doit pas être acceptée")
	}

	// Liste : la clé apparaît, puis disparaît après révocation.
	if liste := magasin.Lister(false, moment); len(liste) != 1 {
		t.Fatalf("liste = %d", len(liste))
	}
	if revoque, err := magasin.Revoquer(info.ID); err != nil || !revoque {
		t.Fatalf("révocation = %v (%v)", revoque, err)
	}
	if obtenu := magasin.Valider(cle, moment); obtenu != nil {
		t.Error("une clé révoquée ne doit plus être acceptée")
	}
	if liste := magasin.Lister(false, moment); len(liste) != 0 {
		t.Errorf("liste après révocation = %d", len(liste))
	}
	if liste := magasin.Lister(true, moment); len(liste) != 1 {
		t.Errorf("liste complète = %d", len(liste))
	}
	if revoque, _ := magasin.Revoquer("inconnu"); revoque {
		t.Error("révoquer une clé inconnue doit retourner faux")
	}
	if supprime, err := magasin.Supprimer(info.ID); err != nil || !supprime {
		t.Fatalf("suppression = %v (%v)", supprime, err)
	}
	if supprime, _ := magasin.Supprimer(info.ID); supprime {
		t.Error("une seconde suppression doit retourner faux")
	}
	if obtenu := magasin.Obtenir(info.ID); obtenu != nil {
		t.Error("la clé doit être absente")
	}
}

// TestMagasinClesValidation vérifie les refus de création.
func TestMagasinClesValidation(t *testing.T) {
	magasin, err := OuvrirMagasinCles(filepath.Join(t.TempDir(), NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, _, err := magasin.Creer("   ", []string{"*"}, 0, moment); err == nil {
		t.Error("un nom vide doit être refusé")
	}
	if _, _, err := magasin.Creer("Test", []string{"inexistante"}, 0, moment); err == nil {
		t.Error("une permission inconnue doit être refusée")
	} else if err.Error() != "Aucune permission valide" {
		t.Errorf("message = %q", err.Error())
	}
}

// TestMagasinClesExpiration vérifie qu'une clé expirée n'est plus valide.
func TestMagasinClesExpiration(t *testing.T) {
	magasin, err := OuvrirMagasinCles(filepath.Join(t.TempDir(), NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	cle, info, err := magasin.Creer("Temporaire", []string{"audit:read"}, 30, moment)
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if info.ExpireLe == nil || *info.ExpireLe != "2026-11-16T12:00:00+00:00" {
		t.Fatalf("expiration = %v", info.ExpireLe)
	}
	if obtenu := magasin.Valider(cle, moment.AddDate(0, 0, 29)); obtenu == nil {
		t.Error("une clé encore valide doit être acceptée")
	}
	if obtenu := magasin.Valider(cle, moment.AddDate(0, 0, 31)); obtenu != nil {
		t.Error("une clé expirée ne doit plus être acceptée")
	}
	if liste := magasin.Lister(false, moment.AddDate(0, 0, 31)); len(liste) != 0 {
		t.Errorf("liste des clés actives = %d", len(liste))
	}
	if info.Expiree(moment) {
		t.Error("Expiree() doit être faux avant l'échéance")
	}
}

// TestAutorisationPermissionsMiseAJour vérifie le contrôle de permission.
func TestAutorisationPermissionsMiseAJour(t *testing.T) {
	info := CleAPIInfo{Permissions: []string{"users:read", "groups:read"}}
	if !info.Autorise() {
		t.Error("sans exigence, l'accès doit être autorisé")
	}
	if !info.Autorise("users:read") || !info.Autorise("groups:read", "users:write") {
		t.Error("une permission détenue doit suffire")
	}
	if info.Autorise("users:write") || info.Autorise("audit:read") {
		t.Error("une permission absente doit être refusée")
	}
	admin := CleAPIInfo{Permissions: []string{"*"}}
	if !admin.Autorise("n'importe:quoi") {
		t.Error("le joker doit tout autoriser")
	}

	magasin, err := OuvrirMagasinCles(filepath.Join(t.TempDir(), NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	_, cree, err := magasin.Creer("Test", []string{"users:read"}, 0, moment)
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	modifie, err := magasin.DefinirPermissions(cree.ID, []string{"audit:read", "inconnue"})
	if err != nil {
		t.Fatalf("définition: %v", err)
	}
	if modifie == nil || len(modifie.Permissions) != 1 || modifie.Permissions[0] != "audit:read" {
		t.Fatalf("permissions = %+v", modifie)
	}
	if vide, err := magasin.DefinirPermissions(cree.ID, nil); err != nil || vide == nil ||
		len(vide.Permissions) != 0 {
		t.Errorf("permissions vidées = %+v (%v)", vide, err)
	}
	if obtenu, err := magasin.DefinirPermissions("inconnu", []string{"*"}); err != nil || obtenu != nil {
		t.Errorf("clé inconnue = %+v (%v)", obtenu, err)
	}
}

// TestMagasinClesRelitPython vérifie qu'un `api_keys.json` écrit par Python
// (clé et champs identiques) est utilisable tel quel.
func TestMagasinClesRelitPython(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichierCles)
	contenu := `[
  {
    "id": "ak_0123456789ab",
    "name": "Python",
    "key_hash": "` + empreinteCle("edusync_laclef") + `",
    "prefix": "edusync_lacl",
    "permissions": ["users:read"],
    "created_at": "2026-10-17T12:00:00+00:00",
    "expires_at": null,
    "last_used_at": null,
    "active": true
  }
]`
	if err := os.WriteFile(chemin, []byte(contenu), 0o600); err != nil {
		t.Fatalf("écriture: %v", err)
	}
	magasin, err := OuvrirMagasinCles(chemin)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	// Le préfixe Python fait 12 caractères : la clé complète doit être
	// acceptée telle quelle.
	info := magasin.Valider("edusync_laclef", time.Now().UTC())
	if info == nil || info.ID != "ak_0123456789ab" {
		t.Fatalf("validation = %+v", info)
	}
	if !info.Autorise("users:read") || info.Autorise("users:write") {
		t.Errorf("permissions = %v", info.Permissions)
	}
}

// TestMagasinClesTolerant vérifie qu'un fichier illisible ne bloque jamais.
func TestMagasinClesTolerant(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichierCles)
	if err := os.WriteFile(chemin, []byte("pas du json"), 0o600); err != nil {
		t.Fatalf("écriture: %v", err)
	}
	magasin, err := OuvrirMagasinCles(chemin)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if liste := magasin.Lister(true, time.Now().UTC()); len(liste) != 0 {
		t.Errorf("liste = %d", len(liste))
	}
	if magasin.Chemin() != chemin {
		t.Errorf("chemin = %q", magasin.Chemin())
	}
	if err := magasin.Enregistrer(); err != nil {
		t.Errorf("enregistrement: %v", err)
	}
}
