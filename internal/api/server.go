package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Serveur HTTP est le serveur de l'API REST.
//
// Il s'appuie sur la bibliothèque standard (`net/http`) : aucune dépendance
// externe, comme le reste du projet. Chaque requête traverse les mêmes étapes
// que côté Python : en-têtes de sécurité, CORS, authentification par clé,
// limitation de débit, routage puis sérialisation JSON.
type Serveur struct {
	// Config : réglages d'écoute (hôte, port, CORS, limitation).
	Config Config
	// Routeur : routes déclarées.
	Routeur *Routeur
	// Magasin : clés d'API (nil = authentification impossible).
	Magasin *MagasinCles
	// Version : version de l'application renvoyée par /health.
	Version string
	// Journalise active la trace des requêtes sur la sortie standard.
	Journalise bool

	mu       sync.Mutex
	serveur  *http.Server
	ecouteur net.Listener
	compteur *compteurDebit
}

// NouveauServeur construit un serveur pour une configuration et un routeur.
func NouveauServeur(cfg Config, routeur *Routeur, magasin *MagasinCles) *Serveur {
	if routeur == nil {
		routeur = NouveauRouteur()
	}
	return &Serveur{
		Config:   cfg,
		Routeur:  routeur,
		Magasin:  magasin,
		compteur: nouveauCompteurDebit(),
	}
}

// URL retourne l'adresse d'écoute affichable.
func (s *Serveur) URL() string {
	return s.Config.BaseURL()
}

// EnEcoute indique si le serveur accepte des connexions.
func (s *Serveur) EnEcoute() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ecouteur != nil
}

// Demarrer met le serveur en écoute (idempotent).
func (s *Serveur) Demarrer() error {
	if erreurs := s.Config.Valider(); len(erreurs) > 0 {
		return nouvelleErreurAPI(http.StatusBadRequest, "invalid_config",
			"Configuration API invalide : "+strings.Join(erreurs, "; "), nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ecouteur != nil {
		return nil
	}

	adresse := fmt.Sprintf("%s:%d", s.Config.Hote, s.Config.Port)
	ecouteur, err := net.Listen("tcp", adresse)
	if err != nil {
		return fmt.Errorf("api: écoute sur %s: %w", adresse, err)
	}
	// Le port 0 laisse le système choisir : les tests et l'interface
	// affichent alors l'adresse réellement ouverte.
	if tcp, ok := ecouteur.Addr().(*net.TCPAddr); ok {
		s.Config.Port = tcp.Port
	}

	serveur := &http.Server{
		Handler:           http.HandlerFunc(s.servir),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.serveur = serveur
	s.ecouteur = ecouteur
	go func() {
		if err := serveur.Serve(ecouteur); err != nil && err != http.ErrServerClosed {
			if s.Journalise {
				log.Printf("api: serveur arrêté: %v", err)
			}
		}
	}()
	return nil
}

// Arreter ferme le serveur (idempotent).
func (s *Serveur) Arreter() error {
	s.mu.Lock()
	serveur, ecouteur := s.serveur, s.ecouteur
	s.serveur, s.ecouteur = nil, nil
	s.mu.Unlock()
	if serveur == nil {
		return nil
	}
	// `Close` plutôt que `Shutdown` : le serveur doit rendre la main
	// immédiatement, sans attendre les requêtes en cours de lecture.
	_ = ecouteur.Close()
	return serveur.Close()
}

// servir traite une requête HTTP : sécurité, CORS, authentification, routage.
func (s *Serveur) servir(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")

	// CORS : l'origine n'est renvoyée que si elle est autorisée.
	origine := r.Header.Get("Origin")
	if origine != "" && s.origineAutorisee(origine) {
		autorisee := origine
		if contientOrigine(s.Config.CORSOrigines, "*") {
			autorisee = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", autorisee)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, X-API-Key, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	chemin := strings.TrimSuffix(r.URL.Path, "/")
	if chemin == "" {
		chemin = "/"
	}
	route, parametres := s.Routeur.Resoudre(r.Method, chemin)
	if route == nil {
		// Un chemin connu avec une autre méthode mérite un 405 explicite.
		if s.Routeur.Existe(chemin) {
			s.ecrire(w, http.StatusMethodNotAllowed,
				ReponseErreur("method_not_allowed",
					"Méthode non autorisée pour cette ressource", nil))
			return
		}
		s.ecrire(w, http.StatusNotFound,
			ReponseErreur("not_found",
				fmt.Sprintf("Route inconnue : %s %s", r.Method, chemin), nil))
		return
	}

	corps, err := lireCorps(r)
	if err != nil {
		s.ecrire(w, http.StatusRequestEntityTooLarge,
			ReponseErreur("payload_too_large", err.Error(), nil))
		return
	}

	requete := &Requete{
		Methode: r.Method,
		Chemin:  chemin,
		Entetes: r.Header.Clone(),
		Corps:   corps,
		Requete: r.URL.Query(),
		Params:  parametres,
	}

	// Les routes publiques (documentation OpenAPI) sont accessibles sans clé ;
	// toutes les autres exigent une clé valide.
	if !route.Publique {
		if s.Magasin == nil {
			s.ecrire(w, http.StatusUnauthorized,
				ReponseErreur("unauthorized", "Clé API invalide ou absente", nil))
			return
		}
		cle := extraireCle(r)
		info := s.Magasin.Valider(cle, time.Now().UTC())
		if info == nil {
			s.ecrire(w, http.StatusUnauthorized,
				ReponseErreur("unauthorized", "Clé API invalide ou absente", nil))
			return
		}
		requete.CleAPI = cle
		requete.InfoCle = info

		if limite := s.Config.LimiteRPM; limite > 0 {
			if reste, ok := s.compteur.autoriser(info.ID, limite); !ok {
				w.Header().Set("Retry-After", "60")
				s.ecrire(w, http.StatusTooManyRequests,
					ReponseErreur("rate_limited",
						fmt.Sprintf("Limite de %d requêtes/minute atteinte", limite), nil))
				return
			} else {
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limite))
				w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", reste))
			}
		}
	}

	if s.Journalise {
		log.Printf("api: %s %s (%s)", r.Method, chemin, requete.NomCle())
	}

	debut := time.Now()
	statut, charge := s.executer(route.Handler, requete)
	w.Header().Set("X-Duration-Ms", fmt.Sprintf("%d", time.Since(debut).Milliseconds()))
	s.ecrire(w, statut, charge)
}

// executer appelle le handler en convertissant un panique en erreur 500.
func (s *Serveur) executer(handler HandlerAPI, requete *Requete) (statut int, charge any) {
	defer func() {
		if probleme := recover(); probleme != nil {
			if s.Journalise {
				log.Printf("api: panique sur %s : %v", requete.Chemin, probleme)
			}
			statut, charge = http.StatusInternalServerError,
				ReponseErreur("internal_error", "Erreur interne du serveur", nil)
		}
	}()
	return handler(requete)
}

// ecrire sérialise une réponse JSON.
func (s *Serveur) ecrire(w http.ResponseWriter, statut int, charge any) {
	donnees, err := json.Marshal(charge)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"internal_error",` +
			`"message":"Erreur de sérialisation","details":{}}}`))
		return
	}
	donnees = append(donnees, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(donnees)))
	w.WriteHeader(statut)
	_, _ = w.Write(donnees)
}

// origineAutorisee indique si l'origine peut recevoir une réponse CORS.
func (s *Serveur) origineAutorisee(origine string) bool {
	origines := s.Config.CORSOrigines
	if len(origines) == 0 {
		return false
	}
	return contientOrigine(origines, "*") || contientOrigine(origines, origine)
}

// extraireCle lit la clé d'API dans `X-API-Key` ou `Authorization: Bearer`.
func extraireCle(r *http.Request) string {
	if cle := strings.TrimSpace(r.Header.Get("X-API-Key")); cle != "" {
		return cle
	}
	entete := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(entete) > 7 && strings.EqualFold(entete[:7], "bearer ") {
		return strings.TrimSpace(entete[7:])
	}
	return ""
}

// lireCorps lit le corps de la requête en refusant au-delà de `CorpsMax`.
func lireCorps(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	if r.ContentLength > CorpsMax {
		return nil, fmt.Errorf("corps trop volumineux (maximum %d octets)", CorpsMax)
	}
	limite := io.LimitReader(r.Body, CorpsMax+1)
	corps, err := io.ReadAll(limite)
	if err != nil {
		return nil, fmt.Errorf("lecture du corps impossible : %w", err)
	}
	if len(corps) > CorpsMax {
		return nil, fmt.Errorf("corps trop volumineux (maximum %d octets)", CorpsMax)
	}
	return corps, nil
}

// NomCle retourne un libellé de clé journalisable (jamais la clé entière).
func (r *Requete) NomCle() string {
	if r.InfoCle == nil {
		return "anonyme"
	}
	return r.InfoCle.Nom + " (" + r.InfoCle.Prefixe + "…)"
}

// contientOrigine indique si la liste contient la valeur.
func contientOrigine(origines []string, valeur string) bool {
	for _, origine := range origines {
		if origine == valeur {
			return true
		}
	}
	return false
}

// ─── Limitation de débit ───────────────────────────────────────────────────

// compteurDebit compte les requêtes par clé sur une fenêtre glissante d'une
// minute.
type compteurDebit struct {
	mu      sync.Mutex
	fenetre map[string]*fenetreDebit
}

// fenetreDebit mémorise le début de fenêtre et le nombre de requêtes.
type fenetreDebit struct {
	debut  time.Time
	nombre int
}

// nouveauCompteurDebit construit un compteur vide.
func nouveauCompteurDebit() *compteurDebit {
	return &compteurDebit{fenetre: map[string]*fenetreDebit{}}
}

// autoriser incrémente le compteur et indique si la requête passe, avec le
// nombre de requêtes restantes.
func (c *compteurDebit) autoriser(cle string, limite int) (int, bool) {
	moment := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Purge opportuniste des fenêtres échues : le nombre de clés est faible,
	// mais il ne doit pas croître indéfiniment.
	for identifiant, fenetre := range c.fenetre {
		if moment.Sub(fenetre.debut) >= time.Minute {
			delete(c.fenetre, identifiant)
		}
	}
	fenetre, ok := c.fenetre[cle]
	if !ok || moment.Sub(fenetre.debut) >= time.Minute {
		fenetre = &fenetreDebit{debut: moment}
		c.fenetre[cle] = fenetre
	}
	if fenetre.nombre >= limite {
		return 0, false
	}
	fenetre.nombre++
	restant := limite - fenetre.nombre
	if restant < 0 {
		restant = 0
	}
	return restant, true
}

// ─── Test à blanc ──────────────────────────────────────────────────────────

// ClientTest permet d'exercer le routeur sans ouvrir de port (tests, CLI).
// Il partage exactement la même chaîne de traitement que le serveur HTTP.
func (s *Serveur) ClientTest(cleAPI string) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &transportTest{
			serveur: s,
			cleAPI:  cleAPI,
		},
	}
}

// transportTest exécute les requêtes du client de test en mémoire.
type transportTest struct {
	serveur *Serveur
	cleAPI  string
}

// RoundTrip implémente http.RoundTripper.
func (t *transportTest) RoundTrip(r *http.Request) (*http.Response, error) {
	enregistreur := &enregistreurReponse{entetes: http.Header{}}
	requete := r.Clone(context.Background())
	if t.cleAPI != "" && requete.Header.Get("X-API-Key") == "" {
		requete.Header.Set("X-API-Key", t.cleAPI)
	}
	t.serveur.servir(enregistreur, requete)
	return enregistreur.reponse(requete), nil
}

// enregistreurReponse capture une réponse en mémoire.
type enregistreurReponse struct {
	entetes     http.Header
	corps       []byte
	statut      int
	enteteEcrit bool
}

// Header implémente http.ResponseWriter.
func (e *enregistreurReponse) Header() http.Header { return e.entetes }

// WriteHeader implémente http.ResponseWriter.
func (e *enregistreurReponse) WriteHeader(statut int) {
	if e.enteteEcrit {
		return
	}
	e.enteteEcrit = true
	e.statut = statut
}

// Write implémente http.ResponseWriter.
func (e *enregistreurReponse) Write(donnees []byte) (int, error) {
	if !e.enteteEcrit {
		e.WriteHeader(http.StatusOK)
	}
	e.corps = append(e.corps, donnees...)
	return len(donnees), nil
}

// reponse construit la réponse HTTP à partir de la capture.
func (e *enregistreurReponse) reponse(requete *http.Request) *http.Response {
	statut := e.statut
	if statut == 0 {
		statut = http.StatusOK
	}
	return &http.Response{
		StatusCode:    statut,
		Status:        fmt.Sprintf("%d %s", statut, http.StatusText(statut)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        e.entetes,
		Body:          io.NopCloser(strings.NewReader(string(e.corps))),
		ContentLength: int64(len(e.corps)),
		Request:       requete,
	}
}
