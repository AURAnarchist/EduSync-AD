// Package updater implémente la vérification et l'installation des mises à
// jour depuis les releases GitHub.
//
// Chaque opération suit le pattern : vérifier → télécharger → vérifier
// l'intégrité (SHA-256) → préparer l'installation → relancer. Le lancement
// effectif (finalize) n'est déclenché qu'après confirmation explicite de
// l'utilisateur, jamais pendant le téléchargement en arrière-plan.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// assetAPI décrit un artefact d'une release GitHub.
type assetAPI struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// releaseAPI décrit la réponse de l'API GitHub releases/latest.
type releaseAPI struct {
	TagName string     `json:"tag_name"`
	Body    string     `json:"body"`
	Assets  []assetAPI `json:"assets"`
}

// UpdateInfo décrit une mise à jour disponible.
type UpdateInfo struct {
	Version      string // ex: "v0.1.0-alpha.2"
	Current      string // version courante
	DownloadURL  string // URL de l'artefact pour la plateforme courante
	ChecksumURL  string // URL du fichier .sha256 associé
	ReleaseNotes string // notes de version (markdown)
}

// checkForUpdateImpl permet de simuler l'OS dans les tests.
// Par défaut retourne runtime.GOOS.
var checkForUpdateOS = func() string { return runtime.GOOS }

// cacheDirFunc permet de simuler le dossier de cache dans les tests.
// Par défaut retourne le dossier utilisateur standard.
var cacheDirFunc = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "EduSyncAD", "updates"), nil
}

// CheckForUpdate interroge l'API GitHub releases/latest et retourne les
// infos de mise à jour si une version plus récente existe.
//
// Retourne (nil, nil) si l'application est à jour ou en cas d'erreur réseau
// (dégradation douce : l'absence de mise à jour n'est pas bloquante).
func CheckForUpdate(ctx context.Context, apiURL, currentVersion string) (*UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "EduSync-AD-Updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Erreur réseau : dégradation douce.
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var release releaseAPI
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, nil
	}

	if !versionSuperieure(release.TagName, currentVersion) {
		return nil, nil
	}

	dl, cs := selectionnerArtefact(release.Assets, checkForUpdateOS())

	return &UpdateInfo{
		Version:      release.TagName,
		Current:      currentVersion,
		DownloadURL:  dl,
		ChecksumURL:  cs,
		ReleaseNotes: release.Body,
	}, nil
}

// selectionnerArtefact choisit l'artefact de téléchargement et son checksum
// selon le système d'exploitation.
func selectionnerArtefact(assets []assetAPI, goos string) (downloadURL, checksumURL string) {
	suffixes, ok := map[string][]string{
		"windows": {"EduSyncAD-Setup.exe", "EduSyncAD-Setup.exe.sha256"},
		"linux":   {"edusync-ad-linux-" + runtime.GOARCH + ".tar.gz", "edusync-ad-linux-" + runtime.GOARCH + ".tar.gz.sha256"},
	}[goos]
	if !ok || len(suffixes) != 2 {
		return "", ""
	}
	for _, a := range assets {
		if a.Name == suffixes[0] {
			downloadURL = a.BrowserDownloadURL
		}
		if a.Name == suffixes[1] {
			checksumURL = a.BrowserDownloadURL
		}
		if downloadURL != "" && checksumURL != "" {
			break
		}
	}
	return downloadURL, checksumURL
}

// DownloadAndInstall télécharge le paquet, vérifie son hash SHA-256 si un
// checksum_url est fourni, et l'enregistre dans le dossier de cache.
//
// Retourne (ok, finalize, err). finalize, si non nil, doit être appelé par
// l'appelant uniquement après confirmation explicite de l'utilisateur : il
// lance l'installateur (Windows) ou remplace le binaire et relance (Linux).
func DownloadAndInstall(ctx context.Context, downloadURL, checksumURL string) (bool, func() error, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return false, nil, err
	}
	req.Header.Set("User-Agent", "EduSync-AD-Updater")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, nil, nil // dégradation douce
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil, nil
	}

	cacheDir, err := cacheDirFunc()
	if err != nil {
		return false, nil, fmt.Errorf("updater: dossier de cache: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return false, nil, fmt.Errorf("updater: création dossier de cache: %w", err)
	}

	updateDir, err := os.MkdirTemp(cacheDir, "edusync_update_*")
	if err != nil {
		return false, nil, fmt.Errorf("updater: création dossier temporaire: %w", err)
	}

	nomFichier := filepath.Base(downloadURL)
	if nomFichier == "." || nomFichier == "/" || nomFichier == "" {
		nomFichier = "mise-a-jour"
	}
	destPath := filepath.Join(updateDir, nomFichier)

	out, err := os.Create(destPath)
	if err != nil {
		os.RemoveAll(updateDir)
		return false, nil, fmt.Errorf("updater: création fichier destination: %w", err)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.RemoveAll(updateDir)
		return false, nil, fmt.Errorf("updater: téléchargement: %w", err)
	}
	out.Close()

	// Vérification du checksum si fourni.
	if checksumURL != "" {
		if !verifierChecksum(destPath, checksumURL) {
			os.RemoveAll(updateDir)
			return false, nil, nil
		}
	}

	// Donner les droits d'exécution (nécessaire sur Linux pour le remplacement du binaire).
	if err := os.Chmod(destPath, 0o755); err != nil {
		os.RemoveAll(updateDir)
		return false, nil, fmt.Errorf("updater: droits d'exécution: %w", err)
	}

	finalize := func() error {
		return finaliserInstallation(updateDir, nomFichier)
	}
	return true, finalize, nil
}

func verifierChecksum(filePath, checksumURL string) bool {
	req, err := http.NewRequest(http.MethodGet, checksumURL, nil)
	if err != nil {
		return true // dégradation douce
	}
	req.Header.Set("User-Agent", "EduSync-AD-Updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return true
	}
	attendu := strings.TrimSpace(string(content))
	attendu = strings.Split(attendu, " ")[0] // format "hash  nom" ou juste "hash"
	if attendu == "" {
		return true
	}

	actual, err := sha256OfFile(filePath)
	if err != nil {
		return false
	}
	return strings.EqualFold(actual, attendu)
}

func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func finaliserInstallation(updateDir, nomFichier string) error {
	switch runtime.GOOS {
	case "windows":
		return finaliserWindows(updateDir, nomFichier)
	case "linux":
		return finaliserLinux(updateDir, nomFichier)
	default:
		return fmt.Errorf("updater: finalisation non supportée sur %s", runtime.GOOS)
	}
}

func finaliserWindows(updateDir, nomFichier string) error {
	installeur := filepath.Join(updateDir, nomFichier)
	bat := filepath.Join(updateDir, "run_installer.bat")
	contenu := fmt.Sprintf(
		"@echo off\ntimeout /t 2 /nobreak > nul\n\"%s\" /VERYSILENT /NORESTART /SUPPRESSMSGBOXES\nrmdir /S /Q \"%s\"\n",
		installeur, updateDir,
	)
	if err := os.WriteFile(bat, []byte(contenu), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe", "/c", bat)
	return cmd.Start()
}

func finaliserLinux(updateDir, nomFichier string) error {
	// Remplacer le binaire courant par le nouveau et se relancer.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	nouveau := filepath.Join(updateDir, nomFichier)
	// Sous Linux, rename peut remplacer un binaire en cours d'exécution
	// (l'inode reste valide pour le processus courant).
	if err := os.Rename(nouveau, exe); err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Start()
}

// parseVersion extrait Maj.Minor.Patch d'une version "vX.Y-Z...".
// Retourne ok=false si la version n'est pas parsable.
func parseVersion(v string) (maj, min, patch int, ok bool) {
	v = strings.TrimPrefix(v, "v")
	v = strings.SplitN(v, "-", 2)[0] // ôte le suffixe -alpha.1 etc.
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0, 0, 0, false
	}
	fmt.Sscanf(parts[0], "%d", &maj)
	fmt.Sscanf(parts[1], "%d", &min)
	fmt.Sscanf(parts[2], "%d", &patch)
	return maj, min, patch, true
}

// versionSuperieure retourne vrai si a est strictement supérieur à b.
func versionSuperieure(a, b string) bool {
	ma, mi, pa, oka := parseVersion(a)
	mb, mi2, pb, okb := parseVersion(b)
	if !oka || !okb {
		return false
	}
	if ma != mb {
		return ma > mb
	}
	if mi != mi2 {
		return mi > mi2
	}
	return pa > pb
}
