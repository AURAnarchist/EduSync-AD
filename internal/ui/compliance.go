package ui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/assistant"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/compliance"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// ─── Conformité & sécurité (M30) ───────────────────────────────────────────

// cheminConfigCompliance retourne le chemin de compliance.json.
func (s *Server) cheminConfigCompliance() string {
	return filepath.Join(s.dossierConfig(), compliance.NomFichier)
}

// pageCompliance affiche la page de conformité et sécurité.
func (s *Server) pageCompliance(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	d := s.donnees(r, Tr(langue, "compliance.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	cfg := compliance.ChargerConfig(s.cheminConfigCompliance())
	d.ComplianceConfig = cfg
	d.ComplianceCheminConfig = s.cheminConfigCompliance()

	s.rendre(w, http.StatusOK, "compliance.html", d)
}

// complianceConfigForm enregistre les réglages de conformité.
func (s *Server) complianceConfigForm(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(langue, "erreur.formulaire"))
		return
	}

	cfg := compliance.ChargerConfig(s.cheminConfigCompliance())
	cfg.MFAActif = valeurCaseACocher(r.FormValue("mfa_actif"))
	cfg.MFARequisPourAdmin = valeurCaseACocher(r.FormValue("mfa_requis_admin"))
	cfg.SessionUnique = valeurCaseACocher(r.FormValue("session_unique_admin"))
	cfg.SignatureExports = valeurCaseACocher(r.FormValue("signature_exports"))

	if _, err := cfg.Sauvegarder(s.cheminConfigCompliance()); err != nil {
		s.erreur(w, Tr(langue, "compliance.erreur_sauvegarde", err.Error()))
		return
	}

	http.Redirect(w, r, "/app/compliance", http.StatusSeeOther)
}

// complianceMFAStatus (HTMX) affiche l'état MFA de l'administrateur.
func (s *Server) complianceMFAStatus(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	cfg := compliance.ChargerConfig(s.cheminConfigCompliance())
	rapport := compliance.VerifierMFAAdmin(cfg)

	s.complianceReponse(w, Tr(langue, "compliance.mfa_titre"),
		complianceRapportMFA(langue, rapport), false)
}

// complianceMFAInit (HTMX) génère un nouveau secret TOTP et des codes de secours.
func (s *Server) complianceMFAInit(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	secret, err := compliance.GenererSecretTOTP()
	if err != nil {
		s.complianceReponse(w, Tr(langue, "compliance.mfa_titre"), html.EscapeString(err.Error()), true)
		return
	}
	codes, _, err := compliance.GenererCodesSecours()
	if err != nil {
		s.complianceReponse(w, Tr(langue, "compliance.mfa_titre"), html.EscapeString(err.Error()), true)
		return
	}

	// Note : on n'enregistre rien encore, l'admin doit d'abord valider
	// qu'il a bien scanné le QR (simulé ici par l'affichage du secret).
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="space-y-4">`)
	fmt.Fprintf(&b, `<p class="text-sm">%s</p>`, html.EscapeString(Tr(langue, "compliance.mfa_init_aide")))
	fmt.Fprintf(&b, `<div class="rounded bg-slate-100 p-3 font-mono text-lg dark:bg-slate-800 text-center">%s</div>`, html.EscapeString(secret))
	fmt.Fprintf(&b, `<p class="text-sm font-semibold">%s</p>`, html.EscapeString(Tr(langue, "compliance.codes_secours")))
	fmt.Fprintf(&b, `<div class="grid grid-cols-2 gap-2 rounded bg-slate-100 p-3 font-mono text-xs dark:bg-slate-800">`)
	for _, c := range codes {
		fmt.Fprintf(&b, `<div>%s</div>`, html.EscapeString(c))
	}
	fmt.Fprintf(&b, `</div>`)
	fmt.Fprintf(&b, `<p class="text-xs text-slate-500 italic">%s</p>`, html.EscapeString(Tr(langue, "compliance.mfa_init_rappel")))
	fmt.Fprintf(&b, `</div>`)

	s.complianceReponse(w, Tr(langue, "compliance.mfa_init_titre"), b.String(), false)
}

// complianceRGPD (HTMX) génère le rapport RGPD.
func (s *Server) complianceRGPD(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	store, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		s.complianceReponse(w, Tr(langue, "compliance.rgpd_titre"), html.EscapeString(err.Error()), true)
		return
	}
	defer store.Fermer()

	if _, err := config.Load(s.opts.ConfigPath); err != nil {
		// Ignore error - we'll use defaults in GenererRapportRGPD if needed
	}

	// Construire les sources pour le rapport RGPD
	var sources compliance.SourcesRGPD

	// Comptes de l'annuaire AD
	if pool, err := s.getPool(sess); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		collecteur := assistant.NouveauCollecteurAD(pool)
		inventaire, err := collecteur.Collecter(ctx, "")
		if err == nil {
			for _, compte := range inventaire.Comptes {
				sources.Comptes = append(sources.Comptes, compliance.CompteRGPD{
					Identifiant: compte.Identifiant,
					NomComplet:  compte.NomComplet,
					Mail:        compte.Mail,
					OU:          compte.OU,
					Actif:       compte.Actif,
					CreeLe:      "0001-01-01T00:00:00Z", // Default if unknown
				})
			}
		}
	}

	// Journal d'actions
	if entrees, err := s.journal.List(audit.Filter{Limit: 5000}); err == nil {
		sources.Entrees = entrees
	}

	// Pré-inscriptions du portail
	if demandes, err := store.ListerDemandes(""); err == nil {
		sources.Demandes = demandes
	}

	// Coffre-fort local
	if coffre, err := s.ouvrirCoffreUI(); err == nil {
		if sams, err := coffre.Liste(); err == nil {
			sources.Secrets = sams
		}
		coffre.Close()
	}

	// Générer le rapport - utiliser directement les options par défaut
	rapport := compliance.GenererRapportRGPD(sources, compliance.OptionsRGPD{}, time.Now())

	if err != nil {
		s.complianceReponse(w, Tr(langue, "compliance.rgpd_titre"), html.EscapeString(err.Error()), true)
		return
	}

	s.complianceReponse(w, Tr(langue, "compliance.rgpd_titre"),
		complianceRapportRGPD(langue, rapport), false)
}

// compliancePSO (HTMX) analyse la granularité des mots de passe.
func (s *Server) compliancePSO(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		cfg = config.DefaultAppConfig()
	}

	rapport := compliance.AnalyserPSO(cfg)
	s.complianceReponse(w, Tr(langue, "compliance.pso_titre"),
		complianceRapportPSO(langue, rapport), false)
}

// complianceReponse écrit un fragment HTML de résultat pour HTMX.
func (s *Server) complianceReponse(w http.ResponseWriter, titre, corps string, erreur bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	classe := "text-slate-700 dark:text-slate-200"
	if erreur {
		classe = "text-rose-700 dark:text-rose-300"
	}
	fmt.Fprintf(w, `<div class="%s text-sm"><p class="font-semibold mb-2">%s</p>%s</div>`,
		classe, html.EscapeString(titre), corps)
}

// complianceRapportMFA rend le rapport MFA en HTML.
func complianceRapportMFA(langue string, rapport compliance.RapportMFA) string {
	var b strings.Builder
	statut := Tr(langue, "compliance.mfa_statut_"+string(rapport.Statut))
	fmt.Fprintf(&b, `<p class="mb-2">%s : <span class="font-semibold">%s</span></p>`,
		html.EscapeString(Tr(langue, "compliance.statut")), html.EscapeString(statut))

	if len(rapport.Details) > 0 {
		b.WriteString(`<ul class="list-disc pl-5 space-y-1 mb-3 text-xs">`)
		for _, d := range rapport.Details {
			fmt.Fprintf(&b, `<li>%s</li>`, html.EscapeString(d))
		}
		b.WriteString(`</ul>`)
	}

	if len(rapport.Recommandations) > 0 {
		fmt.Fprintf(&b, `<p class="text-xs font-semibold uppercase tracking-wider text-slate-500 mb-1">%s</p>`,
			html.EscapeString(Tr(langue, "compliance.recommandations")))
		b.WriteString(`<ul class="list-disc pl-5 space-y-1 text-xs text-amber-700 dark:text-amber-300">`)
		for _, r := range rapport.Recommandations {
			fmt.Fprintf(&b, `<li>%s</li>`, html.EscapeString(r))
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}

// complianceRapportRGPD rend le rapport RGPD en HTML.
func complianceRapportRGPD(langue string, rapport compliance.RapportRGPD) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="text-xs text-slate-500 mb-3">%s %s</p>`,
		html.EscapeString(Tr(langue, "compliance.rgpd_genere_le")), html.EscapeString(rapport.GenerationDate))

	fmt.Fprintf(&b, `<div class="grid grid-cols-2 gap-4 mb-4">`)
	fmt.Fprintf(&b, `<div class="rounded border p-2">`)
	fmt.Fprintf(&b, `<div class="text-xs text-slate-500">%s</div>`, html.EscapeString(Tr(langue, "compliance.rgpd_total_comptes")))
	fmt.Fprintf(&b, `<div class="text-lg font-semibold">%d</div>`, rapport.Inventaire.TotalComptes)
	fmt.Fprintf(&b, `</div>`)
	fmt.Fprintf(&b, `<div class="rounded border p-2">`)
	fmt.Fprintf(&b, `<div class="text-xs text-slate-500">%s</div>`, html.EscapeString(Tr(langue, "compliance.rgpd_comptes_anciens")))
	fmt.Fprintf(&b, `<div class="text-lg font-semibold">%d</div>`, rapport.Inventaire.ComptesAnciens)
	fmt.Fprintf(&b, `</div>`)
	fmt.Fprintf(&b, `</div>`)

	if len(rapport.Recommandations) > 0 {
		fmt.Fprintf(&b, `<p class="text-xs font-semibold mb-1">%s</p>`, html.EscapeString(Tr(langue, "compliance.recommandations")))
		b.WriteString(`<ul class="list-disc pl-5 space-y-1 text-xs">`)
		for _, r := range rapport.Recommandations {
			fmt.Fprintf(&b, `<li><span class="font-semibold text-amber-600">[%s]</span> %s</li>`,
				html.EscapeString(r.Type), html.EscapeString(r.Message))
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}

// complianceRapportPSO rend l'analyse PSO en HTML.
func complianceRapportPSO(langue string, rapport compliance.RapportPSO) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="mb-2">%s : <span class="font-semibold">%d/100</span></p>`,
		html.EscapeString(Tr(langue, "compliance.score")), rapport.Score)

	if len(rapport.Recommandations) > 0 {
		b.WriteString(`<ul class="list-disc pl-5 space-y-1 text-xs">`)
		for _, r := range rapport.Recommandations {
			fmt.Fprintf(&b, `<li>%s</li>`, html.EscapeString(r))
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}
