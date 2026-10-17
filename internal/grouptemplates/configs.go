package grouptemplates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Noms des fichiers de configuration scopée (partagés avec Python : les clés
// et les valeurs restent strictement identiques).
const (
	// FichierConfigsProfils correspond à `PROFILE_CONFIG_FILE` (M13).
	FichierConfigsProfils = "profile_configs.json"
	// FichierConfigsQuotas correspond à `QUOTA_CONFIG_FILE` (M14).
	FichierConfigsQuotas = "quota_configs.json"
	// FichierConfigDossierPerso correspond à `HOME_CONFIG_FILE` (M17).
	FichierConfigDossierPerso = "home_dir_config.json"
)

// ConfigsProfils regroupe les configurations de profil (OU, groupes, défaut).
type ConfigsProfils struct {
	OU     map[string]ConfigProfil
	Groupe map[string]ConfigProfil
	Defaut ConfigProfil
}

// ConfigsQuotas regroupe les configurations de quota (OU, groupes, défaut).
type ConfigsQuotas struct {
	OU     map[string]ParametresQuota
	Groupe map[string]ParametresQuota
	Defaut ParametresQuota
}

// ChargerConfigsProfils charge `profile_configs.json` (fichier absent ou
// corrompu → configuration vide + défaut « local »).
func ChargerConfigsProfils(dossier string) ConfigsProfils {
	configs := ConfigsProfils{
		OU:     map[string]ConfigProfil{},
		Groupe: map[string]ConfigProfil{},
		Defaut: ConfigProfil{TypeProfil: "local"},
	}
	racine, ok := lireObjetJSON(filepath.Join(dossier, FichierConfigsProfils))
	if !ok {
		return configs
	}
	for cle, valeur := range objetDepuisAny(racine["ou_configs"]) {
		configs.OU[cle] = ProfilDepuisDict(valeur)
	}
	for cle, valeur := range objetDepuisAny(racine["group_configs"]) {
		configs.Groupe[cle] = ProfilDepuisDict(valeur)
	}
	if defaut, ok := carteAny(racine["default_config"]); ok {
		configs.Defaut = ProfilDepuisDict(defaut)
	}
	return configs
}

// SauvegarderConfigsProfils écrit `profile_configs.json` et retourne le chemin.
func SauvegarderConfigsProfils(dossier string, configs ConfigsProfils) (string, error) {
	donnees := map[string]any{
		"ou_configs":     profilsVersDicts(configs.OU),
		"group_configs":  profilsVersDicts(configs.Groupe),
		"default_config": configs.Defaut.ToDict(),
	}
	return ecrireJSON(dossier, FichierConfigsProfils, donnees)
}

// ChargerConfigsQuotas charge `quota_configs.json`.
func ChargerConfigsQuotas(dossier string) ConfigsQuotas {
	configs := ConfigsQuotas{
		OU:     map[string]ParametresQuota{},
		Groupe: map[string]ParametresQuota{},
		Defaut: QuotaParDefaut(),
	}
	racine, ok := lireObjetJSON(filepath.Join(dossier, FichierConfigsQuotas))
	if !ok {
		return configs
	}
	for cle, valeur := range objetDepuisAny(racine["ou_configs"]) {
		configs.OU[cle] = QuotaDepuisDict(valeur)
	}
	for cle, valeur := range objetDepuisAny(racine["group_configs"]) {
		configs.Groupe[cle] = QuotaDepuisDict(valeur)
	}
	if defaut, ok := carteAny(racine["default_config"]); ok {
		configs.Defaut = QuotaDepuisDict(defaut)
	}
	return configs
}

// SauvegarderConfigsQuotas écrit `quota_configs.json` et retourne le chemin.
func SauvegarderConfigsQuotas(dossier string, configs ConfigsQuotas) (string, error) {
	donnees := map[string]any{
		"ou_configs":     quotasVersDicts(configs.OU),
		"group_configs":  quotasVersDicts(configs.Groupe),
		"default_config": configs.Defaut.ToDict(),
	}
	return ecrireJSON(dossier, FichierConfigsQuotas, donnees)
}

// ChargerConfigDossierPerso charge `home_dir_config.json`.
func ChargerConfigDossierPerso(dossier string) ConfigDossierPerso {
	racine, ok := lireObjetJSON(filepath.Join(dossier, FichierConfigDossierPerso))
	if !ok {
		return DossierPersoParDefaut()
	}
	return DossierPersoDepuisDict(racine)
}

// SauvegarderConfigDossierPerso écrit `home_dir_config.json`.
func SauvegarderConfigDossierPerso(dossier string, config ConfigDossierPerso) (string, error) {
	return ecrireJSON(dossier, FichierConfigDossierPerso, config.ToDict())
}

// -- Utilitaires ------------------------------------------------------------

func profilsVersDicts(source map[string]ConfigProfil) map[string]any {
	donnees := make(map[string]any, len(source))
	for cle, valeur := range source {
		donnees[cle] = valeur.ToDict()
	}
	return donnees
}

func quotasVersDicts(source map[string]ParametresQuota) map[string]any {
	donnees := make(map[string]any, len(source))
	for cle, valeur := range source {
		donnees[cle] = valeur.ToDict()
	}
	return donnees
}

// objetDepuisAny normalise une valeur JSON en objet dont les valeurs sont des
// dictionnaires typés (`map[string]any`).
func objetDepuisAny(valeur any) map[string]map[string]any {
	resultat := map[string]map[string]any{}
	brut, ok := carteAny(valeur)
	if !ok {
		return resultat
	}
	for cle, item := range brut {
		if carte, ok := carteAny(item); ok {
			resultat[cle] = carte
		}
	}
	return resultat
}

// lireObjetJSON lit un objet JSON ; fichier absent, illisible ou non-objet →
// (nil, false) comme les `except (OSError, ValueError, TypeError)` Python.
func lireObjetJSON(chemin string) (map[string]any, bool) {
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		return nil, false
	}
	var racine any
	if err := json.Unmarshal(contenu, &racine); err != nil {
		return nil, false
	}
	carte, ok := carteAny(racine)
	return carte, ok
}

// ecrireJSON écrit un objet JSON indenté (2 espaces, non-ASCII littéral comme
// `json.dump(..., ensure_ascii=False)`).
func ecrireJSON(dossier, nom string, donnees any) (string, error) {
	chemin := filepath.Join(dossier, nom)
	if err := os.MkdirAll(dossier, 0o750); err != nil {
		return "", fmt.Errorf("grouptemplates: création du dossier: %w", err)
	}
	contenu, err := json.MarshalIndent(donnees, "", "  ")
	if err != nil {
		return "", fmt.Errorf("grouptemplates: sérialisation %s: %w", nom, err)
	}
	if err := os.WriteFile(chemin, contenu, 0o600); err != nil {
		return "", fmt.Errorf("grouptemplates: écriture %s: %w", nom, err)
	}
	return chemin, nil
}
