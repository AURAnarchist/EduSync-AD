package ui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/assistant"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
)

// ─── Assistant IA (M29) ───────────────────────────────────────────────────

// formatsAssistant retourne les formats d'identifiant proposés dans le
// formulaire : préréglages du cahier des charges puis variantes camelCase.
func formatsAssistant() []string {
	formats := make([]string, 0, len(config.PresetsIdentifiants)+len(config.PresetsIdentifiantsCamel))
	formats = append(formats, config.PresetsIdentifiants...)
	formats = append(formats, config.PresetsIdentifiantsCamel...)
	return formats
}

// pageAssistant affiche la page de l'assistant : identifiant intelligent,
// anomalies, nettoyage, sécurité et journal.
func (s *Server) pageAssistant(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	d := s.donnees(r, Tr(langue, "assistant.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	d.AssistantFormats = formatsAssistant()

	if _, err := s.getPool(sess); err != nil {
		d.AssistantADDisponible = false
		d.AssistantAvertissement = Tr(langue, "assistant.avertissement_ad")
	} else {
		d.AssistantADDisponible = true
	}
	s.rendre(w, http.StatusOK, "assistant.html", d)
}

// inventaireAssistant collecte l'inventaire de l'annuaire de la session.
func (s *Server) inventaireAssistant(sess *session) (assistant.Inventaire, error) {
	pool, err := s.getPool(sess)
	if err != nil {
		return assistant.Inventaire{}, err
	}
	ctx, annuler := context.WithTimeout(context.Background(), 30*time.Second)
	defer annuler()
	return assistant.NouveauCollecteurAD(pool).Collecter(ctx, "")
}

// assistantAnomalies détecte les anomalies de l'annuaire.
func (s *Server) assistantAnomalies(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	inventaire, err := s.inventaireAssistant(sess)
	if err != nil {
		s.assistantReponse(w, Tr(langue, "assistant.erreur_annuaire"), html.EscapeString(err.Error()), true)
		return
	}
	anomalies := assistant.DetecterAnomalies(inventaire, assistant.OptionsAnomaliesToutes())
	s.assistantReponse(w, Tr(langue, "assistant.anomalies_titre"),
		assistantListeAnomalies(langue, anomalies), false)
}

// assistantCleanup suggère le nettoyage de fin d'année.
func (s *Server) assistantCleanup(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	inventaire, err := s.inventaireAssistant(sess)
	if err != nil {
		s.assistantReponse(w, Tr(langue, "assistant.erreur_annuaire"), html.EscapeString(err.Error()), true)
		return
	}
	suggestions := assistant.SuggererNettoyage(inventaire, assistant.OptionsNettoyageToutes())
	s.assistantReponse(w, Tr(langue, "assistant.cleanup_titre"),
		assistantListeSuggestions(langue, suggestions), false)
}

// assistantSecurite analyse la politique de mots de passe et le rôle RBAC.
func (s *Server) assistantSecurite(w http.ResponseWriter, r *http.Request, _ *session) {
	langue := s.langue(r)
	if err := r.ParseForm(); err != nil {
		s.assistantReponse(w, Tr(langue, "erreur.formulaire"), "", true)
		return
	}

	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		cfg = config.DefaultAppConfig()
	}
	rapport := assistant.AnalyserSecurite(cfg, assistant.ContexteSecurite{
		Role:         strings.TrimSpace(r.FormValue("role")),
		PorteesVides: valeurCaseACocher(r.FormValue("portees_vides")),
	})
	s.assistantReponse(w, Tr(langue, "assistant.securite_titre"),
		assistantRapportSecurite(langue, rapport), false)
}

// assistantJournal analyse le journal d'actions.
func (s *Server) assistantJournal(w http.ResponseWriter, r *http.Request, _ *session) {
	langue := s.langue(r)

	cheminJournal := s.opts.AuditPath
	if cheminJournal == "" {
		var err error
		if cheminJournal, err = audit.DefaultPath(); err != nil {
			s.assistantReponse(w, Tr(langue, "assistant.erreur_journal"), html.EscapeString(err.Error()), true)
			return
		}
	}
	journal, err := audit.Open(cheminJournal)
	if err != nil {
		s.assistantReponse(w, Tr(langue, "assistant.erreur_journal"), html.EscapeString(err.Error()), true)
		return
	}
	defer func() { _ = journal.Close() }()

	entrees, err := journal.List(audit.Filter{Limit: 500})
	if err != nil {
		s.assistantReponse(w, Tr(langue, "assistant.erreur_journal"), html.EscapeString(err.Error()), true)
		return
	}
	rapport := assistant.AnalyserJournal(entrees)
	s.assistantReponse(w, Tr(langue, "assistant.journal_titre"),
		assistantRapportJournal(langue, rapport), false)
}

// assistantIdentifiant génère un identifiant unique pour un usager.
func (s *Server) assistantIdentifiant(w http.ResponseWriter, r *http.Request, _ *session) {
	langue := s.langue(r)
	if err := r.ParseForm(); err != nil {
		s.assistantReponse(w, Tr(langue, "erreur.formulaire"), "", true)
		return
	}

	prenom := strings.TrimSpace(r.FormValue("prenom"))
	nom := strings.TrimSpace(r.FormValue("nom"))
	if prenom == "" || nom == "" {
		s.assistantReponse(w, Tr(langue, "assistant.identifiant_titre"),
			html.EscapeString(Tr(langue, "assistant.prenom_nom_requis")), true)
		return
	}

	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		cfg = config.DefaultAppConfig()
	}

	existants := map[string]bool{}
	for _, identifiant := range strings.Split(r.FormValue("existants"), ",") {
		if identifiant = strings.ToLower(strings.TrimSpace(identifiant)); identifiant != "" {
			existants[identifiant] = true
		}
	}

	identifiant, doublon, err := assistant.GenererIdentifiantIntelligent(
		cfg, prenom, nom, strings.TrimSpace(r.FormValue("format")), existants)
	if err != nil {
		s.assistantReponse(w, Tr(langue, "assistant.identifiant_titre"), html.EscapeString(err.Error()), true)
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<p class="font-mono text-base font-semibold">%s</p>`, html.EscapeString(identifiant))
	if doublon {
		fmt.Fprintf(&b, `<p class="text-xs">%s</p>`, html.EscapeString(Tr(langue, "assistant.doublon_resolu")))
	}
	s.assistantReponse(w, Tr(langue, "assistant.identifiant_titre"), b.String(), false)
}

// assistantReponse écrit un fragment HTML de résultat pour HTMX.
func (s *Server) assistantReponse(w http.ResponseWriter, titre, corps string, erreur bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	classe := "text-slate-700 dark:text-slate-200"
	if erreur {
		classe = "text-rose-700 dark:text-rose-300"
	}
	fmt.Fprintf(w, `<div class="%s text-sm"><p class="font-semibold">%s</p>%s</div>`,
		classe, html.EscapeString(titre), corps)
}

// libelleGravite traduit un niveau de gravité.
func libelleGravite(langue, gravite string) string {
	switch gravite {
	case assistant.GraviteCritique:
		return Tr(langue, "assistant.critique")
	case assistant.GraviteAvertissement:
		return Tr(langue, "assistant.avertissement")
	default:
		return Tr(langue, "assistant.info")
	}
}

// assistantPuces rend une liste de puces à partir d'éléments déjà échappés.
func assistantPuces(elements []string) string {
	if len(elements) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<ul class="mt-1 list-disc space-y-0.5 pl-5 text-xs">`)
	for _, element := range elements {
		b.WriteString("<li>" + element + "</li>")
	}
	b.WriteString("</ul>")
	return b.String()
}

// assistantListeAnomalies rend la liste des anomalies.
func assistantListeAnomalies(langue string, anomalies []assistant.Anomalie) string {
	if len(anomalies) == 0 {
		return `<p class="text-sm">` + html.EscapeString(Tr(langue, "assistant.aucune_anomalie")) + `</p>`
	}
	elements := make([]string, 0, len(anomalies))
	for _, anomalie := range anomalies {
		objets := make([]string, 0, len(anomalie.Objets))
		for _, objet := range anomalie.Objets {
			objets = append(objets, `<span class="font-mono">`+html.EscapeString(objet)+`</span>`)
		}
		elements = append(elements, fmt.Sprintf("[%s] %s%s",
			html.EscapeString(libelleGravite(langue, anomalie.Gravite)),
			html.EscapeString(anomalie.Message),
			assistantPuces(objets)))
	}
	return assistantPuces(elements)
}

// assistantListeSuggestions rend la liste des suggestions de nettoyage.
func assistantListeSuggestions(langue string, suggestions []assistant.Suggestion) string {
	if len(suggestions) == 0 {
		return `<p class="text-sm">` + html.EscapeString(Tr(langue, "assistant.aucun_nettoyage")) + `</p>`
	}
	elements := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		elements = append(elements, fmt.Sprintf("[%s] %s — %s",
			html.EscapeString(libelleGravite(langue, suggestion.Gravite)),
			html.EscapeString(suggestion.Action),
			html.EscapeString(suggestion.Message)))
	}
	return assistantPuces(elements)
}

// assistantRapportSecurite rend le rapport de sécurité.
func assistantRapportSecurite(langue string, rapport assistant.RapportSecurite) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="mb-2">%s : <span class="font-semibold">%d/100</span></p>`,
		html.EscapeString(Tr(langue, "assistant.score")), rapport.Score)
	if len(rapport.Recommandations) == 0 {
		b.WriteString(`<p class="text-sm">` + html.EscapeString(Tr(langue, "assistant.aucune_recommandation")) + `</p>`)
		return b.String()
	}
	elements := make([]string, 0, len(rapport.Recommandations))
	for _, recommandation := range rapport.Recommandations {
		elements = append(elements, fmt.Sprintf("[%s] %s",
			html.EscapeString(libelleGravite(langue, recommandation.Niveau)),
			html.EscapeString(recommandation.Message)))
	}
	b.WriteString(assistantPuces(elements))
	return b.String()
}

// assistantRapportJournal rend le résumé du journal.
func assistantRapportJournal(langue string, rapport assistant.RapportJournal) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="mb-2">%s : %d · %s : %d · %s : %d · %s : %d</p>`,
		html.EscapeString(Tr(langue, "assistant.total")), rapport.Total,
		html.EscapeString(Tr(langue, "assistant.simulations")), rapport.Simulations,
		html.EscapeString(Tr(langue, "assistant.echecs")), rapport.Echecs,
		html.EscapeString(Tr(langue, "assistant.hors_horaires")), rapport.HorsHoraires)

	elements := []string{}
	for _, compteur := range rapport.ParType {
		elements = append(elements, fmt.Sprintf("%s : %d",
			html.EscapeString(compteur.Cle), compteur.Nombre))
	}
	b.WriteString(assistantPuces(elements))
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(&b, `<p class="mt-1">[%s] %s</p>`,
			html.EscapeString(libelleGravite(langue, recommandation.Niveau)),
			html.EscapeString(recommandation.Message))
	}
	return b.String()
}
