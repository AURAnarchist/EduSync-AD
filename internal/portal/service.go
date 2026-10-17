package portal

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// CompteCreation décrit le compte à créer lors de la validation d'une demande.
type CompteCreation struct {
	// OU est le DN de l'OU d'accueil du compte.
	OU string
	// Nom est le nom complet (« Prénom Nom ») servant de cn et de displayName.
	Nom string
	// Famille alimente sn (nom de famille).
	Famille string
	// Prenom alimente givenName.
	Prenom string
	// Identifiant est le sAMAccountName retenu (unique).
	Identifiant string
	// UPN est le userPrincipalName.
	UPN string
	// Mail est l'adresse de messagerie (peut être vide).
	Mail string
}

// Annuaire est la vue minimale du portail sur l'annuaire Active Directory.
// L'interface permet de tester toute la logique métier sans contrôleur de
// domaine ; l'adaptateur concret est fourni dans adapter.go.
type Annuaire interface {
	// TrouverUtilisateur résout un identifiant ou une adresse de messagerie.
	// Retourne (nil, nil) si aucun compte ne correspond.
	TrouverUtilisateur(requete string) (*Utilisateur, error)
	// DefinirMotDePasse écrit un mot de passe (le garde-fou RBAC s'applique).
	DefinirMotDePasse(dn, motDePasse string) error
	// IdentifiantsExistants retourne les sAMAccountName déjà pris (minuscules).
	IdentifiantsExistants() (map[string]bool, error)
	// CreerCompte crée un compte et demande le changement du mot de passe à la
	// première connexion.
	CreerCompte(compte CompteCreation, motDePasse string) error
}

// InfoConsultation accompagne une consultation d'identifiant réussie.
type InfoConsultation struct {
	Identifiant string
	Mail        string
	Nom         string
}

// ResultatVerification décrit l'issue de VerifierCode.
type ResultatVerification struct {
	// Succes indique que l'action demandée a abouti.
	Succes bool
	// Message est le texte à afficher à l'usager (français).
	Message string
	// Consultation est renseignée pour une consultation d'identifiant réussie.
	Consultation *InfoConsultation
	// ErreursMDP liste les exigences non satisfaites par le mot de passe
	// proposé (le code reste alors valable).
	ErreursMDP []string
}

// Service porte la logique métier du portail, appelée par le serveur HTTP et
// par l'interface d'administration.
type Service struct {
	Config        Config
	AppConfig     config.AppConfig
	Annuaire      Annuaire
	Store         *Store
	SMTP          *mailer.MailConfig
	Envoyeur      mailer.Envoyeur
	CheminJournal string
	Domaine       string
	SessionID     string

	// Maintenant permet d'injecter une horloge dans les tests (nil = time.Now).
	Maintenant func() time.Time
	// EnvoiDirect court-circuite l'envoi SMTP (tests, envoi délégué).
	EnvoiDirect func(dest, sujet, corps string) error

	mu sync.Mutex
}

// NouveauService construit le service avec un magasin existant.
func NouveauService(cfg Config, appConfig config.AppConfig, annuaire Annuaire,
	store *Store, domaine, cheminJournal string) *Service {
	return &Service{
		Config:        cfg,
		AppConfig:     appConfig,
		Annuaire:      annuaire,
		Store:         store,
		Domaine:       domaine,
		CheminJournal: cheminJournal,
		SessionID:     audit.NewSessionID(),
	}
}

// maintenant retourne l'instant courant (horloge injectable).
func (s *Service) maintenant() time.Time {
	if s.Maintenant != nil {
		return s.Maintenant()
	}
	return time.Now()
}

// politique retourne la politique de mot de passe appliquée aux
// réinitialisations.
func (s *Service) politique() models.PasswordPolicy {
	if s.Config.Politique == ProfilPersonnel {
		return s.AppConfig.PolitiqueMDPPersonnel
	}
	return s.AppConfig.PolitiqueMDPEleve
}

// journaliser ajoute une entrée au journal d'actions. Le portail écrit depuis
// les goroutines du serveur HTTP : une connexion SQLite est ouverte puis
// refermée à chaque écriture, comme côté Python.
func (s *Service) journaliser(action, compte, resultat, detail string) {
	if strings.TrimSpace(s.CheminJournal) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	journal, err := audit.Open(s.CheminJournal)
	if err != nil {
		return
	}
	defer journal.Close()
	journal.SetContext("portail", s.Domaine)
	_ = journal.Record(models.ActionLogEntry{
		Timestamp:  horodatageISO(s.maintenant()),
		ActionType: action,
		Compte:     compte,
		Resultat:   resultat,
		SessionID:  s.SessionID,
		Detail:     detail,
	})
}

// envoyer transmet un courriel (SMTP réel ou envoyeur injecté).
func (s *Service) envoyer(dest, sujet, corps string) error {
	if s.EnvoiDirect != nil {
		return s.EnvoiDirect(dest, sujet, corps)
	}
	if s.SMTP == nil {
		return &mailer.ErreurMail{Message: "Configuration SMTP renseignée requise."}
	}
	return mailer.EnvoyerMail(s.SMTP, dest, sujet, corps, s.Config.Titre, s.Envoyeur)
}

// masquerMail ne conserve que le domaine d'une adresse (journal d'actions).
func masquerMail(adresse string) string {
	_, domaine, trouve := strings.Cut(adresse, "@")
	if !trouve || domaine == "" {
		return "-"
	}
	return "…@" + domaine
}

// scopeValide normalise une portée inconnue vers la réinitialisation.
func scopeValide(scope string) string {
	for _, candidat := range Scopes {
		if scope == candidat {
			return scope
		}
	}
	return ScopeReinitialisation
}

// ─── Envoi des codes ──────────────────────────────────────────────────────

// DemanderCode émet un code pour `requete`. Retourne (message, ticket).
//
// Le ticket est vide si aucun code n'a pu être émis — le message affiché reste
// identique dans tous les cas (anti-énumération), sauf pour la limitation de
// débit.
func (s *Service) DemanderCode(requete, scope string) (string, string) {
	scope = scopeValide(scope)
	cible := strings.ToLower(strings.TrimSpace(requete))
	if cible == "" {
		return "Saisissez votre identifiant ou votre adresse de messagerie.", ""
	}

	if recents, err := s.Store.CompterCodesRecents(cible, s.maintenant()); err == nil &&
		recents >= s.Config.EnvoisMax {
		s.journaliser("portail_code", "-", "echec", "limitation de débit")
		return "Trop de demandes successives : réessayez dans quelques minutes.", ""
	}

	utilisateur, err := s.Annuaire.TrouverUtilisateur(requete)
	if err != nil {
		utilisateur = nil
	}
	if utilisateur == nil || utilisateur.Mail == "" {
		detail := "aucune adresse de messagerie"
		compte := "-"
		if utilisateur == nil {
			detail = "compte introuvable"
		} else {
			compte = utilisateur.SAM
		}
		s.journaliser("portail_code", compte, "echec", detail)
		return MessageCodeGenerique, ""
	}

	code, err := chiffresAleatoires(LongueurCode)
	if err != nil {
		s.journaliser("portail_code", utilisateur.SAM, "echec",
			"génération du code impossible : "+err.Error())
		return "L'envoi du code a échoué : contactez votre administrateur.", ""
	}
	ticket, err := s.Store.CreerCode(scope, cible, *utilisateur, code,
		s.Config.CodeMinutes, s.maintenant())
	if err != nil {
		s.journaliser("portail_code", utilisateur.SAM, "echec",
			"enregistrement du code impossible : "+err.Error())
		return "L'envoi du code a échoué : contactez votre administrateur.", ""
	}

	sujet := s.Config.Titre + " — code de vérification"
	corps := "Bonjour,\n\n" +
		"Votre code de vérification est : " + code + "\n\n" +
		fmt.Sprintf("Ce code expire dans %d minutes et ne peut ", s.Config.CodeMinutes) +
		"être utilisé qu'une seule fois.\n" +
		"Si vous n'êtes pas à l'origine de cette demande, ignorez ce courriel " +
		"et prévenez votre administrateur.\n\n" +
		"— " + s.Config.Titre
	if err := s.envoyer(utilisateur.Mail, sujet, corps); err != nil {
		s.journaliser("portail_code", utilisateur.SAM, "echec",
			"envoi impossible : "+err.Error())
		return "L'envoi du code a échoué : contactez votre administrateur.", ""
	}

	s.journaliser("portail_code", utilisateur.SAM, "succes",
		fmt.Sprintf("portee=%s, dest=%s", scope, masquerMail(utilisateur.Mail)))
	return MessageCodeGenerique, ticket
}

// ─── Vérification ─────────────────────────────────────────────────────────

// VerifierCode vérifie le code puis réalise l'action demandée
// (réinitialisation ou consultation).
func (s *Service) VerifierCode(ticket, code, scope, nouveauMotDePasse string) ResultatVerification {
	scope = scopeValide(scope)
	valide, raison, utilisateur, err := s.Store.VerifierCode(ticket, code,
		s.Config.TentativesMax, s.maintenant())
	if err != nil {
		return ResultatVerification{
			Message: "Le service a rencontré une erreur : réessayez dans un instant.",
		}
	}
	if !valide || utilisateur == nil {
		if raison == raisonExpire || raison == raisonTropDeTentatives {
			// Le ticket est à bout : il faut en redemander un.
			_, _ = s.Store.ConsommerCode(ticket)
		}
		message := "Code incorrect ou expiré : demandez un nouveau code."
		switch raison {
		case raisonTropDeTentatives:
			message = "Trop de tentatives : demandez un nouveau code."
		case raisonConsomme:
			message = "Ce code a déjà été utilisé : demandez un nouveau code."
		}
		if raison != raisonInconnu {
			compte := "-"
			if utilisateur != nil {
				compte = utilisateur.SAM
			}
			s.journaliser("portail_code", compte, "echec", "verification="+raison)
		}
		return ResultatVerification{Message: message}
	}

	if scope == ScopeConsultation {
		if consomme, _ := s.Store.ConsommerCode(ticket); !consomme {
			return ResultatVerification{
				Message: "Ce code a déjà été utilisé : demandez un nouveau code."}
		}
		s.journaliser("portail_consultation", utilisateur.SAM, "succes",
			masquerMail(utilisateur.Mail))
		return ResultatVerification{
			Succes:  true,
			Message: "Identifiants consultés.",
			Consultation: &InfoConsultation{
				Identifiant: utilisateur.SAM,
				Mail:        utilisateur.Mail,
				Nom:         utilisateur.NomAffichage(),
			},
		}
	}

	// -- Réinitialisation -----------------------------------------------------
	if utilisateur.Desactive {
		_, _ = s.Store.ConsommerCode(ticket)
		s.journaliser("portail_reinitialisation", utilisateur.SAM, "echec", "compte désactivé")
		return ResultatVerification{
			Message: "Ce compte est désactivé : contactez votre administrateur."}
	}

	erreurs := config.ValiderMotDePasse(s.politique(), nouveauMotDePasse,
		utilisateur.SAM, utilisateur.CN, utilisateur.NomAffichage())
	if len(erreurs) > 0 {
		// Le code reste valable : l'usager corrige son mot de passe.
		return ResultatVerification{
			Message:    strings.Join(erreurs, " "),
			ErreursMDP: erreurs,
		}
	}

	if consomme, _ := s.Store.ConsommerCode(ticket); !consomme {
		return ResultatVerification{
			Message: "Ce code a déjà été utilisé : demandez un nouveau code."}
	}
	if err := s.Annuaire.DefinirMotDePasse(utilisateur.DN, nouveauMotDePasse); err != nil {
		s.journaliser("portail_reinitialisation", utilisateur.SAM, "echec", err.Error())
		return ResultatVerification{
			Message: "Impossible de modifier le mot de passe pour le moment : " +
				"contactez votre administrateur.",
		}
	}
	s.journaliser("portail_reinitialisation", utilisateur.SAM, "succes", "auto-service")
	return ResultatVerification{Succes: true, Message: "Votre mot de passe a été modifié."}
}

// ─── Demandes de création de compte ───────────────────────────────────────

// estProfil indique si le profil est accepté.
func estProfil(valeur string) bool {
	for _, profil := range Profils {
		if valeur == profil {
			return true
		}
	}
	return false
}

// SoumettreDemande enregistre une pré-inscription.
func (s *Service) SoumettreDemande(champs map[string]string) (bool, string) {
	profil := strings.TrimSpace(champs["profil"])
	prenom := strings.TrimSpace(champs["prenom"])
	nom := strings.TrimSpace(champs["nom"])
	mail := strings.TrimSpace(champs["mail"])
	classe := strings.TrimSpace(champs["classe"])
	motif := strings.TrimSpace(champs["motif"])

	if !estProfil(profil) {
		return false, "Choisissez un profil (élève ou personnel)."
	}
	if prenom == "" || nom == "" {
		return false, "Le prénom et le nom sont obligatoires."
	}
	if mail != "" && !strings.Contains(mail, "@") {
		return false, "L'adresse de messagerie saisie est invalide."
	}

	if _, err := s.Store.CreerDemande(profil, prenom, nom, mail, classe, motif,
		s.maintenant()); err != nil {
		return false, "L'enregistrement de la demande a échoué : réessayez dans un instant."
	}
	s.journaliser("portail_demande_compte", strings.TrimSpace(prenom+" "+nom),
		"succes", "profil="+profil)
	return true, "Votre demande a été transmise à l'administrateur : vous recevrez " +
		"les identifiants une fois le compte créé."
}

// ListerDemandes retourne les demandes d'un statut donné (vide = toutes).
func (s *Service) ListerDemandes(statut string) ([]Demande, error) {
	return s.Store.ListerDemandes(statut)
}

// ApprouverDemande crée le compte demandé et retourne les identifiants générés.
// Retourne une *ErreurPortail avec un message affichable en cas d'échec.
func (s *Service) ApprouverDemande(demandeID string) (map[string]string, error) {
	demande, err := s.Store.DemandeParID(demandeID)
	if err != nil {
		return nil, err
	}
	if demande == nil {
		return nil, nouvelleErreurPortail("Cette demande n'existe plus.")
	}
	if demande.Statut != StatutEnAttente {
		return nil, nouvelleErreurPortail("Cette demande a déjà été traitée.")
	}
	if strings.TrimSpace(s.Config.OUAccueil) == "" {
		return nil, nouvelleErreurPortail(
			"Renseignez l'OU d'accueil du portail avant de valider une demande.")
	}

	typeCompte := models.AccountEleve
	if demande.Profil == ProfilPersonnel {
		typeCompte = models.AccountPersonnel
	}
	// Comme côté Python, le format configuré est utilisé tel quel.
	base := s.AppConfig.GenerateIdentifiant(demande.Prenom, demande.Nom, typeCompte)
	sam, err := s.identifiantUnique(base)
	if err != nil {
		return nil, err
	}

	domaineMail := s.AppConfig.DomaineMail
	if domaineMail == "" {
		domaineMail = s.Domaine
	}
	mail := demande.Mail
	if mail == "" {
		local := config.RenderTemplate(s.AppConfig.FormatMail, demande.Prenom,
			demande.Nom, s.maintenant().Year())
		mail = local + "@" + domaineMail
	}

	motDePasse, err := s.AppConfig.GenererMotDePasse(s.politiqueProfil(demande.Profil),
		demande.Prenom, demande.Nom)
	if err != nil {
		return nil, nouvelleErreurPortail(
			"Génération du mot de passe impossible : %s", err)
	}

	cn := strings.TrimSpace(demande.Prenom + " " + demande.Nom)
	upn := sam
	if s.Domaine != "" {
		upn = sam + "@" + s.Domaine
	}
	if err := s.Annuaire.CreerCompte(CompteCreation{
		OU:          s.Config.OUAccueil,
		Nom:         cn,
		Famille:     demande.Nom,
		Prenom:      demande.Prenom,
		Identifiant: sam,
		UPN:         upn,
		Mail:        mail,
	}, motDePasse); err != nil {
		return nil, nouvelleErreurPortail("Création du compte impossible : %s", err)
	}
	dn := fmt.Sprintf("cn=%s,%s", ad.EchapperRDN(cn), s.Config.OUAccueil)

	if _, err := s.Store.DefinirStatutDemande(demandeID, StatutApprouvee, "interface",
		"", s.maintenant()); err != nil {
		return nil, err
	}
	infos := map[string]string{
		"dn":           dn,
		"identifiant":  sam,
		"mot_de_passe": motDePasse,
		"mail":         mail,
		"nom":          cn,
	}
	s.journaliser("portail_demande_compte", cn, "succes",
		"validation, identifiant="+sam)
	if s.Config.NotificationMail && mail != "" {
		s.notifierIdentifiants(infos)
	}
	return infos, nil
}

// politiqueProfil retourne la politique de mot de passe d'un profil de
// demande (élève ou personnel).
func (s *Service) politiqueProfil(profil string) models.PasswordPolicy {
	if profil == ProfilPersonnel {
		return s.AppConfig.PolitiqueMDPPersonnel
	}
	return s.AppConfig.PolitiqueMDPEleve
}

// notifierIdentifiants transmet les identifiants générés (option
// `notification_mail`). Un échec d'envoi ne remet jamais en cause la création
// du compte : il est seulement journalisé, l'administrateur pouvant
// communiquer les identifiants autrement.
func (s *Service) notifierIdentifiants(infos map[string]string) {
	corps := "Bonjour,\n\n" +
		"Votre compte a été créé.\n\n" +
		"Identifiant : " + infos["identifiant"] + "\n" +
		"Mot de passe : " + infos["mot_de_passe"] + "\n" +
		"Adresse de messagerie : " + infos["mail"] + "\n\n" +
		"Ce mot de passe devra être changé à la première connexion.\n\n" +
		"— " + s.Config.Titre
	if err := s.envoyer(infos["mail"], s.Config.Titre+" — vos identifiants", corps); err != nil {
		s.journaliser("portail_demande_compte", infos["identifiant"], "echec",
			"notification impossible : "+err.Error())
		return
	}
	s.journaliser("portail_demande_compte", infos["identifiant"], "succes",
		"notification="+masquerMail(infos["mail"]))
}

// RefuserDemande marque une demande comme refusée.
func (s *Service) RefuserDemande(demandeID, note string) error {
	demande, err := s.Store.DemandeParID(demandeID)
	if err != nil {
		return err
	}
	if demande == nil {
		return nouvelleErreurPortail("Cette demande n'existe plus.")
	}
	if demande.Statut != StatutEnAttente {
		return nouvelleErreurPortail("Cette demande a déjà été traitée.")
	}
	if _, err := s.Store.DefinirStatutDemande(demandeID, StatutRefusee, "interface",
		note, s.maintenant()); err != nil {
		return err
	}
	detail := "refus"
	if note != "" {
		detail = "refus, note=" + note
	}
	s.journaliser("portail_demande_compte", demande.NomComplet(), "succes", detail)
	return nil
}

// identifiantUnique suffixe un identifiant jusqu'à ce qu'il soit libre.
func (s *Service) identifiantUnique(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "compte"
	}
	existants, err := s.Annuaire.IdentifiantsExistants()
	if err != nil {
		return "", err
	}
	candidat := base
	suffixe := 1
	for existants[strings.ToLower(candidat)] {
		suffixe++
		if suffixe > 100 {
			return "", nouvelleErreurPortail(
				"Impossible de générer un identifiant unique pour ce nom.")
		}
		candidat = base + fmt.Sprintf("%d", suffixe)
	}
	return candidat, nil
}
