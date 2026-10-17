// Package rds génère la configuration RDS / Bureau à distance (M21).
//
// Le déploiement RDS se pilote en Remote PowerShell depuis une machine Windows
// disposant du module « RemoteDesktop » : ce paquet génère les scripts
// correspondants (collections de sessions, UPD, RemoteApp, FSLogix). Aucun
// accès direct depuis EduSync-AD n'est possible.
//
// La logique et le format des fichiers (rds.json, rds_plans.json) reprennent
// src/edusync_ad/core/rds.py pour rester compatibles avec la version Python.
package rds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
)

// FichierConfig est le nom du fichier de configuration RDS.
const FichierConfig = "rds.json"

// FichierPlans est le nom du fichier des plans RDS.
const FichierPlans = "rds_plans.json"

// ModesProfil est le libellé affichable de chaque mode de profil.
var ModesProfil = map[string]string{
	"none":    "Aucun",
	"upd":     "UPD — disque de profil par collection",
	"fslogix": "FSLogix — conteneur de profil",
}

// Config décrit les paramètres du déploiement RDS.
type Config struct {
	Broker           string   `json:"broker"`
	Domain           string   `json:"domain"`
	ProfileMode      string   `json:"profile_mode"`
	UPDPath          string   `json:"upd_path"`
	UPDSizeGB        int      `json:"upd_size_gb"`
	UPDIncludePaths  []string `json:"upd_include_paths"`
	UPDExcludePaths  []string `json:"upd_exclude_paths"`
	FSLogixLocations []string `json:"fslogix_locations"`
	FSLogixSizeMB    int      `json:"fslogix_size_mb"`
	FSLogixFlipFlop  bool     `json:"fslogix_flipflop"`
}

// ConfigParDefaut retourne une configuration RDS avec les valeurs par défaut.
func ConfigParDefaut() Config {
	return Config{
		ProfileMode:      "none",
		UPDPath:          `\\sr01\UPD$`,
		UPDSizeGB:        30,
		FSLogixSizeMB:    30000,
		FSLogixFlipFlop:  true,
		UPDIncludePaths:  []string{},
		UPDExcludePaths:  []string{},
		FSLogixLocations: []string{},
	}
}

// Validate vérifie la cohérence de la configuration.
func (c Config) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(c.Broker) == "" {
		erreurs = append(erreurs, "Le Connection Broker est vide.")
	} else if strings.Contains(c.Broker, " ") {
		erreurs = append(erreurs, "Le Connection Broker doit être un FQDN (sans espace).")
	}
	if _, ok := ModesProfil[c.ProfileMode]; !ok {
		erreurs = append(erreurs, "Mode de profil inconnu (none / upd / fslogix).")
	}
	if c.ProfileMode == "upd" {
		if strings.TrimSpace(c.UPDPath) == "" {
			erreurs = append(erreurs, "Chemin UPD vide.")
		} else if !strings.HasPrefix(c.UPDPath, `\\`) {
			erreurs = append(erreurs, `Le chemin UPD doit être un partage UNC (\\serveur\partage).`)
		}
		if c.UPDSizeGB <= 0 {
			erreurs = append(erreurs, "La taille UPD doit être supérieure à 0 Go.")
		}
	}
	if c.ProfileMode == "fslogix" {
		if len(c.FSLogixLocations) == 0 {
			erreurs = append(erreurs, "Aucune localisation VHD FSLogix (VHDLocations).")
		}
		for _, loc := range c.FSLogixLocations {
			if !strings.HasPrefix(loc, `\\`) {
				erreurs = append(erreurs, "Localisation FSLogix non-UNC : "+loc)
			}
		}
		if c.FSLogixSizeMB <= 0 {
			erreurs = append(erreurs, "La taille de profil FSLogix doit être supérieure à 0 Mo.")
		}
	}
	return erreurs
}

// DossierConfig retourne le dossier de configuration partagé avec Python.
func DossierConfig() (string, error) {
	return config.ConfigDir()
}

// ChargerConfig lit la configuration RDS (défaut si absente ou illisible).
func ChargerConfig(chemin string) Config {
	cfg := ConfigParDefaut()
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return cfg
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return cfg
	}
	// Décodage tolérant : les champs absents gardent la valeur par défaut.
	if err := json.Unmarshal(donnees, &cfg); err != nil {
		return ConfigParDefaut()
	}
	return cfg
}

// SauvegarderConfig écrit la configuration RDS (JSON indenté UTF-8).
func SauvegarderConfig(chemin string, cfg Config) error {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return err
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return fmt.Errorf("rds: création du dossier de configuration: %w", err)
	}
	return ecrireJSON(chemin, cfg)
}

// ecrireJSON sérialise en JSON indenté (2 espaces, sans échappement HTML).
func ecrireJSON(chemin string, valeur any) error {
	f, err := os.Create(chemin)
	if err != nil {
		return fmt.Errorf("rds: écriture %s: %w", chemin, err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(valeur); err != nil {
		return fmt.Errorf("rds: encodage JSON: %w", err)
	}
	return nil
}

// ─── Utilitaires ──────────────────────────────────────────────────────────

var separateurMulti = regexp.MustCompile(`[;,]`)

// SplitMulti découpe « a ; b, c » en liste nettoyée et dédoublonnée.
func SplitMulti(texte string) []string {
	parties := []string{}
	for _, brut := range separateurMulti.Split(texte, -1) {
		item := strings.TrimSpace(brut)
		if item != "" && !contient(parties, item) {
			parties = append(parties, item)
		}
	}
	return parties
}

// NormaliserRefGroupe retourne le format « DOMAINE\Groupe » attendu par RDS.
func NormaliserRefGroupe(groupe, domaine string) string {
	groupe = strings.TrimSpace(groupe)
	if groupe == "" {
		return ""
	}
	if strings.Contains(groupe, `\`) {
		return groupe
	}
	domaine = strings.TrimRight(strings.TrimSpace(domaine), `\`)
	if domaine == "" {
		return groupe
	}
	return domaine + `\` + groupe
}

var aliasInvalide = regexp.MustCompile(`[^a-z0-9._-]+`)
var aliasTirets = regexp.MustCompile(`-{2,}`)

// CreerAlias construit un alias RemoteApp : minuscules, [a-z0-9._-].
func CreerAlias(texte string) string {
	alias := aliasInvalide.ReplaceAllString(strings.ToLower(strings.TrimSpace(texte)), "-")
	alias = aliasTirets.ReplaceAllString(alias, "-")
	return strings.Trim(alias, "-._")
}

// contient indique si un slice contient une valeur.
func contient(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if v == cible {
			return true
		}
	}
	return false
}

// ─── Plans ────────────────────────────────────────────────────────────────

// PlanCollection décrit une collection de sessions RDS à créer/configurer.
type PlanCollection struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SessionHosts []string `json:"session_hosts"`
	UserGroups   []string `json:"user_groups"`
}

// UnmarshalJSON applique les valeurs par défaut des champs absents, comme le
// fait Python (listes vides plutôt que nil).
func (p *PlanCollection) UnmarshalJSON(donnees []byte) error {
	type planAlias PlanCollection
	defaut := planAlias{SessionHosts: []string{}, UserGroups: []string{}}
	if err := json.Unmarshal(donnees, &defaut); err != nil {
		return err
	}
	if defaut.SessionHosts == nil {
		defaut.SessionHosts = []string{}
	}
	if defaut.UserGroups == nil {
		defaut.UserGroups = []string{}
	}
	*p = PlanCollection(defaut)
	return nil
}

// Label retourne un libellé lisible du plan de collection.
func (p PlanCollection) Label() string {
	groupes := ""
	if len(p.UserGroups) > 0 {
		groupes = fmt.Sprintf(", %d groupe(s)", len(p.UserGroups))
	}
	return fmt.Sprintf("%s — %d hôte(s)%s", p.Name, len(p.SessionHosts), groupes)
}

// PlanRemoteApp décrit une application RemoteApp à publier.
type PlanRemoteApp struct {
	Alias           string   `json:"alias"`
	DisplayName     string   `json:"display_name"`
	FilePath        string   `json:"file_path"`
	Collection      string   `json:"collection"`
	UserGroups      []string `json:"user_groups"`
	ShowInWeb       bool     `json:"show_in_web"`
	FolderName      string   `json:"folder_name"`
	IconIndex       *int     `json:"icon_index"`
	RequiredCommand string   `json:"required_command_line"`
}

// UnmarshalJSON applique les valeurs par défaut de Python : `show_in_web`
// vaut true et les listes sont vides lorsque les clés sont absentes.
func (p *PlanRemoteApp) UnmarshalJSON(donnees []byte) error {
	type planAlias PlanRemoteApp
	defaut := planAlias{ShowInWeb: true, UserGroups: []string{}}
	if err := json.Unmarshal(donnees, &defaut); err != nil {
		return err
	}
	if defaut.UserGroups == nil {
		defaut.UserGroups = []string{}
	}
	*p = PlanRemoteApp(defaut)
	return nil
}

// Label retourne un libellé lisible du plan RemoteApp.
func (p PlanRemoteApp) Label() string {
	groupes := ""
	if len(p.UserGroups) > 0 {
		groupes = fmt.Sprintf(" → %d groupe(s)", len(p.UserGroups))
	}
	return fmt.Sprintf("%s (%s) → %s%s", p.Alias, p.DisplayName, p.Collection, groupes)
}

// ConstruirePlanCollection agrège hôtes et groupes (dédoublonnés, normalisés).
func ConstruirePlanCollection(name, description string, sessionHosts, userGroups []string, cfg Config) PlanCollection {
	var hotes []string
	for _, entree := range sessionHosts {
		for _, hote := range SplitMulti(entree) {
			hote = strings.TrimRight(strings.TrimSpace(hote), ".")
			if hote != "" && !contient(hotes, hote) {
				hotes = append(hotes, hote)
			}
		}
	}
	var groupes []string
	for _, entree := range userGroups {
		for _, groupe := range SplitMulti(entree) {
			normalise := NormaliserRefGroupe(groupe, cfg.Domain)
			if normalise != "" && !contient(groupes, normalise) {
				groupes = append(groupes, normalise)
			}
		}
	}
	if hotes == nil {
		hotes = []string{}
	}
	if groupes == nil {
		groupes = []string{}
	}
	return PlanCollection{
		Name:         strings.TrimSpace(name),
		Description:  strings.TrimSpace(description),
		SessionHosts: hotes,
		UserGroups:   groupes,
	}
}

// ValiderCollection vérifie un plan de collection.
func ValiderCollection(plan PlanCollection, cfg Config) []string {
	var erreurs []string
	if strings.TrimSpace(plan.Name) == "" {
		erreurs = append(erreurs, "Nom de collection vide.")
	}
	if len(plan.SessionHosts) == 0 {
		nom := plan.Name
		if nom == "" {
			nom = "?"
		}
		erreurs = append(erreurs, fmt.Sprintf("Collection « %s » : au moins un hébergeur requis.", nom))
	}
	if strings.TrimSpace(cfg.Broker) == "" {
		erreurs = append(erreurs, "Connection Broker vide.")
	}
	return erreurs
}

var aliasValide = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValiderRemoteApp vérifie un plan RemoteApp.
func ValiderRemoteApp(plan PlanRemoteApp) []string {
	var erreurs []string
	if strings.TrimSpace(plan.Alias) == "" {
		erreurs = append(erreurs, "Alias RemoteApp vide.")
	} else if !aliasValide.MatchString(plan.Alias) {
		erreurs = append(erreurs, fmt.Sprintf("Alias « %s » invalide (lettres, chiffres, . _ - seulement).", plan.Alias))
	}
	if strings.TrimSpace(plan.DisplayName) == "" {
		erreurs = append(erreurs, "Nom d'affichage vide.")
	}
	if strings.TrimSpace(plan.FilePath) == "" {
		erreurs = append(erreurs, "Chemin de l'exécutable vide.")
	}
	if strings.TrimSpace(plan.Collection) == "" {
		alias := plan.Alias
		if alias == "" {
			alias = "?"
		}
		erreurs = append(erreurs, fmt.Sprintf("RemoteApp « %s » : collection non précisée.", alias))
	}
	return erreurs
}

// plansFichier contient la structure du fichier rds_plans.json.
type plansFichier struct {
	Collections []PlanCollection `json:"collections"`
	RemoteApps  []PlanRemoteApp  `json:"remoteapps"`
}

// ChargerPlans lit les plans RDS (listes vides si absent ou illisible).
func ChargerPlans(chemin string) ([]PlanCollection, []PlanRemoteApp) {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return nil, nil
		}
		chemin = filepath.Join(dir, FichierPlans)
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, nil
	}
	var contenu plansFichier
	if err := json.Unmarshal(donnees, &contenu); err != nil {
		return nil, nil
	}
	return contenu.Collections, contenu.RemoteApps
}

// EnregistrerPlans écrit les plans RDS (JSON indenté UTF-8).
func EnregistrerPlans(chemin string, collections []PlanCollection, remoteapps []PlanRemoteApp) error {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return err
		}
		chemin = filepath.Join(dir, FichierPlans)
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return fmt.Errorf("rds: création du dossier des plans: %w", err)
	}
	if collections == nil {
		collections = []PlanCollection{}
	}
	if remoteapps == nil {
		remoteapps = []PlanRemoteApp{}
	}
	return ecrireJSON(chemin, plansFichier{Collections: collections, RemoteApps: remoteapps})
}

// ─── Scripts PowerShell ───────────────────────────────────────────────────

// echapperPS échappe une valeur pour une chaîne PowerShell entre apostrophes.
func echapperPS(valeur string) string {
	return strings.ReplaceAll(valeur, "'", "''")
}

// tableauPS construit un tableau PowerShell @('a', 'b').
func tableauPS(valeurs []string) string {
	items := make([]string, 0, len(valeurs))
	for _, v := range valeurs {
		items = append(items, "'"+echapperPS(v)+"'")
	}
	return "@(" + strings.Join(items, ", ") + ")"
}

// ScriptCollections génère le script de création/configuration des collections
// (et de l'UPD si le mode de profil est « upd »).
func ScriptCollections(plans []PlanCollection, cfg Config) (string, error) {
	if erreurs := cfg.Validate(); len(erreurs) > 0 {
		return "", errors.New(strings.Join(erreurs, "; "))
	}
	if len(plans) == 0 {
		return "", errors.New("Aucune collection à générer.")
	}
	for _, plan := range plans {
		if bad := ValiderCollection(plan, cfg); len(bad) > 0 {
			return "", errors.New(strings.Join(bad, "; "))
		}
	}

	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Collections RDS (M21)",
		" Broker : " + cfg.Broker,
		fmt.Sprintf(" Collections : %d — profils : %s", len(plans), ModesProfil[cfg.ProfileMode]),
		" Module requis : RemoteDesktop (tools de gestion RDS / RSAT-RDS).",
		" S'exécute sur un serveur Windows disposant du module (broker ou admin).",
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"$broker = '" + echapperPS(cfg.Broker) + "'",
		"",
	}

	for _, plan := range plans {
		lignes = append(lignes,
			"# --- collection : "+plan.Name+" ---",
			"$collectionName = '"+echapperPS(plan.Name)+"'",
			"$existing = Get-RDSessionCollection -ConnectionBroker $broker -ErrorAction SilentlyContinue | Where-Object { $_.CollectionName -eq $collectionName }",
			"if (-not $existing) {",
			"    New-RDSessionCollection -CollectionName $collectionName"+
				siNonVide(" -CollectionDescription '"+echapperPS(plan.Description)+"'", plan.Description != "")+
				" -SessionHost "+tableauPS(plan.SessionHosts)+" -ConnectionBroker $broker",
			"} else {",
			"    # collection existante : ajout des hébergeurs manquants",
			"    $hosts = @(Get-RDSessionHost -CollectionName $collectionName -ConnectionBroker $broker -ErrorAction SilentlyContinue | ForEach-Object { $_.SessionHost })",
			"    foreach ($h in "+tableauPS(plan.SessionHosts)+") {",
			"        if ($hosts -notcontains $h) {",
			"            New-RDSessionHost -CollectionName $collectionName -SessionHost $h -ConnectionBroker $broker",
			"        }",
			"    }",
			"}",
		)
		if len(plan.UserGroups) > 0 {
			lignes = append(lignes,
				"# groupes autorisés (la liste REMPLACE 'Domain Users' par défaut :",
				"# inclure 'DOMAINE\\Domain Users' pour le conserver)",
				"Set-RDSessionCollectionConfiguration -CollectionName $collectionName -UserGroup "+tableauPS(plan.UserGroups)+" -ConnectionBroker $broker",
			)
		}
		if cfg.ProfileMode == "upd" {
			lignes = append(lignes, "# disque de profil (UPD) de la collection")
			upd := []string{
				"Set-RDSessionCollectionConfiguration -CollectionName $collectionName `",
				"    -EnableUserProfileDisk `",
				fmt.Sprintf("    -MaxUserProfileDiskSizeGB %d `", cfg.UPDSizeGB),
				"    -DiskPath '" + echapperPS(cfg.UPDPath) + "' `",
			}
			if len(cfg.UPDIncludePaths) > 0 {
				upd = append(upd, "    -IncludeFolderPath "+tableauPS(cfg.UPDIncludePaths)+" `")
			}
			if len(cfg.UPDExcludePaths) > 0 {
				upd = append(upd, "    -ExcludeFolderPath "+tableauPS(cfg.UPDExcludePaths)+" `")
			}
			upd = append(upd, "    -ConnectionBroker $broker")
			lignes = append(lignes, upd...)
		}
		lignes = append(lignes, "")
	}

	lignes = append(lignes, "Write-Host 'Collections RDS configurées.'")
	return strings.Join(lignes, "\n") + "\n", nil
}

// siNonVide retourne suffixe si condition est vraie, sinon "".
func siNonVide(suffixe string, condition bool) string {
	if condition {
		return suffixe
	}
	return ""
}

// ScriptRemoteApp génère le script de publication des RemoteApp.
func ScriptRemoteApp(plans []PlanRemoteApp, cfg Config) (string, error) {
	if strings.TrimSpace(cfg.Broker) == "" {
		return "", errors.New("Connection Broker vide.")
	}
	if len(plans) == 0 {
		return "", errors.New("Aucun RemoteApp à générer.")
	}
	for _, plan := range plans {
		if bad := ValiderRemoteApp(plan); len(bad) > 0 {
			return "", errors.New(strings.Join(bad, "; "))
		}
	}

	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Applications RemoteApp (M21)",
		fmt.Sprintf(" Broker : %s — applications : %d", cfg.Broker, len(plans)),
		" -UserGroups limite l'affichage de l'app dans RD Web Access aux groupes listés.",
		" App existante → mise à jour (Set-RDRemoteApp), sinon création.",
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"$broker = '" + echapperPS(cfg.Broker) + "'",
		"",
	}

	for _, plan := range plans {
		suffixe := ""
		if plan.FolderName != "" {
			suffixe += " -FolderName '" + echapperPS(plan.FolderName) + "' "
		}
		if plan.IconIndex != nil {
			suffixe += fmt.Sprintf(" -IconIndex %d ", *plan.IconIndex)
		}
		if len(plan.UserGroups) > 0 {
			suffixe += " -UserGroups " + tableauPS(plan.UserGroups) + " "
		}
		showInWeb := "$false"
		if plan.ShowInWeb {
			showInWeb = "$true"
		}

		appLigne := "    New-RDRemoteApp -CollectionName $collectionName -Alias $alias " +
			"-DisplayName '" + echapperPS(plan.DisplayName) + "' -FilePath '" + echapperPS(plan.FilePath) + "' " +
			"-ShowInWebAccess " + showInWeb + " " +
			suffixe +
			siNonVide("-CommandLineSetting AllowOnlySpecifiedCommandLine -RequiredCommandLine '"+echapperPS(plan.RequiredCommand)+"' ", plan.RequiredCommand != "") +
			"-ConnectionBroker $broker"
		setLigne := "    Set-RDRemoteApp -CollectionName $collectionName -Alias $alias " +
			"-DisplayName '" + echapperPS(plan.DisplayName) + "' -FilePath '" + echapperPS(plan.FilePath) + "' " +
			"-ShowInWebAccess " + showInWeb + " " +
			suffixe +
			"-ConnectionBroker $broker"

		lignes = append(lignes,
			"# --- "+plan.Alias+" ("+plan.DisplayName+") ---",
			"$collectionName = '"+echapperPS(plan.Collection)+"'",
			"$alias = '"+echapperPS(plan.Alias)+"'",
			"$existing = Get-RDRemoteApp -CollectionName $collectionName -ConnectionBroker $broker -ErrorAction SilentlyContinue | Where-Object { $_.Alias -eq $alias }",
			"if (-not $existing) {",
			appLigne,
			"} else {",
			setLigne,
			"}",
			"",
		)
	}

	lignes = append(lignes, "Write-Host 'RemoteApp publiés.'")
	return strings.Join(lignes, "\n") + "\n", nil
}

// ScriptFSLogix génère le script de configuration FSLogix (registre).
func ScriptFSLogix(cfg Config) (string, error) {
	if cfg.ProfileMode != "fslogix" {
		return "", errors.New("Le mode de profil sélectionné n'est pas FSLogix.")
	}
	if len(cfg.FSLogixLocations) == 0 {
		return "", errors.New("Aucune localisation VHD FSLogix.")
	}
	for _, loc := range cfg.FSLogixLocations {
		if !strings.HasPrefix(loc, `\\`) {
			return "", errors.New("Localisation FSLogix non-UNC : " + loc)
		}
	}
	if cfg.FSLogixSizeMB <= 0 {
		return "", errors.New("La taille de profil FSLogix doit être supérieure à 0 Mo.")
	}

	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Profils FSLogix (M21)",
		fmt.Sprintf(" Conteneurs : %d partage(s), taille %d Mo", len(cfg.FSLogixLocations), cfg.FSLogixSizeMB),
		" À exécuter sur CHAQUE RD Session Host (agent FSLogix installé,",
		" redémarrage requis après application).",
		"#>",
		"$ErrorActionPreference = 'Stop'",
		`$path = 'HKLM:\SOFTWARE\FSLogix\Profiles'`,
		"if (-not (Test-Path $path)) { New-Item -Path $path -Force | Out-Null }",
		"Set-ItemProperty -Path $path -Name 'Enabled' -Value 1 -Type DWord",
		"Set-ItemProperty -Path $path -Name 'VHDLocations' -Value " + tableauPS(cfg.FSLogixLocations) + " -Type MultiString",
		fmt.Sprintf("Set-ItemProperty -Path $path -Name 'SizeInMB' -Value %d -Type DWord", cfg.FSLogixSizeMB),
		"Set-ItemProperty -Path $path -Name 'DeleteLocalProfileWhenVHDShouldApply' -Value 1 -Type DWord",
	}
	if cfg.FSLogixFlipFlop {
		lignes = append(lignes,
			"Set-ItemProperty -Path $path -Name 'FlipFlopProfileDir' -Value 1 -Type DWord")
	}
	lignes = append(lignes,
		"Write-Host 'FSLogix configuré (redémarrage des RD Session Host requis).'")
	return strings.Join(lignes, "\n") + "\n", nil
}

// SauvegarderScript écrit un script PowerShell (UTF-8 avec BOM, requis par
// Windows PowerShell 5.1).
func SauvegarderScript(dest, contenu string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("rds: création du dossier du script: %w", err)
	}
	donnees := append([]byte{0xEF, 0xBB, 0xBF}, []byte(contenu)...)
	if err := os.WriteFile(dest, donnees, 0o644); err != nil {
		return fmt.Errorf("rds: écriture du script %s: %w", dest, err)
	}
	return nil
}
