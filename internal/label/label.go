// Package label implémente les étiquettes & trombinoscopes avancés (M24).
//
// Quatre apports par rapport à l'export v1 (M6) :
//
//  1. Éditeur de modèles d'étiquettes — positionnement libre des champs (en mm
//     depuis le coin haut-gauche de l'étiquette), police, corps, couleur,
//     alignement, QR code et photo ; chaque modèle est un document JSON
//     réutilisable, modifiable puis ré-enregistré.
//  2. Modèles pré-enregistrés — Avery L7160 (21 étiquettes) et L7163
//     (14 étiquettes), géométrie reprise de core/export.py pour rester
//     identique à l'impression, plus un badge 85 × 55 mm et une carte de
//     visite 85 × 54 mm.
//  3. Trombinoscope — grille photo + nom sur A4 ou A3, export PDF, photo AD
//     (jpegPhoto/thumbnailPhoto) ou emplacement réservé.
//  4. Envoi d'une étiquette par mail — voir le paquet internal/mailer.
//
// Le format de `label_templates.json` est **strictement identique** à celui de
// la version Python (src/edusync_ad/core/label_studio.py) : aller-retour
// vérifié dans les deux sens.
package label

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // enregistre le décodeur JPEG pour image.DecodeConfig
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/jung-kurt/gofpdf"

	"github.com/estemobs/EduSync-AD/internal/config"
)

const (
	// NomFichier est le nom du fichier des modèles personnalisés.
	NomFichier = "label_templates.json"
	// EpsMM est la tolérance d'arrondi pour les contrôles de débordement.
	EpsMM = 0.1
	// PoliceMinPT est la taille de police minimale lors de la réduction auto.
	PoliceMinPT = 4.0
)

// TaillePagesMM associe un nom de page à ses dimensions (mm).
var TaillePagesMM = map[string][2]float64{
	"A4": {210.0, 297.0},
	"A3": {297.0, 420.0},
}

var (
	reID      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	reCouleur = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	reNonID   = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// TypesElement catalogue les types d'éléments d'une étiquette.
var TypesElement = map[string]string{
	"texte":    "Champ (variable par compte)",
	"statique": "Texte fixe (logo, établissement…)",
	"photo":    "Photo d'identité",
	"qr":       "QR code",
}

// Alignements associe les noms français aux valeurs reportlab/gofpdf.
var Alignements = map[string]string{"gauche": "left", "centre": "center", "droite": "right"}

// Polices liste les polices « cœur » disponibles sans fichier externe.
var Polices = []string{
	"Helvetica",
	"Helvetica-Bold",
	"Helvetica-Oblique",
	"Times-Roman",
	"Times-Bold",
	"Courier",
}

// ChampsExport liste les champs disponibles pour l'export (CSV et étiquettes).
// Les clés sont identiques à core/export.py.
var ChampsExport = map[string]string{
	"identifiant":  "Identifiant",
	"nom_complet":  "Nom complet",
	"prenom":       "Prénom",
	"nom":          "Nom",
	"classe":       "Classe / OU",
	"mail":         "Adresse mail",
	"etat":         "État (actif/désactivé)",
	"mot_de_passe": "Mot de passe",
}

// ThemeCouleur décrit un thème de couleur d'étiquette.
type ThemeCouleur struct {
	Key      string
	Nom      string
	FondHex  string
	TexteHex string
}

// ThemesCouleur reprend LABEL_COLOR_THEMES de core/export.py.
var ThemesCouleur = map[string]ThemeCouleur{
	"bleu":   {Key: "bleu", Nom: "Bleu", FondHex: "#DCEBFC", TexteHex: "#1B4F91"},
	"vert":   {Key: "vert", Nom: "Vert", FondHex: "#E1F5E9", TexteHex: "#1E6B3C"},
	"jaune":  {Key: "jaune", Nom: "Jaune", FondHex: "#FEF6DC", TexteHex: "#8A6D1D"},
	"corail": {Key: "corail", Nom: "Corail", FondHex: "#FDE7E4", TexteHex: "#B23A2E"},
	"gris":   {Key: "gris", Nom: "Gris", FondHex: "#ECECEC", TexteHex: "#333333"},
}

// ThemeParDefaut est le thème utilisé par les modèles intégrés.
const ThemeParDefaut = "bleu"

// FormatAvery décrit la géométrie d'un format Avery (cf. core/export.py).
type FormatAvery struct {
	Key             string
	Nom             string
	LargeurMM       float64
	HauteurMM       float64
	Colonnes        int
	Lignes          int
	MargeGaucheMM   float64
	MargeHautMM     float64
	PasHorizontalMM float64
	PasVerticalMM   float64
}

// ParPlanche retourne le nombre d'étiquettes par planche.
func (f FormatAvery) ParPlanche() int { return f.Colonnes * f.Lignes }

// FormatsAvery reprend LABEL_FORMATS de core/export.py.
var FormatsAvery = map[string]FormatAvery{
	"avery_l7160": {
		Key: "avery_l7160", Nom: "Avery L7160 — 21 étiquettes/planche (63,5 × 38,1 mm)",
		LargeurMM: 63.5, HauteurMM: 38.1, Colonnes: 3, Lignes: 7,
		MargeGaucheMM: 7.5, MargeHautMM: 15.5, PasHorizontalMM: 66.0, PasVerticalMM: 38.1,
	},
	"avery_l7163": {
		Key: "avery_l7163", Nom: "Avery L7163 — 14 étiquettes/planche, format large (99,1 × 38,1 mm)",
		LargeurMM: 99.1, HauteurMM: 38.1, Colonnes: 2, Lignes: 7,
		MargeGaucheMM: 3.5, MargeHautMM: 15.2, PasHorizontalMM: 103.0, PasVerticalMM: 38.1,
	},
}

// Planche décrit une planche imprimable : page + grille d'étiquettes.
type Planche struct {
	Key             string
	Nom             string
	Page            string // "A4" | "A3"
	LargeurMM       float64
	HauteurMM       float64
	Colonnes        int
	Lignes          int
	MargeGaucheMM   float64
	MargeHautMM     float64
	PasHorizontalMM float64
	PasVerticalMM   float64
}

// ParPlanche retourne le nombre d'étiquettes par planche.
func (p Planche) ParPlanche() int { return p.Colonnes * p.Lignes }

// PageWidthMM retourne la largeur de page en mm.
func (p Planche) PageWidthMM() float64 { return TaillePagesMM[p.Page][0] }

// PageHeightMM retourne la hauteur de page en mm.
func (p Planche) PageHeightMM() float64 { return TaillePagesMM[p.Page][1] }

// LabelSizeMM retourne la taille d'une étiquette (largeur, hauteur) en mm.
func (p Planche) LabelSizeMM() (float64, float64) { return p.LargeurMM, p.HauteurMM }

// OriginLabel retourne le coin haut-gauche d'une étiquette (mm depuis le coin
// haut-gauche de la page).
func (p Planche) OriginLabel(row, col int) (float64, float64) {
	return p.MargeGaucheMM + float64(col)*p.PasHorizontalMM,
		p.MargeHautMM + float64(row)*p.PasVerticalMM
}

// Validate vérifie la cohérence de la planche (messages français, comme Python).
func (p Planche) Validate() []string {
	var erreurs []string
	if _, ok := TaillePagesMM[p.Page]; !ok {
		return []string{fmt.Sprintf("Planche « %s » : page « %s » inconnue (A4 ou A3).", p.Key, p.Page)}
	}
	if p.Colonnes < 1 || p.Lignes < 1 {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : la grille doit avoir au moins 1 colonne et 1 ligne.", p.Key))
	}
	if p.LargeurMM <= 0 || p.HauteurMM <= 0 {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : dimensions d'étiquette invalides.", p.Key))
	}
	if p.MargeGaucheMM < 0 || p.MargeHautMM < 0 {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : marges négatives.", p.Key))
	}
	if p.PasHorizontalMM+EpsMM < p.LargeurMM {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : pas horizontal < largeur d'étiquette (chevauchement).", p.Key))
	}
	if p.PasVerticalMM+EpsMM < p.HauteurMM {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : pas vertical < hauteur d'étiquette (chevauchement).", p.Key))
	}
	usedW := p.MargeGaucheMM + float64(p.Colonnes-1)*p.PasHorizontalMM + p.LargeurMM
	if usedW > p.PageWidthMM()+EpsMM {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : déborde en largeur (%.1f mm > %.0f mm).", p.Key, usedW, p.PageWidthMM()))
	}
	usedH := p.MargeHautMM + float64(p.Lignes-1)*p.PasVerticalMM + p.HauteurMM
	if usedH > p.PageHeightMM()+EpsMM {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » : déborde en hauteur (%.1f mm > %.0f mm).", p.Key, usedH, p.PageHeightMM()))
	}
	return erreurs
}

// Planches liste toutes les planches disponibles (Avery + badge + carte).
var Planches = construirePlanches()

func construirePlanches() map[string]*Planche {
	out := make(map[string]*Planche, len(FormatsAvery)+2)
	for cle, f := range FormatsAvery {
		out[cle] = &Planche{
			Key: cle, Nom: f.Nom, Page: "A4",
			LargeurMM: f.LargeurMM, HauteurMM: f.HauteurMM,
			Colonnes: f.Colonnes, Lignes: f.Lignes,
			MargeGaucheMM: f.MargeGaucheMM, MargeHautMM: f.MargeHautMM,
			PasHorizontalMM: f.PasHorizontalMM, PasVerticalMM: f.PasVerticalMM,
		}
	}
	out["badge_85x55"] = &Planche{
		Key: "badge_85x55", Nom: "Badge porte-badge — 85 × 55 mm (8 par planche)",
		Page: "A4", LargeurMM: 85.0, HauteurMM: 55.0, Colonnes: 2, Lignes: 4,
		MargeGaucheMM: 10.0, MargeHautMM: 15.0, PasHorizontalMM: 92.0, PasVerticalMM: 62.0,
	}
	out["carte_85x54"] = &Planche{
		Key: "carte_85x54", Nom: "Carte de visite — 85 × 54 mm (8 par planche)",
		Page: "A4", LargeurMM: 85.0, HauteurMM: 54.0, Colonnes: 2, Lignes: 4,
		MargeGaucheMM: 20.0, MargeHautMM: 20.0, PasHorizontalMM: 90.0, PasVerticalMM: 65.0,
	}
	return out
}

// ElementLabel est un élément positionné dans l'étiquette (coordonnées en mm,
// origine haut-gauche).
type ElementLabel struct {
	Kind     string  `json:"kind"`
	Field    string  `json:"field"` // clé de ChampsExport pour "texte" et "qr"
	Text     string  `json:"text"`  // valeur pour "statique"
	XMM      float64 `json:"x"`
	YMM      float64 `json:"y"`
	WMM      float64 `json:"w"`
	HMM      float64 `json:"h"`
	Font     string  `json:"font"`
	SizePT   float64 `json:"size"`
	ColorHex string  `json:"color"`
	Align    string  `json:"align"`
}

// ElementLabelDefaut retourne un élément texte par défaut (comme Python).
func ElementLabelDefaut() ElementLabel {
	return ElementLabel{
		Kind: "texte", Field: "nom_complet",
		XMM: 3.0, YMM: 3.0, WMM: 40.0, HMM: 9.0,
		Font: "Helvetica-Bold", SizePT: 11.0, ColorHex: "#1B4F91", Align: "centre",
	}
}

// Name retourne le libellé court utilisé dans les messages et l'UI.
func (e ElementLabel) Name() string {
	switch e.Kind {
	case "statique":
		if t := strings.TrimSpace(e.Text); t != "" {
			return t
		}
		return "texte fixe"
	case "photo":
		return "photo"
	case "qr":
		return "QR " + e.Field
	default:
		return e.Field
	}
}

// Value retourne la valeur affichée pour ce compte (vide si absente).
func (e ElementLabel) Value(user map[string]any) string {
	if e.Kind == "statique" {
		return e.Text
	}
	return valeurChaine(user[e.Field])
}

// Validate vérifie l'élément ; labelSize (optionnel) contrôle le débordement.
func (e ElementLabel) Validate(labelSize *[2]float64) []string {
	var erreurs []string
	if _, ok := TypesElement[e.Kind]; !ok {
		erreurs = append(erreurs, fmt.Sprintf("Élément « %s » : type « %s » inconnu.", premierNonVide(e.Field, e.Text), e.Kind))
	}
	if e.Kind == "texte" || e.Kind == "qr" {
		if _, ok := ChampsExport[e.Field]; !ok {
			erreurs = append(erreurs, fmt.Sprintf("Élément « %s » : champ inconnu (%s).", e.Field, clesTriees(ChampsExport)))
		}
	}
	if e.Kind == "statique" && strings.TrimSpace(e.Text) == "" {
		erreurs = append(erreurs, "Texte fixe vide.")
	}
	if (e.Kind == "texte" || e.Kind == "statique") && !contientChaine(Polices, e.Font) {
		erreurs = append(erreurs, fmt.Sprintf("Police « %s » inconnue.", e.Font))
	}
	if _, ok := Alignements[e.Align]; !ok {
		erreurs = append(erreurs, fmt.Sprintf("Alignement « %s » invalide (gauche/centre/droite).", e.Align))
	}
	if !reCouleur.MatchString(e.ColorHex) {
		erreurs = append(erreurs, fmt.Sprintf("Couleur « %s » invalide (format #RRGGBB).", e.ColorHex))
	}
	if e.SizePT <= 0 {
		erreurs = append(erreurs, "Corps de police positif requis.")
	}
	if e.WMM <= 0 || e.HMM <= 0 {
		erreurs = append(erreurs, "Largeur et hauteur strictement positives requises.")
	}
	if e.XMM < -EpsMM || e.YMM < -EpsMM {
		erreurs = append(erreurs, "Position négative hors de l'étiquette.")
	}
	if labelSize != nil {
		if e.XMM+e.WMM > labelSize[0]+EpsMM {
			erreurs = append(erreurs, fmt.Sprintf("Élément « %s » : déborde à droite de l'étiquette.", e.Name()))
		}
		if e.YMM+e.HMM > labelSize[1]+EpsMM {
			erreurs = append(erreurs, fmt.Sprintf("Élément « %s » : déborde en bas de l'étiquette.", e.Name()))
		}
	}
	return erreurs
}

// ToDict sérialise l'élément au format Python (arrondis compris).
func (e ElementLabel) ToDict() map[string]any {
	return map[string]any{
		"kind":  e.Kind,
		"field": e.Field,
		"text":  e.Text,
		"x":     arrondi(e.XMM, 2),
		"y":     arrondi(e.YMM, 2),
		"w":     arrondi(e.WMM, 2),
		"h":     arrondi(e.HMM, 2),
		"font":  e.Font,
		"size":  arrondi(e.SizePT, 1),
		"color": e.ColorHex,
		"align": e.Align,
	}
}

// ElementDepuisDict reconstruit un élément depuis le format Python.
func ElementDepuisDict(data map[string]any) ElementLabel {
	return ElementLabel{
		Kind:     chaineDefaut(data["kind"], "texte"),
		Field:    chaineDefaut(data["field"], "nom_complet"),
		Text:     chaineDefaut(data["text"], ""),
		XMM:      nombreDefaut(data["x"], 3.0),
		YMM:      nombreDefaut(data["y"], 3.0),
		WMM:      nombreDefaut(data["w"], 40.0),
		HMM:      nombreDefaut(data["h"], 9.0),
		Font:     chaineDefaut(data["font"], "Helvetica-Bold"),
		SizePT:   nombreDefaut(data["size"], 11.0),
		ColorHex: chaineDefaut(data["color"], "#1B4F91"),
		Align:    chaineDefaut(data["align"], "centre"),
	}
}

// ModeleLabel est un modèle d'étiquette : planche + décor + éléments.
type ModeleLabel struct {
	ID         string         `json:"id"`
	Nom        string         `json:"name"`
	Planche    string         `json:"sheet"`
	FondHex    string         `json:"background"`
	BordureHex string         `json:"border"`
	Integre    bool           `json:"builtin"`
	Elements   []ElementLabel `json:"elements"`
}

// Spec retourne la planche du modèle (nil si inconnue).
func (m ModeleLabel) Spec() *Planche { return Planches[m.Planche] }

// Validate vérifie le modèle (messages français, comme Python).
func (m ModeleLabel) Validate() []string {
	var erreurs []string
	if !reID.MatchString(m.ID) {
		erreurs = append(erreurs, fmt.Sprintf("Identifiant « %s » invalide (minuscules, chiffres, « - » et « _ »).", m.ID))
	}
	if strings.TrimSpace(m.Nom) == "" {
		erreurs = append(erreurs, "Nom du modèle vide.")
	}
	spec := m.Spec()
	if spec == nil {
		erreurs = append(erreurs, fmt.Sprintf("Planche « %s » inconnue.", m.Planche))
		return erreurs
	}
	erreurs = append(erreurs, spec.Validate()...)
	for _, kv := range []struct{ nom, valeur string }{{"fond", m.FondHex}, {"bordure", m.BordureHex}} {
		if kv.valeur != "" && !reCouleur.MatchString(kv.valeur) {
			erreurs = append(erreurs, fmt.Sprintf("Couleur de %s « %s » invalide (format #RRGGBB).", kv.nom, kv.valeur))
		}
	}
	if len(m.Elements) == 0 {
		erreurs = append(erreurs, "Aucun élément : le modèle doit contenir au moins un champ.")
	}
	taille := [2]float64{}
	taille[0], taille[1] = spec.LabelSizeMM()
	for _, el := range m.Elements {
		erreurs = append(erreurs, el.Validate(&taille)...)
	}
	return erreurs
}

// Clone retourne une copie profonde non « intégrée ».
func (m ModeleLabel) Clone() ModeleLabel {
	elements := make([]ElementLabel, len(m.Elements))
	copy(elements, m.Elements)
	m.Elements = elements
	m.Integre = false
	return m
}

// ElementIndexAt retourne le dernier élément contenant le point (hit-test).
func (m ModeleLabel) ElementIndexAt(xMM, yMM float64) (int, bool) {
	trouve := -1
	for i, el := range m.Elements {
		if el.XMM <= xMM && xMM <= el.XMM+el.WMM && el.YMM <= yMM && yMM <= el.YMM+el.HMM {
			trouve = i
		}
	}
	return trouve, trouve >= 0
}

// ToDict sérialise le modèle au format Python.
func (m ModeleLabel) ToDict() map[string]any {
	elements := make([]map[string]any, 0, len(m.Elements))
	for _, el := range m.Elements {
		elements = append(elements, el.ToDict())
	}
	return map[string]any{
		"id":         m.ID,
		"name":       m.Nom,
		"sheet":      m.Planche,
		"background": m.FondHex,
		"border":     m.BordureHex,
		"builtin":    m.Integre,
		"elements":   elements,
	}
}

// ModeleDepuisDict reconstruit un modèle depuis le format Python.
func ModeleDepuisDict(data map[string]any) ModeleLabel {
	modele := ModeleLabel{
		ID:         chaineDefaut(data["id"], ""),
		Nom:        chaineDefaut(data["name"], ""),
		Planche:    chaineDefaut(data["sheet"], "avery_l7160"),
		FondHex:    chaineDefaut(data["background"], "#FFFFFF"),
		BordureHex: chaineDefaut(data["border"], ""),
		Integre:    booleenDefaut(data["builtin"], false),
	}
	if brut, ok := data["elements"]; ok {
		for _, item := range listeCartes(brut) {
			modele.Elements = append(modele.Elements, ElementDepuisDict(item))
		}
	}
	return modele
}

// listeCartes normalise une liste d'éléments JSON (issue de encoding/json ou
// construite en mémoire par ToDict).
func listeCartes(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if carte, ok := item.(map[string]any); ok {
				out = append(out, carte)
			}
		}
		return out
	case []map[string]any:
		return t
	default:
		return nil
	}
}

// ModelesIntegres retourne les 4 modèles livrés avec l'application (tous valides).
func ModelesIntegres() []ModeleLabel {
	bleu := ThemesCouleur[ThemeParDefaut]
	vert := ThemesCouleur["vert"]
	return []ModeleLabel{
		{
			ID: "etiquette_classe", Nom: "Étiquette de classe — Avery L7160",
			Planche: "avery_l7160", FondHex: bleu.FondHex, BordureHex: bleu.TexteHex, Integre: true,
			Elements: []ElementLabel{
				{Kind: "texte", Field: "nom_complet", XMM: 3, YMM: 5, WMM: 44, HMM: 9, Font: "Helvetica-Bold", SizePT: 11, ColorHex: bleu.TexteHex, Align: "centre"},
				{Kind: "texte", Field: "identifiant", XMM: 3, YMM: 16, WMM: 44, HMM: 7, Font: "Helvetica", SizePT: 9, ColorHex: bleu.TexteHex, Align: "centre"},
				{Kind: "texte", Field: "classe", XMM: 3, YMM: 25, WMM: 44, HMM: 7, Font: "Helvetica", SizePT: 8, ColorHex: "#4A6FA5", Align: "centre"},
				{Kind: "qr", Field: "identifiant", XMM: 49, YMM: 12, WMM: 12, HMM: 12, Font: "Helvetica-Bold", SizePT: 11, ColorHex: bleu.TexteHex, Align: "centre"},
			},
		},
		{
			ID: "etiquette_large_photo", Nom: "Étiquette large + photo — Avery L7163",
			Planche: "avery_l7163", FondHex: vert.FondHex, BordureHex: vert.TexteHex, Integre: true,
			Elements: []ElementLabel{
				{Kind: "photo", Field: "", XMM: 4, YMM: 5, WMM: 20, HMM: 26, Font: "Helvetica-Bold", SizePT: 11, ColorHex: "#1B4F91", Align: "centre"},
				{Kind: "texte", Field: "nom_complet", XMM: 27, YMM: 8, WMM: 45, HMM: 10, Font: "Helvetica-Bold", SizePT: 13, ColorHex: vert.TexteHex, Align: "gauche"},
				{Kind: "texte", Field: "identifiant", XMM: 27, YMM: 19, WMM: 45, HMM: 7, Font: "Helvetica", SizePT: 9, ColorHex: vert.TexteHex, Align: "gauche"},
				{Kind: "texte", Field: "classe", XMM: 27, YMM: 27, WMM: 45, HMM: 7, Font: "Helvetica", SizePT: 8, ColorHex: "#4A6FA5", Align: "gauche"},
				{Kind: "qr", Field: "identifiant", XMM: 76, YMM: 11, WMM: 14, HMM: 14, Font: "Helvetica-Bold", SizePT: 11, ColorHex: vert.TexteHex, Align: "centre"},
			},
		},
		{
			ID: "badge_porteur", Nom: "Badge porte-badge — 85 × 55 mm",
			Planche: "badge_85x55", FondHex: "#FFFFFF", BordureHex: "#1B4F91", Integre: true,
			Elements: []ElementLabel{
				{Kind: "statique", Text: "ÉTABLISSEMENT", XMM: 38, YMM: 3, WMM: 42, HMM: 6, Font: "Helvetica-Bold", SizePT: 8, ColorHex: "#4A6FA5", Align: "gauche"},
				{Kind: "photo", XMM: 5, YMM: 8, WMM: 30, HMM: 40, Font: "Helvetica-Bold", SizePT: 11, ColorHex: "#1B4F91", Align: "centre"},
				{Kind: "texte", Field: "nom_complet", XMM: 38, YMM: 12, WMM: 42, HMM: 10, Font: "Helvetica-Bold", SizePT: 12, ColorHex: "#1B4F91", Align: "gauche"},
				{Kind: "texte", Field: "classe", XMM: 38, YMM: 24, WMM: 42, HMM: 8, Font: "Helvetica", SizePT: 10, ColorHex: "#333333", Align: "gauche"},
				{Kind: "texte", Field: "identifiant", XMM: 38, YMM: 34, WMM: 42, HMM: 7, Font: "Helvetica", SizePT: 9, ColorHex: "#333333", Align: "gauche"},
				{Kind: "qr", Field: "identifiant", XMM: 38, YMM: 41, WMM: 12, HMM: 12, Font: "Helvetica-Bold", SizePT: 11, ColorHex: "#1B4F91", Align: "centre"},
			},
		},
		{
			ID: "carte_visite", Nom: "Carte de visite — 85 × 54 mm",
			Planche: "carte_85x54", FondHex: "#FFFFFF", BordureHex: "#ECECEC", Integre: true,
			Elements: []ElementLabel{
				{Kind: "statique", Text: "NOM DE L'ÉTABLISSEMENT", XMM: 5, YMM: 4, WMM: 60, HMM: 7, Font: "Times-Bold", SizePT: 9, ColorHex: "#1B4F91", Align: "gauche"},
				{Kind: "texte", Field: "nom_complet", XMM: 5, YMM: 13, WMM: 60, HMM: 9, Font: "Helvetica-Bold", SizePT: 12, ColorHex: "#333333", Align: "gauche"},
				{Kind: "texte", Field: "identifiant", XMM: 5, YMM: 24, WMM: 55, HMM: 6, Font: "Helvetica", SizePT: 8.5, ColorHex: "#333333", Align: "gauche"},
				{Kind: "texte", Field: "mail", XMM: 5, YMM: 32, WMM: 55, HMM: 6, Font: "Helvetica", SizePT: 8.5, ColorHex: "#333333", Align: "gauche"},
				{Kind: "texte", Field: "classe", XMM: 5, YMM: 40, WMM: 55, HMM: 6, Font: "Helvetica", SizePT: 8, ColorHex: "#4A6FA5", Align: "gauche"},
				{Kind: "qr", Field: "identifiant", XMM: 65, YMM: 18, WMM: 15, HMM: 15, Font: "Helvetica-Bold", SizePT: 11, ColorHex: "#1B4F91", Align: "centre"},
			},
		},
	}
}

// CheminModelesParDefaut retourne le chemin par défaut des modèles.
func CheminModelesParDefaut() string {
	dir, err := config.ConfigDir()
	if err != nil || dir == "" {
		return NomFichier
	}
	return filepath.Join(dir, NomFichier)
}

// ChargerModeles retourne les modèles intégrés + personnalisés enregistrés
// (fichier absent ou illisible → intégrés seuls).
func ChargerModeles(chemin string) []ModeleLabel {
	if chemin == "" {
		chemin = CheminModelesParDefaut()
	}
	data, err := os.ReadFile(chemin)
	if err != nil {
		return ModelesIntegres()
	}
	var brut any
	if err := json.Unmarshal(data, &brut); err != nil {
		return ModelesIntegres()
	}
	var items []any
	switch v := brut.(type) {
	case []any:
		items = v
	case map[string]any:
		liste, ok := v["modeles"].([]any)
		if !ok {
			return ModelesIntegres()
		}
		items = liste
	default:
		return ModelesIntegres()
	}
	var modeles []ModeleLabel
	for _, item := range items {
		carte, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modele := ModeleDepuisDict(carte)
		if strings.TrimSpace(modele.ID) != "" && strings.TrimSpace(modele.Nom) != "" {
			modeles = append(modeles, modele)
		}
	}
	return modeles
}

// SauvegarderModeles écrit les modèles au format Python (objet {"modeles": [...]}).
func SauvegarderModeles(modeles []ModeleLabel, chemin string) (string, error) {
	if chemin == "" {
		chemin = CheminModelesParDefaut()
	}
	if dir := filepath.Dir(chemin); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("label: création du dossier %s: %w", dir, err)
		}
	}
	dicts := make([]map[string]any, 0, len(modeles))
	for _, m := range modeles {
		dicts = append(dicts, m.ToDict())
	}
	data, err := json.MarshalIndent(map[string]any{"modeles": dicts}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("label: encodage JSON: %w", err)
	}
	if err := os.WriteFile(chemin, data, 0o644); err != nil {
		return "", fmt.Errorf("label: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}

// IDModeleUnique retourne un identifiant libre pour une duplication
// (base, base-2, base-3…), accents pliés et casse ignorée.
func IDModeleUnique(modeles []ModeleLabel, base string) string {
	plie := plierAccents(strings.ToLower(strings.TrimSpace(base)))
	nettoye := strings.Trim(reNonID.ReplaceAllString(plie, "-"), "-")
	if nettoye == "" || !reID.MatchString(nettoye) {
		nettoye = "modele"
	}
	pris := make(map[string]bool, len(modeles))
	for _, m := range modeles {
		pris[m.ID] = true
	}
	candidat := nettoye
	for i := 2; pris[candidat]; i++ {
		candidat = fmt.Sprintf("%s-%d", nettoye, i)
	}
	return candidat
}

// DupliquerModele copie profondément un modèle avec un nouvel id/nom.
func DupliquerModele(modeles []ModeleLabel, modele ModeleLabel, nouveauNom string) ModeleLabel {
	clone := modele.Clone()
	clone.ID = IDModeleUnique(modeles, modele.ID)
	if nom := strings.TrimSpace(nouveauNom); nom != "" {
		clone.Nom = nom
	} else {
		clone.Nom = modele.Nom + " (copie)"
	}
	return clone
}

// ExporterModele écrit un modèle isolé en JSON.
func ExporterModele(modele ModeleLabel, dest string) (string, error) {
	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("label: création du dossier %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(modele.ToDict(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("label: encodage JSON: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("label: écriture de %s: %w", dest, err)
	}
	return dest, nil
}

// ImporterModele lit un modèle JSON (échoue si invalide).
func ImporterModele(chemin string) (ModeleLabel, error) {
	data, err := os.ReadFile(chemin)
	if err != nil {
		return ModeleLabel{}, fmt.Errorf("label: lecture du modèle impossible : %w", err)
	}
	var brut any
	if err := json.Unmarshal(data, &brut); err != nil {
		return ModeleLabel{}, fmt.Errorf("label: lecture du modèle impossible : %w", err)
	}
	carte, ok := brut.(map[string]any)
	if !ok {
		return ModeleLabel{}, fmt.Errorf("label: format de modèle invalide (objet JSON attendu)")
	}
	modele := ModeleDepuisDict(carte)
	if errs := modele.Validate(); len(errs) > 0 {
		return ModeleLabel{}, fmt.Errorf("label: modèle invalide : %s", strings.Join(errs, "; "))
	}
	return modele, nil
}

// ─── Rendu PDF ───────────────────────────────────────────────────────────

// RendreEtiquettesPDF génère la planche d'étiquettes PDF pour « users » et
// retourne les octets du PDF ainsi que le nombre de pages.
//
// « photos » est indexé par identifiant (sAMAccountName, minuscules) : photo AD
// (jpegPhoto/thumbnailPhoto) ou fichier traité par M12. Un utilisateur peut
// aussi porter une clé « photo » ([]byte) dans sa ligne.
func RendreEtiquettesPDF(users []map[string]any, modele ModeleLabel, photos map[string][]byte) ([]byte, int, error) {
	if errs := modele.Validate(); len(errs) > 0 {
		return nil, 0, fmt.Errorf("label: modèle d'étiquette invalide : %s", strings.Join(errs, "; "))
	}
	planche := modele.Spec()
	parPlanche := planche.ParPlanche()

	pdf := gofpdf.New("P", "mm", planche.Page, "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)

	compteur := 0
	pages := 0
	if len(users) == 0 {
		pdf.AddPage()
	}
	for index, user := range users {
		pos := index % parPlanche
		if pos == 0 {
			pdf.AddPage()
			if index > 0 {
				pages++
			}
		}
		lx, ly := planche.OriginLabel(pos/planche.Colonnes, pos%planche.Colonnes)
		dessinerDecor(pdf, modele.FondHex, modele.BordureHex, lx, ly, planche.LargeurMM, planche.HauteurMM)

		for _, el := range modele.Elements {
			valeur := el.Value(user)
			ex, ey := lx+el.XMM, ly+el.YMM
			switch el.Kind {
			case "photo":
				photo := photoPour(user, photos)
				dessinerPhoto(pdf, photo, ex, ey, el.WMM, el.HMM, "#E4E4E4", &compteur)
			case "qr":
				if valeur == "" {
					continue
				}
				dessinerQR(pdf, valeur, ex, ey, math.Min(el.WMM, el.HMM), el.ColorHex, &compteur)
			default:
				if valeur != "" {
					dessinerTexte(pdf, valeur, ex, ey, el.WMM, el.HMM, el.SizePT, el.Font, el.ColorHex, el.Align)
				}
			}
		}
	}
	pages++

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, 0, fmt.Errorf("label: génération PDF: %w", err)
	}
	if err := pdf.Error(); err != nil {
		return nil, 0, fmt.Errorf("label: génération PDF: %w", err)
	}
	return out.Bytes(), pages, nil
}

// OptionsTrombinoscope regroupe les paramètres de mise en page d'un
// trombinoscope.
type OptionsTrombinoscope struct {
	Page      string // "A4" (défaut) ou "A3"
	Colonnes  int    // 0 = 4 (défaut), borné à [1, 8]
	Titre     string
	SousTitre string
	Photos    map[string][]byte
	AccentHex string // vide = thème par défaut
}

// ColonnesParDefaut est le nombre de colonnes par défaut du trombinoscope.
const ColonnesParDefaut = 4

// GenererTrombinoscopePDF génère une grille photo + nom sur A4 ou A3 et
// retourne les octets du PDF ainsi que le nombre de pages.
func GenererTrombinoscopePDF(entries []map[string]any, opts OptionsTrombinoscope) ([]byte, int, error) {
	page := opts.Page
	if page == "" {
		page = "A4"
	}
	if _, ok := TaillePagesMM[page]; !ok {
		return nil, 0, fmt.Errorf("label: format de page « %s » inconnu (A4 ou A3)", page)
	}
	colonnes := opts.Colonnes
	if colonnes == 0 {
		colonnes = ColonnesParDefaut
	}
	colonnes = max(1, min(colonnes, 8))
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("label: aucun compte à mettre en page")
	}

	accent := opts.AccentHex
	if accent == "" {
		accent = ThemesCouleur[ThemeParDefaut].TexteHex
	}
	photos := opts.Photos
	if photos == nil {
		photos = map[string][]byte{}
	}

	pageW, pageH := TaillePagesMM[page][0], TaillePagesMM[page][1]
	const (
		margeMM = 12.0
		gapMM   = 6.0
	)
	cellW := (pageW - 2*margeMM - float64(colonnes-1)*gapMM) / float64(colonnes)
	photoW := cellW - 6.0
	photoH := photoW * 4 / 3
	const (
		nomHMM    = 7.0
		classeHMM = 5.5
		padMM     = 3.0
	)
	cellH := padMM*2 + photoH + nomHMM + classeHMM

	var headerH float64
	switch {
	case opts.Titre != "" && opts.SousTitre != "":
		headerH = 16.0
	case opts.Titre != "" || opts.SousTitre != "":
		headerH = 10.0
	default:
		headerH = 4.0
	}
	rows := int(math.Floor((pageH - 2*margeMM - headerH) / (cellH + gapMM)))
	if rows < 1 {
		return nil, 0, fmt.Errorf("label: trombinoscope : la page est trop petite pour cette mise en page")
	}

	pdf := gofpdf.New("P", "mm", page, "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)

	parPage := rows * colonnes
	compteur := 0
	pages := 0
	for index, entry := range entries {
		pos := index % parPage
		if pos == 0 {
			pdf.AddPage()
			if index > 0 {
				pages++
			}
			dessinerEnteteTrombi(pdf, opts.Titre, opts.SousTitre, pageW, pageH, margeMM, headerH, accent)
		}
		row, col := pos/colonnes, pos%colonnes
		xMM := margeMM + float64(col)*(cellW+gapMM)
		yTopMM := margeMM + headerH + float64(row)*(cellH+gapMM)
		dessinerCelluleTrombi(pdf, entry, xMM, yTopMM, cellW, cellH, photoW, photoH, padMM, nomHMM, classeHMM, photos, accent, &compteur)
	}
	pages++

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, 0, fmt.Errorf("label: génération PDF: %w", err)
	}
	if err := pdf.Error(); err != nil {
		return nil, 0, fmt.Errorf("label: génération PDF: %w", err)
	}
	return out.Bytes(), pages, nil
}

// Initiales retourne les initiales d'un nom complet.
func Initiales(libelle string) string {
	champs := strings.Fields(strings.TrimSpace(libelle))
	var alpha []string
	for _, champ := range champs {
		for _, r := range champ {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0xC0 && r <= 0x24F) {
				alpha = append(alpha, champ)
				break
			}
		}
	}
	var b strings.Builder
	for i, champ := range alpha {
		if i >= 2 {
			break
		}
		for _, r := range champ {
			b.WriteRune(r)
			break
		}
	}
	initiales := strings.ToUpper(b.String())
	if len([]rune(initiales)) >= 2 {
		return initiales
	}
	// Repli : deux premiers caractères alphanumériques du libellé.
	var nettoies []rune
	for _, r := range strings.TrimSpace(libelle) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0xC0 && r <= 0x24F) {
			nettoies = append(nettoies, r)
		}
	}
	texte := strings.ToUpper(string(nettoies))
	if r := []rune(texte); len(r) > 0 {
		if len(r) > 2 {
			r = r[:2]
		}
		return string(r)
	}
	return "?"
}

// BuildLigneExport construit une ligne d'export à partir des attributs
// utilisateur bruts (dn, sam, cn, givenName, sn, mail, disabled…).
func BuildLigneExport(attrs map[string]any) map[string]any {
	dn := valeurChaine(attrs["dn"])
	parent := ""
	if idx := strings.Index(dn, ","); idx >= 0 {
		parent = dn[idx+1:]
	}
	feuille := ""
	if parent != "" {
		feuille = parent
		if idx := strings.Index(feuille, ","); idx >= 0 {
			feuille = feuille[:idx]
		}
	}
	classe := feuille
	if idx := strings.Index(feuille, "="); idx >= 0 {
		classe = feuille[idx+1:]
	}
	identifiant := valeurChaine(attrs["sam"])
	if identifiant == "" {
		identifiant = valeurChaine(attrs["sAMAccountName"])
	}
	etat := "Actif"
	if booleenDefaut(attrs["disabled"], false) {
		etat = "Désactivé"
	}
	return map[string]any{
		"identifiant":  identifiant,
		"nom_complet":  valeurChaine(attrs["cn"]),
		"prenom":       valeurChaine(attrs["givenName"]),
		"nom":          valeurChaine(attrs["sn"]),
		"classe":       classe,
		"mail":         valeurChaine(attrs["mail"]),
		"etat":         etat,
		"mot_de_passe": "",
	}
}

// ─── Dessin bas niveau (gofpdf) ─────────────────────────────────────────

func dessinerDecor(pdf *gofpdf.Fpdf, fondHex, bordureHex string, x, y, w, h float64) {
	if c, ok := hexVersRGB(fondHex); ok {
		pdf.SetFillColor(c[0], c[1], c[2])
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.Rect(x, y, w, h, "F")
	if bordureHex != "" {
		if c, ok := hexVersRGB(bordureHex); ok {
			pdf.SetDrawColor(c[0], c[1], c[2])
			pdf.SetLineWidth(0.6)
			pdf.Rect(x+0.5, y+0.5, w-1, h-1, "D")
		}
	}
}

// policeGofpdf traduit une police reportlab en couple (famille, style) gofpdf.
func policeGofpdf(police string) (string, string) {
	switch police {
	case "Helvetica-Bold":
		return "Helvetica", "B"
	case "Helvetica-Oblique":
		return "Helvetica", "I"
	case "Times-Roman":
		return "Times", ""
	case "Times-Bold":
		return "Times", "B"
	case "Courier":
		return "Courier", ""
	default:
		return "Helvetica", ""
	}
}

func appliquerPolice(pdf *gofpdf.Fpdf, police string, taille float64) {
	famille, style := policeGofpdf(police)
	pdf.SetFont(famille, style, taille)
}

// dessinerTexte écrit un texte sur une ligne, centré verticalement dans la
// boîte, réduit puis tronqué pour tenir dans la largeur.
func dessinerTexte(pdf *gofpdf.Fpdf, texte string, x, yTop, w, h, taillePT float64, police, couleurHex, align string) {
	taille := taillePT
	appliquerPolice(pdf, police, taille)
	for taille > PoliceMinPT && pdf.GetStringWidth(texte) > w {
		taille -= 0.5
		appliquerPolice(pdf, police, taille)
	}
	if pdf.GetStringWidth(texte) > w {
		runes := []rune(texte)
		for len(runes) > 0 && pdf.GetStringWidth(string(runes)+"…") > w {
			runes = runes[:len(runes)-1]
		}
		if len(runes) > 0 {
			texte = string(runes) + "…"
		} else {
			texte = ""
		}
	}
	baseline := yTop + h/2 + taille*0.36
	if c, ok := hexVersRGB(couleurHex); ok {
		pdf.SetTextColor(c[0], c[1], c[2])
	}
	appliquerPolice(pdf, police, taille)
	largeur := pdf.GetStringWidth(texte)
	switch align {
	case "gauche":
		pdf.Text(x, baseline, texte)
	case "droite":
		pdf.Text(x+w-largeur, baseline, texte)
	default:
		pdf.Text(x+(w-largeur)/2, baseline, texte)
	}
}

func dessinerQR(pdf *gofpdf.Fpdf, valeur string, x, y, tailleMM float64, couleurHex string, compteur *int) {
	code, err := qr.Encode(valeur, qr.M, qr.Auto)
	if err != nil {
		return
	}
	const pixels = 256
	code, err = barcode.Scale(code, pixels, pixels)
	if err != nil {
		return
	}
	img := image.Image(code)
	if c, ok := hexVersRGBA(couleurHex); ok {
		img = coloriser(img, c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return
	}
	nom := fmt.Sprintf("qr-%d", *compteur)
	*compteur++
	pdf.RegisterImageOptionsReader(nom, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(buf.Bytes()))
	pdf.ImageOptions(nom, x, y, tailleMM, tailleMM, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
}

func dessinerPhoto(pdf *gofpdf.Fpdf, donnees []byte, x, y, w, h float64, placeholderHex string, compteur *int) bool {
	if len(donnees) > 0 {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(donnees))
		if err == nil && cfg.Width > 0 && cfg.Height > 0 {
			var it string
			switch format {
			case "jpeg":
				it = "JPG"
			case "png":
				it = "PNG"
			}
			if it != "" {
				ech := math.Min(w/float64(cfg.Width), h/float64(cfg.Height))
				dw, dh := float64(cfg.Width)*ech, float64(cfg.Height)*ech
				nom := fmt.Sprintf("photo-%d", *compteur)
				*compteur++
				pdf.RegisterImageOptionsReader(nom, gofpdf.ImageOptions{ImageType: it}, bytes.NewReader(donnees))
				pdf.ImageOptions(nom, x+(w-dw)/2, y+(h-dh)/2, dw, dh, false, gofpdf.ImageOptions{ImageType: it}, 0, "")
				return true
			}
		}
	}
	if c, ok := hexVersRGB(placeholderHex); ok {
		pdf.SetFillColor(c[0], c[1], c[2])
	} else {
		pdf.SetFillColor(228, 228, 228)
	}
	pdf.RoundedRect(x, y, w, h, 3, "1234", "F")
	return false
}

// photoPour cherche la photo d'un utilisateur : d'abord la table indexée par
// identifiant (minuscules), puis une éventuelle clé « photo » de la ligne.
func photoPour(user map[string]any, photos map[string][]byte) []byte {
	if photos != nil {
		if p := photos[strings.ToLower(valeurChaine(user["identifiant"]))]; len(p) > 0 {
			return p
		}
	}
	return octetsDe(user["photo"])
}

func dessinerEnteteTrombi(pdf *gofpdf.Fpdf, titre, sousTitre string, pageW, pageH, margeMM, headerH float64, accent string) {
	if titre == "" && sousTitre == "" {
		return
	}
	topY := pageH - margeMM
	largeur := pageW - 2*margeMM
	curseur := 0.0
	if titre != "" {
		dessinerTexte(pdf, titre, margeMM, topY, largeur, 9, 16, "Helvetica-Bold", accent, "gauche")
		curseur = 9.0
	}
	if sousTitre != "" {
		dessinerTexte(pdf, sousTitre, margeMM, topY-curseur, largeur, 5, 9, "Helvetica", "#666666", "gauche")
	}
	if c, ok := hexVersRGB(accent); ok {
		pdf.SetDrawColor(c[0], c[1], c[2])
	}
	pdf.SetLineWidth(0.8)
	y := topY + (headerH - 1)
	pdf.Line(margeMM, y, pageW-margeMM, y)
}

func dessinerCelluleTrombi(pdf *gofpdf.Fpdf, entry map[string]any, x, yTop, cellW, cellH, photoW, photoH, pad, nomH, classeH float64, photos map[string][]byte, accent string, compteur *int) {
	pdf.SetFillColor(255, 255, 255)
	pdf.SetDrawColor(217, 217, 217)
	pdf.SetLineWidth(0.5)
	pdf.RoundedRect(x, yTop, cellW, cellH, 3, "1234", "FD")

	identifiant := valeurChaine(entry["identifiant"])
	photoX := x + (cellW-photoW)/2
	photoTop := yTop + pad
	photo := photos[strings.ToLower(identifiant)]
	if len(photo) == 0 {
		photo = octetsDe(entry["photo"])
	}
	dessine := dessinerPhoto(pdf, photo, photoX, photoTop, photoW, photoH, "#EFEFEF", compteur)
	if !dessine {
		nom := valeurChaine(entry["nom_complet"])
		if nom == "" {
			nom = identifiant
		}
		dessinerTexte(pdf, Initiales(nom), photoX, photoTop, photoW, photoH, 22, "Helvetica-Bold", "#9A9A9A", "centre")
	}

	texteTop := photoTop + photoH + 1
	nom := valeurChaine(entry["nom_complet"])
	if nom == "" {
		nom = valeurChaine(entry["cn"])
	}
	if nom == "" {
		nom = identifiant
	}
	dessinerTexte(pdf, nom, x+pad, texteTop, cellW-2*pad, nomH, 10, "Helvetica-Bold", "#333333", "centre")
	if classe := valeurChaine(entry["classe"]); classe != "" {
		dessinerTexte(pdf, classe, x+pad, texteTop+nomH, cellW-2*pad, classeH, 8.5, "Helvetica", accent, "centre")
	}
}

// coloriser remplace les modules sombres d'un QR par une couleur donnée.
func coloriser(img image.Image, c color.RGBA) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if (r+g+bl)/3 < 0x8000 {
				out.SetRGBA(x, y, c)
			} else {
				out.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	return out
}

// ─── Utilitaires ─────────────────────────────────────────────────────────

func hexVersRGB(hex string) ([3]int, bool) {
	if !reCouleur.MatchString(hex) {
		return [3]int{}, false
	}
	var r, g, b int
	_, _ = fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b)
	return [3]int{r, g, b}, true
}

func hexVersRGBA(hex string) (color.RGBA, bool) {
	if !reCouleur.MatchString(hex) {
		return color.RGBA{}, false
	}
	var r, g, b int
	_, _ = fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, true
}

func valeurChaine(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func octetsDe(v any) []byte {
	switch t := v.(type) {
	case []byte:
		return t
	case string:
		return []byte(t)
	default:
		return nil
	}
}

func chaineDefaut(v any, defaut string) string {
	if v == nil {
		return defaut
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func nombreDefaut(v any, defaut float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
	}
	return defaut
}

func booleenDefaut(v any, defaut bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return defaut
}

func arrondi(v float64, decimales int) float64 {
	facteur := math.Pow(10, float64(decimales))
	return math.RoundToEven(v*facteur) / facteur
}

func contientChaine(liste []string, cible string) bool {
	for _, s := range liste {
		if s == cible {
			return true
		}
	}
	return false
}

func premierNonVide(valeurs ...string) string {
	for _, v := range valeurs {
		if v != "" {
			return v
		}
	}
	return ""
}

func clesTriees(m map[string]string) string {
	cles := make([]string, 0, len(m))
	for k := range m {
		cles = append(cles, k)
	}
	// Tri simple (les libellés d'erreur ne sont pas comparés mot à mot).
	for i := 1; i < len(cles); i++ {
		for j := i; j > 0 && cles[j-1] > cles[j]; j-- {
			cles[j-1], cles[j] = cles[j], cles[j-1]
		}
	}
	return strings.Join(cles, ", ")
}

// tableAccents associe les lettres latines décomposables à leur base. Reproduit
// le résultat de NFKD + suppression des diacritiques pour les langues latines.
var tableAccents = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i",
	'ĵ': "j",
	'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s",
	'ţ': "t", 'ť': "t", 'ŧ': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
}

// plierAccents remplace les lettres accentuées par leur base (la casse doit
// déjà être abaissée avant l'appel).
func plierAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		if base, ok := tableAccents[r]; ok {
			b.WriteString(base)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
