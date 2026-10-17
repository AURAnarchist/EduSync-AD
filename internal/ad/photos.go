// Package ad fournit la gestion des photos d'identité (M12 de la roadmap).
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
//
// Les photos sont stockées dans l'attribut LDAP thumbnailPhoto (vignette
// affichée par l'explorateur Windows) ou jpegPhoto (photo de type passeport
// utilisée par les étiquettes et le trombinoscope). Avant écriture, l'image est
// recadrée au format 3:4, redimensionnée puis ré-encodée en JPEG, et sa taille
// est limitée à 200 Ko.
package ad

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // enregistre le décodeur GIF pour image.Decode
	"image/jpeg"
	_ "image/png" // enregistre le décodeur PNG pour image.Decode
	"os"
	"path/filepath"

	"github.com/go-ldap/ldap/v3"
)

// PhotoType définit le type de photo.
type PhotoType string

const (
	// PhotoTypeVignette photo de petite taille pour la vignette/explorateur.
	PhotoTypeVignette PhotoType = "vignette"
	// PhotoTypePasseport photo de type passeport pour les étiquettes/trombinoscope.
	PhotoTypePasseport PhotoType = "passeport"
)

const (
	// TailleMaxPhoto est la taille maximale acceptée pour une photo (200 Ko).
	TailleMaxPhoto = 200 * 1024
	// qualiteJPEG est la qualité d'encodage JPEG des photos préparées.
	qualiteJPEG = 85
	// photoVignetteLargeur et photoVignetteHauteur définissent la taille de la
	// vignette recadrée au format 3:4.
	photoVignetteLargeur = 96
	photoVignetteHauteur = 128
	// photoPasseportLargeur et photoPasseportHauteur définissent la taille de la
	// photo passeport recadrée au format 3:4.
	photoPasseportLargeur = 300
	photoPasseportHauteur = 400
)

// Valide vérifie que le type de photo est connu.
func (t PhotoType) Valide() bool {
	switch t {
	case PhotoTypeVignette, PhotoTypePasseport:
		return true
	}
	return false
}

// Sentinelles d'erreur pour les photos.
// ErrUserDNManquant est définie dans profiles.go (même package).
var (
	// ErrPhotoTypeInvalide est retournée quand le type de photo est inconnu.
	ErrPhotoTypeInvalide = errors.New("ad: type de photo invalide")
	// ErrCheminPhotoManquant est retournée quand le chemin est obligatoire mais absent.
	ErrCheminPhotoManquant = errors.New("ad: chemin de photo obligatoire")
	// ErrPhotoTropGrosse est retournée quand la photo dépasse la taille maximale.
	ErrPhotoTropGrosse = errors.New("ad: photo trop volumineuse (max 200 Ko)")
	// ErrPhotoIllisible est retournée quand le fichier n'est pas un JPEG exploitable.
	ErrPhotoIllisible = errors.New("ad: photo illisible (JPEG attendu)")
)

// PhotoParams décrit les paramètres de définition d'une photo.
type PhotoParams struct {
	UserDN string    // DN de l'utilisateur cible
	Type   PhotoType // type de photo (vignette, passeport)
	Chemin string    // chemin du fichier photo sur le disque
	Taille int64     // taille connue du fichier (0 = inconnue, relue sur le disque)
}

// Validate vérifie la cohérence des paramètres.
func (p *PhotoParams) Validate() error {
	if p.UserDN == "" {
		return ErrUserDNManquant
	}
	if !p.Type.Valide() {
		return ErrPhotoTypeInvalide
	}
	if p.Chemin == "" {
		return ErrCheminPhotoManquant
	}
	if p.Taille > TailleMaxPhoto {
		return ErrPhotoTropGrosse
	}
	return nil
}

// PhotoResult décrit le résultat d'une opération de photo.
type PhotoResult struct {
	UserDN   string    // DN de l'utilisateur
	Type     PhotoType // type appliqué
	Chemin   string    // chemin du fichier photo
	Taille   int64     // taille en octets
	Statut   string    // "succes" | "echec" | "skip"
	Detaille string    // détail / message d'erreur
}

// attributPhoto retourne l'attribut LDAP cible selon le type de photo.
func attributPhoto(t PhotoType) string {
	if t == PhotoTypePasseport {
		return "jpegPhoto"
	}
	return "thumbnailPhoto"
}

// dimensionsCible retourne les dimensions de recadrage 3:4 pour un type donné.
func dimensionsCible(t PhotoType) (largeur, hauteur int) {
	if t == PhotoTypePasseport {
		return photoPasseportLargeur, photoPasseportHauteur
	}
	return photoVignetteLargeur, photoVignetteHauteur
}

// lireFichierPhoto lit intégralement un fichier photo depuis le disque.
func lireFichierPhoto(chemin string) ([]byte, error) {
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture de %s: %w", chemin, err)
	}
	return donnees, nil
}

// tailleFichier retourne la taille en octets d'un fichier photo.
func tailleFichier(chemin string) (int64, error) {
	info, err := os.Stat(chemin)
	if err != nil {
		return 0, fmt.Errorf("ad: stat de %s: %w", chemin, err)
	}
	return info.Size(), nil
}

// preparerPhotoFichier lit, valide (taille + format) puis prépare la photo
// avant écriture dans l'annuaire. Elle retourne les octets encodés en JPEG
// ainsi que leur taille finale.
func preparerPhotoFichier(params PhotoParams) ([]byte, int64, error) {
	donnees, err := lireFichierPhoto(params.Chemin)
	if err != nil {
		return nil, 0, err
	}
	if len(donnees) > TailleMaxPhoto {
		return nil, int64(len(donnees)), ErrPhotoTropGrosse
	}

	largeur, hauteur := dimensionsCible(params.Type)
	prete, err := preparerImageJPEG(donnees, largeur, hauteur)
	if err != nil {
		return nil, int64(len(donnees)), err
	}
	return prete, int64(len(prete)), nil
}

// preparerImageJPEG décode une image JPEG, la recadre au ratio largeur:hauteur
// cible (centré), la redimensionne puis la ré-encode en JPEG.
func preparerImageJPEG(donnees []byte, largeur, hauteur int) ([]byte, error) {
	source, err := jpeg.Decode(bytes.NewReader(donnees))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPhotoIllisible, err)
	}

	recadree := recadrerRatio(source, largeur, hauteur)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, recadree, &jpeg.Options{Quality: qualiteJPEG}); err != nil {
		return nil, fmt.Errorf("ad: encodage JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// PreparerPhotoJPEG lit un fichier photo (JPEG, PNG ou GIF), le recadre au
// ratio 3:4 du type demandé et retourne les octets JPEG normalisés ainsi que
// leurs dimensions. Utilisé pour la photo de profil cloud (M19) : la photo
// déposée dans Entra ID suit le même traitement que celle écrite dans l'AD.
func PreparerPhotoJPEG(chemin string, t PhotoType) ([]byte, int, int, error) {
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ad: lecture de %s: %w", chemin, err)
	}
	source, _, err := image.Decode(bytes.NewReader(donnees))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: %v", ErrPhotoIllisible, err)
	}
	largeur, hauteur := dimensionsCible(t)
	recadree := recadrerRatio(source, largeur, hauteur)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, recadree, &jpeg.Options{Quality: qualiteJPEG}); err != nil {
		return nil, 0, 0, fmt.Errorf("ad: encodage JPEG: %w", err)
	}
	return buf.Bytes(), largeur, hauteur, nil
}

// recadrerRatio recadre l'image source au ratio cible (centré) puis
// l'échantillonne aux dimensions demandées. L'échantillonnage utilise le pixel
// source le plus proche, ce qui suffit pour une vignette et évite toute
// dépendance externe.
func recadrerRatio(source image.Image, largeur, hauteur int) *image.RGBA {
	b := source.Bounds()
	largeurSource, hauteurSource := float64(b.Dx()), float64(b.Dy())

	cadreLargeur, cadreHauteur := largeurSource, hauteurSource
	if largeurSource/hauteurSource > float64(largeur)/float64(hauteur) {
		// Image trop large : on rogne les côtés.
		cadreLargeur = hauteurSource * float64(largeur) / float64(hauteur)
	} else {
		// Image trop haute : on rogne le haut et le bas.
		cadreHauteur = largeurSource * float64(hauteur) / float64(largeur)
	}
	offsetX := (largeurSource - cadreLargeur) / 2
	offsetY := (hauteurSource - cadreHauteur) / 2

	dest := image.NewRGBA(image.Rect(0, 0, largeur, hauteur))
	for y := 0; y < hauteur; y++ {
		sy := int(offsetY + (float64(y)+0.5)*cadreHauteur/float64(hauteur))
		sy = borner(sy, 0, b.Dy()-1)
		for x := 0; x < largeur; x++ {
			sx := int(offsetX + (float64(x)+0.5)*cadreLargeur/float64(largeur))
			sx = borner(sx, 0, b.Dx()-1)
			dest.Set(x, y, source.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dest
}

// borner contraint une valeur dans l'intervalle [min, max].
func borner(valeur, min, max int) int {
	if valeur < min {
		return min
	}
	if valeur > max {
		return max
	}
	return valeur
}

// PhotoSimulate simule l'application d'une photo (vérifie l'existence du compte
// et du fichier, sans modifier l'annuaire).
func (p *Pool) PhotoSimulate(ctx context.Context, params PhotoParams) (*PhotoResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &PhotoResult{
			UserDN:   params.UserDN,
			Type:     params.Type,
			Chemin:   params.Chemin,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	taille, err := tailleFichier(params.Chemin)
	if err != nil {
		return &PhotoResult{
			UserDN:   u.DN,
			Type:     params.Type,
			Chemin:   params.Chemin,
			Statut:   "echec",
			Detaille: fmt.Sprintf("fichier introuvable: %v", err),
		}, nil
	}
	if taille > TailleMaxPhoto {
		return &PhotoResult{
			UserDN:   u.DN,
			Type:     params.Type,
			Chemin:   params.Chemin,
			Taille:   taille,
			Statut:   "echec",
			Detaille: ErrPhotoTropGrosse.Error(),
		}, nil
	}

	return &PhotoResult{
		UserDN:   u.DN,
		Type:     params.Type,
		Chemin:   params.Chemin,
		Taille:   taille,
		Statut:   "succes",
		Detaille: fmt.Sprintf("photo %s à appliquer pour %s (%s)", params.Type, u.SAMAccountName, FormateTaille(taille)),
	}, nil
}

// PhotoExecute applique réellement la photo (écrit thumbnailPhoto ou
// jpegPhoto après recadrage 3:4 et ré-encodage JPEG).
func (p *Pool) PhotoExecute(ctx context.Context, params PhotoParams) (*PhotoResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.PhotoSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation échouée: %s", sim.Detaille)
	}

	// Préparation de l'image (recadrage 3:4, redimensionnement, encodage).
	donnees, taille, err := preparerPhotoFichier(params)
	if err != nil {
		return &PhotoResult{
			UserDN:   sim.UserDN,
			Type:     params.Type,
			Chemin:   params.Chemin,
			Taille:   taille,
			Statut:   "echec",
			Detaille: err.Error(),
		}, nil
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	attribut := attributPhoto(params.Type)
	modReq := ldap.NewModifyRequest(params.UserDN, nil)
	// Les attributs photo sont binaires : la valeur est transmise telle quelle
	// avec le tag ASN.1 OCTET STRING (go-ldap encode les []string en octets).
	modReq.Replace(attribut, []string{string(donnees)})
	if err := conn.Modify(modReq); err != nil {
		return &PhotoResult{
			UserDN:   sim.UserDN,
			Type:     params.Type,
			Chemin:   params.Chemin,
			Taille:   taille,
			Statut:   "echec",
			Detaille: fmt.Sprintf("écriture %s: %v", attribut, err),
		}, nil
	}

	return &PhotoResult{
		UserDN:   sim.UserDN,
		Type:     params.Type,
		Chemin:   params.Chemin,
		Taille:   taille,
		Statut:   "succes",
		Detaille: fmt.Sprintf("photo %s appliquée pour %s (%s)", params.Type, sim.UserDN, FormateTaille(taille)),
	}, nil
}

// PhotoBulkParams décrit une opération de photos en masse.
type PhotoBulkParams struct {
	UserDNs []string  // DN des utilisateurs (vide = tous de l'OU)
	OU      string    // OU pour sélectionner tous les utilisateurs
	Type    PhotoType // type de photo
	Dossier string    // dossier contenant les photos (nommage <sAMAccountName>.jpg)
}

// Validate vérifie les paramètres en masse.
func (p *PhotoBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	if !p.Type.Valide() {
		return ErrPhotoTypeInvalide
	}
	if p.Dossier == "" {
		return errors.New("ad: dossier de photos obligatoire")
	}
	return nil
}

// PhotoBulkExecute applique la photo à plusieurs utilisateurs. Le fichier est
// cherché dans Dossier sous le nom <sAMAccountName>.jpg.
func (p *Pool) PhotoBulkExecute(ctx context.Context, params PhotoBulkParams) ([]PhotoResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	var users []*User
	var err error

	if len(params.UserDNs) > 0 {
		for _, dn := range params.UserDNs {
			u, e := p.UserGet(ctx, dn)
			if e == nil {
				users = append(users, u)
			}
		}
	} else {
		users, err = p.UserSearch(ctx, params.OU,
			"(&(objectClass=user)(objectCategory=person))",
			[]string{"dn", "sAMAccountName", "displayName"}, 0)
		if err != nil {
			return nil, fmt.Errorf("recherche utilisateurs dans %s: %w", params.OU, err)
		}
	}

	results := make([]PhotoResult, 0, len(users))
	for _, u := range users {
		chemin := filepath.Join(params.Dossier, u.SAMAccountName+".jpg")
		userParams := PhotoParams{
			UserDN: u.DN,
			Type:   params.Type,
			Chemin: chemin,
		}
		result, err := p.PhotoExecute(ctx, userParams)
		if err != nil {
			result = &PhotoResult{
				UserDN:   u.DN,
				Type:     params.Type,
				Chemin:   chemin,
				Statut:   "echec",
				Detaille: err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}
