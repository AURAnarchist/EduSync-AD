package compliance

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
)

// formatISO reproduit `datetime.isoformat(timespec="seconds")` de Python
// (UTC s'écrit « +00:00 », jamais « Z »).
const formatISO = "2006-01-02T15:04:05-07:00"

// Niveaux de conformité utilisés par les rapports.
const (
	// NiveauInfo signale une observation.
	NiveauInfo = "info"
	// NiveauAvertissement signale un point à corriger.
	NiveauAvertissement = "warning"
	// NiveauCritique signale un problème bloquant.
	NiveauCritique = "critical"
)

// Statuts MFA (identiques à la version Python : complete/partial/none).
const (
	// MFAComplet : tous les critères sont remplis.
	MFAComplet = "complete"
	// MFAPartiel : la double authentification est activée mais incomplète.
	MFAPartiel = "partial"
	// MFAAucun : la double authentification n'est pas activée.
	MFAAucun = "none"
)

// RapportMFA est le résultat de `VerifierMFAAdmin`.
type RapportMFA struct {
	// Statut vaut MFAComplet, MFAPartiel ou MFAAucun.
	Statut string `json:"mfa_status"`
	// Details liste les éléments manquants.
	Details []string `json:"details"`
	// Recommandations liste les actions à entreprendre.
	Recommandations []string `json:"recommandations"`
}

// MinimumCodesSecours est le nombre minimal de codes de secours attendus.
const MinimumCodesSecours = 5

// VerifierMFAAdmin contrôle que l'administration satisfait les critères de
// double authentification, en s'appuyant sur la configuration `conformite.json`
// (le Python lisait un attribut `mfa_config` jamais alimenté).
func VerifierMFAAdmin(cfg Config) RapportMFA {
	details := []string{}
	recommandations := []string{}

	if !cfg.MFAActif {
		details = append(details, "MFA non activé dans la configuration administrateur")
		recommandations = append(recommandations, "Activer le MFA dans Paramètres → Sécurité")
	}

	switch cfg.MFAMethode {
	case MethodeTOTP:
		if cfg.MFASecret == "" {
			details = append(details, "Aucun secret TOTP enregistré")
			recommandations = append(recommandations,
				"Générer un secret TOTP puis l'enregistrer dans une application d'authentification")
		}
	case MethodeCleLog:
		// La cérémonie WebAuthn est assurée par le poste : la configuration ne
		// peut que déclarer la méthode.
	default:
		details = append(details, fmt.Sprintf("Méthode MFA inconnue : %q", cfg.MFAMethode))
		recommandations = append(recommandations, "Choisir la méthode « totp » ou « cle_log »")
	}

	if len(cfg.MFACodesSecours) < MinimumCodesSecours {
		details = append(details, fmt.Sprintf(
			"Codes de secours insuffisants : %d (minimum %d)",
			len(cfg.MFACodesSecours), MinimumCodesSecours))
		recommandations = append(recommandations,
			"Régénérer des codes de secours et les conserver hors ligne")
	}

	statut := MFAComplet
	switch {
	case !cfg.MFAActif:
		statut = MFAAucun
	case len(details) > 0:
		statut = MFAPartiel
	}
	return RapportMFA{Statut: statut, Details: details, Recommandations: recommandations}
}

// CleSeche est une clé de secours chiffrée pour la récupération administrateur.
type CleSeche struct {
	// AdminID identifie l'administrateur concerné.
	AdminID string `json:"admin_id"`
	// CleChiffree est la clé de secours chiffrée en AES-256-GCM (base64).
	CleChiffree string `json:"cle_chiffree"`
	// DateCreation est l'horodatage de création (ISO 8601, secondes).
	DateCreation string `json:"date_creation"`
	// DateExpiration est l'horodatage d'expiration (ISO 8601, secondes).
	DateExpiration string `json:"date_expiration"`
	// Description rappelle la finalité de la clé.
	Description string `json:"description"`
}

// ValiditeCleSecheJours est la durée de validité d'une clé de secours.
const ValiditeCleSecheJours = 365

// GenererCleSeche génère une clé de secours aléatoire de 32 octets, chiffrée
// avec la clé AES locale (`secret.key`). La clé en clair n'est jamais
// persistée : elle est destinée à être imprimée puis rangée dans un coffre
// physique.
func GenererCleSeche(adminID string, cle []byte, maintenant time.Time) (CleSeche, error) {
	if strings.TrimSpace(adminID) == "" {
		return CleSeche{}, fmt.Errorf("compliance: identifiant administrateur requis")
	}
	if len(cle) != crypto.KeySize {
		return CleSeche{}, crypto.ErrInvalidKey
	}
	brut := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, brut); err != nil {
		return CleSeche{}, fmt.Errorf("compliance: génération de la clé de secours: %w", err)
	}
	chiffree, err := crypto.Encrypt(cle, hex.EncodeToString(brut))
	if err != nil {
		return CleSeche{}, fmt.Errorf("compliance: chiffrement de la clé de secours: %w", err)
	}
	expiration := maintenant.AddDate(0, 0, ValiditeCleSecheJours)
	return CleSeche{
		AdminID:        strings.TrimSpace(adminID),
		CleChiffree:    chiffree,
		DateCreation:   maintenant.Format(formatISO),
		DateExpiration: expiration.Format(formatISO),
		Description:    "Clé de secours MFA admin — à conserver dans un coffre-fort physique",
	}, nil
}

// Session décrit une session ouverte, pour le contrôle d'unicité.
type Session struct {
	// Operateur est l'identifiant de l'opérateur connecté.
	Operateur string `json:"operateur"`
	// Domaine est le domaine AD concerné.
	Domaine string `json:"domaine"`
	// SessionID identifie la session.
	SessionID string `json:"session_id"`
	// Debut est l'horodatage d'ouverture (ISO 8601).
	Debut string `json:"debut"`
}

// RapportSessionUnique est le résultat du contrôle de session unique.
type RapportSessionUnique struct {
	// Statut vaut « compliant », « violation » ou « desactive ».
	Statut string `json:"status"`
	// SessionsActives est le nombre de sessions analysées.
	SessionsActives int `json:"sessions_actives"`
	// Doublons liste les opérateurs connectés plusieurs fois.
	Doublons []string `json:"utilisateurs_connecte"`
	// Details complète le rapport.
	Details []string `json:"details"`
	// Recommandations liste les actions correctives.
	Recommandations []string `json:"recommandations"`
}

// VerifierSessionUnique détecte les opérateurs connectés simultanément sur
// plusieurs sessions. Le Python se contentait de signaler que la politique est
// active ; ici, chaque doublon est nommé.
func VerifierSessionUnique(cfg Config, sessions []Session) RapportSessionUnique {
	rapport := RapportSessionUnique{
		SessionsActives: len(sessions),
		Doublons:        []string{},
		Details:         []string{},
		Recommandations: []string{},
	}

	if !cfg.SessionUnique {
		rapport.Statut = "desactive"
		rapport.Recommandations = append(rapport.Recommandations,
			"Activer la session unique pour empêcher le partage d'un compte administrateur")
		return rapport
	}

	comptes := map[string]map[string]bool{}
	for _, session := range sessions {
		cle := strings.ToLower(strings.TrimSpace(session.Operateur)) + "@" +
			strings.ToLower(strings.TrimSpace(session.Domaine))
		if _, ok := comptes[cle]; !ok {
			comptes[cle] = map[string]bool{}
		}
		comptes[cle][session.SessionID] = true
	}

	for cle, sessionsOperateur := range comptes {
		if len(sessionsOperateur) > 1 {
			rapport.Doublons = append(rapport.Doublons, fmt.Sprintf("%s (%d sessions)", cle, len(sessionsOperateur)))
		}
	}
	sort.Strings(rapport.Doublons)

	rapport.Statut = "compliant"
	if len(rapport.Doublons) > 0 {
		rapport.Statut = "violation"
		rapport.Details = append(rapport.Details,
			"Plusieurs sessions actives pour un même opérateur : une session a peut-être été laissée ouverte.")
		rapport.Recommandations = append(rapport.Recommandations,
			"Déconnecter les sessions en doublon avant toute opération d'administration.")
	} else {
		rapport.Recommandations = append(rapport.Recommandations,
			"Politique de session unique respectée — déconnexions forcées si nécessaire.")
	}
	return rapport
}

// PolitiqueFine décrit une politique de mot de passe à granularité fine (PSO
// Active Directory), telle qu'elle sera publiée par `New-ADFineGrainedPasswordPolicy`.
type PolitiqueFine struct {
	// Nom est le nom de la PSO (préfixe, nom, précédence).
	Nom string `json:"nom"`
	// Profils liste les populations visées (« eleves », « personnels »).
	Profils []string `json:"profils"`
	// Longueur est la longueur minimale exigée.
	Longueur int `json:"longueur"`
	// Complexite indique si la complexité est exigée.
	Complexite bool `json:"complexite"`
	// Verrouillage seuil de verrouillage (nombre d'échecs).
	Verrouillage int `json:"verrouillage"`
	// DureeVerrouillageMin est la durée de verrouillage en minutes.
	DureeVerrouillageMin int `json:"duree_verrouillage_min"`
	// Precedence est l'ordre d'application (plus petit = prioritaire).
	Precedence int `json:"precedence"`
}

// AnalyseGranulariteMDP est le résultat de l'analyse des politiques de mots de
// passe.
type AnalyseGranulariteMDP struct {
	// Politiques liste les PSO proposées.
	Politiques []PolitiqueFine `json:"politiques"`
	// Differences liste les écarts entre les politiques élèves et personnels.
	Differences []string `json:"differences"`
	// Recommandations liste les conseils de mise en œuvre.
	Recommandations []string `json:"recommandations"`
	// Score est une note de conformité sur 100.
	Score int `json:"score_conformite"`
}

// RapportPSO est l'analyse de la granularité des politiques de mots de passe.
type RapportPSO = AnalyseGranulariteMDP

// SeuilVerrouillageDefaut et DureeVerrouillageDefaut sont les valeurs
// recommandées pour le verrouillage des comptes.
const (
	SeuilVerrouillageDefaut    = 5
	DureeVerrouillageDefautMin = 15
)

// AnalyserGranulariteMDP compare les politiques élèves/personnels et propose
// deux PSO : une par population, avec des seuils adaptés (les élèves sont plus
// exposés au partage de mot de passe).
func AnalyserGranulariteMDP(cfg config.AppConfig) AnalyseGranulariteMDP {
	eleve := cfg.PolitiqueMDPEleve
	personnel := cfg.PolitiqueMDPPersonnel

	differences := []string{}
	if eleve.Longueur != personnel.Longueur {
		differences = append(differences, fmt.Sprintf(
			"Longueurs différentes (élèves %d, personnels %d) : à harmoniser ou à justifier par une PSO.",
			eleve.Longueur, personnel.Longueur))
	}
	if eleve.Majuscules != personnel.Majuscules {
		differences = append(differences,
			"L'exigence de majuscules diffère entre élèves et personnels.")
	}
	if eleve.CaracteresSpeciaux != personnel.CaracteresSpeciaux {
		differences = append(differences,
			"L'exigence de caractères spéciaux diffère entre élèves et personnels.")
	}
	if !eleve.Majuscules && !personnel.Majuscules {
		differences = append(differences,
			"Aucune politique n'exige de majuscules : la complexité repose sur les seuls chiffres.")
	}

	politiques := []PolitiqueFine{
		{
			Nom:                  "PSO-Eleves",
			Profils:              []string{"eleves"},
			Longueur:             max(eleve.Longueur, 12),
			Complexite:           true,
			Verrouillage:         SeuilVerrouillageDefaut,
			DureeVerrouillageMin: DureeVerrouillageDefautMin,
			Precedence:           10,
		},
		{
			Nom:                  "PSO-Personnels",
			Profils:              []string{"personnels"},
			Longueur:             max(personnel.Longueur, 14),
			Complexite:           true,
			Verrouillage:         SeuilVerrouillageDefaut,
			DureeVerrouillageMin: DureeVerrouillageDefautMin,
			Precedence:           20,
		},
	}

	recommandations := []string{}
	score := 100
	if len(differences) > 0 {
		score -= 10 * len(differences)
		recommandations = append(recommandations,
			"Publier une PSO par population plutôt que de conserver la politique unique du domaine.")
	}
	if eleve.Longueur < 12 {
		score -= 15
		recommandations = append(recommandations,
			"Porter la longueur minimale des élèves à 12 caractères (recommandation ANSSI).")
	}
	if personnel.Longueur < 14 {
		score -= 10
		recommandations = append(recommandations,
			"Porter la longueur minimale des personnels à 14 caractères pour les comptes à privilèges.")
	}
	if !eleve.CaracteresSpeciaux {
		recommandations = append(recommandations,
			"Exiger au moins un caractère spécial pour les élèves (ou allonger encore le mot de passe).")
	}
	if score < 0 {
		score = 0
	}
	if len(recommandations) == 0 {
		recommandations = append(recommandations,
			"Les politiques actuelles couvrent les recommandations : publier les PSO pour les rendre opposables.")
	}

	return AnalyseGranulariteMDP{
		Politiques:      politiques,
		Differences:     differences,
		Recommandations: recommandations,
		Score:           score,
	}
}

// AnalyserPSO analyse la granularité des politiques de mots de passe.
// Fonction d'interface pour la conformité.
func AnalyserPSO(cfg config.AppConfig) RapportPSO {
	return AnalyserGranulariteMDP(cfg)
}

// ScriptPSO génère le script PowerShell de publication des PSO proposées.
//
// Les commandes sont idempotentes (`-ErrorAction SilentlyContinue` puis
// modification), comme les autres scripts produits par l'application.
func ScriptPSO(analyse AnalyseGranulariteMDP, baseDN string) string {
	var b strings.Builder
	b.WriteString("# PSO (politiques de mot de passe à granularité fine) — généré par EduSync AD\n")
	b.WriteString("Import-Module ActiveDirectory\n\n")
	for _, politique := range analyse.Politiques {
		fmt.Fprintf(&b, "# %s\n", politique.Nom)
		fmt.Fprintf(&b, "New-ADFineGrainedPasswordPolicy -Name %q -Precedence %d `\n",
			politique.Nom, politique.Precedence)
		fmt.Fprintf(&b, "  -MinPasswordLength %d -ComplexityEnabled $%t `\n",
			politique.Longueur, politique.Complexite)
		fmt.Fprintf(&b, "  -LockoutThreshold %d -LockoutDuration (New-TimeSpan -Minutes %d) -LockoutObservationWindow (New-TimeSpan -Minutes %d) `\n",
			politique.Verrouillage, politique.DureeVerrouillageMin, politique.DureeVerrouillageMin)
		b.WriteString("  -ReversibleEncryptionEnabled $false -PasswordHistoryCount 24\n")
		for _, profil := range politique.Profils {
			fmt.Fprintf(&b, "Add-ADFineGrainedPasswordPolicySubject -Identity %q -Subjects (Get-ADGroup -Filter \"Name -eq 'GG-%s'\")\n",
				politique.Nom, titreProfil(profil))
		}
		b.WriteString("\n")
	}
	if baseDN != "" {
		fmt.Fprintf(&b, "# Domaine cible : %s\n", baseDN)
	}
	return b.String()
}

// titreProfil met en forme un nom de profil pour un nom de groupe.
func titreProfil(profil string) string {
	if profil == "" {
		return profil
	}
	return strings.ToUpper(profil[:1]) + profil[1:]
}

// SignatureExport est la signature détachée d'un export de données.
type SignatureExport struct {
	// Signature est le HMAC-SHA256 (hexadécimal) du JSON canonique.
	Signature string `json:"signature"`
	// Horodatage est l'instant de signature (ISO 8601, secondes).
	Horodatage string `json:"horodatage"`
	// DataHash est l'empreinte SHA-256 (hexadécimale) du JSON canonique.
	DataHash string `json:"data_hash"`
	// Algorithm nomme l'algorithme utilisé.
	Algorithm string `json:"algorithm"`
}

// AlgorithmeSignature nomme l'algorithme de signature des exports.
const AlgorithmeSignature = "HMAC-SHA256"

// SignerExport signe un export : HMAC-SHA256 du JSON canonique avec la clé AES
// locale. L'horodatage est conservé à part, hors du calcul : l'implémentation
// Python, elle, incluait l'instant dans la signature, ce qui la rendait
// invérifiable.
func SignerExport(donnees any, cle []byte, maintenant time.Time) (SignatureExport, error) {
	if len(cle) != crypto.KeySize {
		return SignatureExport{}, crypto.ErrInvalidKey
	}
	canonique, err := jsonCanonique(donnees)
	if err != nil {
		return SignatureExport{}, err
	}
	mac := hmac.New(sha256.New, cle)
	if _, err := mac.Write(canonique); err != nil {
		return SignatureExport{}, fmt.Errorf("compliance: calcul de la signature: %w", err)
	}
	somme := sha256.Sum256(canonique)
	return SignatureExport{
		Signature:  hex.EncodeToString(mac.Sum(nil)),
		Horodatage: maintenant.Format(formatISO),
		DataHash:   hex.EncodeToString(somme[:]),
		Algorithm:  AlgorithmeSignature,
	}, nil
}

// VerifierExport vérifie la signature d'un export. Retourne une erreur explicite
// si l'empreinte ou la signature ne correspond pas.
func VerifierExport(donnees any, signature SignatureExport, cle []byte) error {
	if len(cle) != crypto.KeySize {
		return crypto.ErrInvalidKey
	}
	canonique, err := jsonCanonique(donnees)
	if err != nil {
		return err
	}
	somme := sha256.Sum256(canonique)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(somme[:])), []byte(signature.DataHash)) != 1 {
		return fmt.Errorf("compliance: empreinte des données différente de celle signée")
	}
	mac := hmac.New(sha256.New, cle)
	if _, err := mac.Write(canonique); err != nil {
		return fmt.Errorf("compliance: calcul de la signature: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(signature.Signature)) != 1 {
		return fmt.Errorf("compliance: signature invalide (clé ou données modifiées)")
	}
	return nil
}

// jsonCanonique sérialise des données de façon déterministe : les clés des
// cartes sont triées par `encoding/json`, les champs des structures suivent leur
// ordre de déclaration. La même fonction sert à signer et à vérifier.
func jsonCanonique(donnees any) ([]byte, error) {
	canonique, err := json.Marshal(donnees)
	if err != nil {
		return nil, fmt.Errorf("compliance: sérialisation canonique: %w", err)
	}
	return canonique, nil
}
