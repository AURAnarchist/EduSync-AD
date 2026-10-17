package grouptemplates

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fauxAnnuaire conserve en mémoire les OU et groupes créés.
type fauxAnnuaire struct {
	ous             []string
	groupes         []GroupeExistant
	creationsOU     []string
	creationsGroupe []creationGroupe
}

type creationGroupe struct {
	DN     string
	Sam    string
	Portee string
}

func (f *fauxAnnuaire) ListerOUEnfants(_ context.Context, parentDN string) ([]string, error) {
	suffixe := "," + parentDN
	var enfants []string
	for _, ou := range f.ous {
		if strings.HasSuffix(strings.ToLower(ou), strings.ToLower(suffixe)) {
			prefixe := ou[:len(ou)-len(suffixe)]
			if !strings.Contains(prefixe, ",") {
				enfants = append(enfants, ou)
			}
		}
	}
	return enfants, nil
}

func (f *fauxAnnuaire) CreerOU(_ context.Context, parentDN, rdn string) error {
	dn := "ou=" + rdn + "," + parentDN
	f.ous = append(f.ous, dn)
	f.creationsOU = append(f.creationsOU, dn)
	return nil
}

func (f *fauxAnnuaire) ListerGroupes(_ context.Context, conteneur string) ([]GroupeExistant, error) {
	suffixe := "," + conteneur
	var resultat []GroupeExistant
	for _, groupe := range f.groupes {
		if strings.HasSuffix(strings.ToLower(groupe.DN), strings.ToLower(suffixe)) {
			resultat = append(resultat, groupe)
		}
	}
	return resultat, nil
}

func (f *fauxAnnuaire) CreerGroupe(_ context.Context, dn, sam, portee string) error {
	f.groupes = append(f.groupes, GroupeExistant{DN: dn, SAM: sam})
	f.creationsGroupe = append(f.creationsGroupe, creationGroupe{DN: dn, Sam: sam, Portee: portee})
	return nil
}

const (
	domaineTest  = "lycee.local"
	baseDNTest   = "dc=lycee,dc=local"
	elevesDNTest = "ou=Eleves,dc=lycee,dc=local"
)

func chaine(valeur string) *string { return &valeur }

func optionsTest(dossier string) OptionsInstantiation {
	return OptionsInstantiation{
		OUParente:        chaine(elevesDNTest),
		DossierConfig:    dossier,
		DossierArtefacts: "",
	}
}

func TestInstancierFluxComplet(t *testing.T) {
	dossier := t.TempDir()
	annuaire := &fauxAnnuaire{}
	modele := modeleIntegre(t, "classe_eleve")
	opts := optionsTest(dossier)
	opts.DossierArtefacts = filepath.Join(dossier, "artefacts")

	resultat, err := Instancier(context.Background(), annuaire, modele, map[string]string{"classe": "3emeA"}, opts)
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}

	if !resultat.OUCreee {
		t.Fatal("l'OU doit être signalée comme créée")
	}
	attenduOU := "ou=Eleves-3emeA," + elevesDNTest
	if resultat.OUDN != attenduOU {
		t.Fatalf("DN d'OU inattendu : %s", resultat.OUDN)
	}
	if len(resultat.Groupes) != 1 {
		t.Fatalf("1 groupe attendu, obtenu %d", len(resultat.Groupes))
	}
	groupe := resultat.Groupes[0]
	if groupe.Nom != "3emeA" || groupe.DN != "cn=3emeA,"+attenduOU || groupe.Sam != "3emeA" || !groupe.Cree {
		t.Fatalf("groupe inattendu : %+v", groupe)
	}
	if len(annuaire.ous) != 1 || annuaire.ous[0] != attenduOU {
		t.Fatalf("OU non créée dans l'annuaire : %v", annuaire.ous)
	}

	if !contientFragment(resultat.Enregistres, "profil") || !contientFragment(resultat.Enregistres, "quota") {
		t.Fatalf("configs scopées non enregistrées : %v", resultat.Enregistres)
	}
	profils := ChargerConfigsProfils(dossier)
	if _, ok := profils.OU[attenduOU]; !ok {
		t.Fatalf("profil non enregistré pour %s : %v", attenduOU, profils.OU)
	}
	quotas := ChargerConfigsQuotas(dossier)
	quota, ok := quotas.OU[attenduOU]
	if !ok || quota.TailleGo != 5.0 {
		t.Fatalf("quota 5 Go attendu pour %s : %+v", attenduOU, quotas.OU)
	}
}

func TestInstancierPorteesGroupes(t *testing.T) {
	annuaire := &fauxAnnuaire{}
	resultat, err := Instancier(context.Background(), annuaire,
		modeleIntegre(t, "personnel_admin"), map[string]string{}, optionsTest(t.TempDir()))
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	portees := map[string]string{}
	for _, creation := range annuaire.creationsGroupe {
		portees[creation.Sam] = creation.Portee
	}
	if portees["Administrateurs"] != "universal" || portees["Personnel-Admin"] != "global" {
		t.Fatalf("portées écrites inattendues : %v", portees)
	}
	if resultat.OUDN != "ou=Personnel-Admin,"+elevesDNTest {
		t.Fatalf("DN d'OU inattendu : %s", resultat.OUDN)
	}
}

func TestInstancierIdempotence(t *testing.T) {
	annuaire := &fauxAnnuaire{}
	opts := optionsTest(t.TempDir())
	modele := modeleIntegre(t, "classe_eleve")
	valeurs := map[string]string{"classe": "3emeA"}

	premier, err := Instancier(context.Background(), annuaire, modele, valeurs, opts)
	if err != nil {
		t.Fatalf("première instanciation : %v", err)
	}
	second, err := Instancier(context.Background(), annuaire, modele, valeurs, opts)
	if err != nil {
		t.Fatalf("seconde instanciation : %v", err)
	}
	if !premier.OUCreee || second.OUCreee {
		t.Fatal("la seconde instanciation ne doit pas recréer l'OU")
	}
	if !premier.Groupes[0].Cree || second.Groupes[0].Cree {
		t.Fatal("la seconde instanciation ne doit pas recréer le groupe")
	}
	if len(annuaire.ous) != 1 {
		t.Fatalf("une seule OU attendue : %v", annuaire.ous)
	}
	if len(annuaire.creationsGroupe) != 1 {
		t.Fatalf("un seul groupe créé attendu : %v", annuaire.creationsGroupe)
	}
}

func TestInstancierSansOUGroupesDansParent(t *testing.T) {
	annuaire := &fauxAnnuaire{}
	modele := modeleIntegre(t, "personnel_admin").Clone()
	modele.MotifOU = ""

	resultat, err := Instancier(context.Background(), annuaire, modele, map[string]string{}, optionsTest(t.TempDir()))
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	if resultat.OUDN != "" || resultat.OUCreee {
		t.Fatalf("aucune OU dédiée attendue : %+v", resultat)
	}
	for _, groupe := range resultat.Groupes {
		if !strings.HasSuffix(groupe.DN, ","+elevesDNTest) {
			t.Fatalf("groupe hors de l'OU parente : %s", groupe.DN)
		}
	}
}

func TestInstancierDossierPersoGlobal(t *testing.T) {
	dossier := t.TempDir()
	annuaire := &fauxAnnuaire{}
	modele := modeleIntegre(t, "service_technique")

	if _, err := Instancier(context.Background(), annuaire, modele, map[string]string{}, optionsTest(dossier)); err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dossier, FichierConfigDossierPerso)); !os.IsNotExist(err) {
		t.Fatal("la config globale ne doit pas être écrite par défaut")
	}

	opts := optionsTest(dossier)
	opts.DossierPersoGlobal = true
	resultat, err := Instancier(context.Background(), annuaire, modele, map[string]string{}, opts)
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	if !contientFragment(resultat.Enregistres, "globale") {
		t.Fatalf("enregistrement global manquant : %v", resultat.Enregistres)
	}
	if ChargerConfigDossierPerso(dossier).RacinePartage != RacinePartagePersoDefaut {
		t.Fatal("la configuration globale des dossiers personnels n'a pas été écrite")
	}
}

func TestInstancierArtefacts(t *testing.T) {
	dossier := t.TempDir()
	artefacts := filepath.Join(dossier, "arts")
	annuaire := &fauxAnnuaire{}
	opts := optionsTest(dossier)
	opts.DossierArtefacts = artefacts

	resultat, err := Instancier(context.Background(), annuaire,
		modeleIntegre(t, "classe_eleve"), map[string]string{"classe": "3emeA"}, opts)
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}

	entrees, err := os.ReadDir(artefacts)
	if err != nil {
		t.Fatalf("lecture des artefacts : %v", err)
	}
	var noms []string
	for _, entree := range entrees {
		noms = append(noms, entree.Name())
	}
	sort.Strings(noms)
	attendus := []string{"espaces_classes.ps1", "instantiation-3emeA.json", "mapping-lecteur-h.bat", "quotas_fsr.ps1"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("artefacts inattendus : %v", noms)
	}

	brut, err := os.ReadFile(filepath.Join(artefacts, "mapping-lecteur-h.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(brut), "\ufeff") {
		t.Fatal("un .bat ne doit jamais commencer par un BOM")
	}
	if !strings.HasPrefix(string(brut), "@echo off") {
		t.Fatalf("contenu du .bat inattendu : %q", string(brut))
	}

	partage := lireSansBOM(t, filepath.Join(artefacts, "espaces_classes.ps1"))
	if !strings.Contains(partage, "New-SmbShare -Name '3emeA'") {
		t.Fatalf("partage SMB absent :\n%s", partage)
	}
	if !strings.Contains(partage, `\\srv\classes\3emeA`) {
		t.Fatalf("chemin de l'espace absent :\n%s", partage)
	}

	quota := lireSansBOM(t, filepath.Join(artefacts, "quotas_fsr.ps1"))
	if !strings.Contains(quota, "New-FsrmQuotaTemplate") || !strings.Contains(quota, "5368709120") {
		t.Fatalf("script FSRM incomplet :\n%s", quota)
	}

	if resultat.CheminRapport == "" {
		t.Fatal("le chemin du rapport doit être renseigné")
	}
	if _, err := os.Stat(resultat.CheminRapport); err != nil {
		t.Fatalf("rapport absent : %v", err)
	}
}

func TestInstancierRapport(t *testing.T) {
	dossier := t.TempDir()
	artefacts := filepath.Join(dossier, "arts2")
	annuaire := &fauxAnnuaire{}
	modele := modeleIntegre(t, "classe_eleve")
	modele.Licences = []string{"SkuA3-student"}
	opts := optionsTest(dossier)
	opts.DossierArtefacts = artefacts

	_, err := Instancier(context.Background(), annuaire, modele, map[string]string{"classe": "6emeB"}, opts)
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}

	contenu, err := os.ReadFile(filepath.Join(artefacts, "instantiation-6emeB.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rapport map[string]any
	if err := json.Unmarshal(contenu, &rapport); err != nil {
		t.Fatalf("rapport illisible : %v", err)
	}

	valeurs, _ := carteAny(rapport["valeurs"])
	if valeurs["classe"] != "6emeB" {
		t.Fatalf("valeurs inattendues : %v", valeurs)
	}
	if rapport["ou_parente"] != elevesDNTest {
		t.Fatalf("OU parente inattendue : %v", rapport["ou_parente"])
	}
	groupes, _ := rapport["groupes"].([]any)
	premier, _ := carteAny(groupes[0])
	if premier["scope"] != "global" {
		t.Fatalf("portée du groupe inattendue : %v", premier["scope"])
	}
	heures, _ := carteAny(rapport["heures_connexion"])
	if heures["preset"] != "Heures cours" || heures["resume"] == "" {
		t.Fatalf("heures de connexion inattendues : %v", heures)
	}
	politique, _ := carteAny(rapport["politique_mdp"])
	if politique["longueur"] != float64(12) {
		t.Fatalf("politique MDP inattendue : %v", politique)
	}
	licences, _ := rapport["licences_o365"].([]any)
	if len(licences) != 1 || licences[0] != "SkuA3-student" {
		t.Fatalf("licences inattendues : %v", licences)
	}
	dossierPerso, _ := carteAny(rapport["dossier_personnel"])
	if dossierPerso["drive_letter"] != "H:" {
		t.Fatalf("dossier personnel inattendu : %v", dossierPerso)
	}
	if rapport["horodatage"] == "" {
		t.Fatal("horodatage manquant")
	}
}

func TestInstancierSansArtefacts(t *testing.T) {
	resultat, err := Instancier(context.Background(), &fauxAnnuaire{},
		modeleIntegre(t, "classe_eleve"), map[string]string{"classe": "X"}, optionsTest(t.TempDir()))
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	if len(resultat.Artefacts) != 0 || resultat.CheminRapport != "" {
		t.Fatalf("aucun artefact attendu : %+v", resultat)
	}
}

func TestInstancierResume(t *testing.T) {
	resultat, err := Instancier(context.Background(), &fauxAnnuaire{},
		modeleIntegre(t, "classe_eleve"), map[string]string{"classe": "3emeA"}, optionsTest(t.TempDir()))
	if err != nil {
		t.Fatalf("instanciation : %v", err)
	}
	resume := resultat.Resume()
	for _, fragment := range []string{"Classe élève", "ou=Eleves-3emeA", "créée", "créé(s)"} {
		if !strings.Contains(resume, fragment) {
			t.Fatalf("fragment %q absent du résumé :\n%s", fragment, resume)
		}
	}
}

func TestInstancierEchecsAvantEcriture(t *testing.T) {
	annuaire := &fauxAnnuaire{}

	// Valeurs manquantes.
	_, err := Instancier(context.Background(), annuaire,
		modeleIntegre(t, "classe_eleve"), map[string]string{}, optionsTest(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "classe") {
		t.Fatalf("valeur manquante attendue en erreur : %v", err)
	}
	if len(annuaire.ous) != 0 || len(annuaire.groupes) != 0 {
		t.Fatal("aucune écriture AD ne doit précéder la validation")
	}

	// Script invalide.
	modele := modeleIntegre(t, "classe_eleve").Clone()
	script := *modele.ScriptLogon
	script.Contenu = ""
	modele.ScriptLogon = &script
	_, err = Instancier(context.Background(), annuaire, modele,
		map[string]string{"classe": "X"}, optionsTest(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "contenu du script") {
		t.Fatalf("script invalide attendu en erreur : %v", err)
	}
	if len(annuaire.ous) != 0 {
		t.Fatal("aucune écriture AD ne doit précéder la validation du script")
	}

	// OU parente absente.
	opts := optionsTest(t.TempDir())
	opts.OUParente = chaine("")
	_, err = Instancier(context.Background(), annuaire,
		modeleIntegre(t, "classe_eleve"), map[string]string{"classe": "X"}, opts)
	if err == nil || !strings.Contains(err.Error(), "parente") {
		t.Fatalf("OU parente manquante attendue en erreur : %v", err)
	}
}

func TestResultatToDictSerialisable(t *testing.T) {
	resultat := &ResultatInstantiation{IDModele: "x", NomModele: "X"}
	donnees := resultat.ToDict()
	if donnees["id"] != "x" {
		t.Fatalf("id inattendu : %v", donnees["id"])
	}
	if groupes, _ := donnees["groupes"].([]any); len(groupes) != 0 {
		t.Fatalf("liste de groupes vide attendue : %v", groupes)
	}
	if _, err := json.Marshal(donnees); err != nil {
		t.Fatalf("résultat non sérialisable : %v", err)
	}
	if !strings.Contains(resultat.Resume(), "Modèle « X » instancié.") {
		t.Fatalf("résumé inattendu : %s", resultat.Resume())
	}
}

// lireSansBOM lit un fichier en retirant l'éventuel BOM UTF-8.
func lireSansBOM(t *testing.T, chemin string) string {
	t.Helper()
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(string(contenu), "\ufeff")
}
