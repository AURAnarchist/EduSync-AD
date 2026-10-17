package ui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/grouptemplates"
)

// ─── Modèles de groupes (M23) ─────────────────────────────────────────────

// annuaireSimule est un annuaire vide : rien n'existe et rien n'est écrit.
// Il permet un calcul de cible « à blanc » depuis l'interface.
type annuaireSimule struct{}

func (annuaireSimule) ListerOUEnfants(context.Context, string) ([]string, error) {
	return nil, nil
}

func (annuaireSimule) CreerOU(context.Context, string, string) error { return nil }

func (annuaireSimule) ListerGroupes(context.Context, string) ([]grouptemplates.GroupeExistant, error) {
	return nil, nil
}

func (annuaireSimule) CreerGroupe(context.Context, string, string, string) error { return nil }

// cheminModelesGT retourne le chemin de group_templates.json.
func (s *Server) cheminModelesGT() string {
	return filepath.Join(s.dossierConfig(), grouptemplates.NomFichier)
}

// trouverModeleGT retourne un modèle de groupe par identifiant.
func trouverModeleGT(modeles []grouptemplates.ModeleGroupe, id string) (grouptemplates.ModeleGroupe, bool) {
	for _, modele := range modeles {
		if modele.ID == id {
			return modele, true
		}
	}
	return grouptemplates.ModeleGroupe{}, false
}

// valeursDepuisTexte analyse un bloc « champ=valeur » (une par ligne).
func valeursDepuisTexte(texte string) (map[string]string, error) {
	valeurs := map[string]string{}
	for _, ligne := range strings.Split(texte, "\n") {
		ligne = strings.TrimSpace(ligne)
		if ligne == "" {
			continue
		}
		parties := strings.SplitN(ligne, "=", 2)
		if len(parties) != 2 || strings.TrimSpace(parties[0]) == "" {
			return nil, fmt.Errorf("grouptemplates: valeur attendue au format champ=valeur (reçu %q)", ligne)
		}
		valeurs[strings.TrimSpace(parties[0])] = strings.TrimSpace(parties[1])
	}
	return valeurs, nil
}

func (s *Server) pageGroupTemplates(w http.ResponseWriter, r *http.Request, sess *session) {
	d := s.donnees(r, Tr(s.langue(r), "gt.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	d.ModelesGT = grouptemplates.ChargerModeles(s.cheminModelesGT())
	s.rendre(w, http.StatusOK, "group_templates.html", d)
}

// gtOptions construit les options d'instanciation depuis le formulaire.
func gtOptions(r *http.Request) (grouptemplates.OptionsInstantiation, map[string]string, error) {
	valeurs, err := valeursDepuisTexte(r.FormValue("valeurs"))
	if err != nil {
		return grouptemplates.OptionsInstantiation{}, nil, err
	}
	parent := strings.TrimSpace(r.FormValue("parent_dn"))
	opts := grouptemplates.OptionsInstantiation{
		OUParente:          &parent,
		DossierArtefacts:   strings.TrimSpace(r.FormValue("artifacts")),
		SansScope:          !valeurCaseACocher(r.FormValue("scope")),
		DossierPersoGlobal: valeurCaseACocher(r.FormValue("home_global")),
		Domaine:            strings.TrimSpace(r.FormValue("domaine")),
		DossierConfig:      "",
	}
	return opts, valeurs, nil
}

// groupTemplatesApercu calcule un plan à blanc (aucune écriture AD).
func (s *Server) groupTemplatesApercu(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	langue := s.langue(r)
	modele, ok := trouverModeleGT(grouptemplates.ChargerModeles(s.cheminModelesGT()), strings.TrimSpace(r.FormValue("modele")))
	if !ok {
		s.gtMessage(w, Tr(langue, "gt.modele_introuvable"), true)
		return
	}
	opts, valeurs, err := gtOptions(r)
	if err != nil {
		s.gtMessage(w, err.Error(), true)
		return
	}
	opts.SansScope = true
	resultat, err := grouptemplates.Instancier(context.Background(), annuaireSimule{}, modele, valeurs, opts)
	if err != nil {
		s.gtMessage(w, err.Error(), true)
		return
	}
	s.gtResultatFragment(w, langue, resultat, Tr(langue, "gt.apercu_statut"))
}

// groupTemplatesInstancier applique le modèle au domaine connecté.
func (s *Server) groupTemplatesInstancier(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	langue := s.langue(r)
	modele, ok := trouverModeleGT(grouptemplates.ChargerModeles(s.cheminModelesGT()), strings.TrimSpace(r.FormValue("modele")))
	if !ok {
		s.gtMessage(w, Tr(langue, "gt.modele_introuvable"), true)
		return
	}
	opts, valeurs, err := gtOptions(r)
	if err != nil {
		s.gtMessage(w, err.Error(), true)
		return
	}
	pool, err := s.getPool(sess)
	if err != nil {
		s.gtMessage(w, Tr(langue, "gt.ad_requis"), true)
		return
	}
	opts.DossierConfig = s.dossierConfig()
	resultat, err := grouptemplates.Instancier(r.Context(), grouptemplates.NouvelAnnuaire(pool), modele, valeurs, opts)
	if err != nil {
		s.gtMessage(w, err.Error(), true)
		return
	}
	s.gtResultatFragment(w, langue, resultat, Tr(langue, "gt.instancie_statut"))
}

// groupTemplatesDupliquer crée une copie d'un modèle.
func (s *Server) groupTemplatesDupliquer(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	modeles := grouptemplates.ChargerModeles(s.cheminModelesGT())
	source, ok := trouverModeleGT(modeles, strings.TrimSpace(r.FormValue("modele")))
	if !ok {
		s.erreur(w, Tr(s.langue(r), "gt.modele_introuvable"))
		return
	}
	clone := grouptemplates.DupliquerModele(modeles, source, strings.TrimSpace(r.FormValue("nom")))
	if _, err := grouptemplates.SauvegarderModeles(s.dossierConfig(), append(modeles, clone)); err != nil {
		s.erreur(w, err.Error())
		return
	}
	http.Redirect(w, r, "/app/group-templates", http.StatusSeeOther)
}

// groupTemplatesSupprimer retire un modèle personnalisé.
func (s *Server) groupTemplatesSupprimer(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	modeles := grouptemplates.ChargerModeles(s.cheminModelesGT())
	restants := make([]grouptemplates.ModeleGroupe, 0, len(modeles))
	supprime := false
	for _, modele := range modeles {
		if modele.ID == id {
			supprime = true
			continue
		}
		restants = append(restants, modele)
	}
	if !supprime {
		s.erreur(w, Tr(s.langue(r), "gt.modele_introuvable"))
		return
	}
	if _, err := grouptemplates.SauvegarderModeles(s.dossierConfig(), restants); err != nil {
		s.erreur(w, err.Error())
		return
	}
	http.Redirect(w, r, "/app/group-templates", http.StatusSeeOther)
}

// groupTemplatesExporter télécharge un modèle en JSON ou en XML.
func (s *Server) groupTemplatesExporter(w http.ResponseWriter, r *http.Request, _ *session) {
	langue := s.langue(r)
	modele, ok := trouverModeleGT(grouptemplates.ChargerModeles(s.cheminModelesGT()), strings.TrimSpace(r.URL.Query().Get("id")))
	if !ok {
		s.erreur(w, Tr(langue, "gt.modele_introuvable"))
		return
	}
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	var (
		donnees []byte
		err     error
		ext     string
		typ     string
	)
	switch format {
	case "xml":
		donnees, err = grouptemplates.OctetsXML(modele)
		ext, typ = "xml", "application/xml"
	default:
		donnees, err = grouptemplates.OctetsJSON(modele)
		ext, typ = "json", "application/json"
	}
	if err != nil {
		s.erreur(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", typ+"; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, modele.ID, ext))
	_, _ = w.Write(donnees)
}

// gtResultatFragment rend le compte rendu d'un plan ou d'une instanciation.
func (s *Server) gtResultatFragment(w http.ResponseWriter, langue string, resultat *grouptemplates.ResultatInstantiation, statut string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">`)
	b.WriteString(`<h2 class="text-lg font-semibold">` + Tr(langue, "gt.resultat") + `</h2>`)
	b.WriteString(fmt.Sprintf(`<p class="text-sm"><strong>%s :</strong> %s</p>`,
		Tr(langue, "gt.statut"), html.EscapeString(statut)))
	b.WriteString(`<pre class="mt-3 whitespace-pre-wrap rounded-lg bg-slate-50 p-3 text-xs dark:bg-slate-800">` +
		html.EscapeString(resultat.Resume()) + `</pre>`)
	if len(resultat.Artefacts) > 0 {
		b.WriteString(fmt.Sprintf(`<p class="mt-3 text-sm font-medium">%s</p><ul class="mt-1 list-disc pl-5 text-xs font-mono">`,
			Tr(langue, "gt.artefacts")))
		for _, artefact := range resultat.Artefacts {
			b.WriteString(`<li>` + html.EscapeString(artefact) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}
	if len(resultat.Notes) > 0 {
		b.WriteString(fmt.Sprintf(`<p class="mt-3 text-sm font-medium">%s</p><ul class="mt-1 list-disc pl-5 text-xs">`,
			Tr(langue, "gt.notes")))
		for _, note := range resultat.Notes {
			b.WriteString(`<li>` + html.EscapeString(note) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</div>`)
	_, _ = w.Write([]byte(b.String()))
}

// gtMessage rend un message d'erreur ou d'information.
func (s *Server) gtMessage(w http.ResponseWriter, message string, erreur bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	classe := "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
	if erreur {
		w.WriteHeader(http.StatusUnprocessableEntity)
		classe = "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200"
	}
	fmt.Fprintf(w, `<div class="rounded-lg border %s p-3 text-sm">%s</div>`, classe, html.EscapeString(message))
}
