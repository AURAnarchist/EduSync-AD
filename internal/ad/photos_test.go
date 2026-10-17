package ad

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// ecrireJPEGTemp écrit un JPEG uni de dimensions données dans un dossier
// temporaire et retourne son chemin.
func ecrireJPEGTemp(t *testing.T, largeur, hauteur int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, largeur, hauteur))
	for y := 0; y < hauteur; y++ {
		for x := 0; x < largeur; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	chemin := filepath.Join(t.TempDir(), "photo.jpg")
	f, err := os.Create(chemin)
	if err != nil {
		t.Fatalf("création du fichier test: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encodage du JPEG test: %v", err)
	}
	return chemin
}

// TestPhotoTypeValide vérifie les types de photo supportés.
func TestPhotoTypeValide(t *testing.T) {
	cas := []struct {
		typ    PhotoType
		valide bool
	}{
		{PhotoTypeVignette, true},
		{PhotoTypePasseport, true},
		{PhotoType("inconnu"), false},
	}
	for _, c := range cas {
		if c.typ.Valide() != c.valide {
			t.Errorf("PhotoType %q.Valide() = %v, attendu %v", c.typ, !c.valide, c.valide)
		}
	}
}

// TestPhotoParamsValidate vérifie la validation des paramètres.
func TestPhotoParamsValidate(t *testing.T) {
	// DN manquant.
	p := &PhotoParams{Chemin: "/photos/jean.jpg"}
	if err := p.Validate(); !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Type invalide.
	p = &PhotoParams{UserDN: "CN=Test,OU=Users,DC=X", Type: "invalide", Chemin: "/photos/jean.jpg"}
	if err := p.Validate(); !errors.Is(err, ErrPhotoTypeInvalide) {
		t.Errorf("Validate type invalide: err=%v, attendu ErrPhotoTypeInvalide", err)
	}

	// Chemin vide.
	p = &PhotoParams{UserDN: "CN=Test,OU=Users,DC=X", Type: PhotoTypeVignette, Chemin: ""}
	if err := p.Validate(); !errors.Is(err, ErrCheminPhotoManquant) {
		t.Errorf("Validate chemin vide: err=%v, attendu ErrCheminPhotoManquant", err)
	}

	// Taille annoncée trop grande.
	p = &PhotoParams{UserDN: "CN=Test,OU=Users,DC=X", Type: PhotoTypeVignette, Chemin: "/photos/jean.jpg", Taille: TailleMaxPhoto + 1}
	if err := p.Validate(); !errors.Is(err, ErrPhotoTropGrosse) {
		t.Errorf("Validate taille trop grande: err=%v, attendu ErrPhotoTropGrosse", err)
	}

	// Valide complet.
	p = &PhotoParams{UserDN: "CN=Test,OU=Users,DC=X", Type: PhotoTypeVignette, Chemin: "/photos/jean.jpg", Taille: 150 * 1024}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate complet: err=%v, attendu nil", err)
	}
}

// TestPhotoBulkParamsValidate vérifie la validation en masse.
func TestPhotoBulkParamsValidate(t *testing.T) {
	// Aucun DN ni OU.
	p := &PhotoBulkParams{}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans DN ni OU devrait échouer")
	}

	// Sans dossier.
	p = &PhotoBulkParams{OU: "OU=Users,DC=X", Type: PhotoTypeVignette}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans dossier devrait échouer")
	}

	// Avec OU seulement.
	p = &PhotoBulkParams{OU: "OU=Users,DC=X", Type: PhotoTypeVignette, Dossier: "/photos"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec OU: err=%v, attendu nil", err)
	}

	// Avec UserDNs seulement.
	p = &PhotoBulkParams{UserDNs: []string{"CN=Test,OU=Users,DC=X"}, Type: PhotoTypeVignette, Dossier: "/photos"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec UserDNs: err=%v, attendu nil", err)
	}
}

// TestLireFichierPhoto vérifie la lecture d'un fichier depuis le disque.
func TestLireFichierPhoto(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "donnees.bin")
	contenu := []byte{0x01, 0x02, 0xFF, 0x00, 0x42}
	if err := os.WriteFile(chemin, contenu, 0o600); err != nil {
		t.Fatalf("écriture du fichier test: %v", err)
	}

	obtenu, err := lireFichierPhoto(chemin)
	if err != nil {
		t.Fatalf("lireFichierPhoto: %v", err)
	}
	if string(obtenu) != string(contenu) {
		t.Errorf("contenu = %v, attendu %v", obtenu, contenu)
	}

	if _, err := lireFichierPhoto(filepath.Join(t.TempDir(), "absent.jpg")); err == nil {
		t.Error("lireFichierPhoto sur fichier absent devrait échouer")
	}
}

// TestTailleFichier vérifie la taille d'un fichier.
func TestTailleFichier(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "donnees.bin")
	if err := os.WriteFile(chemin, make([]byte, 1234), 0o600); err != nil {
		t.Fatalf("écriture du fichier test: %v", err)
	}

	taille, err := tailleFichier(chemin)
	if err != nil {
		t.Fatalf("tailleFichier: %v", err)
	}
	if taille != 1234 {
		t.Errorf("taille = %d, attendu 1234", taille)
	}

	if _, err := tailleFichier(filepath.Join(t.TempDir(), "absent.jpg")); err == nil {
		t.Error("tailleFichier sur fichier absent devrait échouer")
	}
}

// TestRecadrerRatio vérifie le recadrage au format 3:4.
func TestRecadrerRatio(t *testing.T) {
	// Image carrée en entrée : la sortie doit être au format 3:4.
	source := image.NewRGBA(image.Rect(0, 0, 400, 400))
	obtenu := recadrerRatio(source, 96, 128)
	if obtenu.Bounds().Dx() != 96 || obtenu.Bounds().Dy() != 128 {
		t.Errorf("dimensions = %dx%d, attendu 96x128", obtenu.Bounds().Dx(), obtenu.Bounds().Dy())
	}

	// Image très large : la largeur ne doit pas déborder.
	obtenu = recadrerRatio(image.NewRGBA(image.Rect(0, 0, 800, 200)), 96, 128)
	if obtenu.Bounds().Dx() != 96 || obtenu.Bounds().Dy() != 128 {
		t.Errorf("dimensions (large) = %dx%d, attendu 96x128", obtenu.Bounds().Dx(), obtenu.Bounds().Dy())
	}

	// Image très haute : la hauteur ne doit pas déborder.
	obtenu = recadrerRatio(image.NewRGBA(image.Rect(0, 0, 200, 800)), 96, 128)
	if obtenu.Bounds().Dx() != 96 || obtenu.Bounds().Dy() != 128 {
		t.Errorf("dimensions (haut) = %dx%d, attendu 96x128", obtenu.Bounds().Dx(), obtenu.Bounds().Dy())
	}
}

// TestPreparerImageJPEG vérifie le recadrage et le ré-encodage JPEG.
func TestPreparerImageJPEG(t *testing.T) {
	chemin := ecrireJPEGTemp(t, 400, 400)
	donnees, err := lireFichierPhoto(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}

	prete, err := preparerImageJPEG(donnees, photoVignetteLargeur, photoVignetteHauteur)
	if err != nil {
		t.Fatalf("preparerImageJPEG: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(prete))
	if err != nil {
		t.Fatalf("l'image préparée n'est pas un JPEG valide: %v", err)
	}
	if img.Bounds().Dx() != photoVignetteLargeur || img.Bounds().Dy() != photoVignetteHauteur {
		t.Errorf("dimensions = %dx%d, attendu %dx%d",
			img.Bounds().Dx(), img.Bounds().Dy(), photoVignetteLargeur, photoVignetteHauteur)
	}

	if _, err := preparerImageJPEG([]byte("pas une image"), 96, 128); !errors.Is(err, ErrPhotoIllisible) {
		t.Errorf("données non-JPEG: err=%v, attendu ErrPhotoIllisible", err)
	}
}

// TestPreparerPhotoFichier vérifie la préparation complète depuis un fichier.
func TestPreparerPhotoFichier(t *testing.T) {
	chemin := ecrireJPEGTemp(t, 640, 480)

	// Photo valide.
	donnees, taille, err := preparerPhotoFichier(PhotoParams{Type: PhotoTypePasseport, Chemin: chemin})
	if err != nil {
		t.Fatalf("preparerPhotoFichier: %v", err)
	}
	if int64(len(donnees)) != taille {
		t.Errorf("taille annoncée %d != %d octets", taille, len(donnees))
	}
	img, err := jpeg.Decode(bytes.NewReader(donnees))
	if err != nil {
		t.Fatalf("image préparée invalide: %v", err)
	}
	if img.Bounds().Dx() != photoPasseportLargeur || img.Bounds().Dy() != photoPasseportHauteur {
		t.Errorf("dimensions passeport = %dx%d, attendu %dx%d",
			img.Bounds().Dx(), img.Bounds().Dy(), photoPasseportLargeur, photoPasseportHauteur)
	}

	// Fichier trop volumineux.
	gros := filepath.Join(t.TempDir(), "gros.jpg")
	if err := os.WriteFile(gros, make([]byte, TailleMaxPhoto+1), 0o600); err != nil {
		t.Fatalf("écriture du gros fichier: %v", err)
	}
	if _, _, err := preparerPhotoFichier(PhotoParams{Type: PhotoTypeVignette, Chemin: gros}); !errors.Is(err, ErrPhotoTropGrosse) {
		t.Errorf("fichier trop gros: err=%v, attendu ErrPhotoTropGrosse", err)
	}

	// Fichier absent.
	if _, _, err := preparerPhotoFichier(PhotoParams{Type: PhotoTypeVignette, Chemin: filepath.Join(t.TempDir(), "absent.jpg")}); err == nil {
		t.Error("fichier absent devrait échouer")
	}
}

// TestAttributPhoto vérifie le choix de l'attribut LDAP selon le type.
func TestAttributPhoto(t *testing.T) {
	if got := attributPhoto(PhotoTypeVignette); got != "thumbnailPhoto" {
		t.Errorf("attributPhoto(vignette) = %q, attendu thumbnailPhoto", got)
	}
	if got := attributPhoto(PhotoTypePasseport); got != "jpegPhoto" {
		t.Errorf("attributPhoto(passeport) = %q, attendu jpegPhoto", got)
	}
}
