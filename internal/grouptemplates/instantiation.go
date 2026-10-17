package grouptemplates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/classspaces"
	"github.com/estemobs/EduSync-AD/internal/config"
)

// GroupeExistant décrit un groupe présent dans l'annuaire.
type GroupeExistant struct {
	DN  string
	SAM string
}

// Annuaire expose les opérations AD nécessaires à l'instanciation d'un modèle.
// L'interface permet de tester l'orchestration sans contrôleur de domaine ;
// `PoolAnnuaire` relie l'implémentation à `internal/ad`.
type Annuaire interface {
	// ListerOUEnfants retourne les DN des OU filles de parentDN.
	ListerOUEnfants(ctx context.Context, parentDN string) ([]string, error)
	// CreerOU crée l'OU nommée rdn sous parentDN.
	CreerOU(ctx context.Context, parentDN, rdn string) error
	// ListerGroupes retourne les groupes présents dans un conteneur.
	ListerGroupes(ctx context.Context, conteneur string) ([]GroupeExistant, error)
	// CreerGroupe crée un groupe de sécurité avec la portée demandée
	// (« global », « domainlocal » ou « universal »).
	CreerGroupe(ctx context.Context, dn, sam, portee string) error
}

// ActionGroupe consigne la création (ou l'existence) d'un groupe.
type ActionGroupe struct {
	Nom    string
	DN     string
	Sam    string
	Portee string
	Cree   bool
}

// ToDict sérialise l'action au format Python (`asdict`).
func (a ActionGroupe) ToDict() map[string]any {
	return map[string]any{
		"name":    a.Nom,
		"dn":      a.DN,
		"sam":     a.Sam,
		"scope":   a.Portee,
		"created": a.Cree,
	}
}

// ResultatInstantiation consigne le résultat complet d'une instanciation.
type ResultatInstantiation struct {
	IDModele      string
	NomModele     string
	Valeurs       map[string]string
	OUDN          string
	OUCreee       bool
	Groupes       []ActionGroupe
	Artefacts     []string
	Enregistres   []string
	Notes         []string
	CheminRapport string
}

// GroupesCrees retourne les groupes effectivement créés.
func (r *ResultatInstantiation) GroupesCrees() []ActionGroupe {
	var crees []ActionGroupe
	for _, groupe := range r.Groupes {
		if groupe.Cree {
			crees = append(crees, groupe)
		}
	}
	return crees
}

// Resume retourne un compte rendu lisible de l'instanciation.
func (r *ResultatInstantiation) Resume() string {
	lignes := []string{fmt.Sprintf("Modèle « %s » instancié.", r.NomModele)}
	if r.OUDN != "" {
		etat := "existante"
		if r.OUCreee {
			etat = "créée"
		}
		lignes = append(lignes, fmt.Sprintf("OU : %s (%s).", r.OUDN, etat))
	}
	crees := len(r.GroupesCrees())
	existants := len(r.Groupes) - crees
	detail := fmt.Sprintf("%d groupe(s)", len(r.Groupes))
	if crees > 0 {
		detail += fmt.Sprintf(", %d créé(s)", crees)
	}
	if existants > 0 {
		detail += fmt.Sprintf(", %d déjà présent(s)", existants)
	}
	lignes = append(lignes, detail+".")
	if len(r.Artefacts) > 0 {
		lignes = append(lignes, fmt.Sprintf("%d artefact(s) généré(s).", len(r.Artefacts)))
	}
	for _, item := range r.Enregistres {
		lignes = append(lignes, "✓ "+item)
	}
	return strings.Join(lignes, "\n")
}

// ToDict sérialise le résultat au format Python.
func (r *ResultatInstantiation) ToDict() map[string]any {
	valeurs := map[string]any{}
	for cle, valeur := range r.Valeurs {
		valeurs[cle] = valeur
	}
	groupes := make([]any, 0, len(r.Groupes))
	for _, groupe := range r.Groupes {
		groupes = append(groupes, groupe.ToDict())
	}
	artefacts := make([]any, 0, len(r.Artefacts))
	for _, artefact := range r.Artefacts {
		artefacts = append(artefacts, artefact)
	}
	enregistres := make([]any, 0, len(r.Enregistres))
	for _, item := range r.Enregistres {
		enregistres = append(enregistres, item)
	}
	notes := make([]any, 0, len(r.Notes))
	for _, note := range r.Notes {
		notes = append(notes, note)
	}
	return map[string]any{
		"modele":      r.NomModele,
		"id":          r.IDModele,
		"valeurs":     valeurs,
		"ou_dn":       r.OUDN,
		"ou_creee":    r.OUCreee,
		"groupes":     groupes,
		"artefacts":   artefacts,
		"enregistres": enregistres,
		"notes":       notes,
		"horodatage":  time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}
}

// OptionsInstantiation pilote l'instanciation. La valeur zéro reproduit les
// valeurs par défaut de Python (`register_scoped=True`, `apply_home_globally=False`,
// aucun artefact, OU parente du modèle).
type OptionsInstantiation struct {
	// OUParente remplace l'OU parente du modèle (nil = celle du modèle).
	OUParente *string
	// DossierArtefacts est le répertoire de génération ("" = aucun artefact).
	DossierArtefacts string
	// SansScope désactive l'enregistrement des configs scopées profil/quota.
	SansScope bool
	// DossierPersoGlobal remplace la configuration générale des dossiers perso.
	DossierPersoGlobal bool
	// Domaine préfixe les identifiants des ACE du script d'espaces.
	Domaine string
	// DossierConfig est le répertoire des configs partagées ("" = config).
	DossierConfig string
}

// Instancier crée l'OU et les groupes du modèle puis génère les artefacts
// (script de connexion, script d'espaces, script FSRM, rapport JSON).
//
// L'opération est idempotente : une OU ou un groupe déjà présent n'est pas
// recréé. La validation et le calcul des cibles interviennent avant toute
// écriture AD.
func Instancier(
	ctx context.Context,
	annuaire Annuaire,
	modele ModeleGroupe,
	valeurs map[string]string,
	opts OptionsInstantiation,
) (*ResultatInstantiation, error) {
	erreurs := append(modele.Validate(), ValiderValeurs(modele, valeurs)...)
	if len(erreurs) > 0 {
		return nil, errors.New(strings.Join(erreurs, "; "))
	}

	parent := modele.OUParenteDN
	if opts.OUParente != nil {
		parent = *opts.OUParente
	}
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil, errors.New("Aucune OU parente (à renseigner dans le modèle ou à l'instanciation).")
	}

	resultat := &ResultatInstantiation{
		IDModele:  modele.ID,
		NomModele: modele.Nom,
		Valeurs:   map[string]string{},
	}
	for cle, valeur := range valeurs {
		resultat.Valeurs[cle] = valeur
	}

	// 0. Cible calculée + script re-scopé validé AVANT toute écriture AD.
	conteneur := parent
	ouDN := ""
	rdn := ""
	if modele.MotifOU != "" {
		rendu, err := RendreMotif(modele.MotifOU, valeurs)
		if err != nil {
			return nil, err
		}
		rdn = rendu
		ouDN = fmt.Sprintf("ou=%s,%s", EchapperRDN(rdn), parent)
		conteneur = ouDN
	}
	var script *ScriptLogon
	if modele.ScriptLogon != nil {
		copie := *modele.ScriptLogon
		script = &copie
		if ouDN != "" {
			script.Portee = "ou"
			script.PorteeDN = ouDN
		}
		if erreursScript := script.Validate(); len(erreursScript) > 0 {
			return nil, fmt.Errorf("Script de connexion : %s", strings.Join(erreursScript, "; "))
		}
	}

	// 1. OU dédiée (si motif) — idempotente.
	if ouDN != "" {
		enfants, err := annuaire.ListerOUEnfants(ctx, parent)
		if err != nil {
			return nil, err
		}
		existe := false
		cible := strings.ToLower(ouDN)
		for _, enfant := range enfants {
			if strings.ToLower(strings.TrimSpace(enfant)) == cible {
				existe = true
				break
			}
		}
		if !existe {
			if err := annuaire.CreerOU(ctx, parent, rdn); err != nil {
				return nil, err
			}
		}
		resultat.OUDN = ouDN
		resultat.OUCreee = !existe
		conteneur = ouDN
	}

	// 2. Groupes auto — idempotents.
	existants, err := annuaire.ListerGroupes(ctx, conteneur)
	if err != nil {
		return nil, err
	}
	connus := map[string]bool{}
	for _, groupe := range existants {
		connus[strings.ToLower(strings.TrimSpace(groupe.DN))] = true
	}
	for _, spec := range modele.Groupes {
		nom, err := RendreMotif(spec.MotifNom, valeurs)
		if err != nil {
			return nil, err
		}
		dn := fmt.Sprintf("cn=%s,%s", EchapperRDN(nom), conteneur)
		sam := SamGroupe(nom)
		cree := !connus[strings.ToLower(dn)]
		if cree {
			if err := annuaire.CreerGroupe(ctx, dn, sam, spec.Portee); err != nil {
				return nil, err
			}
			connus[strings.ToLower(dn)] = true
		}
		resultat.Groupes = append(resultat.Groupes, ActionGroupe{
			Nom: nom, DN: dn, Sam: sam, Portee: spec.Portee, Cree: cree,
		})
	}

	// 3. Configs scopées OU (M13 profil, M14 quota — résolution groupe > OU > défaut).
	dossierConfig := opts.DossierConfig
	if dossierConfig == "" {
		resolu, err := config.ConfigDir()
		if err != nil {
			return nil, err
		}
		dossierConfig = resolu
	}
	if !opts.SansScope && resultat.OUDN != "" {
		if modele.Profil != nil {
			configs := ChargerConfigsProfils(dossierConfig)
			configs.OU[resultat.OUDN] = *modele.Profil
			if _, err := SauvegarderConfigsProfils(dossierConfig, configs); err != nil {
				return nil, err
			}
			resultat.Enregistres = append(resultat.Enregistres,
				"profil (profilPath) → "+resultat.OUDN)
		}
		if modele.Quota != nil {
			configs := ChargerConfigsQuotas(dossierConfig)
			configs.OU[resultat.OUDN] = *modele.Quota
			if _, err := SauvegarderConfigsQuotas(dossierConfig, configs); err != nil {
				return nil, err
			}
			resultat.Enregistres = append(resultat.Enregistres, fmt.Sprintf(
				"quota dossier personnel (%s Go) → %s",
				formatageCompact(modele.Quota.TailleGo), resultat.OUDN))
		}
	}
	if modele.Dossier != nil && opts.DossierPersoGlobal {
		if _, err := SauvegarderConfigDossierPerso(dossierConfig, *modele.Dossier); err != nil {
			return nil, err
		}
		resultat.Enregistres = append(resultat.Enregistres,
			"configuration globale des dossiers personnels (remplacée)")
	}

	// 4. Artefacts.
	if opts.DossierArtefacts != "" {
		if err := os.MkdirAll(opts.DossierArtefacts, 0o750); err != nil {
			return nil, fmt.Errorf("grouptemplates: création du dossier d'artefacts: %w", err)
		}
		if script != nil {
			chemin, err := deployerScript(*script, opts.DossierArtefacts)
			if err != nil {
				return nil, err
			}
			resultat.Artefacts = append(resultat.Artefacts, chemin)
		}

		var plansEspaces []classspaces.Plan
		if modele.Espace != nil {
			for _, action := range resultat.Groupes {
				plansEspaces = append(plansEspaces, classspaces.Plan{
					GroupDN:   action.DN,
					GroupName: action.Nom,
					Path:      modele.Espace.CheminPour(action.Nom),
				})
			}
			contenu := classspaces.ScriptCreation(plansEspaces, opts.Domaine)
			chemin := filepath.Join(opts.DossierArtefacts, "espaces_classes.ps1")
			if err := ecrireScriptPS(chemin, contenu); err != nil {
				return nil, err
			}
			resultat.Artefacts = append(resultat.Artefacts, chemin)
		}

		if modele.Quota != nil && len(plansEspaces) > 0 {
			var chemins []string
			for _, plan := range plansEspaces {
				chemins = append(chemins, plan.Path)
			}
			contenu := ScriptFSRM(*modele.Quota, chemins)
			chemin := filepath.Join(opts.DossierArtefacts, "quotas_fsr.ps1")
			if err := ecrireScriptPS(chemin, contenu); err != nil {
				return nil, err
			}
			resultat.Artefacts = append(resultat.Artefacts, chemin)
		}

		rapport := resultat.ToDict()
		rapport["heures_connexion"] = rapportHeures(modele)
		if modele.Politique != nil {
			rapport["politique_mdp"] = PolitiqueVersDict(*modele.Politique)
		} else {
			rapport["politique_mdp"] = nil
		}
		rapport["licences_o365"] = listesChaines(modele.Licences)
		if modele.Dossier != nil {
			rapport["dossier_personnel"] = modele.Dossier.ToDict()
		} else {
			rapport["dossier_personnel"] = nil
		}
		rapport["ou_parente"] = parent

		nom := fmt.Sprintf("instantiation-%s.json", nomSûr(modele.ID, valeurs))
		cheminRapport := filepath.Join(opts.DossierArtefacts, nom)
		donnees, err := json.MarshalIndent(rapport, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("grouptemplates: sérialisation du rapport: %w", err)
		}
		if err := os.WriteFile(cheminRapport, donnees, 0o600); err != nil {
			return nil, fmt.Errorf("grouptemplates: écriture du rapport: %w", err)
		}
		resultat.Artefacts = append(resultat.Artefacts, cheminRapport)
		resultat.CheminRapport = cheminRapport
	}

	// 5. Notes d'application différée (champs consignés, non appliqués ici).
	if modele.Politique != nil {
		resultat.Notes = append(resultat.Notes,
			"Politique de mot de passe : appliquée à la génération lors de la "+
				"création des comptes (réglage MDP de la création de comptes).")
	}
	if modele.PrereglageHeure != "" {
		resultat.Notes = append(resultat.Notes, fmt.Sprintf(
			"Heures de connexion « %s » : à appliquer aux comptes depuis l'onglet "+
				"Heures de connexion (M16).", modele.PrereglageHeure))
	}
	if len(modele.Licences) > 0 {
		resultat.Notes = append(resultat.Notes,
			"Licences O365 : à assigner via le module Microsoft 365 (M19) — "+
				"usage_location requis avant assignation.")
	}
	if modele.Dossier != nil && !opts.DossierPersoGlobal {
		resultat.Notes = append(resultat.Notes,
			"Dossier personnel : appliqué à la création des comptes (M17) — "+
				"cocher « config globale » pour remplacer la configuration générale.")
	}
	return resultat, nil
}

// EchapperRDN échappe un nom relatif de DN comme `ldap3.utils.dn.escape_rdn`.
// L'implémentation vit dans le paquet ad (partagée avec le portail M27).
func EchapperRDN(valeur string) string {
	return ad.EchapperRDN(valeur)
}

// deployerScript écrit un script de connexion dans un répertoire (convention
// M15 : `.bat`/`.vbs` sans BOM, `.ps1` avec BOM) et retourne son chemin.
func deployerScript(script ScriptLogon, dossier string) (string, error) {
	chemin := filepath.Join(dossier, script.NomFichierScript())
	if script.Type == "ps1" {
		if err := ecrireScriptPS(chemin, script.Contenu); err != nil {
			return "", err
		}
		return chemin, nil
	}
	if err := os.WriteFile(chemin, []byte(script.Contenu), 0o600); err != nil {
		return "", fmt.Errorf("grouptemplates: écriture du script: %w", err)
	}
	return chemin, nil
}

// ecrireScriptPS écrit un script PowerShell en UTF-8 avec BOM.
func ecrireScriptPS(chemin, contenu string) error {
	donnees := append([]byte{0xEF, 0xBB, 0xBF}, []byte(contenu)...)
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return fmt.Errorf("grouptemplates: écriture du script: %w", err)
	}
	return nil
}

// ScriptFSRM génère le script PowerShell appliquant un quota FSRM à une liste
// de dossiers (miroir de `QuotaManager.powershell_script`).
func ScriptFSRM(quota ParametresQuota, chemins []string) string {
	seuil := "Quota souple — dépassement toléré (journalisé)"
	if quota.LimiteDure {
		seuil = "Hard limit — le dépassement bloque l'écriture"
	}
	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Quotas de disque (M14)",
		fmt.Sprintf(" Quotas : %d dossier(s) — rôle FSRM requis sur ce serveur", len(chemins)),
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"Import-Module FSRM",
		"",
		fmt.Sprintf("$template = '%s'", quota.NomModele()),
		fmt.Sprintf("# %s Go, alerte à %d %% — %s", formatageCompact(quota.TailleGo), quota.SeuilAlerte, seuil),
		"if (-not (Get-FsrmQuotaTemplate -Name $template -ErrorAction SilentlyContinue)) {",
		fmt.Sprintf("    New-FsrmQuotaTemplate -Name $template -Size %d -Threshold @{ Usage = %d; Action = 'EventOnUse' }",
			quota.TailleOctets(), quota.SeuilAlerte),
		"}",
		"",
	}
	limiteSouple := "$true"
	if quota.LimiteDure {
		limiteSouple = "$false"
	}
	for _, chemin := range chemins {
		lignes = append(lignes,
			fmt.Sprintf("$path = '%s'", chemin),
			"if (Test-Path -LiteralPath $path) {",
			"    if (Get-FsrmQuota -Path $path -ErrorAction SilentlyContinue) {",
			fmt.Sprintf("        Set-FsrmQuota -Path $path -Size %d -Template '%s'", quota.TailleOctets(), quota.NomModele()),
			"    } else {",
			fmt.Sprintf("        New-FsrmQuota -Path $path -Size %d -Template '%s' -SoftLimit:%s", quota.TailleOctets(), quota.NomModele(), limiteSouple),
			"    }",
			"} else {",
			`    Write-Warning "Dossier absent, quota non appliqué : $path"`,
			"}",
			"",
		)
	}
	lignes = append(lignes, "Write-Host 'Quotas FSRM appliqués.'")
	return strings.Join(lignes, "\n") + "\n"
}

// -- Heures de connexion (résumé du rapport) --------------------------------

// joursAffichage liste les jours dans l'ordre d'affichage (lundi → dimanche).
var joursAffichage = []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}

// PrereglageHeures décrit un préréglage d'heures de connexion.
type PrereglageHeures struct {
	Nom         string
	Description string
	Plages      []PlageHeures
}

// PlageHeures décrit une plage (jours affichés, heure début, heure fin exclue).
type PlageHeures struct {
	Jours []int
	Debut int
	Fin   int
}

// prereglagesHeures retourne les préréglages intégrés (miroir de
// `LogonHoursPreset.builtin()`).
func prereglagesHeures() []PrereglageHeures {
	semaine := []int{0, 1, 2, 3, 4}
	return []PrereglageHeures{
		{
			Nom:         "Heures cours",
			Description: "Lun–Ven 8h–18h, Sam 8h–12h (rythme scolaire)",
			Plages: []PlageHeures{
				{Jours: semaine, Debut: 8, Fin: 18},
				{Jours: []int{5}, Debut: 8, Fin: 12},
			},
		},
		{
			Nom:         "Heures admin",
			Description: "Lun–Ven 7h–20h, Sam 9h–13h (personnel encadrant)",
			Plages: []PlageHeures{
				{Jours: semaine, Debut: 7, Fin: 20},
				{Jours: []int{5}, Debut: 9, Fin: 13},
			},
		},
		{
			Nom:         "Personnalisé",
			Description: "Conserver la grille éditée manuellement (aucun modèle)",
			Plages:      []PlageHeures{},
		},
	}
}

// nomsPrereglages retourne les noms des préréglages intégrés.
func nomsPrereglages() []string {
	noms := make([]string, 0, 3)
	for _, prereglage := range prereglagesHeures() {
		noms = append(noms, prereglage.Nom)
	}
	return noms
}

// construireGrille construit une grille 7×24 en ordre d'affichage.
func construireGrille(prereglage PrereglageHeures) [7][24]bool {
	var grille [7][24]bool
	for _, plage := range prereglage.Plages {
		for _, jour := range plage.Jours {
			if jour < 0 || jour > 6 {
				continue
			}
			debut := max(plage.Debut, 0)
			fin := min(plage.Fin, 24)
			for heure := debut; heure < fin; heure++ {
				grille[jour][heure] = true
			}
		}
	}
	return grille
}

// resumeGrille produit un résumé compact d'une grille (miroir de `grid_summary`).
func resumeGrille(grille [7][24]bool) string {
	sansRestriction := true
	vide := true
	for jour := 0; jour < 7; jour++ {
		for heure := 0; heure < 24; heure++ {
			if grille[jour][heure] {
				vide = false
			} else {
				sansRestriction = false
			}
		}
	}
	if sansRestriction {
		return "Toutes heures (sans restriction)"
	}
	if vide {
		return "Aucune heure autorisée"
	}

	type groupeJours struct {
		jours   []int
		horaire []int
	}
	var groupes []groupeJours
	for jour := 0; jour < 7; jour++ {
		var horaire []int
		for heure := 0; heure < 24; heure++ {
			if grille[jour][heure] {
				horaire = append(horaire, heure)
			}
		}
		if len(groupes) > 0 {
			dernier := groupes[len(groupes)-1]
			if egalHoraire(dernier.horaire, horaire) && dernier.jours[len(dernier.jours)-1] == jour-1 {
				groupes[len(groupes)-1].jours = append(groupes[len(groupes)-1].jours, jour)
				continue
			}
		}
		groupes = append(groupes, groupeJours{jours: []int{jour}, horaire: horaire})
	}

	var parties []string
	for _, groupe := range groupes {
		libelle := joursAffichage[groupe.jours[0]]
		if len(groupe.jours) > 1 {
			libelle = fmt.Sprintf("%s–%s", joursAffichage[groupe.jours[0]], joursAffichage[groupe.jours[len(groupe.jours)-1]])
		}
		if len(groupe.horaire) == 0 {
			parties = append(parties, libelle+" fermé")
		} else {
			parties = append(parties, libelle+" "+plagesHoraire(groupe.horaire))
		}
	}
	return strings.Join(parties, " ; ")
}

// plagesHoraire formate des heures autorisées en plages « 08:00-12:00 ».
func plagesHoraire(heures []int) string {
	if len(heures) == 0 {
		return "fermé"
	}
	var plages []string
	debut := heures[0]
	precedent := heures[0]
	for _, heure := range heures[1:] {
		if heure == precedent+1 {
			precedent = heure
			continue
		}
		plages = append(plages, fmt.Sprintf("%s-%s", etiquetteHeure(debut), etiquetteHeure(precedent+1)))
		debut = heure
		precedent = heure
	}
	plages = append(plages, fmt.Sprintf("%s-%s", etiquetteHeure(debut), etiquetteHeure(precedent+1)))
	return strings.Join(plages, ", ")
}

func etiquetteHeure(heure int) string {
	return fmt.Sprintf("%02d:00", heure)
}

func egalHoraire(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rapportHeures construit la section « heures_connexion » du rapport.
func rapportHeures(modele ModeleGroupe) map[string]any {
	if modele.PrereglageHeure == "" {
		return nil
	}
	for _, prereglage := range prereglagesHeures() {
		if prereglage.Nom == modele.PrereglageHeure {
			return map[string]any{
				"preset": prereglage.Nom,
				"resume": resumeGrille(construireGrille(prereglage)),
			}
		}
	}
	return nil
}

// nomSûr calcule le nom du rapport d'instanciation.
func nomSûr(idModele string, valeurs map[string]string) string {
	cles := make([]string, 0, len(valeurs))
	for cle := range valeurs {
		cles = append(cles, cle)
	}
	sort.Strings(cles)
	premier := ""
	for _, cle := range cles {
		if strings.TrimSpace(valeurs[cle]) != "" {
			premier = valeurs[cle]
			break
		}
	}
	if premier == "" {
		premier = idModele
	}
	netoye := strings.Trim(reNomSur.ReplaceAllString(premier, "-"), "-.")
	if netoye == "" {
		return idModele
	}
	return netoye
}

func listesChaines(source []string) []any {
	liste := make([]any, 0, len(source))
	for _, item := range source {
		liste = append(liste, item)
	}
	return liste
}
