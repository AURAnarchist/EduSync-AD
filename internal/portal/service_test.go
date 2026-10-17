package portal

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// annuaireFactice remplace Active Directory dans les tests : comptes en
// mémoire, aucun réseau.
type annuaireFactice struct {
	utilisateurs map[string]*Utilisateur
	existants    map[string]bool
	mdpDefinis   map[string]string
	comptes      []CompteCreation
	mdpComptes   []string
	errCreation  error
}

func nouvelAnnuaireFactice() *annuaireFactice {
	utilisateur := utilisateurTest()
	return &annuaireFactice{
		utilisateurs: map[string]*Utilisateur{
			"l.martin":              &utilisateur,
			"lea.martin@college.fr": &utilisateur,
			"j.durand":              {DN: "CN=Jean Durand,OU=Personnels,DC=college,DC=fr", SAM: "j.durand", CN: "Jean Durand", Mail: "jean.durand@college.fr"},
			"p.desactive":           {DN: "CN=Paul Désactivé,OU=Eleves,DC=college,DC=fr", SAM: "p.desactive", CN: "Paul Désactivé", Mail: "paul@college.fr", Desactive: true},
			"q.sansmail":            {DN: "CN=Quentin Sans Mail,OU=Eleves,DC=college,DC=fr", SAM: "q.sansmail", CN: "Quentin Sans Mail"},
		},
		existants:  map[string]bool{"l.martin": true, "j.durand": true},
		mdpDefinis: map[string]string{},
	}
}

func (a *annuaireFactice) TrouverUtilisateur(requete string) (*Utilisateur, error) {
	if utilisateur, ok := a.utilisateurs[strings.TrimSpace(requete)]; ok {
		copie := *utilisateur
		return &copie, nil
	}
	return nil, nil
}

func (a *annuaireFactice) DefinirMotDePasse(dn, motDePasse string) error {
	a.mdpDefinis[dn] = motDePasse
	return nil
}

func (a *annuaireFactice) IdentifiantsExistants() (map[string]bool, error) {
	return a.existants, nil
}

func (a *annuaireFactice) CreerCompte(compte CompteCreation, motDePasse string) error {
	if a.errCreation != nil {
		return a.errCreation
	}
	a.comptes = append(a.comptes, compte)
	a.mdpComptes = append(a.mdpComptes, motDePasse)
	a.existants[strings.ToLower(compte.Identifiant)] = true
	return nil
}

// bouchonTest regroupe le service et ses dépendances observables.
type bouchonTest struct {
	service  *Service
	annuaire *annuaireFactice
	store    *Store
	moment   time.Time
	envois   []*envoi
	erreur   error
	journal  string
}

type envoi struct {
	dest, sujet, corps string
}

func nouveauBouchonTest(t *testing.T) *bouchonTest {
	t.Helper()
	store := nouveauStoreTest(t)
	annuaire := nouvelAnnuaireFactice()

	cfg := ConfigDefaut()
	cfg.OUAccueil = "OU=Eleves,DC=college,DC=fr"
	appConfig := config.DefaultAppConfig()
	appConfig.DomaineMail = "college.fr"
	appConfig.FormatMail = "{P}.{N}"
	appConfig.PolitiqueMDPEleve = models.PasswordPolicy{
		Longueur: 8, Majuscules: true, Chiffres: true,
	}

	bouchon := &bouchonTest{
		annuaire: annuaire,
		store:    store,
		moment:   time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		journal:  filepath.Join(t.TempDir(), "journal.db"),
	}
	bouchon.service = NouveauService(cfg, appConfig, annuaire, store,
		"college.fr", bouchon.journal)
	bouchon.service.Maintenant = func() time.Time { return bouchon.moment }
	bouchon.service.EnvoiDirect = func(dest, sujet, corps string) error {
		if bouchon.erreur != nil {
			return bouchon.erreur
		}
		bouchon.envois = append(bouchon.envois, &envoi{dest, sujet, corps})
		return nil
	}
	return bouchon
}

// dernierCode retourne le code contenu dans le dernier courriel envoyé.
func (b *bouchonTest) dernierCode(t *testing.T) string {
	t.Helper()
	if len(b.envois) == 0 {
		t.Fatal("aucun courriel envoyé")
	}
	corps := b.envois[len(b.envois)-1].corps
	const marqueur = "Votre code de vérification est : "
	index := strings.Index(corps, marqueur)
	if index < 0 {
		t.Fatalf("code absent du courriel : %s", corps)
	}
	reste := corps[index+len(marqueur):]
	ligne, _, _ := strings.Cut(reste, "\n")
	return strings.TrimSpace(ligne)
}

// entreesJournal relit le journal d'actions du portail.
func (b *bouchonTest) entreesJournal(t *testing.T) []audit.Entry {
	t.Helper()
	journal, err := audit.Open(b.journal)
	if err != nil {
		t.Fatalf("ouverture du journal: %v", err)
	}
	defer journal.Close()
	entrees, err := journal.List(audit.Filter{})
	if err != nil {
		t.Fatalf("lecture du journal: %v", err)
	}
	return entrees
}

// TestDemanderCodeCompteInconnu vérifie qu'aucun code n'est émis et que la
// réponse est la même que pour un compte existant (anti-énumération).
func TestDemanderCodeCompteInconnu(t *testing.T) {
	b := nouveauBouchonTest(t)

	message, ticket := b.service.DemanderCode("inconnu", ScopeReinitialisation)
	if ticket != "" {
		t.Fatalf("ticket inattendu : %q", ticket)
	}
	if message != MessageCodeGenerique {
		t.Fatalf("message = %q", message)
	}
	if len(b.envois) != 0 {
		t.Fatalf("courriel envoyé à tort : %+v", b.envois)
	}

	// Compte sans adresse de messagerie : même réponse, aucun envoi.
	message, ticket = b.service.DemanderCode("q.sansmail", ScopeReinitialisation)
	if ticket != "" || message != MessageCodeGenerique {
		t.Fatalf("compte sans mail : %q %q", message, ticket)
	}

	// Requête vide : message spécifique.
	if message, ticket := b.service.DemanderCode("   ", ScopeReinitialisation); ticket != "" ||
		!strings.Contains(message, "Saisissez votre identifiant") {
		t.Fatalf("requête vide : %q %q", message, ticket)
	}

	journal := b.entreesJournal(t)
	if len(journal) != 2 {
		t.Fatalf("journal = %d entrées", len(journal))
	}
	for _, entree := range journal {
		if entree.Utilisateur != "portail" || entree.Resultat != "echec" {
			t.Fatalf("entrée = %+v", entree)
		}
	}
}

// TestDemanderCodePuisVerificationConsultation couvre le parcours nominal de
// consultation d'identifiant.
func TestDemanderCodePuisVerificationConsultation(t *testing.T) {
	b := nouveauBouchonTest(t)

	message, ticket := b.service.DemanderCode("l.martin", ScopeConsultation)
	if message != MessageCodeGenerique || ticket == "" {
		t.Fatalf("message = %q, ticket = %q", message, ticket)
	}
	if len(b.envois) != 1 || b.envois[0].dest != "lea.martin@college.fr" {
		t.Fatalf("envois = %+v", b.envois)
	}
	if !strings.Contains(b.envois[0].sujet, "code de vérification") {
		t.Fatalf("sujet = %q", b.envois[0].sujet)
	}
	code := b.dernierCode(t)

	// Mauvais code.
	resultat := b.service.VerifierCode(ticket, "000000", ScopeConsultation, "")
	if resultat.Succes || !strings.Contains(resultat.Message, "Code incorrect") {
		t.Fatalf("mauvais code : %+v", resultat)
	}

	// Bon code.
	resultat = b.service.VerifierCode(ticket, code, ScopeConsultation, "")
	if !resultat.Succes || resultat.Consultation == nil {
		t.Fatalf("bon code : %+v", resultat)
	}
	if resultat.Consultation.Identifiant != "l.martin" ||
		resultat.Consultation.Mail != "lea.martin@college.fr" ||
		resultat.Consultation.Nom != "Léa Martin" {
		t.Fatalf("consultation = %+v", resultat.Consultation)
	}

	// Le code ne sert qu'une fois.
	resultat = b.service.VerifierCode(ticket, code, ScopeConsultation, "")
	if resultat.Succes || !strings.Contains(resultat.Message, "déjà été utilisé") {
		t.Fatalf("rejeu : %+v", resultat)
	}

	// La recherche par adresse de messagerie fonctionne aussi.
	if _, ticket2 := b.service.DemanderCode("lea.martin@college.fr",
		ScopeConsultation); ticket2 == "" {
		t.Fatal("aucun ticket pour la recherche par adresse")
	}
}

// TestVerifierCodeReinitialisation vérifie la réinitialisation de mot de passe
// et la validation de la politique.
func TestVerifierCodeReinitialisation(t *testing.T) {
	b := nouveauBouchonTest(t)

	_, ticket := b.service.DemanderCode("l.martin", ScopeReinitialisation)
	code := b.dernierCode(t)

	// Mot de passe trop faible : refusé, mais le code reste utilisable.
	resultat := b.service.VerifierCode(ticket, code, ScopeReinitialisation, "court")
	if resultat.Succes || len(resultat.ErreursMDP) == 0 {
		t.Fatalf("mot de passe faible : %+v", resultat)
	}
	if !strings.Contains(resultat.Message, "au moins 8 caractères") {
		t.Fatalf("message = %q", resultat.Message)
	}
	if len(b.annuaire.mdpDefinis) != 0 {
		t.Fatal("aucun mot de passe ne doit être écrit pour un refus")
	}

	// Mot de passe valide.
	resultat = b.service.VerifierCode(ticket, code, ScopeReinitialisation, "Voyage2026!")
	if !resultat.Succes {
		t.Fatalf("réinitialisation : %+v", resultat)
	}
	dn := utilisateurTest().DN
	if b.annuaire.mdpDefinis[dn] != "Voyage2026!" {
		t.Fatalf("mot de passe défini = %q", b.annuaire.mdpDefinis[dn])
	}

	journal := b.entreesJournal(t)
	var succes bool
	for _, entree := range journal {
		if entree.ActionType == "portail_reinitialisation" && entree.Resultat == "succes" &&
			entree.Compte == "l.martin" {
			succes = true
		}
	}
	if !succes {
		t.Fatalf("journal = %+v", journal)
	}
}

// TestVerifierCodeCompteDesactive vérifie qu'un compte désactivé ne peut pas
// réinitialiser son mot de passe.
func TestVerifierCodeCompteDesactive(t *testing.T) {
	b := nouveauBouchonTest(t)
	_, ticket := b.service.DemanderCode("p.desactive", ScopeReinitialisation)
	code := b.dernierCode(t)

	resultat := b.service.VerifierCode(ticket, code, ScopeReinitialisation, "Voyage2026!")
	if resultat.Succes || !strings.Contains(resultat.Message, "désactivé") {
		t.Fatalf("compte désactivé : %+v", resultat)
	}
	if len(b.annuaire.mdpDefinis) != 0 {
		t.Fatal("aucun mot de passe ne doit être écrit")
	}
	// Le ticket a été consommé.
	if resultat = b.service.VerifierCode(ticket, code, ScopeReinitialisation,
		"Voyage2026!"); resultat.Succes {
		t.Fatalf("le ticket devait être épuisé : %+v", resultat)
	}
}

// TestDemanderCodeLimitation vérifie le plafond d'envois par identifiant.
func TestDemanderCodeLimitation(t *testing.T) {
	b := nouveauBouchonTest(t)
	for i := 0; i < b.service.Config.EnvoisMax; i++ {
		b.moment = b.moment.Add(time.Second)
		if _, ticket := b.service.DemanderCode("l.martin", ScopeConsultation); ticket == "" {
			t.Fatalf("envoi %d refusé", i+1)
		}
	}
	message, ticket := b.service.DemanderCode("l.martin", ScopeConsultation)
	if ticket != "" || !strings.Contains(message, "Trop de demandes") {
		t.Fatalf("limitation : %q %q", message, ticket)
	}
}

// TestDemanderCodeEchecEnvoi vérifie qu'un échec SMTP ne laisse pas de code
// utilisable.
func TestDemanderCodeEchecEnvoi(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.erreur = errors.New("smtp indisponible")

	message, ticket := b.service.DemanderCode("l.martin", ScopeReinitialisation)
	if ticket != "" || !strings.Contains(message, "L'envoi du code a échoué") {
		t.Fatalf("échec d'envoi : %q %q", message, ticket)
	}
}

// TestSoumettreDemande couvre les contrôles de saisie et l'enregistrement.
func TestSoumettreDemande(t *testing.T) {
	b := nouveauBouchonTest(t)

	cas := []struct {
		champs  map[string]string
		attendu string
	}{
		{map[string]string{"profil": "autre", "prenom": "Léa", "nom": "Martin"},
			"Choisissez un profil"},
		{map[string]string{"profil": ProfilEleve, "prenom": "", "nom": "Martin"},
			"prénom et le nom sont obligatoires"},
		{map[string]string{"profil": ProfilEleve, "prenom": "Léa", "nom": "Martin",
			"mail": "pas-une-adresse"}, "invalide"},
	}
	for _, c := range cas {
		accepte, msg := b.service.SoumettreDemande(c.champs)
		if accepte || !strings.Contains(msg, c.attendu) {
			t.Errorf("champs %v : %v %q", c.champs, accepte, msg)
		}
	}

	accepte, msg := b.service.SoumettreDemande(map[string]string{
		"profil": ProfilEleve, "prenom": " Léa ", "nom": " Martin ",
		"mail": "lea.martin@college.fr", "classe": "6e A", "motif": "arrivée",
	})
	if !accepte || !strings.Contains(msg, "transmise à l'administrateur") {
		t.Fatalf("demande : %v %q", accepte, msg)
	}
	demandes, err := b.service.ListerDemandes(StatutEnAttente)
	if err != nil || len(demandes) != 1 {
		t.Fatalf("demandes = %+v (%v)", demandes, err)
	}
	if demandes[0].Prenom != "Léa" || demandes[0].Nom != "Martin" {
		t.Fatalf("demande = %+v", demandes[0])
	}
}

// TestApprouverDemande couvre la création du compte, l'unicité de
// l'identifiant et la notification.
func TestApprouverDemande(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.service.AppConfig.IdentifiantFormatEleve = "prenom.nom"

	demande, err := b.store.CreerDemande(ProfilEleve, "Léa", "Martin",
		"", "6e A", "", b.moment)
	if err != nil {
		t.Fatal(err)
	}

	infos, err := b.service.ApprouverDemande(demande.ID)
	if err != nil {
		t.Fatalf("ApprouverDemande: %v", err)
	}
	if len(b.annuaire.comptes) != 1 {
		t.Fatalf("comptes créés = %+v", b.annuaire.comptes)
	}
	compte := b.annuaire.comptes[0]
	if compte.OU != "OU=Eleves,DC=college,DC=fr" || compte.Nom != "Léa Martin" ||
		compte.Famille != "Martin" || compte.Prenom != "Léa" {
		t.Fatalf("compte = %+v", compte)
	}
	if compte.Identifiant != "lea.martin" {
		t.Fatalf("identifiant = %q", compte.Identifiant)
	}
	if compte.UPN != "lea.martin@college.fr" {
		t.Fatalf("UPN = %q", compte.UPN)
	}
	// Adresse reconstruite depuis format_mail et le domaine.
	if compte.Mail != "lea.martin@college.fr" {
		t.Fatalf("mail = %q", compte.Mail)
	}
	if infos["identifiant"] != "lea.martin" || infos["mot_de_passe"] == "" ||
		infos["dn"] != "cn=Léa Martin,OU=Eleves,DC=college,DC=fr" {
		t.Fatalf("infos = %+v", infos)
	}
	if len(b.envois) != 1 || b.envois[0].dest != "lea.martin@college.fr" {
		t.Fatalf("notification = %+v", b.envois)
	}
	if !strings.Contains(b.envois[0].corps, infos["mot_de_passe"]) {
		t.Fatal("le courriel doit contenir le mot de passe généré")
	}

	// La demande est marquée approuvée.
	relue, _ := b.store.DemandeParID(demande.ID)
	if relue.Statut != StatutApprouvee || relue.TraitePar != "interface" {
		t.Fatalf("demande = %+v", relue)
	}

	// Deuxième validation : refusée.
	if _, err := b.service.ApprouverDemande(demande.ID); err == nil ||
		!strings.Contains(err.Error(), "déjà été traitée") {
		t.Fatalf("double validation : %v", err)
	}
}

// TestApprouverDemandeIdentifiantUnique vérifie que l'identifiant déjà pris est
// suffixé.
func TestApprouverDemandeIdentifiantUnique(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.service.AppConfig.IdentifiantFormatEleve = "l.martin"

	demande, _ := b.store.CreerDemande(ProfilEleve, "Léa", "Martin", "", "", "", b.moment)
	infos, err := b.service.ApprouverDemande(demande.ID)
	if err != nil {
		t.Fatalf("ApprouverDemande: %v", err)
	}
	if infos["identifiant"] != "l.martin2" {
		t.Fatalf("identifiant = %q, attendu l.martin2", infos["identifiant"])
	}
}

// TestApprouverDemandeSansOU vérifie qu'une OU d'accueil absente bloque la
// validation.
func TestApprouverDemandeSansOU(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.service.Config.OUAccueil = ""

	demande, _ := b.store.CreerDemande(ProfilEleve, "Léa", "Martin", "", "", "", b.moment)
	if _, err := b.service.ApprouverDemande(demande.ID); err == nil ||
		!strings.Contains(err.Error(), "OU d'accueil") {
		t.Fatalf("erreur = %v", err)
	}
}

// TestApprouverDemandeEchecAD vérifie qu'un échec de création annulaire
// remonte un message affichable sans marquer la demande traitée.
func TestApprouverDemandeEchecAD(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.annuaire.errCreation = errors.New("LDAP indisponible")

	demande, _ := b.store.CreerDemande(ProfilEleve, "Léa", "Martin", "", "", "", b.moment)
	_, err := b.service.ApprouverDemande(demande.ID)
	if err == nil || !strings.Contains(err.Error(), "Création du compte impossible") {
		t.Fatalf("erreur = %v", err)
	}
	relue, _ := b.store.DemandeParID(demande.ID)
	if relue.Statut != StatutEnAttente {
		t.Fatalf("demande = %+v", relue)
	}
}

// TestRefuserDemande vérifie le refus et sa journalisation.
func TestRefuserDemande(t *testing.T) {
	b := nouveauBouchonTest(t)
	demande, _ := b.store.CreerDemande(ProfilPersonnel, "Jean", "Durand", "", "", "", b.moment)

	if err := b.service.RefuserDemande(demande.ID, "doublon"); err != nil {
		t.Fatalf("RefuserDemande: %v", err)
	}
	relue, _ := b.store.DemandeParID(demande.ID)
	if relue.Statut != StatutRefusee || relue.Note != "doublon" {
		t.Fatalf("demande = %+v", relue)
	}
	if err := b.service.RefuserDemande(demande.ID, ""); err == nil ||
		!strings.Contains(err.Error(), "déjà été traitée") {
		t.Fatalf("double refus : %v", err)
	}
	if err := b.service.RefuserDemande("inconnue", ""); err == nil ||
		!strings.Contains(err.Error(), "n'existe plus") {
		t.Fatalf("demande inconnue : %v", err)
	}
}

// TestPolitiqueParProfil vérifie que la politique de mot de passe suit le
// profil de la demande.
func TestPolitiqueParProfil(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.service.AppConfig.PolitiqueMDPEleve = models.PasswordPolicy{Longueur: 8}
	b.service.AppConfig.PolitiqueMDPPersonnel = models.PasswordPolicy{Longueur: 12}

	if got := b.service.politiqueProfil(ProfilEleve); got.Longueur != 8 {
		t.Errorf("élève = %d", got.Longueur)
	}
	if got := b.service.politiqueProfil(ProfilPersonnel); got.Longueur != 12 {
		t.Errorf("personnel = %d", got.Longueur)
	}

	// La politique de vérification suit `config.politique`.
	b.service.Config.Politique = ProfilPersonnel
	if got := b.service.politique(); got.Longueur != 12 {
		t.Errorf("vérification = %d", got.Longueur)
	}
}

// TestScopeValideNormalise vérifie qu'une portée inconnue retombe sur la
// réinitialisation.
func TestScopeValideNormalise(t *testing.T) {
	if got := scopeValide("n'importe quoi"); got != ScopeReinitialisation {
		t.Errorf("scope = %q", got)
	}
	if got := scopeValide(ScopeConsultation); got != ScopeConsultation {
		t.Errorf("scope = %q", got)
	}
}

// TestMasquerMail vérifie qu'une adresse de journal ne conserve que le domaine.
func TestMasquerMail(t *testing.T) {
	if got := masquerMail("lea.martin@college.fr"); got != "…@college.fr" {
		t.Errorf("masquerMail = %q", got)
	}
	if got := masquerMail("sans-arobase"); got != "-" {
		t.Errorf("masquerMail = %q", got)
	}
	if got := masquerMail("lea@"); got != "-" {
		t.Errorf("masquerMail = %q", got)
	}
}

// TestIdentifiantUniqueErreur vérifie le message d'erreur quand aucun
// identifiant libre n'est trouvé.
func TestIdentifiantUniqueErreur(t *testing.T) {
	b := nouveauBouchonTest(t)
	for i := 1; i <= 101; i++ {
		b.annuaire.existants[fmt.Sprintf("dup%d", i)] = true
	}
	b.annuaire.existants["dup"] = true
	_, err := b.service.identifiantUnique("dup")
	if err == nil || !strings.Contains(err.Error(), "Impossible de générer un identifiant unique") {
		t.Fatalf("erreur = %v", err)
	}
}
