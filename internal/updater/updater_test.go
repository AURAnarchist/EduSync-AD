package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// nouveauServeurAPI simule l'API GitHub releases/latest.
func nouveauServeurAPI(t *testing.T, release releaseAPI) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// nouveauServeurFichier simule un serveur HTTP servant un fichier binaire.
func nouveauServeurFichier(t *testing.T, contenu []byte) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(contenu)))
		_, _ = w.Write(contenu)
	}))
	t.Cleanup(srv.Close)
	return srv, srv.URL
}

func TestCheckUpdateVersionDisponible(t *testing.T) {
	srv := nouveauServeurAPI(t, releaseAPI{
		TagName: "v0.1.0-alpha.2",
		Body:    "Notes de version alpha 2",
		Assets: []assetAPI{
			{Name: "EduSyncAD-Setup.exe", BrowserDownloadURL: "https://gh.example.com/setup.exe"},
			{Name: "EduSyncAD-Setup.exe.sha256", BrowserDownloadURL: "https://gh.example.com/setup.exe.sha256"},
			{Name: "edusync-ad-linux-amd64.tar.gz", BrowserDownloadURL: "https://gh.example.com/linux.tar.gz"},
			{Name: "edusync-ad-linux-amd64.tar.gz.sha256", BrowserDownloadURL: "https://gh.example.com/linux.tar.gz.sha256"},
		},
	})

	info, err := CheckForUpdate(context.Background(), srv.URL, "0.0.0-dev")
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info == nil {
		t.Fatal("attendu une mise à jour disponible, obtenu nil")
	}
	if info.Version != "v0.1.0-alpha.2" {
		t.Errorf("version = %q, attendu v0.1.0-alpha.2", info.Version)
	}
	if info.Current != "0.0.0-dev" {
		t.Errorf("current = %q, attendu 0.0.0-dev", info.Current)
	}
	// Le test tourne sur Linux → l'artefact linux doit être sélectionné.
	if !strings.Contains(info.DownloadURL, "linux.tar.gz") {
		t.Errorf("download_url = %q, attendu un artefact linux", info.DownloadURL)
	}
	if !strings.Contains(info.ChecksumURL, "sha256") {
		t.Errorf("checksum_url = %q, attendu un .sha256", info.ChecksumURL)
	}
	if info.ReleaseNotes != "Notes de version alpha 2" {
		t.Errorf("release_notes = %q", info.ReleaseNotes)
	}
}

func TestCheckUpdateDejaAJour(t *testing.T) {
	srv := nouveauServeurAPI(t, releaseAPI{
		TagName: "v0.0.0-dev",
		Assets:  []assetAPI{},
	})

	info, err := CheckForUpdate(context.Background(), srv.URL, "0.0.0-dev")
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info != nil {
		t.Errorf("attendu nil (déjà à jour), obtenu %+v", info)
	}
}

func TestCheckUpdateErreurReseauDegradationDouce(t *testing.T) {
	// Serveur qui refuse toute connexion → dégradation douce (nil, nil).
	info, err := CheckForUpdate(context.Background(), "http://127.0.0.1:1/invalid", "0.0.0-dev")
	if err != nil {
		t.Errorf("CheckForUpdate doit dégrader en douceur en cas d'erreur réseau, obtenu : %v", err)
	}
	if info != nil {
		t.Errorf("attendu nil en cas d'erreur réseau, obtenu %+v", info)
	}
}

func TestSelectionArtefact(t *testing.T) {
	assets := []assetAPI{
		{Name: "EduSyncAD-Setup.exe", BrowserDownloadURL: "https://gh.example.com/setup.exe"},
		{Name: "EduSyncAD-Setup.exe.sha256", BrowserDownloadURL: "https://gh.example.com/setup.sha256"},
		{Name: "edusync-ad-linux-amd64.tar.gz", BrowserDownloadURL: "https://gh.example.com/linux.tar.gz"},
		{Name: "edusync-ad-linux-amd64.tar.gz.sha256", BrowserDownloadURL: "https://gh.example.com/linux.sha256"},
		{Name: "EduSyncAD-linux.AppImage", BrowserDownloadURL: "https://gh.example.com/appimage"},
	}

	dl, cs := selectionnerArtefact(assets, "windows")
	if !strings.Contains(dl, "setup.exe") {
		t.Errorf("windows download_url = %q, attendu setup.exe", dl)
	}
	if !strings.Contains(cs, "sha256") {
		t.Errorf("windows checksum_url = %q, attendu .sha256", cs)
	}

	dl, cs = selectionnerArtefact(assets, "linux")
	if !strings.Contains(dl, "linux.tar.gz") {
		t.Errorf("linux download_url = %q, attendu linux.tar.gz", dl)
	}
	if !strings.Contains(cs, "sha256") {
		t.Errorf("linux checksum_url = %q, attendu .sha256", cs)
	}

	// Aucun artefact correspondant → URLs vides.
	dl, cs = selectionnerArtefact(assets, "darwin")
	if dl != "" || cs != "" {
		t.Errorf("darwin: attendu URLs vides, obtenu dl=%q cs=%q", dl, cs)
	}
}

func TestDownloadEtVerificationChecksumValide(t *testing.T) {
	donnees := []byte("contenu factice du binaire de mise à jour")
	checksum := hashMemoire(donnees)

	_, url := nouveauServeurFichier(t, donnees)
	_, urlChecksum := nouveauServeurFichier(t, []byte(checksum+"  fichier.tar.gz\n"))

	ok, _, err := DownloadAndInstall(context.Background(), url, urlChecksum)
	if err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}
	if !ok {
		t.Fatal("attendu succès, obtenu échec")
	}
}

func TestDownloadVerificationChecksumInvalide(t *testing.T) {
	donnees := []byte("contenu factice")
	mauvaisHash := "0000000000000000000000000000000000000000000000000000000000000000"

	_, url := nouveauServeurFichier(t, donnees)
	_, urlChecksum := nouveauServeurFichier(t, []byte(mauvaisHash+"\n"))

	ok, _, err := DownloadAndInstall(context.Background(), url, urlChecksum)
	if err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}
	if ok {
		t.Error("attendu échec (hash invalide), obtenu succès")
	}
}

func TestDownloadSansChecksumDegradation(t *testing.T) {
	donnees := []byte("contenu sans checksum")
	_, url := nouveauServeurFichier(t, donnees)

	ok, _, err := DownloadAndInstall(context.Background(), url, "")
	if err != nil {
		t.Fatalf("DownloadAndInstall sans checksum: %v", err)
	}
	if !ok {
		t.Error("attendu succès même sans checksum (dégradation douce)")
	}
}

func TestDownloadURIInexistante(t *testing.T) {
	ok, _, err := DownloadAndInstall(context.Background(), "http://127.0.0.1:1/inexistant", "")
	if err != nil {
		t.Fatalf("DownloadAndInstall URL inexistante: %v", err)
	}
	if ok {
		t.Error("attendu échec pour URL inexistante")
	}
}

func TestDownloadPersisteFichierDansCache(t *testing.T) {
	// Écraser cacheDirFunc pour que le test puisse vérifier le dossier utilisé.
	cacheDir, err := cacheDirTest()
	if err != nil {
		t.Fatalf("cacheDirTest: %v", err)
	}
	ancien := cacheDirFunc
	cacheDirFunc = func() (string, error) { return cacheDir, nil }
	defer func() { cacheDirFunc = ancien }()

	donnees := []byte("contenu à persister dans le cache")
	checksum := hashMemoire(donnees)

	_, url := nouveauServeurFichier(t, donnees)
	_, urlChecksum := nouveauServeurFichier(t, []byte(checksum+"\n"))

	ok, _, err := DownloadAndInstall(context.Background(), url, urlChecksum)
	if err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}
	if !ok {
		t.Fatal("attendu succès")
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("ReadDir cache: %v", err)
	}
	var trouve bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "edusync_update_") && e.IsDir() {
			sousEntries, err := os.ReadDir(filepath.Join(cacheDir, e.Name()))
			if err != nil {
				t.Fatalf("ReadDir dossier update: %v", err)
			}
			for _, se := range sousEntries {
				if !se.IsDir() {
					trouve = true
					contenu, err := os.ReadFile(filepath.Join(cacheDir, e.Name(), se.Name()))
					if err != nil {
						t.Fatalf("lecture fichier téléchargé: %v", err)
					}
					if string(contenu) != string(donnees) {
						t.Errorf("contenu du fichier téléchargé ne correspond pas")
					}
					break
				}
			}
			break
		}
	}
	if !trouve {
		t.Error("aucun fichier de mise à jour trouvé dans le cache")
	}
}

func cacheDirTest() (string, error) {
	dir := filepath.Join(os.TempDir(), "edusync-updater-test")
	_ = os.MkdirAll(dir, 0o700)
	return dir, nil
}

// hashMemoire calcule le SHA-256 d'une slice d'octets en mémoire.
func hashMemoire(donnees []byte) string {
	somme := sha256.Sum256(donnees)
	return hex.EncodeToString(somme[:])
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		v               string
		maj, min, patch int
		ok              bool
	}{
		{"v0.1.0-alpha.1", 0, 1, 0, true},
		{"1.17.1", 1, 17, 1, true},
		{"0.0.0-dev", 0, 0, 0, true},
		{"invalide", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, patch, ok := parseVersion(c.v)
		if ok != c.ok {
			t.Errorf("parseVersion(%q): ok=%v, attendu %v", c.v, ok, c.ok)
			continue
		}
		if ok && (maj != c.maj || min != c.min || patch != c.patch) {
			t.Errorf("parseVersion(%q): %d.%d.%d, attendu %d.%d.%d", c.v, maj, min, patch, c.maj, c.min, c.patch)
		}
	}
}

func TestVersionSuperieure(t *testing.T) {
	cases := []struct {
		a, b    string
		attendu bool
	}{
		{"v0.1.0", "v0.0.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.0.0", "v0.1.0", false},
		{"v1.17.1", "v1.17.0", true},
		{"v0.1.0-alpha.1", "v0.0.0", true},
		{"v0.0.0-dev", "v0.0.0", false},
	}
	for _, c := range cases {
		got := versionSuperieure(c.a, c.b)
		if got != c.attendu {
			t.Errorf("versionSuperieure(%q, %q) = %v, attendu %v", c.a, c.b, got, c.attendu)
		}
	}
}

func TestSelectionArtefactUtiliseRuntimeGOOSParDefaut(t *testing.T) {
	// Vérifie que la sélection par défaut utilise bien l'OS courant.
	dl, _ := selectionnerArtefact([]assetAPI{
		{Name: "edusync-ad-linux-amd64.tar.gz", BrowserDownloadURL: "https://gh.example.com/linux.tar.gz"},
	}, runtime.GOOS)
	if runtime.GOOS == "linux" && !strings.Contains(dl, "linux.tar.gz") {
		t.Errorf("download_url = %q, attendu linux.tar.gz sur %s", dl, runtime.GOOS)
	}
}
