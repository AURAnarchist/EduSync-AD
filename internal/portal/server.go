package portal

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CorpsMax est la taille maximale acceptée pour un corps de formulaire.
const CorpsMax = 16 * 1024

// LabelsScope associe chaque portée à son libellé.
var LabelsScope = map[string]string{
	ScopeReinitialisation: "Réinitialiser mon mot de passe",
	ScopeConsultation:     "Consulter mon identifiant",
}

// LibellesProfil sont les libellés des profils d'une demande.
var LibellesProfil = map[string]string{
	ProfilEleve:     "Élève",
	ProfilPersonnel: "Personnel",
}

const cssPortail = `
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin: 0; padding: 2rem 1rem; font-family: system-ui, -apple-system,
       "Segoe UI", Roboto, sans-serif; line-height: 1.5; background: #f4f6fb;
       color: #1c2333; }
main { max-width: 34rem; margin: 0 auto; background: #fff; padding: 1.75rem;
       border-radius: 12px; box-shadow: 0 6px 24px rgba(20, 30, 60, .08); }
h1 { font-size: 1.35rem; margin: 0 0 .25rem; }
h2 { font-size: 1.1rem; margin: 0 0 .75rem; }
.lead { margin-top: 0; color: #55607a; }
a { color: #1d4ed8; }
.cards { list-style: none; padding: 0; margin: 1rem 0 0; display: grid;
         gap: .75rem; }
.cards a { display: block; padding: .9rem 1rem; border: 1px solid #d7ddec;
           border-radius: 10px; text-decoration: none; font-weight: 600; }
.cards a:hover { border-color: #1d4ed8; background: #f0f4ff; }
.cards p { margin: .35rem 0 0; font-weight: 400; font-size: .9rem;
           color: #55607a; }
label { display: block; font-weight: 600; margin: .9rem 0 .3rem; }
input, select, textarea { width: 100%; padding: .55rem .65rem; font-size: 1rem;
                          border: 1px solid #c3cade; border-radius: 8px;
                          background: #fff; color: inherit; }
button { margin-top: 1.1rem; padding: .6rem 1.1rem; font-size: 1rem;
         font-weight: 600; color: #fff; background: #1d4ed8;
         border: 0; border-radius: 8px; cursor: pointer; }
button:hover { background: #1740b5; }
.msg { padding: .75rem .9rem; border-radius: 8px; margin: 1rem 0;
       font-size: .95rem; }
.msg.ok { background: #e7f7ec; border: 1px solid #a7ddba; color: #14603a; }
.msg.err { background: #fdecec; border: 1px solid #f0b4b4; color: #8a1f1f; }
.msg.info { background: #eef2ff; border: 1px solid #c3cbf5; color: #26327a; }
.hint { font-size: .85rem; color: #55607a; margin: .35rem 0 0; }
footer { max-width: 34rem; margin: 1rem auto 0; text-align: center;
         font-size: .85rem; color: #6b748c; }
footer a { color: inherit; }
dl { margin: .5rem 0 0; }
dt { font-size: .8rem; text-transform: uppercase; letter-spacing: .04em;
     color: #6b748c; margin-top: .75rem; }
dd { margin: .1rem 0 0; font-weight: 600; word-break: break-all; }
`

// Serveur gère le cycle de vie du serveur HTTP du portail.
//
// Toutes les valeurs affichées passent par html.EscapeString (équivalent de
// `html.escape` côté Python) : aucune donnée de l'annuaire ou de la requête
// n'est écrite brute dans la page.
type Serveur struct {
	Service *Service
	Hote    string
	Port    int

	mu      sync.Mutex
	serveur *http.Server
	ecoute  net.Listener
}

// NouveauServeur construit le serveur (le port 0 demande un port éphémère).
func NouveauServeur(service *Service, hote string, port int) *Serveur {
	if hote == "" {
		hote = "127.0.0.1"
	}
	return &Serveur{Service: service, Hote: hote, Port: port}
}

// EnEcoute indique si le serveur tourne.
func (s *Serveur) EnEcoute() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ecoute != nil
}

// URL retourne l'adresse du portail (port réellement attribué si en écoute).
func (s *Serveur) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urlSansVerrou()
}

// urlSansVerrou calcule l'URL ; l'appelant doit détenir le verrou.
func (s *Serveur) urlSansVerrou() string {
	if s.ecoute == nil {
		return s.Service.Config.BaseURL()
	}
	hote, port, err := net.SplitHostPort(s.ecoute.Addr().String())
	if err != nil {
		return s.Service.Config.BaseURL()
	}
	if hote == "0.0.0.0" || hote == "::" || hote == "" || hote == "*" {
		hote = "127.0.0.1"
	}
	if strings.Contains(hote, ":") {
		hote = "[" + hote + "]"
	}
	return fmt.Sprintf("http://%s:%s", hote, port)
}

// Demarrer met le serveur en écoute dans une goroutine détachée et retourne
// l'URL. Idempotent.
func (s *Serveur) Demarrer() (string, error) {
	s.mu.Lock()
	if s.ecoute != nil {
		url := s.urlSansVerrou()
		s.mu.Unlock()
		return url, nil
	}
	ecoute, err := net.Listen("tcp", net.JoinHostPort(s.Hote, fmt.Sprintf("%d", s.Port)))
	if err != nil {
		s.mu.Unlock()
		return "", nouvelleErreurPortail(
			"Impossible d'écouter sur %s:%d — %s", s.Hote, s.Port, err)
	}
	serveur := &http.Server{
		Handler:           s.routeur(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.ecoute = ecoute
	s.serveur = serveur
	s.mu.Unlock()

	go func() {
		// ErrServerClosed est attendu à l'arrêt : toute autre erreur met fin à
		// la boucle sans planter l'application.
		_ = serveur.Serve(ecoute)
	}()
	return s.URL(), nil
}

// Arreter arrête le serveur (idempotent).
func (s *Serveur) Arreter() {
	s.mu.Lock()
	serveur, ecoute := s.serveur, s.ecoute
	s.serveur, s.ecoute = nil, nil
	s.mu.Unlock()
	if serveur == nil || ecoute == nil {
		return
	}
	_ = serveur.Close()
	_ = ecoute.Close()
}

// routeur assemble le routeur minimal (les 5 routes du portail).
func (s *Serveur) routeur() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.diffuser(w, r)
	})
}

// diffuser aiguille la requête vers la page correspondante.
func (s *Serveur) diffuser(w http.ResponseWriter, r *http.Request) {
	chemin := strings.TrimRight(r.URL.Path, "/")
	if chemin == "" {
		chemin = "/"
	}

	var formulaire url.Values
	if r.Method == http.MethodPost {
		lu, ok := s.lireFormulaire(w, r)
		if !ok {
			return
		}
		formulaire = lu
	}

	type pageHandler func(http.ResponseWriter, *http.Request, url.Values)
	routeurs := map[string]pageHandler{
		"/":         s.pageAccueil,
		"/code":     s.pageCode,
		"/verifier": s.pageVerifier,
		"/demande":  s.pageDemande,
		"/etat":     s.pageEtat,
	}
	gerer, ok := routeurs[chemin]
	if !ok {
		gerer = s.pageIntrouvable
	}
	// Rien ne doit fuir vers le client en cas d'erreur imprévue.
	defer func() {
		if recovered := recover(); recovered != nil {
			s.pageErreur(w, http.StatusInternalServerError, "Erreur interne",
				"Le service a rencontré une erreur : réessayez dans un instant.")
		}
	}()
	gerer(w, r, formulaire)
}

// ─── Utilitaires ──────────────────────────────────────────────────────────

// entetesAppliques pose les en-têtes communs à toutes les réponses.
func entetesAppliques(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// Banner figé : ne révèle ni le langage ni la version de l'application.
	w.Header().Set("Server", "EduSyncPortail")
}

// envoyerTexte écrit une réponse texte.
func envoyerTexte(w http.ResponseWriter, code int, contentType string, corps []byte) {
	entetesAppliques(w)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(corps)))
	w.WriteHeader(code)
	_, _ = w.Write(corps)
}

// htmlPage envoie une page HTML.
func (s *Serveur) htmlPage(w http.ResponseWriter, code int, corps string) {
	envoyerTexte(w, code, "text/html; charset=utf-8", []byte(corps))
}

// jsonPage envoie un document JSON (sans échappement HTML, comme Python).
func (s *Serveur) jsonPage(w http.ResponseWriter, code int, charge any) {
	var buf strings.Builder
	encodeur := json.NewEncoder(&buf)
	encodeur.SetEscapeHTML(false)
	if err := encodeur.Encode(charge); err != nil {
		s.pageErreur(w, http.StatusInternalServerError, "Erreur interne",
			"Le service a rencontré une erreur : réessayez dans un instant.")
		return
	}
	envoyerTexte(w, code, "application/json; charset=utf-8",
		[]byte(strings.TrimRight(buf.String(), "\n")))
}

// mise EnPage assemble la coquille commune à toutes les pages.
func (s *Serveur) miseEnPage(titre, corps string) string {
	site := html.EscapeString(s.Service.Config.Titre)
	return "<!DOCTYPE html>\n" +
		"<html lang=\"fr\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + html.EscapeString(titre) + " — " + site + "</title>\n" +
		"<style>" + cssPortail + "</style>\n</head>\n<body>\n<main>\n" +
		"<h1>" + site + "</h1>\n" + corps + "\n</main>\n" +
		"<footer><a href=\"/\">Retour à l'accueil</a> · " + site + "</footer>\n" +
		"</body>\n</html>\n"
}

// message rend un bandeau d'information, d'avertissement ou d'erreur.
func message(kind, texte string) string {
	return fmt.Sprintf(`<p class="msg %s">%s</p>`, kind, html.EscapeString(texte))
}

// pageErreur rend une page d'erreur générique.
func (s *Serveur) pageErreur(w http.ResponseWriter, code int, titre, texte string, indice ...string) {
	corps := "<h2>" + html.EscapeString(titre) + "</h2>" + message("err", texte)
	if len(indice) > 0 && indice[0] != "" {
		corps += `<p class="hint">` + indice[0] + `</p>`
	}
	s.htmlPage(w, code, s.miseEnPage(titre, corps))
}

// champ lit la première valeur d'un champ de formulaire (chaîne vide si absent).
func champ(valeurs url.Values, nom string) string {
	if valeurs == nil {
		return ""
	}
	return strings.TrimSpace(valeurs.Get(nom))
}

// lireFormulaire lit un corps « application/x-www-form-urlencoded » borné.
func (s *Serveur) lireFormulaire(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	longueur := r.ContentLength
	if longueur < 0 || longueur > CorpsMax {
		code := http.StatusBadRequest
		if longueur > CorpsMax {
			code = http.StatusRequestEntityTooLarge
		}
		s.pageErreur(w, code, "Requête refusée",
			"Le formulaire envoyé est vide ou trop volumineux.")
		return nil, false
	}
	corps, err := io.ReadAll(io.LimitReader(r.Body, CorpsMax+1))
	if err != nil {
		s.pageErreur(w, http.StatusBadRequest, "Requête invalide",
			"Formulaire illisible.")
		return nil, false
	}
	if len(corps) > CorpsMax {
		s.pageErreur(w, http.StatusRequestEntityTooLarge, "Requête refusée",
			"Le formulaire envoyé est vide ou trop volumineux.")
		return nil, false
	}
	typeContenu := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if typeContenu != "" && typeContenu != "application/x-www-form-urlencoded" {
		s.pageErreur(w, http.StatusUnsupportedMediaType, "Format non pris en charge",
			"Formulaire attendu.")
		return nil, false
	}
	valeurs, err := url.ParseQuery(string(corps))
	if err != nil {
		s.pageErreur(w, http.StatusBadRequest, "Requête invalide", "Formulaire illisible.")
		return nil, false
	}
	return valeurs, true
}

// scopeDe détermine la portée demandée (formulaire ou chaîne de requête).
func (s *Serveur) scopeDe(formulaire url.Values, r *http.Request) string {
	valeur := ""
	if formulaire != nil {
		valeur = champ(formulaire, "portee")
	} else {
		valeur = strings.TrimSpace(r.URL.Query().Get("portee"))
	}
	return scopeValide(valeur)
}

// ─── Pages ────────────────────────────────────────────────────────────────

// pageIntrouvable rend la page 404.
func (s *Serveur) pageIntrouvable(w http.ResponseWriter, _ *http.Request, _ url.Values) {
	s.pageErreur(w, http.StatusNotFound, "Page introuvable",
		"Cette adresse n'existe pas sur le portail.",
		`<a href="/">Retour à l'accueil</a>`)
}

// pageAccueil présente les trois opérations.
func (s *Serveur) pageAccueil(w http.ResponseWriter, _ *http.Request, _ url.Values) {
	cfg := s.Service.Config
	corps := `<p class="lead">` + html.EscapeString(
		"Choisissez l'opération souhaitée. Un code de vérification vous sera "+
			"envoyé à votre adresse de messagerie.") + "</p>\n" +
		`<ul class="cards">` + "\n" +
		`<li><a href="/code?portee=` + ScopeReinitialisation + `">` +
		html.EscapeString(LabelsScope[ScopeReinitialisation]) + "</a>" +
		"<p>Vous recevez un code par courriel, puis vous choisissez un nouveau " +
		"mot de passe.</p></li>\n" +
		`<li><a href="/code?portee=` + ScopeConsultation + `">` +
		html.EscapeString(LabelsScope[ScopeConsultation]) + "</a>" +
		"<p>Retrouvez votre identifiant et votre adresse de messagerie, après " +
		"vérification par code.</p></li>\n" +
		`<li><a href="/demande">Demander la création d'un compte</a>` +
		"<p>Votre demande est examinée par l'administrateur : les identifiants " +
		"vous sont transmis une fois le compte créé.</p></li>\n" +
		"</ul>\n" +
		`<p class="hint">Codes valides ` + fmt.Sprintf("%d", cfg.CodeMinutes) +
		" minutes, " + fmt.Sprintf("%d", cfg.TentativesMax) + " tentatives maximum.</p>"
	s.htmlPage(w, http.StatusOK, s.miseEnPage("Accueil", corps))
}

// corpsFormulaireCode rend le formulaire de demande de code.
func (s *Serveur) corpsFormulaireCode(scope, msg, ticket string) string {
	libelle := LabelsScope[scope]
	if libelle == "" {
		libelle = LabelsScope[ScopeReinitialisation]
	}
	parties := []string{"<h2>" + html.EscapeString(libelle) + "</h2>"}
	if msg != "" {
		parties = append(parties, message("info", msg))
	}
	parties = append(parties,
		`<form method="post" action="/code">`+
			`<input type="hidden" name="portee" value="`+html.EscapeString(scope)+`">`+
			`<label for="identifiant">Identifiant ou adresse de messagerie</label>`+
			`<input id="identifiant" name="identifiant" type="text" `+
			`autocomplete="username" autocapitalize="none" spellcheck="false" `+
			`autofocus required maxlength="254">`+
			`<p class="hint">Le code est envoyé à l'adresse de messagerie `+
			`associée à ce compte.</p>`+
			`<button type="submit">Envoyer le code</button>`+
			`</form>`)
	if ticket != "" {
		parties = append(parties,
			`<p class="msg ok">Un code vous attend : saisissez-le pour `+
				`poursuivre.</p>`+
				`<a href="/verifier?ticket=`+html.EscapeString(ticket)+
				`&amp;portee=`+html.EscapeString(scope)+`">`+
				`<button type="button">Saisir le code reçu</button></a>`)
	}
	return strings.Join(parties, "\n")
}

// pageCode affiche (GET) ou traite (POST) la demande d'un code.
func (s *Serveur) pageCode(w http.ResponseWriter, r *http.Request, formulaire url.Values) {
	scope := s.scopeDe(formulaire, r)
	if formulaire == nil {
		s.htmlPage(w, http.StatusOK, s.miseEnPage(LabelsScope[scope],
			s.corpsFormulaireCode(scope, "", "")))
		return
	}
	msgRecu, ticket := s.Service.DemanderCode(champ(formulaire, "identifiant"), scope)
	s.htmlPage(w, http.StatusOK, s.miseEnPage(LabelsScope[scope],
		s.corpsFormulaireCode(scope, msgRecu, ticket)))
}

// corpsFormulaireVerifier rend le formulaire de saisie du code.
func (s *Serveur) corpsFormulaireVerifier(scope, ticket string) string {
	libelle := LabelsScope[scope]
	if libelle == "" {
		libelle = LabelsScope[ScopeReinitialisation]
	}
	parties := []string{
		"<h2>" + html.EscapeString(libelle) + "</h2>",
		`<form method="post" action="/verifier">` +
			`<input type="hidden" name="ticket" value="` + html.EscapeString(ticket) + `">` +
			`<input type="hidden" name="portee" value="` + html.EscapeString(scope) + `">` +
			`<label for="code">Code reçu par courriel</label>` +
			`<input id="code" name="code" type="text" inputmode="numeric" ` +
			`autocomplete="one-time-code" autofocus required maxlength="16" ` +
			`placeholder="000000" style="letter-spacing:.4em">`,
	}
	if scope == ScopeReinitialisation {
		parties = append(parties,
			`<label for="mot_de_passe">Nouveau mot de passe</label>`+
				`<input id="mot_de_passe" name="mot_de_passe" type="password" `+
				`autocomplete="new-password" required>`+
				`<label for="confirmation">Confirmation</label>`+
				`<input id="confirmation" name="confirmation" type="password" `+
				`autocomplete="new-password" required>`+
				`<p class="hint">`+html.EscapeString(s.indicePolitique())+`</p>`)
	}
	parties = append(parties,
		`<button type="submit">Vérifier</button>`,
		`</form>`,
		`<p class="hint"><a href="/code?portee=`+html.EscapeString(scope)+`">`+
			`Demander un nouveau code</a></p>`)
	return strings.Join(parties, "\n")
}

// indicePolitique décrit la politique appliquée aux réinitialisations.
func (s *Serveur) indicePolitique() string {
	return fmt.Sprintf(
		"Politique appliquée : %s. Le code reste valable si le mot de passe "+
			"proposé ne convient pas.", s.Service.Config.Politique)
}

// pageVerifier affiche (GET) ou traite (POST) la vérification d'un code.
func (s *Serveur) pageVerifier(w http.ResponseWriter, r *http.Request, formulaire url.Values) {
	scope := s.scopeDe(formulaire, r)
	source := formulaire
	if source == nil {
		source = r.URL.Query()
	}
	ticket := champ(source, "ticket")
	if ticket == "" {
		s.pageErreur(w, http.StatusBadRequest, "Code absent",
			"Demandez d'abord un code depuis la page d'accueil.",
			`<a href="/">Retour à l'accueil</a>`)
		return
	}

	if formulaire == nil {
		s.htmlPage(w, http.StatusOK, s.miseEnPage(LabelsScope[scope],
			s.corpsFormulaireVerifier(scope, ticket)))
		return
	}

	code := champ(formulaire, "code")
	motDePasse := champ(formulaire, "mot_de_passe")
	confirmation := champ(formulaire, "confirmation")
	if scope == ScopeReinitialisation && motDePasse != confirmation {
		s.htmlPage(w, http.StatusBadRequest, s.miseEnPage(LabelsScope[scope],
			message("err", "Les deux saisies du mot de passe diffèrent.")+
				s.corpsFormulaireVerifier(scope, ticket)))
		return
	}

	resultat := s.Service.VerifierCode(ticket, code, scope, motDePasse)
	var corps string
	switch {
	case resultat.Succes && resultat.Consultation != nil:
		infos := resultat.Consultation
		details := ""
		for _, ligne := range [][2]string{
			{"Nom", infos.Nom},
			{"Identifiant", infos.Identifiant},
			{"Adresse de messagerie", infos.Mail},
		} {
			valeur := ligne[1]
			if valeur == "" {
				valeur = "—"
			}
			details += "<dt>" + html.EscapeString(ligne[0]) + "</dt><dd>" +
				html.EscapeString(valeur) + "</dd>"
		}
		corps = "<h2>Vos identifiants</h2>" +
			message("ok", resultat.Message) +
			"<dl>" + details + "</dl>" +
			`<p class="hint">Conservez ces informations : votre ` +
			"identifiant reste le même pendant toute votre scolarité.</p>"
	case resultat.Succes:
		corps = "<h2>Mot de passe modifié</h2>" +
			message("ok", resultat.Message) +
			`<p class="hint">Utilisez votre nouvel identifiant et votre ` +
			"nouveau mot de passe pour vous connecter.</p>"
	default:
		corps = "<h2>Vérification</h2>" +
			message("err", resultat.Message) +
			`<p class="hint"><a href="/code?portee=` + html.EscapeString(scope) + `">` +
			"Demander un nouveau code</a></p>"
	}
	codeStatut := http.StatusBadRequest
	if resultat.Succes {
		codeStatut = http.StatusOK
	}
	s.htmlPage(w, codeStatut, s.miseEnPage("Vérification", corps))
}

// corpsFormulaireDemande rend le formulaire de demande de création de compte.
func (s *Serveur) corpsFormulaireDemande() string {
	return "<h2>Demande de création de compte</h2>" +
		`<form method="post" action="/demande">` +
		`<label for="profil">Profil</label>` +
		`<select id="profil" name="profil">` +
		`<option value="` + ProfilEleve + `">Élève</option>` +
		`<option value="` + ProfilPersonnel + `">Personnel</option>` +
		`</select>` +
		`<label for="prenom">Prénom</label>` +
		`<input id="prenom" name="prenom" type="text" required maxlength="64">` +
		`<label for="nom">Nom</label>` +
		`<input id="nom" name="nom" type="text" required maxlength="64">` +
		`<label for="mail">Adresse de messagerie (facultative)</label>` +
		`<input id="mail" name="mail" type="email" maxlength="254" ` +
		`autocomplete="email">` +
		`<label for="classe">Classe ou service (facultatif)</label>` +
		`<input id="classe" name="classe" type="text" maxlength="64">` +
		`<label for="motif">Motif (facultatif)</label>` +
		`<textarea id="motif" name="motif" rows="3" maxlength="500"></textarea>` +
		`<button type="submit">Envoyer la demande</button>` +
		`</form>` +
		`<p class="hint">Aucun compte n'est créé à cette étape : ` +
		"l'administrateur valide chaque demande depuis l'application.</p>"
}

// pageDemande affiche (GET) ou traite (POST) une demande de compte.
func (s *Serveur) pageDemande(w http.ResponseWriter, r *http.Request, formulaire url.Values) {
	if formulaire == nil {
		s.htmlPage(w, http.StatusOK,
			s.miseEnPage("Demande de compte", s.corpsFormulaireDemande()))
		return
	}
	champs := map[string]string{}
	for _, cle := range []string{"profil", "prenom", "nom", "mail", "classe", "motif"} {
		champs[cle] = champ(formulaire, cle)
	}
	accepte, msg := s.Service.SoumettreDemande(champs)
	genre := "ok"
	if !accepte {
		genre = "err"
	}
	corps := message(genre, msg)
	if accepte {
		corps += `<p class="hint">Vous pouvez fermer cette page.</p>`
	} else {
		corps += s.corpsFormulaireDemande()
	}
	statut := http.StatusOK
	if !accepte {
		statut = http.StatusBadRequest
	}
	s.htmlPage(w, statut, s.miseEnPage("Demande de compte", corps))
}

// pageEtat publie l'état du service (diagnostic JSON).
func (s *Serveur) pageEtat(w http.ResponseWriter, _ *http.Request, _ url.Values) {
	cfg := s.Service.Config
	attentes := -1
	if demandes, err := s.Service.ListerDemandes(StatutEnAttente); err == nil {
		attentes = len(demandes)
	}
	s.jsonPage(w, http.StatusOK, map[string]any{
		"service":             "portail",
		"titre":               cfg.Titre,
		"actif":               cfg.Actif,
		"base_url":            cfg.BaseURL(),
		"code_minutes":        cfg.CodeMinutes,
		"tentatives_max":      cfg.TentativesMax,
		"demandes_en_attente": attentes,
	})
}
