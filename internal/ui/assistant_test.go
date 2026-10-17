package ui

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/assistant"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// preparerAssistant ouvre un serveur d'interface et une session locale.
// `avecPool` installe un pool AD sans validation (contrôleur injoignable) pour
// exercer les routes qui ont besoin de l'annuaire.
func preparerAssistant(t *testing.T, avecPool bool) (*Server, string, map[string]string) {
	t.Helper()
	s, dir := nouveauServeur(t)
	cookie := ouvrirSessionLocale(t, s)

	if avecPool {
		pool, err := ad.NewPoolNoValidate(ad.Config{
			Domain: "EDUSYNC.LOCAL", Controller: "127.0.0.1", Port: 1,
			AuthMethod: ad.AuthSimple, BindDN: "Administrator",
			BindPassword: "TestPass123", TLSSkipVerify: true,
			Timeout: 500 * time.Millisecond, MaxConns: 2,
		})
		if err != nil {
			t.Fatalf("pool sans validation: %v", err)
		}
		s.mu.Lock()
		s.adPools["Administrator@EDUSYNC.LOCAL"] = pool
		s.mu.Unlock()
	}
	return s, dir, entetesSession(cookie)
}

// TestAssistantPage vérifie le rendu de la page : formats proposés et
// avertissement AD quand aucun pool n'est disponible.
func TestAssistantPage(t *testing.T) {
	s, _, entetes := preparerAssistant(t, false)

	w := demander(t, s, "GET", "/app/assistant", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	corps := w.Body.String()
	for _, attendu := range []string{"Assistant IA", "prenom.nom", "prenomNom", "Aucune connexion AD"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("page sans %q", attendu)
		}
	}
}

// TestAssistantPageAvecAD vérifie que l'avertissement disparaît dès qu'un pool
// est disponible pour la session.
func TestAssistantPageAvecAD(t *testing.T) {
	s, _, entetes := preparerAssistant(t, true)

	w := demander(t, s, "GET", "/app/assistant", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "Aucune connexion AD") {
		t.Error("l'avertissement AD ne devrait pas s'afficher avec un pool")
	}
}

// TestAssistantIdentifiant vérifie la génération d'identifiant et la résolution
// de doublon depuis le formulaire.
func TestAssistantIdentifiant(t *testing.T) {
	s, _, entetes := preparerAssistant(t, false)

	w := demander(t, s, "POST", "/app/assistant/identifiant", url.Values{
		"prenom": {"Thomas"}, "nom": {"Martin"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "thomas.martin") {
		t.Errorf("corps = %.300s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Doublon résolu") {
		t.Error("aucun doublon ne devait être résolu")
	}

	w = demander(t, s, "POST", "/app/assistant/identifiant", url.Values{
		"prenom": {"Thomas"}, "nom": {"Martin"},
		"format": {"prenomNom"}, "existants": {"thomas.martin"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "thomasMartin") {
		t.Errorf("format imposé ignoré : %.300s", w.Body.String())
	}
}

// TestAssistantIdentifiantDoublon vérifie l'affichage du message de doublon.
func TestAssistantIdentifiantDoublon(t *testing.T) {
	s, _, entetes := preparerAssistant(t, false)

	w := demander(t, s, "POST", "/app/assistant/identifiant", url.Values{
		"prenom": {"Thomas"}, "nom": {"Martin"},
		"existants": {"thomas.martin, AUTRE"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "thomas.martin2") {
		t.Errorf("doublon non résolu : %.300s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Doublon résolu") {
		t.Errorf("message de doublon absent : %.300s", w.Body.String())
	}
}

// TestAssistantIdentifiantRequis refuse une saisie incomplète.
func TestAssistantIdentifiantRequis(t *testing.T) {
	s, _, entetes := preparerAssistant(t, false)

	w := demander(t, s, "POST", "/app/assistant/identifiant", url.Values{
		"nom": {"Martin"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Le prénom et le nom sont obligatoires") {
		t.Errorf("corps = %.300s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "text-rose-700") {
		t.Error("l'erreur doit être signalée visuellement")
	}
}

// TestAssistantSecurite vérifie l'analyse de sécurité depuis la page.
func TestAssistantSecurite(t *testing.T) {
	s, _, entetes := preparerAssistant(t, false)

	w := demander(t, s, "POST", "/app/assistant/securite", url.Values{
		"role": {"super_admin"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "Score de sécurité") {
		t.Errorf("score absent : %.300s", corps)
	}
	if !strings.Contains(corps, "super-administrateur") {
		t.Errorf("recommandation RBAC absente : %.300s", corps)
	}
}

// TestAssistantJournal vérifie l'analyse du journal injecté dans le serveur.
func TestAssistantJournal(t *testing.T) {
	s, dir, entetes := preparerAssistant(t, false)

	// Une entrée hors horaires et en échec pour déclencher des recommandations.
	journal, err := audit.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("ouverture du journal: %v", err)
	}
	if err := journal.Record(models.ActionLogEntry{
		Timestamp: "2026-10-17T23:00:00+00:00", ActionType: "delete_user",
		Resultat: "echec", Utilisateur: "aide",
	}); err != nil {
		t.Fatalf("écriture: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("fermeture: %v", err)
	}

	w := demander(t, s, "POST", "/app/assistant/journal", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "Entrées") {
		t.Errorf("résumé absent : %.300s", corps)
	}
	if !strings.Contains(corps, "Hors horaires") {
		t.Errorf("compteur hors horaires absent : %.300s", corps)
	}
}

// TestAssistantAnomaliesSansAD vérifie le message d'erreur quand l'annuaire
// n'est pas joignable.
func TestAssistantAnomaliesSansAD(t *testing.T) {
	s, _, entetes := preparerAssistant(t, true)

	w := demander(t, s, "POST", "/app/assistant/anomalies", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Annuaire indisponible") {
		t.Errorf("corps = %.300s", w.Body.String())
	}
}

// TestAssistantRendus vérifie les fragments HTML produits à partir de données
// forgées : c'est ici que sont couvertes les branches d'affichage, sans
// dépendre d'un annuaire réel.
func TestAssistantRendus(t *testing.T) {
	anomalies := []assistant.Anomalie{{
		Type: assistant.AnomalieIdentifiantsDoubles, Gravite: assistant.GraviteCritique,
		Message: "L'identifiant « lea.martin » est utilisé par 2 comptes.",
		Objets:  []string{"CN=Léa Martin,OU=6e_A,DC=x", "CN=Léo Martin,OU=6e_A,DC=x"},
	}}
	html := assistantListeAnomalies("fr", anomalies)
	if !strings.Contains(html, "critique") || !strings.Contains(html, "&#39;") {
		t.Errorf("anomalies = %s", html)
	}
	if got := assistantListeAnomalies("fr", nil); !strings.Contains(got, "Aucune anomalie") {
		t.Errorf("liste vide = %s", got)
	}

	suggestions := []assistant.Suggestion{{
		Type: assistant.SuggestionGroupesVides, Action: "supprimer",
		Gravite: assistant.GraviteAvertissement, Message: "1 groupe(s) sans membre.",
		Objets: []string{"CN=Vide,DC=x"},
	}}
	if got := assistantListeSuggestions("fr", suggestions); !strings.Contains(got, "supprimer") {
		t.Errorf("suggestions = %s", got)
	}
	if got := assistantListeSuggestions("fr", nil); !strings.Contains(got, "Aucun nettoyage") {
		t.Errorf("suggestions vides = %s", got)
	}

	rapportSecurite := assistant.RapportSecurite{
		Score: 89,
		Recommandations: []assistant.Recommandation{{
			Type: "rbac", Niveau: assistant.GraviteAvertissement,
			Message: "Une délégation ne définit aucune portée.",
		}},
	}
	if got := assistantRapportSecurite("fr", rapportSecurite); !strings.Contains(got, "89/100") {
		t.Errorf("sécurité = %s", got)
	}
	if got := assistantRapportSecurite("fr", assistant.RapportSecurite{Score: 100}); !strings.Contains(got, "Aucune recommandation") {
		t.Errorf("sécurité sans alerte = %s", got)
	}

	rapportJournal := assistant.RapportJournal{
		Total: 3, Simulations: 1, Echecs: 1, HorsHoraires: 1,
		ParType: []assistant.Compteur{{Cle: "create_user", Nombre: 2}},
		Recommandations: []assistant.Recommandation{{
			Type: "journal", Niveau: assistant.GraviteInfo, Message: "1 action hors horaires.",
		}},
	}
	if got := assistantRapportJournal("fr", rapportJournal); !strings.Contains(got, "create_user") {
		t.Errorf("journal = %s", got)
	}

	// Les libellés de gravité sont traduits dans les deux langues.
	if got := libelleGravite("en", assistant.GraviteCritique); got != "critical" {
		t.Errorf("libellé en = %q", got)
	}
	if got := libelleGravite("fr", "inconnu"); got != "info" {
		t.Errorf("libellé par défaut = %q", got)
	}

	// Les formats proposés couvrent les préréglages et les variantes camelCase.
	if formats := formatsAssistant(); len(formats) < 20 {
		t.Errorf("formatsAssistant = %d entrées", len(formats))
	}
}
