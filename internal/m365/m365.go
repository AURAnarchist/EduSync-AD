// Package m365 contient la logique métier Office 365 / Microsoft Entra ID
// (M19) : comptes cloud et licences, groupes AD → Entra (avec Teams), photos
// d'identité vers le cloud et check-list d'identité hybride.
//
// La logique reprend src/edusync_ad/core/m365.py pour rester compatible avec
// Python. Le client Graph est injectable (interface ClientGraph) pour les
// tests.
package m365

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/graph"
)

// LicencesEducation associe un skuPartNumber éducation à son libellé.
var LicencesEducation = map[string]string{
	"M365EDU_A1":         "Microsoft 365 A1 (Éducation)",
	"M365EDU_A3_FACULTY": "Microsoft 365 A3 — enseignants",
	"M365EDU_A3_STUDENT": "Microsoft 365 A3 — élèves",
	"M365EDU_A5_FACULTY": "Microsoft 365 A5 — enseignants",
	"M365EDU_A5_STUDENT": "Microsoft 365 A5 — élèves",
	"ENTERPRISEPACK":     "Microsoft 365 E3",
}

// LicenceParDefaut est la licence éducation proposée par défaut.
const LicenceParDefaut = "M365EDU_A1"

// TypesGroupe décrit les types de groupe Entra supportés.
var TypesGroupe = map[string]string{
	"m365":         "Groupe Microsoft 365 (base d'un Teams)",
	"distribution": "Groupe de distribution",
	"security":     "Groupe de sécurité",
}

// PolitiqueMotDePasseCloud retourne la politique imposée par Entra ID.
func PolitiqueMotDePasseCloud() ad.PasswordPolicy {
	return ad.PasswordPolicy{
		Length:           20,
		RequireUppercase: true,
		RequireDigits:    true,
		RequireSpecial:   true,
	}
}

// MailNickname normalise un nom de groupe en mailNickname autorisé par Entra ID
// (accents retirés, minuscules, seuls [a-z0-9._-] conservés).
func MailNickname(nom string) string {
	texte := strings.ToLower(retirerAccents(nom))
	var b strings.Builder
	for _, r := range texte {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	resultat := strings.Trim(b.String(), ".")
	if resultat == "" {
		return "groupe"
	}
	return resultat
}

// retirerAccents retire les accents des caractères latins usuels.
func retirerAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		switch r {
		case 'à', 'â', 'ä', 'á', 'ã', 'å':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'î', 'ï', 'í':
			b.WriteRune('i')
		case 'ô', 'ö', 'ó', 'õ':
			b.WriteRune('o')
		case 'û', 'ü', 'ú', 'ù':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		case 'ñ':
			b.WriteRune('n')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SplitDisplayName découpe un « cn » « Prénom Nom » (suffixe de doublon
// « (sam) » retiré).
func SplitDisplayName(cn, sam string) (string, string) {
	texte := strings.TrimSpace(cn)
	if sam != "" && strings.HasSuffix(texte, "("+sam+")") {
		texte = strings.TrimSpace(texte[:len(texte)-len(sam)-2])
	}
	if texte == "" {
		return "", ""
	}
	idx := -1
	for i, r := range texte {
		if unicode.IsSpace(r) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return texte, ""
	}
	return texte[:idx], strings.TrimSpace(texte[idx:])
}

// ─── Plans ────────────────────────────────────────────────────────────────

// PlanUtilisateurCloud décrit un compte cloud à créer (ou déjà présent).
type PlanUtilisateurCloud struct {
	SAM         string `json:"sam"`
	UPN         string `json:"upn"`
	Prenom      string `json:"prenom"`
	Nom         string `json:"nom"`
	DisplayName string `json:"display_name"`
	Disabled    bool   `json:"disabled"`
	Existing    bool   `json:"existing"`
	Password    string `json:"password,omitempty"`
}

// Label retourne un libellé lisible du plan.
func (p PlanUtilisateurCloud) Label() string {
	statut := "à créer"
	if p.Existing {
		statut = "existant"
	}
	return fmt.Sprintf("%s → %s  [%s]", p.SAM, p.UPN, statut)
}

// CompteAD décrit un compte AD minimal alimentant les plans.
type CompteAD struct {
	SAM      string `json:"sam"`
	CN       string `json:"cn"`
	Disabled bool   `json:"disabled"`
}

// ConstruirePlansUtilisateurs prévoit les comptes cloud depuis des comptes AD.
func ConstruirePlansUtilisateurs(comptes []CompteAD, domaine string, upnExistants []string) []PlanUtilisateurCloud {
	existants := map[string]bool{}
	for _, u := range upnExistants {
		existants[strings.ToLower(strings.TrimSpace(u))] = true
	}
	plans := []PlanUtilisateurCloud{}
	for _, compte := range comptes {
		sam := strings.TrimSpace(compte.SAM)
		if sam == "" {
			continue
		}
		cn := compte.CN
		if cn == "" {
			cn = sam
		}
		prenom, nom := SplitDisplayName(cn, sam)
		upn := sam
		if domaine != "" {
			upn = sam + "@" + domaine
		}
		plans = append(plans, PlanUtilisateurCloud{
			SAM: sam, UPN: upn, Prenom: prenom, Nom: nom, DisplayName: cn,
			Disabled: compte.Disabled, Existing: existants[strings.ToLower(upn)],
		})
	}
	return plans
}

// SpecGroupe décrit un groupe à synchroniser : nom + membres (UPN).
type SpecGroupe struct {
	Nom     string   `json:"name"`
	Membres []string `json:"members"`
}

// PlanGroupe décrit un groupe Entra à créer/synchroniser depuis un groupe AD.
type PlanGroupe struct {
	Nom          string   `json:"name"`
	MailNickname string   `json:"mail_nickname"`
	Kind         string   `json:"kind"`
	Membres      []string `json:"members"`
	ExistingID   string   `json:"existing_id,omitempty"`
}

// Label retourne un libellé lisible du plan de groupe.
func (p PlanGroupe) Label() string {
	statut := "à créer"
	if p.ExistingID != "" {
		statut = "existant"
	}
	return fmt.Sprintf("%s  [%s, %d membre(s)]", p.Nom, statut, len(p.Membres))
}

// ConstruirePlansGroupes construit les plans de groupes (dédupliqués par
// mailNickname). existingByNickname : mailNickname minuscule → id Entra.
func ConstruirePlansGroupes(specs []SpecGroupe, kind string, existingByNickname map[string]string) []PlanGroupe {
	existants := map[string]string{}
	for k, v := range existingByNickname {
		existants[strings.ToLower(k)] = v
	}
	plans := []PlanGroupe{}
	vus := map[string]bool{}
	for _, spec := range specs {
		nom := strings.TrimSpace(spec.Nom)
		if nom == "" {
			continue
		}
		nick := MailNickname(nom)
		if vus[nick] {
			continue
		}
		vus[nick] = true
		membres := make([]string, 0, len(spec.Membres))
		for _, m := range spec.Membres {
			membres = append(membres, m)
		}
		plans = append(plans, PlanGroupe{
			Nom: nom, MailNickname: nick, Kind: kind, Membres: membres,
			ExistingID: existants[nick],
		})
	}
	return plans
}

// ResultatSync décrit l'issue d'une opération de synchronisation.
type ResultatSync struct {
	Key    string `json:"key"`
	Action string `json:"action"` // "created" | "exists" | "updated" | "error"
	Detail string `json:"detail"`
}

// OK indique si l'opération a réussi.
func (r ResultatSync) OK() bool { return r.Action != "error" }

// ─── Photos ───────────────────────────────────────────────────────────────

// ConstruireMappingPhotos associe des fichiers photo aux UPN selon les
// conventions de nommage (identifiant.jpg, prenom_nom.jpg, …).
func ConstruireMappingPhotos(dossier string, comptes []CompteAD, domaine string) (map[string]string, error) {
	fichiers := map[string]string{}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".bmp", ".tiff", ".webp"} {
		err := filepath.WalkDir(dossier, func(chemin string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(chemin), ext) {
				return nil
			}
			stem := strings.ToLower(strings.TrimSuffix(filepath.Base(chemin), filepath.Ext(chemin)))
			if _, existe := fichiers[stem]; !existe {
				fichiers[stem] = chemin
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("m365: exploration des photos: %w", err)
		}
	}

	utilises := map[string]bool{}
	mapping := map[string]string{}
	for _, compte := range comptes {
		sam := compte.SAM
		cn := compte.CN
		if cn == "" {
			cn = sam
		}
		prenom, nom := SplitDisplayName(cn, sam)
		cles := []string{
			strings.ToLower(prenom + "_" + nom),
			strings.ToLower(sam),
			strings.ToLower(prenom + "." + nom),
			strings.ToLower(nom + "_" + prenom),
		}
		for _, cle := range cles {
			if chemin, ok := fichiers[cle]; ok && !utilises[cle] {
				upn := sam
				if domaine != "" {
					upn = sam + "@" + domaine
				}
				mapping[upn] = chemin
				utilises[cle] = true
				break
			}
		}
	}
	return mapping, nil
}

// ─── Client Graph (interface injectable) ──────────────────────────────────

// ClientGraph décrit les opérations Graph utilisées par le module M19.
type ClientGraph interface {
	Organization() (map[string]any, error)
	ListUsers() ([]map[string]any, error)
	ListGroups() ([]map[string]any, error)
	SubscribedSkus() (map[string]string, error)
	CreateUser(params graph.ParamsCreationUtilisateur) (map[string]any, error)
	AssignLicense(userID, skuID string) (map[string]any, error)
	CreateGroup(params graph.ParamsCreationGroupe) (map[string]any, error)
	AddGroupMember(groupID, memberID string) error
	CreateTeam(groupID string) error
	UploadUserPhoto(userID string, jpeg []byte) error
}

// Manager enchaîne les opérations Graph du module M19.
type Manager struct {
	Config graph.Config
	Client ClientGraph
}

// NewManager construit un manager ; un client nil utilise un vrai client Graph.
func NewManager(cfg graph.Config, client ClientGraph) *Manager {
	if client == nil {
		client = graph.NewClient(cfg)
	}
	return &Manager{Config: cfg, Client: client}
}

// TestConnection valide la connexion et retourne le nom du locataire.
func (m *Manager) TestConnection() (string, error) {
	organisation, err := m.Client.Organization()
	if err != nil {
		return "", err
	}
	if nom := graph.Chaine(organisation["displayName"]); nom != "" {
		return nom, nil
	}
	if m.Config.TenantID != "" {
		return m.Config.TenantID, nil
	}
	return "OK", nil
}

// AvailableLicenses retourne les licences éducation souscrites (partNumber →
// libellé + skuId).
func (m *Manager) AvailableLicenses() (map[string]map[string]string, error) {
	skus, err := m.Client.SubscribedSkus()
	if err != nil {
		return nil, err
	}
	resultat := map[string]map[string]string{}
	for part, skuID := range skus {
		if libelle, ok := LicencesEducation[part]; ok {
			resultat[part] = map[string]string{"label": libelle, "sku_id": skuID}
		}
	}
	return resultat, nil
}

// SyncUsers crée les comptes absents d'Entra ID et attribue la licence. Un
// compte déjà présent est signalé « exists » (jamais modifié).
func (m *Manager) SyncUsers(plans []PlanUtilisateurCloud, licensePart string, onProgress func(PlanUtilisateurCloud)) ([]ResultatSync, error) {
	skuID := ""
	if licensePart != "" {
		skus, err := m.Client.SubscribedSkus()
		if err != nil {
			return nil, err
		}
		skuID = skus[licensePart]
		if skuID == "" {
			return nil, &graph.GraphError{Message: fmt.Sprintf(
				"Licence absente du locataire : %s. Vérifiez les licences souscrites dans le portail Microsoft 365.", licensePart)}
		}
	}

	resultats := []ResultatSync{}
	for i := range plans {
		plan := plans[i]
		if plan.Existing {
			resultats = append(resultats, ResultatSync{plan.UPN, "exists", "compte déjà présent"})
		} else {
			if plan.Password == "" {
				politique := PolitiqueMotDePasseCloud()
				motDePasse, err := politique.Generate()
				if err != nil {
					resultats = append(resultats, ResultatSync{plan.UPN, "error", err.Error()})
					if onProgress != nil {
						onProgress(plan)
					}
					continue
				}
				plan.Password = motDePasse
				plans[i].Password = motDePasse
			}
			nomAffichage := plan.DisplayName
			if nomAffichage == "" {
				nomAffichage = plan.SAM
			}
			utilisateur, err := m.Client.CreateUser(graph.ParamsCreationUtilisateur{
				UserPrincipalName: plan.UPN,
				DisplayName:       nomAffichage,
				MailNickname:      MailNickname(plan.SAM),
				Password:          plan.Password,
				GivenName:         plan.Prenom,
				Surname:           plan.Nom,
				AccountEnabled:    !plan.Disabled,
				UsageLocation:     m.Config.UsageLocation,
			})
			if err != nil {
				resultats = append(resultats, ResultatSync{plan.UPN, "error", err.Error()})
			} else {
				detail := "compte créé"
				if skuID != "" {
					if _, err := m.Client.AssignLicense(graph.Chaine(utilisateur["id"]), skuID); err != nil {
						resultats = append(resultats, ResultatSync{plan.UPN, "error", err.Error()})
						if onProgress != nil {
							onProgress(plan)
						}
						continue
					}
					detail += " + licence attribuée"
				}
				resultats = append(resultats, ResultatSync{plan.UPN, "created", detail})
			}
		}
		if onProgress != nil {
			onProgress(plan)
		}
	}
	return resultats, nil
}

// CloudIndexes retourne les index réutilisables pendant un lot :
// (UPN→id, mailNickname→id).
func (m *Manager) CloudIndexes() (map[string]string, map[string]string, error) {
	utilisateurs, err := m.Client.ListUsers()
	if err != nil {
		return nil, nil, err
	}
	groupes, err := m.Client.ListGroups()
	if err != nil {
		return nil, nil, err
	}
	parUPN := map[string]string{}
	for _, u := range utilisateurs {
		if upn := graph.Chaine(u["userPrincipalName"]); upn != "" {
			parUPN[strings.ToLower(upn)] = graph.Chaine(u["id"])
		}
	}
	parNickname := map[string]string{}
	for _, g := range groupes {
		if nick := graph.Chaine(g["mailNickname"]); nick != "" {
			parNickname[strings.ToLower(nick)] = graph.Chaine(g["id"])
		}
	}
	return parUPN, parNickname, nil
}

// SyncGroups crée les groupes manquants et ajoute leurs membres (UPN → id).
func (m *Manager) SyncGroups(plans []PlanGroupe, createTeam bool, usersByUPN, groupsByNickname map[string]string, onProgress func(PlanGroupe)) ([]ResultatSync, error) {
	if usersByUPN == nil || groupsByNickname == nil {
		idxUtilisateurs, idxGroupes, err := m.CloudIndexes()
		if err != nil {
			return nil, err
		}
		if usersByUPN == nil {
			usersByUPN = idxUtilisateurs
		}
		if groupsByNickname == nil {
			groupsByNickname = idxGroupes
		}
	}
	utilisateurs, existants := usersByUPN, groupsByNickname

	resultats := []ResultatSync{}
	for _, plan := range plans {
		groupID := existants[strings.ToLower(plan.MailNickname)]
		create := false
		var err error
		if groupID == "" {
			var groupe map[string]any
			groupe, err = m.Client.CreateGroup(graph.ParamsCreationGroupe{
				DisplayName: plan.Nom, MailNickname: plan.MailNickname, Kind: plan.Kind,
			})
			if err != nil {
				resultats = append(resultats, ResultatSync{plan.Nom, "error", err.Error()})
				if onProgress != nil {
					onProgress(plan)
				}
				continue
			}
			groupID = graph.Chaine(groupe["id"])
			create = true
		}

		ajoutes := 0
		for _, upn := range plan.Membres {
			userID := utilisateurs[strings.ToLower(upn)]
			if userID == "" {
				continue
			}
			if err := m.Client.AddGroupMember(groupID, userID); err != nil {
				// Déjà membre (AAD_DataConsistencyError) : non bloquant.
				continue
			}
			ajoutes++
		}

		equipe := ""
		if create && plan.Kind == "m365" && createTeam {
			if err := m.Client.CreateTeam(groupID); err == nil {
				equipe = " + Teams créé"
			}
		}

		statut := "déjà présent"
		action := "exists"
		if create {
			statut = "créé"
			action = "created"
		}
		resultats = append(resultats, ResultatSync{
			plan.Nom, action,
			fmt.Sprintf("groupe %s, %d membre(s) ajouté(s)%s", statut, ajoutes, equipe),
		})
		if onProgress != nil {
			onProgress(plan)
		}
	}
	return resultats, nil
}

// SyncPhotos dépose chaque photo (recadrée/compressée comme en AD) comme photo
// de profil cloud.
func (m *Manager) SyncPhotos(mapping map[string]string, usersByUPN map[string]string, onProgress func(string)) ([]ResultatSync, error) {
	if usersByUPN == nil {
		idxUtilisateurs, _, err := m.CloudIndexes()
		if err != nil {
			return nil, err
		}
		usersByUPN = idxUtilisateurs
	}
	utilisateurs := usersByUPN

	cles := make([]string, 0, len(mapping))
	for k := range mapping {
		cles = append(cles, k)
	}
	sort.Strings(cles)

	resultats := []ResultatSync{}
	for _, upn := range cles {
		chemin := mapping[upn]
		userID := utilisateurs[strings.ToLower(upn)]
		if userID == "" {
			resultats = append(resultats, ResultatSync{upn, "error", "compte introuvable dans Entra ID"})
			if onProgress != nil {
				onProgress(upn)
			}
			continue
		}
		jpeg, largeur, hauteur, err := ad.PreparerPhotoJPEG(chemin, ad.PhotoTypePasseport)
		if err != nil {
			resultats = append(resultats, ResultatSync{upn, "error", err.Error()})
			if onProgress != nil {
				onProgress(upn)
			}
			continue
		}
		if err := m.Client.UploadUserPhoto(userID, jpeg); err != nil {
			resultats = append(resultats, ResultatSync{upn, "error", err.Error()})
			if onProgress != nil {
				onProgress(upn)
			}
			continue
		}
		resultats = append(resultats, ResultatSync{
			upn, "updated",
			fmt.Sprintf("photo %d×%d (%.0f Ko)", largeur, hauteur, float64(len(jpeg))/1024),
		})
		if onProgress != nil {
			onProgress(upn)
		}
	}
	return resultats, nil
}

// ─── Hybride (PHS / ADFS / PTA) ───────────────────────────────────────────

// HybridChecklist retourne les étapes d'exploitation de l'identité hybride
// AD ↔ Entra ID (title, détail).
func HybridChecklist() [][2]string {
	return [][2]string{
		{
			"Synchronisation des mots de passe (PHS) — recommandé",
			"Installer Microsoft Entra Connect sur un serveur Windows joints au domaine, " +
				"cocher « Synchronisation du mot de passe » dans l'assistant d'installation, " +
				"puis vérifier dans Entra ID > Connecteurs de synchronisation > " +
				"Détails du connecteur > Synchronisation des mots de passe : « Activé ».",
		},
		{
			"Fédération (AD FS)",
			"Prévoir deux serveurs ADFS en haute disponibilité (load-balancer), " +
				"certificat de fédération valide, et exécuter `Set-MsolDomainAuthentication` " +
				"(ou l'assistant de fédération Entra Connect) pour basculer le domaine en fédéré. " +
				"Inutile si PHS suffit à l'établissement.",
		},
		{
			"Authentification transmise (PTA)",
			"Déployer 1 à 3 agents Pass-through Authentication sur des serveurs joins de domaine " +
				"avec HTTPS sortant vers login.microsoftonline.com ; les mots de passe sont validés " +
				"en direct auprès de l'AD sans les stocker dans le cloud.",
		},
		{
			"Écriture retour des mots de passe (password writeback)",
			"Dans Entra Connect, activer l'écriture retour pour permettre la réinitialisation " +
				"autoservice et l'expiration de mot de passe côté cloud — indispensable avec PHS/PTA.",
		},
		{
			"Localisation d'usage & licences",
			"Chaque compte cloud doit porter un usageLocation (ISO 2, ex : FR) avant toute " +
				"attribution de licence — c'est cette valeur que le module applique à la création.",
		},
		{
			"Cohérence des identifiants",
			"L'UPN cloud reprend l'identifiant AD (samAccountName@domaine) : identique à celui " +
				"créé par les modules de création de comptes, afin que PHS apparie les comptes " +
				"sans créer de doublon.",
		},
	}
}

// HybridChecklistMarkdown génère le rapport Markdown de la check-list hybride.
func HybridChecklistMarkdown() string {
	lignes := []string{
		"# Identité hybride AD ↔ Microsoft Entra ID (M19)",
		"",
		"Étapes manuelles — Microsoft Entra Connect ne peut pas être configuré depuis Microsoft Graph.",
		"",
	}
	for i, etape := range HybridChecklist() {
		lignes = append(lignes, fmt.Sprintf("## %d. %s", i+1, etape[0]))
		lignes = append(lignes, "")
		lignes = append(lignes, etape[1])
		lignes = append(lignes, "")
	}
	return strings.Join(lignes, "\n")
}
