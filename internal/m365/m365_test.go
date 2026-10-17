package m365

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/graph"
)

const domaineTest = "lycee.local"

// fauxClient est un faux client Graph : enregistre les appels et simule l'état.
type fauxClient struct {
	users           []map[string]any
	groups          []map[string]any
	skus            map[string]string
	failCreateUser  bool
	failCreateGroup bool
	existingMembers map[string]bool
	seq             int

	creationsUtilisateur []graph.ParamsCreationUtilisateur
	licencesAttribuees   [][2]string
	creationsGroupe      []graph.ParamsCreationGroupe
	membresAjoutes       [][2]string
	equipes              []string
	photos               []struct {
		userID string
		jpeg   []byte
	}
}

func nouveauFauxClient() *fauxClient {
	return &fauxClient{
		skus:            map[string]string{"M365EDU_A1": "sku-a1", "M365EDU_A3_FACULTY": "sku-a3"},
		existingMembers: map[string]bool{},
	}
}

func (f *fauxClient) Organization() (map[string]any, error) {
	return map[string]any{"displayName": "Lycée de Test"}, nil
}

func (f *fauxClient) ListUsers() ([]map[string]any, error)  { return f.users, nil }
func (f *fauxClient) ListGroups() ([]map[string]any, error) { return f.groups, nil }

func (f *fauxClient) SubscribedSkus() (map[string]string, error) { return f.skus, nil }

func (f *fauxClient) CreateUser(params graph.ParamsCreationUtilisateur) (map[string]any, error) {
	if f.failCreateUser {
		return nil, &graph.GraphError{Message: "UPN déjà utilisé", Status: 409, Code: "Request_BadRequest"}
	}
	f.creationsUtilisateur = append(f.creationsUtilisateur, params)
	f.seq++
	utilisateur := map[string]any{"id": "u-" + itoa(f.seq), "userPrincipalName": params.UserPrincipalName}
	f.users = append(f.users, utilisateur)
	return utilisateur, nil
}

func (f *fauxClient) AssignLicense(userID, skuID string) (map[string]any, error) {
	f.licencesAttribuees = append(f.licencesAttribuees, [2]string{userID, skuID})
	return map[string]any{"id": userID}, nil
}

func (f *fauxClient) CreateGroup(params graph.ParamsCreationGroupe) (map[string]any, error) {
	if f.failCreateGroup {
		return nil, &graph.GraphError{Message: "mailNickname déjà utilisé", Status: 400, Code: "Request_BadRequest"}
	}
	f.creationsGroupe = append(f.creationsGroupe, params)
	f.seq++
	groupe := map[string]any{"id": "g-" + itoa(f.seq), "displayName": params.DisplayName, "mailNickname": params.MailNickname}
	f.groups = append(f.groups, groupe)
	return groupe, nil
}

func (f *fauxClient) AddGroupMember(groupID, memberID string) error {
	cle := groupID + "|" + memberID
	if f.existingMembers[cle] {
		return &graph.GraphError{Message: "one or more added objects already exist", Status: 400}
	}
	f.existingMembers[cle] = true
	f.membresAjoutes = append(f.membresAjoutes, [2]string{groupID, memberID})
	return nil
}

func (f *fauxClient) CreateTeam(groupID string) error {
	f.equipes = append(f.equipes, groupID)
	return nil
}

func (f *fauxClient) UploadUserPhoto(userID string, jpeg []byte) error {
	f.photos = append(f.photos, struct {
		userID string
		jpeg   []byte
	}{userID, jpeg})
	return nil
}

// itoa convertit un entier en chaîne (évite strconv pour lisibilité du test).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var chiffres []byte
	for n > 0 {
		chiffres = append([]byte{byte('0' + n%10)}, chiffres...)
		n /= 10
	}
	return string(chiffres)
}

func managerTest(client ClientGraph) *Manager {
	return NewManager(graph.Config{
		TenantID:      "11111111-2222-3333-4444-555555555555",
		ClientID:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		ClientSecret:  "x",
		UsageLocation: "FR",
	}, client)
}

// imagePNG écrit une petite image PNG dans un dossier et retourne son chemin.
func imagePNG(t *testing.T, dossier, nom string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	chemin := filepath.Join(dossier, nom)
	fichier, err := os.Create(chemin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fichier.Close() }()
	if err := png.Encode(fichier, img); err != nil {
		t.Fatal(err)
	}
	return chemin
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func TestMailNickname(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"6ème A", "6emea"},
		{"Terminale S – Projet", "terminalesprojet"},
		{"classe #1 & 2", "classe12"},
		{"???", "groupe"},
	}
	for _, c := range cas {
		if got := MailNickname(c.entree); got != c.attendu {
			t.Errorf("MailNickname(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
	if MailNickname(MailNickname("6ème A")) != MailNickname("6ème A") {
		t.Error("MailNickname n'est pas idempotent")
	}
}

func TestSplitDisplayName(t *testing.T) {
	if p, n := SplitDisplayName("Jean Dupont", ""); p != "Jean" || n != "Dupont" {
		t.Errorf("découpage = %q / %q", p, n)
	}
	if p, n := SplitDisplayName("Camille De La Croix", ""); p != "Camille" || n != "De La Croix" {
		t.Errorf("découpage = %q / %q", p, n)
	}
	if p, n := SplitDisplayName("Paul Martin (pmartin)", "pmartin"); p != "Paul" || n != "Martin" {
		t.Errorf("suffixe doublon = %q / %q", p, n)
	}
	if p, n := SplitDisplayName("Chercheur", ""); p != "Chercheur" || n != "" {
		t.Errorf("mono = %q / %q", p, n)
	}
}

func TestPolitiqueMotDePasseEtCatalogue(t *testing.T) {
	policy := PolitiqueMotDePasseCloud()
	if policy.Length < 16 || !policy.RequireUppercase || !policy.RequireDigits || !policy.RequireSpecial {
		t.Errorf("politique faible : %+v", policy)
	}
	if _, ok := LicencesEducation[LicenceParDefaut]; !ok {
		t.Error("licence par défaut absente du catalogue")
	}
	if !strings.Contains(LicencesEducation["M365EDU_A1"], "A1") {
		t.Error("libellé A1 inattendu")
	}
	if len(TypesGroupe) != 3 {
		t.Errorf("types de groupe = %d", len(TypesGroupe))
	}
	for _, kind := range []string{"m365", "distribution", "security"} {
		if _, ok := TypesGroupe[kind]; !ok {
			t.Errorf("type %q manquant", kind)
		}
	}
}

// ─── Plans ────────────────────────────────────────────────────────────────

func comptesTest() []CompteAD {
	return []CompteAD{
		{SAM: "jdurand", CN: "Jean Durand"},
		{SAM: "amartin", CN: "Alice Martin (amartin)", Disabled: true},
		{SAM: "", CN: "Incomplet"},
	}
}

func TestPlansUtilisateurs(t *testing.T) {
	plans := ConstruirePlansUtilisateurs(comptesTest(), domaineTest, nil)
	if len(plans) != 2 {
		t.Fatalf("plans = %d (ligne sans sam ignorée)", len(plans))
	}
	if plans[0].UPN != "jdurand@lycee.local" || plans[0].Prenom != "Jean" || plans[0].Nom != "Durand" {
		t.Errorf("plan 0 = %+v", plans[0])
	}
	if plans[1].DisplayName != "Alice Martin (amartin)" || !plans[1].Disabled {
		t.Errorf("plan 1 = %+v", plans[1])
	}
}

func TestPlansUtilisateursExistants(t *testing.T) {
	plans := ConstruirePlansUtilisateurs(comptesTest(), domaineTest, []string{"JDURAND@LYCEE.LOCAL"})
	if !plans[0].Existing || plans[1].Existing {
		t.Errorf("existing = %v / %v", plans[0].Existing, plans[1].Existing)
	}
	if !strings.Contains(plans[0].Label(), "[existant]") || !strings.Contains(plans[1].Label(), "[à créer]") {
		t.Errorf("labels = %q / %q", plans[0].Label(), plans[1].Label())
	}
}

func TestPlansGroupes(t *testing.T) {
	specs := []SpecGroupe{
		{Nom: "6ème A", Membres: []string{"a@l", "b@l"}},
		{Nom: "6eme A", Membres: []string{"c@l"}},
	}
	plans := ConstruirePlansGroupes(specs, "distribution", nil)
	if len(plans) != 1 {
		t.Fatalf("plans = %d (déduplication par mailNickname)", len(plans))
	}
	if plans[0].Kind != "distribution" || len(plans[0].Membres) != 2 || plans[0].Membres[0] != "a@l" {
		t.Errorf("plan = %+v", plans[0])
	}

	existants := ConstruirePlansGroupes([]SpecGroupe{{Nom: "Profs"}}, "m365", map[string]string{"profs": "g-99"})
	if existants[0].ExistingID != "g-99" || !strings.Contains(existants[0].Label(), "[existant") {
		t.Errorf("plan existant = %+v", existants[0])
	}

	if vides := ConstruirePlansGroupes([]SpecGroupe{{Nom: "   "}}, "m365", nil); len(vides) != 0 {
		t.Errorf("nom vide non ignoré : %v", vides)
	}
}

// ─── Photos ───────────────────────────────────────────────────────────────

func TestMappingPhotos(t *testing.T) {
	dossier := t.TempDir()
	cheminJean := imagePNG(t, dossier, "jdurand.png")
	cheminAlice := imagePNG(t, dossier, "alice_martin.png")

	mapping, err := ConstruireMappingPhotos(dossier, []CompteAD{
		{SAM: "jdurand", CN: "Jean Durand"},
		{SAM: "amartin", CN: "Alice Martin"},
		{SAM: "autre", CN: "Autre Élève"},
	}, domaineTest)
	if err != nil {
		t.Fatalf("ConstruireMappingPhotos: %v", err)
	}
	if mapping["jdurand@lycee.local"] != cheminJean {
		t.Errorf("mapping jean = %q", mapping["jdurand@lycee.local"])
	}
	if mapping["amartin@lycee.local"] != cheminAlice {
		t.Errorf("mapping alice = %q", mapping["amartin@lycee.local"])
	}
	if len(mapping) != 2 {
		t.Errorf("mapping = %v", mapping)
	}

	vide, err := ConstruireMappingPhotos(t.TempDir(), []CompteAD{{SAM: "x", CN: "X Y"}}, domaineTest)
	if err != nil {
		t.Fatal(err)
	}
	if len(vide) != 0 {
		t.Errorf("mapping vide = %v", vide)
	}
}

// ─── Manager : connexion ──────────────────────────────────────────────────

func TestConnexionEtLicences(t *testing.T) {
	manager := managerTest(nouveauFauxClient())
	nom, err := manager.TestConnection()
	if err != nil || nom != "Lycée de Test" {
		t.Errorf("TestConnection = %q, %v", nom, err)
	}

	client := nouveauFauxClient()
	client.skus = map[string]string{"M365EDU_A1": "sku-a1", "EXCHANGESTANDARD": "sku-exch"}
	licences, err := managerTest(client).AvailableLicenses()
	if err != nil {
		t.Fatal(err)
	}
	if len(licences) != 1 || licences["M365EDU_A1"]["sku_id"] != "sku-a1" {
		t.Errorf("licences = %v", licences)
	}
	if !strings.Contains(licences["M365EDU_A1"]["label"], "A1") {
		t.Errorf("libellé = %q", licences["M365EDU_A1"]["label"])
	}
}

func TestLicenceInconnueAvantCreation(t *testing.T) {
	client := nouveauFauxClient()
	client.skus = map[string]string{"M365EDU_A1": "sku-a1"}
	manager := managerTest(client)
	plans := []PlanUtilisateurCloud{{SAM: "a", UPN: "a@l", Prenom: "A", Nom: "A", DisplayName: "A A"}}
	_, err := manager.SyncUsers(plans, "M365EDU_A5_FACULTY", nil)
	if err == nil || !strings.Contains(err.Error(), "Licence absente") {
		t.Fatalf("erreur = %v", err)
	}
	if len(client.creationsUtilisateur) != 0 || len(client.licencesAttribuees) != 0 {
		t.Error("création effectuée malgré la licence inconnue")
	}
}

// ─── Manager : comptes ────────────────────────────────────────────────────

func plansUtilisateurs() []PlanUtilisateurCloud {
	return []PlanUtilisateurCloud{
		{SAM: "jdurand", UPN: "jdurand@lycee.local", Prenom: "Jean", Nom: "Durand", DisplayName: "Jean Durand"},
		{SAM: "amartin", UPN: "amartin@lycee.local", Prenom: "Alice", Nom: "Martin", DisplayName: "Alice Martin", Existing: true},
	}
}

func TestSyncUsersCreeEtLicencie(t *testing.T) {
	client := nouveauFauxClient()
	resultats, err := managerTest(client).SyncUsers(plansUtilisateurs(), "M365EDU_A1", nil)
	if err != nil {
		t.Fatal(err)
	}
	crees := 0
	var detailCreation string
	for _, r := range resultats {
		if r.Action == "created" {
			crees++
			detailCreation = r.Detail
		}
	}
	if crees != 1 || !strings.Contains(detailCreation, "licence") {
		t.Errorf("résultats = %+v", resultats)
	}
	if len(client.creationsUtilisateur) != 1 {
		t.Fatalf("créations = %d", len(client.creationsUtilisateur))
	}
	params := client.creationsUtilisateur[0]
	if !params.AccountEnabled || params.UsageLocation != "FR" || params.GivenName != "Jean" {
		t.Errorf("params = %+v", params)
	}
	if len(params.Password) < 16 {
		t.Errorf("mot de passe trop court : %q", params.Password)
	}
	if len(client.licencesAttribuees) != 1 || client.licencesAttribuees[0][1] != "sku-a1" {
		t.Errorf("licences = %v", client.licencesAttribuees)
	}
}

func TestSyncUsersExistantJamaisTouche(t *testing.T) {
	client := nouveauFauxClient()
	resultats, err := managerTest(client).SyncUsers(plansUtilisateurs(), "M365EDU_A1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resultats {
		if r.Key == "amartin@lycee.local" && r.Action != "exists" {
			t.Errorf("compte existant traité : %+v", r)
		}
	}
	for _, params := range client.creationsUtilisateur {
		if params.UserPrincipalName == "amartin@lycee.local" {
			t.Error("compte existant recréé")
		}
	}
	if len(client.creationsUtilisateur) != 1 || len(client.licencesAttribuees) != 1 {
		t.Errorf("créations=%d licences=%d", len(client.creationsUtilisateur), len(client.licencesAttribuees))
	}
}

func TestSyncUsersSansLicence(t *testing.T) {
	client := nouveauFauxClient()
	if _, err := managerTest(client).SyncUsers(plansUtilisateurs(), "", nil); err != nil {
		t.Fatal(err)
	}
	if len(client.licencesAttribuees) != 0 {
		t.Errorf("licence attribuée sans demande : %v", client.licencesAttribuees)
	}
}

func TestSyncUsersEchecRapporte(t *testing.T) {
	client := nouveauFauxClient()
	client.failCreateUser = true
	resultats, err := managerTest(client).SyncUsers(plansUtilisateurs(), "M365EDU_A1", nil)
	if err != nil {
		t.Fatal(err)
	}
	erreurs := 0
	for _, r := range resultats {
		if r.Action == "error" {
			erreurs++
			if r.OK() {
				t.Error("erreur marquée OK")
			}
			if !strings.Contains(r.Detail, "UPN déjà utilisé") {
				t.Errorf("détail = %q", r.Detail)
			}
		}
	}
	if erreurs != 1 {
		t.Errorf("erreurs = %d", erreurs)
	}
	if resultats[1].Action != "exists" || !resultats[1].OK() {
		t.Errorf("le lot ne continue pas : %+v", resultats[1])
	}
}

func TestSyncUsersDesactiveResteDesactive(t *testing.T) {
	client := nouveauFauxClient()
	plans := []PlanUtilisateurCloud{{SAM: "x", UPN: "x@l", Prenom: "X", Nom: "Y", DisplayName: "X Y", Disabled: true}}
	if _, err := managerTest(client).SyncUsers(plans, "", nil); err != nil {
		t.Fatal(err)
	}
	if client.creationsUtilisateur[0].AccountEnabled {
		t.Error("compte désactivé créé activé")
	}
}

func TestSyncUsersProgression(t *testing.T) {
	client := nouveauFauxClient()
	var vus []string
	if _, err := managerTest(client).SyncUsers(plansUtilisateurs(), "", func(p PlanUtilisateurCloud) {
		vus = append(vus, p.SAM)
	}); err != nil {
		t.Fatal(err)
	}
	if len(vus) != 2 || vus[0] != "jdurand" || vus[1] != "amartin" {
		t.Errorf("progression = %v", vus)
	}
}

// ─── Manager : groupes ────────────────────────────────────────────────────

func TestSyncGroupsCreeEtAjouteMembres(t *testing.T) {
	client := nouveauFauxClient()
	client.users = []map[string]any{
		{"id": "u-1", "userPrincipalName": "a@lycee.local"},
		{"id": "u-2", "userPrincipalName": "b@lycee.local"},
	}
	plans := ConstruirePlansGroupes([]SpecGroupe{
		{Nom: "6ème A", Membres: []string{"a@lycee.local", "b@lycee.local", "inconnu@l"}},
	}, "m365", nil)

	resultats, err := managerTest(client).SyncGroups(plans, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "created" || !strings.Contains(resultats[0].Detail, "2 membre(s)") {
		t.Errorf("résultat = %+v", resultats[0])
	}
	if len(client.creationsGroupe) != 1 || client.creationsGroupe[0].MailNickname != "6emea" || client.creationsGroupe[0].Kind != "m365" {
		t.Errorf("créations = %+v", client.creationsGroupe)
	}
	if len(client.membresAjoutes) != 2 {
		t.Errorf("membres ajoutés = %d (inconnu ignoré)", len(client.membresAjoutes))
	}
}

func TestSyncGroupsExistantNonRecree(t *testing.T) {
	client := nouveauFauxClient()
	client.users = []map[string]any{{"id": "u-1", "userPrincipalName": "a@lycee.local"}}
	client.groups = []map[string]any{{"id": "g-1", "mailNickname": "6emea"}}
	plans := ConstruirePlansGroupes([]SpecGroupe{{Nom: "6ème A", Membres: []string{"a@lycee.local"}}}, "m365",
		map[string]string{"6emea": "g-1"})
	resultats, err := managerTest(client).SyncGroups(plans, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "exists" || len(client.creationsGroupe) != 0 {
		t.Errorf("groupe recréé : %+v", resultats[0])
	}
	if len(client.membresAjoutes) != 1 || client.membresAjoutes[0] != [2]string{"g-1", "u-1"} {
		t.Errorf("membres = %v", client.membresAjoutes)
	}
}

func TestSyncGroupsEquipeSeulementNouveauM365(t *testing.T) {
	client := nouveauFauxClient()
	plans := ConstruirePlansGroupes([]SpecGroupe{{Nom: "CE1"}}, "m365", nil)
	if _, err := managerTest(client).SyncGroups(plans, true, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(client.equipes) != 1 {
		t.Errorf("équipes = %v", client.equipes)
	}

	client2 := nouveauFauxClient()
	client2.groups = []map[string]any{{"id": "g-9", "mailNickname": "ce2"}}
	plans2 := ConstruirePlansGroupes([]SpecGroupe{{Nom: "CE2"}}, "m365", map[string]string{"ce2": "g-9"})
	if _, err := managerTest(client2).SyncGroups(plans2, true, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(client2.equipes) != 0 {
		t.Errorf("équipe recréée pour un groupe existant : %v", client2.equipes)
	}
}

func TestSyncGroupsPasEquipePourSecurite(t *testing.T) {
	client := nouveauFauxClient()
	plans := ConstruirePlansGroupes([]SpecGroupe{{Nom: "RT"}}, "security", nil)
	if _, err := managerTest(client).SyncGroups(plans, true, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(client.equipes) != 0 {
		t.Errorf("équipe créée pour un groupe de sécurité : %v", client.equipes)
	}
}

func TestSyncGroupsEchecRapporte(t *testing.T) {
	client := nouveauFauxClient()
	client.failCreateGroup = true
	plans := ConstruirePlansGroupes([]SpecGroupe{{Nom: "CE1"}}, "m365", nil)
	resultats, err := managerTest(client).SyncGroups(plans, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "error" || resultats[0].OK() {
		t.Errorf("échec non rapporté : %+v", resultats[0])
	}
}

// ─── Manager : photos ─────────────────────────────────────────────────────

func TestSyncPhotosDeposeJPEG(t *testing.T) {
	chemin := imagePNG(t, t.TempDir(), "x.png")
	client := nouveauFauxClient()
	client.users = []map[string]any{{"id": "u-1", "userPrincipalName": "x@lycee.local"}}
	resultats, err := managerTest(client).SyncPhotos(map[string]string{"x@lycee.local": chemin}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "updated" {
		t.Fatalf("résultat = %+v", resultats[0])
	}
	if len(client.photos) != 1 || client.photos[0].userID != "u-1" {
		t.Fatalf("photos = %+v", client.photos)
	}
	if len(client.photos[0].jpeg) < 2 || client.photos[0].jpeg[0] != 0xFF || client.photos[0].jpeg[1] != 0xD8 {
		t.Error("la photo déposée n'est pas un JPEG normalisé")
	}
}

func TestSyncPhotosCompteInconnu(t *testing.T) {
	chemin := imagePNG(t, t.TempDir(), "y.png")
	resultats, err := managerTest(nouveauFauxClient()).SyncPhotos(map[string]string{"fantome@lycee.local": chemin}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "error" || !strings.Contains(resultats[0].Detail, "introuvable") {
		t.Errorf("résultat = %+v", resultats[0])
	}
}

func TestSyncPhotosFichierAbsent(t *testing.T) {
	client := nouveauFauxClient()
	client.users = []map[string]any{{"id": "u-1", "userPrincipalName": "x@l"}}
	resultats, err := managerTest(client).SyncPhotos(map[string]string{"x@l": filepath.Join(t.TempDir(), "absent.png")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultats[0].Action != "error" {
		t.Errorf("résultat = %+v", resultats[0])
	}
}

// ─── Hybride ──────────────────────────────────────────────────────────────

func TestChecklistHybride(t *testing.T) {
	liste := HybridChecklist()
	if len(liste) == 0 {
		t.Fatal("check-list vide")
	}
	var titres strings.Builder
	for _, etape := range liste {
		if etape[0] == "" || etape[1] == "" {
			t.Errorf("étape incomplète : %+v", etape)
		}
		titres.WriteString(strings.ToLower(etape[0]) + " ")
	}
	titre := titres.String()
	if !strings.Contains(titre, "phs") || !(strings.Contains(titre, "ad fs") || strings.Contains(titre, "adfs")) || !strings.Contains(titre, "pta") {
		t.Errorf("titres = %q", titre)
	}

	markdown := HybridChecklistMarkdown()
	if !strings.HasPrefix(markdown, "# Identité hybride") || !strings.Contains(markdown, "Entra Connect") {
		t.Errorf("markdown = %q", markdown[:min(80, len(markdown))])
	}
	if strings.Count(markdown, "## ") != len(liste) {
		t.Errorf("sections = %d, attendu %d", strings.Count(markdown, "## "), len(liste))
	}
}
