package grouptemplates

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// noeudXML est un nœud XML générique (structure « balise + attributs + texte »)
// permettant de représenter l'arbre typé produit par `_value_to_xml`.
type noeudXML struct {
	Tag     string
	Attrs   map[string]string
	Texte   string
	Enfants []*noeudXML
}

// OctetsXML retourne la représentation XML typée d'un modèle (concaténation
// directe, sans indentation, comme `ElementTree.write`).
func OctetsXML(modele ModeleGroupe) ([]byte, error) {
	racine := &noeudXML{Tag: "modele", Attrs: map[string]string{"type": "dict"}}
	donnees := modele.ToDict()
	for _, cle := range clesTrieesMap(donnees) {
		valeurVersXML(racine, cle, donnees[cle])
	}
	var tampon bytes.Buffer
	tampon.WriteString("<?xml version='1.0' encoding='utf-8'?>\n")
	ecrireNoeud(&tampon, racine)
	return tampon.Bytes(), nil
}

// ExporterModeleXML écrit un modèle au format XML typé (aller-retour
// strictement équivalent au JSON).
func ExporterModeleXML(modele ModeleGroupe, chemin string) (string, error) {
	if err := os.MkdirAll(dirDe(chemin), 0o750); err != nil {
		return "", fmt.Errorf("grouptemplates: création du dossier: %w", err)
	}
	contenu, err := OctetsXML(modele)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(chemin, contenu, 0o600); err != nil {
		return "", fmt.Errorf("grouptemplates: écriture XML: %w", err)
	}
	return chemin, nil
}

// ImporterModeleXML lit un modèle depuis un fichier XML typé.
func ImporterModeleXML(chemin string) (ModeleGroupe, error) {
	racine, err := lireXML(chemin)
	if err != nil {
		return ModeleGroupe{}, fmt.Errorf("Lecture du modèle XML impossible : %w", err)
	}
	if racine == nil {
		return ModeleGroupe{}, fmt.Errorf("Lecture du modèle XML impossible : fichier vide")
	}
	if racine.Tag != "modele" {
		return ModeleGroupe{}, fmt.Errorf("Racine XML inattendue « %s » (attendu : modèle).", racine.Tag)
	}
	carte, ok := valeurDepuisXML(racine).(map[string]any)
	if !ok {
		return ModeleGroupe{}, fmt.Errorf("Format de modèle XML invalide.")
	}
	modele := ModeleDepuisDict(carte)
	if erreurs := modele.Validate(); len(erreurs) > 0 {
		return ModeleGroupe{}, fmt.Errorf("Modèle invalide : %s", strings.Join(erreurs, "; "))
	}
	return modele, nil
}

// valeurVersXML ajoute à `parent` un enfant représentant `valeur`, avec le
// type XML attendu par `_xml_to_value`.
func valeurVersXML(parent *noeudXML, tag string, valeur any) *noeudXML {
	noeud := &noeudXML{Tag: tag, Attrs: map[string]string{}}
	parent.Enfants = append(parent.Enfants, noeud)

	switch v := valeur.(type) {
	case nil:
		noeud.Attrs["type"] = "null"
	case bool:
		noeud.Attrs["type"] = "bool"
		if v {
			noeud.Texte = "true"
		} else {
			noeud.Texte = "false"
		}
	case int:
		noeud.Attrs["type"] = "number"
		noeud.Texte = strconv.Itoa(v)
	case int32:
		noeud.Attrs["type"] = "number"
		noeud.Texte = strconv.FormatInt(int64(v), 10)
	case int64:
		noeud.Attrs["type"] = "number"
		noeud.Texte = strconv.FormatInt(v, 10)
	case float64:
		noeud.Attrs["type"] = "number"
		noeud.Texte = reprNombre(v)
	case map[string]any:
		noeud.Attrs["type"] = "dict"
		for _, cle := range clesTrieesMap(v) {
			valeurVersXML(noeud, cle, v[cle])
		}
	case []any:
		noeud.Attrs["type"] = "list"
		for _, item := range v {
			valeurVersXML(noeud, "item", item)
		}
	case []string:
		noeud.Attrs["type"] = "list"
		for _, item := range v {
			valeurVersXML(noeud, "item", item)
		}
	default:
		texte := valeurChaine(valeur)
		noeud.Attrs["type"] = "str"
		if contientControle(texte) {
			// \r et caractères de contrôle : le parseur XML les normalise,
			// donc base64 pour un aller-retour strictement équivalent.
			noeud.Attrs["encoding"] = "b64"
			noeud.Texte = base64.StdEncoding.EncodeToString([]byte(texte))
		} else {
			noeud.Texte = texte
		}
	}
	return noeud
}

// valeurDepuisXML convertit un nœud XML typé en valeur Go, en reproduisant
// `_xml_to_value` (les nombres entiers redeviennent des entiers).
func valeurDepuisXML(noeud *noeudXML) any {
	kind := noeud.Attrs["type"]
	if kind == "" {
		kind = "str"
	}
	switch kind {
	case "null":
		return nil
	case "bool":
		return strings.ToLower(strings.TrimSpace(noeud.Texte)) == "true"
	case "number":
		nombre, err := strconv.ParseFloat(strings.TrimSpace(noeud.Texte), 64)
		if err != nil {
			return strings.TrimSpace(noeud.Texte)
		}
		if !math.IsInf(nombre, 0) && !math.IsNaN(nombre) && nombre == math.Trunc(nombre) {
			return int(nombre)
		}
		return nombre
	case "dict":
		carte := map[string]any{}
		for _, enfant := range noeud.Enfants {
			carte[enfant.Tag] = valeurDepuisXML(enfant)
		}
		return carte
	case "list":
		liste := []any{}
		for _, enfant := range noeud.Enfants {
			liste = append(liste, valeurDepuisXML(enfant))
		}
		return liste
	default:
		if noeud.Attrs["encoding"] == "b64" {
			donnees, err := base64.StdEncoding.DecodeString(noeud.Texte)
			if err != nil {
				return noeud.Texte
			}
			return string(donnees)
		}
		return noeud.Texte
	}
}

// ecrireNoeud sérialise un nœud XML (écriture directe, sans indentation :
// identique à `ElementTree.write`).
func ecrireNoeud(tampon *bytes.Buffer, noeud *noeudXML) {
	tampon.WriteByte('<')
	tampon.WriteString(noeud.Tag)
	attributs := make([]string, 0, len(noeud.Attrs))
	for cle := range noeud.Attrs {
		attributs = append(attributs, cle)
	}
	sort.Strings(attributs)
	for _, cle := range attributs {
		tampon.WriteByte(' ')
		tampon.WriteString(cle)
		tampon.WriteString(`="`)
		_ = xml.EscapeText(tampon, []byte(noeud.Attrs[cle]))
		tampon.WriteByte('"')
	}
	if len(noeud.Enfants) == 0 && noeud.Texte == "" {
		tampon.WriteString(" />")
		return
	}
	tampon.WriteByte('>')
	if noeud.Texte != "" {
		_ = xml.EscapeText(tampon, []byte(noeud.Texte))
	}
	for _, enfant := range noeud.Enfants {
		ecrireNoeud(tampon, enfant)
	}
	tampon.WriteString("</")
	tampon.WriteString(noeud.Tag)
	tampon.WriteByte('>')
}

// lireXML construit l'arbre générique depuis un fichier XML.
func lireXML(chemin string) (*noeudXML, error) {
	fichier, err := os.Open(chemin)
	if err != nil {
		return nil, err
	}
	defer fichier.Close()

	decodeur := xml.NewDecoder(fichier)
	var racine *noeudXML
	var pile []*noeudXML
	for {
		token, err := decodeur.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			noeud := &noeudXML{Tag: t.Name.Local, Attrs: map[string]string{}}
			for _, attribut := range t.Attr {
				noeud.Attrs[attribut.Name.Local] = attribut.Value
			}
			if len(pile) == 0 {
				racine = noeud
			} else {
				parent := pile[len(pile)-1]
				parent.Enfants = append(parent.Enfants, noeud)
			}
			pile = append(pile, noeud)
		case xml.EndElement:
			if len(pile) > 0 {
				pile = pile[:len(pile)-1]
			}
		case xml.CharData:
			if len(pile) > 0 {
				pile[len(pile)-1].Texte += string(t)
			}
		}
	}
	return racine, nil
}

// reprNombre reproduit `repr(float)` de Python (« 5.0 », « 12.5 », « 1e+21 »).
func reprNombre(valeur float64) string {
	if math.IsInf(valeur, 0) || math.IsNaN(valeur) {
		return strconv.FormatFloat(valeur, 'g', -1, 64)
	}
	if valeur == math.Trunc(valeur) && math.Abs(valeur) < 1e16 {
		return strconv.FormatFloat(valeur, 'f', 1, 64)
	}
	return strconv.FormatFloat(valeur, 'g', -1, 64)
}

// contientControle indique si la chaîne contient un caractère de contrôle
// (< 0x20 ou 0x7F), auquel cas elle sera encodée en base64.
func contientControle(texte string) bool {
	for _, r := range texte {
		if r < 0x20 || r == 0x7F {
			return true
		}
	}
	return false
}

func clesTrieesMap(carte map[string]any) []string {
	cles := make([]string, 0, len(carte))
	for cle := range carte {
		cles = append(cles, cle)
	}
	sort.Strings(cles)
	return cles
}

func dirDe(chemin string) string {
	if index := strings.LastIndexAny(chemin, `/\`); index >= 0 {
		return chemin[:index]
	}
	return "."
}
