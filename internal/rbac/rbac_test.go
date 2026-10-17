package rbac

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	ouA    = "OU=3emeA,OU=eleves,DC=lycee,DC=local"
	ouB    = "OU=3emeB,OU=eleves,DC=lycee,DC=local"
	groupe = "CN=3emeA,OU=groupes,DC=lycee,DC=local"
)

// grantTest construit un délégué de test.
func grantTest(options ...func(*Grant)) Grant {
	grant := Grant{ID: UniqueGrantID(nil), Operateur: "alice", Role: LectureSeule, Actif: true}
	for _, option := range options {
		option(&grant)
	}
	return grant
}

func avecID(id string) func(*Grant)        { return func(g *Grant) { g.ID = id } }
func avecOperateur(op string) func(*Grant) { return func(g *Grant) { g.Operateur = op } }
func avecRole(role Role) func(*Grant)      { return func(g *Grant) { g.Role = role } }
func avecSite(site string) func(*Grant)    { return func(g *Grant) { g.Site = site } }
func avecOUs(ous ...string) func(*Grant)   { return func(g *Grant) { g.OUs = ous } }
func avecGroupes(g ...string) func(*Grant) { return func(grant *Grant) { grant.Groupes = g } }
func inactif() func(*Grant)                { return func(g *Grant) { g.Actif = false } }

// ─── Matrice des rôles ───────────────────────────────────────────────────

func TestMatriceDesRoles(t *testing.T) {
	var valeurs []string
	for _, role := range RoleOrder {
		valeurs = append(valeurs, string(role))
	}
	attendu := []string{"super_admin", "admin_site", "admin_classe", "helpdesk", "lecture_seule"}
	if len(valeurs) != len(attendu) {
		t.Fatalf("RoleOrder = %v", valeurs)
	}
	for i := range attendu {
		if valeurs[i] != attendu[i] {
			t.Fatalf("RoleOrder = %v", valeurs)
		}
	}
	if len(RolePermissions) != len(RoleOrder) || len(RoleLabels) != len(RoleOrder) {
		t.Errorf("les matrices devraient couvrir tous les rôles")
	}
	for _, role := range RoleOrder {
		for permission := range RolePermissions[role] {
			if _, ok := Permissions[permission]; !ok {
				t.Errorf("permission inventée %q pour %s", permission, role)
			}
		}
		lecture := false
		for _, p := range ReadPermissions {
			if RolePermissions[role][p] {
				lecture = true
			}
		}
		if !lecture {
			t.Errorf("%s ne peut pas consulter", role)
		}
	}
}

func TestSuperAdminPossedeTout(t *testing.T) {
	if len(RolePermissions[SuperAdmin]) != len(Permissions) {
		t.Errorf("super_admin = %d permissions", len(RolePermissions[SuperAdmin]))
	}
}

func TestAdminSiteToutSaufDelegation(t *testing.T) {
	if len(RolePermissions[AdminSite]) != len(Permissions)-1 || RolePermissions[AdminSite]["admin"] {
		t.Errorf("admin_site = %v", RolePermissions[AdminSite])
	}
}

func TestLectureSeuleNeLitQue(t *testing.T) {
	if len(RolePermissions[LectureSeule]) != len(ReadPermissions) {
		t.Errorf("lecture_seule = %v", RolePermissions[LectureSeule])
	}
}

func TestSeulSuperAdminGereLaDelegation(t *testing.T) {
	for _, role := range RoleOrder {
		if RolePermissions[role]["admin"] && role != SuperAdmin {
			t.Errorf("%s gère la délégation", role)
		}
	}
}

func TestRolesScopesSansAdministrationNiSuppression(t *testing.T) {
	for _, role := range ScopedRoles {
		perms := RolePermissions[role]
		if perms["admin"] || perms["delete_user"] || perms["delete_ou"] {
			t.Errorf("%s = %v", role, perms)
		}
	}
}

func TestRoleCreantDesComptesPeutFinirLeCycle(t *testing.T) {
	for _, role := range RoleOrder {
		perms := RolePermissions[role]
		if perms["create_user"] && (!perms["reset_password"] || !perms["disable_account"]) {
			t.Errorf("%s crée des comptes sans pouvoir finir le cycle", role)
		}
	}
}

func TestHelpdeskReinitialiseMaisNeCreeJamais(t *testing.T) {
	perms := RolePermissions[Helpdesk]
	for _, requis := range []string{"reset_password", "disable_account", "modify_user"} {
		if !perms[requis] {
			t.Errorf("helpdesk sans %s", requis)
		}
	}
	if perms["create_user"] || perms["delete_user"] {
		t.Errorf("helpdesk = %v", perms)
	}
}

func TestRoleLabelEstLaLegendeFrancaise(t *testing.T) {
	if RoleLabel(AdminClasse) != "Admin classe" {
		t.Errorf("label = %q", RoleLabel(AdminClasse))
	}
	if grantTest(avecRole(Helpdesk)).RoleLabel() != "Helpdesk" {
		t.Error("RoleLabel du délégué incorrect")
	}
}

// ─── Résolution opérateur implique rôle ──────────────────────────────────

func TestAucunDelegueGardeLEAccesComplet(t *testing.T) {
	policy := PolicyForGrants("admin", "lycee.local", []Grant{})
	if policy.Role != SuperAdmin || !policy.CanManageDelegation() {
		t.Errorf("policy = %+v", policy)
	}
	if !contient(policy.Source, "aucun délégué") {
		t.Errorf("source = %q", policy.Source)
	}
}

func TestOperateurAbsentEstLectureSeule(t *testing.T) {
	grants := []Grant{grantTest(avecOperateur("alice"), avecRole(AdminSite))}
	policy := PolicyForGrants("bob", "lycee.local", grants)
	if policy.Role != LectureSeule || policy.Has("create_user") || policy.CanManageDelegation() {
		t.Errorf("policy = %+v", policy)
	}
	if !contient(policy.Source, "non référencé") {
		t.Errorf("source = %q", policy.Source)
	}
}

func TestDeleguesInactifsNeRestreignentPersonne(t *testing.T) {
	grants := []Grant{grantTest(avecOperateur("alice"), avecRole(AdminSite), inactif())}
	if PolicyForGrants("alice", "lycee.local", grants).Role != SuperAdmin {
		t.Error("alice devrait rester super_admin")
	}
	if PolicyForGrants("bob", "lycee.local", grants).Role != SuperAdmin {
		t.Error("bob devrait rester super_admin")
	}
}

func TestDelegueCorrespondantAppliqueRoleEtPortees(t *testing.T) {
	grants := []Grant{grantTest(avecRole(AdminClasse), avecOUs(ouA))}
	policy := PolicyForGrants("alice", "lycee.local", grants)
	if policy.Role != AdminClasse || len(policy.OUs) != 1 || !policy.IsScoped() || policy.Readonly() {
		t.Errorf("policy = %+v", policy)
	}
	if !contient(policy.Source, "alice") {
		t.Errorf("source = %q", policy.Source)
	}
}

func TestNomOperateurNormalise(t *testing.T) {
	grants := []Grant{grantTest(avecOperateur(`LYCEE\alice`), avecRole(Helpdesk), avecOUs(ouA))}
	for _, nom := range []string{"alice", "ALICE", " alice ", `lycee\alice`, "alice@lycee.local"} {
		if PolicyForGrants(nom, "lycee.local", grants).Role != Helpdesk {
			t.Errorf("nom %q non normalisé", nom)
		}
	}
}

func TestDelegueDUnAutreSiteNeSAppliquePas(t *testing.T) {
	grants := []Grant{
		grantTest(avecOperateur("alice"), avecRole(AdminSite), avecSite("college-b.local")),
		grantTest(avecOperateur("bob"), avecRole(Helpdesk), avecOUs(ouA)),
	}
	if PolicyForGrants("alice", "lycee.local", grants).Role != LectureSeule {
		t.Error("alice ne devrait pas s'appliquer sur un autre site")
	}
	if PolicyForGrants("alice", "", grants).Role != LectureSeule {
		t.Error("alice sans site ne devrait pas s'appliquer")
	}
	if PolicyForGrants("alice", "COLLEGE-B.LOCAL", grants).Role != AdminSite {
		t.Error("alice devrait s'appliquer sur son site")
	}
	if PolicyForGrants("bob", "autre.local", grants).Role != Helpdesk {
		t.Error("bob (site vide) devrait s'appliquer partout")
	}
}

func TestMatchingGrantsFiltreRoleSiteActivation(t *testing.T) {
	grants := []Grant{
		grantTest(avecID("g1"), avecOperateur("alice"), avecRole(AdminSite), avecSite("b.local")),
		grantTest(avecID("g2"), avecOperateur("alice"), avecRole(Helpdesk), avecOUs(ouA)),
		grantTest(avecID("g3"), avecOperateur("alice"), avecRole(LectureSeule), inactif()),
	}
	matched := MatchingGrants(grants, "alice", "lycee.local")
	if len(matched) != 1 || matched[0].ID != "g2" {
		t.Errorf("matched = %+v", matched)
	}
	if len(MatchingGrants(grants, "charlie", "lycee.local")) != 0 {
		t.Error("charlie ne devrait correspondre à rien")
	}
}

func TestCumulGardeLeRoleLePlusLargeEtLUnionDesPortees(t *testing.T) {
	grants := []Grant{
		grantTest(avecID("g1"), avecOperateur("alice"), avecRole(Helpdesk), avecOUs(ouA), avecGroupes(groupe)),
		grantTest(avecID("g2"), avecOperateur("alice"), avecRole(AdminClasse), avecOUs(ouB)),
	}
	policy := PolicyForGrants("alice", "lycee.local", grants)
	if policy.Role != AdminClasse {
		t.Errorf("role = %s", policy.Role)
	}
	if len(policy.OUs) != 2 || len(policy.Groupes) != 1 {
		t.Errorf("portées = %+v", policy)
	}
}

// ─── Portées ─────────────────────────────────────────────────────────────

func TestNormalizeDN(t *testing.T) {
	attendu := "ou=3emea,ou=eleves,dc=lycee,dc=local"
	if got := NormalizeDN(" OU = 3emeA , OU = eleves , DC=lycee , DC=local "); got != attendu {
		t.Errorf("NormalizeDN = %q", got)
	}
	if NormalizeDN("") != "" {
		t.Error("NormalizeDN(\"\") devrait être vide")
	}
}

func TestDNInScopesCouvreLeSousArbre(t *testing.T) {
	if !DNInScopes(ouA, []string{ouA}) {
		t.Error("le DN racine devrait être dans les portées")
	}
	if !DNInScopes("cn=thomas.martin,"+ouA, []string{ouA}) {
		t.Error("un enfant devrait être dans les portées")
	}
	if !DNInScopes("ou=3emeA-2,"+ouA, []string{ouA}) {
		t.Error("une sous-OU devrait être dans les portées")
	}
	if !DNInScopes("cn=3emeA,ou=groupes,dc=lycee,dc=local", []string{"OU=groupes,DC=lycee,DC=local"}) {
		t.Error("un groupe devrait être dans sa portée")
	}
}

func TestDNInScopesInsensibleCasseEtEspaces(t *testing.T) {
	if !DNInScopes("cn=thomas martin, "+ouA, []string{ouA}) {
		t.Error("espaces non tolérés")
	}
	if !DNInScopes("cn=ThomasMartin,ou=3emea,ou=eleves,dc=lycee,dc=local",
		[]string{"OU = 3emeA , OU = eleves , DC=lycee, DC=local"}) {
		t.Error("casse non tolérée")
	}
}

func TestDNInScopesRejetteFreresEtVides(t *testing.T) {
	if DNInScopes(ouB, []string{ouA}) {
		t.Error("OU_B ne devrait pas être dans OU_A")
	}
	if DNInScopes("cn=thomas.martin,"+ouB, []string{ouA}) {
		t.Error("un enfant d'OU_B ne devrait pas être dans OU_A")
	}
	if DNInScopes("", []string{ouA}) || DNInScopes(ouA, nil) || DNInScopes(ouA, []string{""}) {
		t.Error("les valeurs vides devraient être rejetées")
	}
}

func TestRoleScopeNEcritQueDansSesPortees(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: AdminClasse, OUs: []string{ouA}, Groupes: []string{groupe}}
	if !policy.May("create_ou", "ou=4emeB,"+ouA) {
		t.Error("dans la portée : devrait être autorisé")
	}
	if !policy.May("create_group", groupe) {
		t.Error("groupe délégué : devrait être autorisé")
	}
	if policy.May("create_ou", ouB) || policy.May("create_ou", "cn=thomas.martin,"+ouB) {
		t.Error("hors portée : devrait être refusé")
	}
	if policy.May("move_user", "cn=thomas.martin,"+ouA, ouB) {
		t.Error("toutes les cibles doivent être couvertes")
	}
}

func TestLecturesJamaisScoped(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: AdminClasse, OUs: []string{ouA}}
	if !policy.May("read_user", ouB) || !policy.May("export_data", ouB) || !policy.May("read_audit") {
		t.Error("les lectures ne devraient jamais être limitées")
	}
}

func TestRoleScopeSansPorteeNecritPas(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: Helpdesk}
	if policy.May("reset_password", "cn=Existing User,"+ouA) || policy.May("disable_account", ouA) {
		t.Error("un rôle scopé sans portée ne devrait pas écrire")
	}
}

func TestRoleNonScopeIgnoreLesPortees(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: AdminSite}
	if !policy.May("delete_ou", ouB) || !policy.May("move_user", "cn=x,"+ouB, ouA) {
		t.Error("un rôle non scopé ne devrait pas être limité")
	}
}

// ─── Décisions ───────────────────────────────────────────────────────────

func TestCheckLeveUnMessageExploitable(t *testing.T) {
	policy := &Policy{Operator: "bob", Role: LectureSeule}
	err := policy.Check("delete_user", ouA)
	if !errors.Is(err, ErrInsufficientRights) {
		t.Fatalf("erreur = %v", err)
	}
	message := err.Error()
	for _, attendu := range []string{"Supprimer définitivement un compte", "Lecture seule", "bob"} {
		if !contient(message, attendu) {
			t.Errorf("message %q sans %q", message, attendu)
		}
	}
}

func TestCheckRefuseUnePermissionNonDeclaree(t *testing.T) {
	policy := &Policy{Operator: "admin", Role: SuperAdmin}
	err := policy.Check("une_permission_inventee")
	if !errors.Is(err, ErrInsufficientRights) || !contient(err.Error(), "verrouillage") {
		t.Errorf("erreur = %v", err)
	}
}

func TestCheckRefuseUneCibleHorsPortee(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: AdminClasse, OUs: []string{ouA}}
	err := policy.Check("create_ou", ouB)
	if !errors.Is(err, ErrInsufficientRights) || !contient(err.Error(), "hors des OU") {
		t.Errorf("erreur = %v", err)
	}
	if !contient(err.Error(), ouA) {
		t.Errorf("message %q sans la portée", err.Error())
	}
}

func TestCheckPasseQuandAutorise(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: AdminClasse, OUs: []string{ouA}}
	if err := policy.Check("create_ou", "ou=4emeB,"+ouA); err != nil {
		t.Errorf("dans la portée: %v", err)
	}
	if err := policy.Check("read_user", ouB); err != nil {
		t.Errorf("lecture: %v", err)
	}
}

func TestHasAnyEtDescribe(t *testing.T) {
	policy := &Policy{Operator: "alice", Role: Helpdesk, OUs: []string{ouA}, Groupes: []string{groupe}}
	if !policy.HasAny("delete_user", "reset_password") {
		t.Error("HasAny devrait être vrai")
	}
	if policy.HasAny("delete_user", "create_user") {
		t.Error("HasAny devrait être faux")
	}
	described := policy.Describe()
	if !contient(described, "Helpdesk") || !contient(described, "1 OU, 1 groupe(s)") {
		t.Errorf("describe = %q", described)
	}
	if !(&Policy{Operator: "bob", Role: LectureSeule}).Readonly() {
		t.Error("lecture_seule devrait être readonly")
	}
}

// ─── Persistance (delegations.json) ──────────────────────────────────────

func TestDelegationsAllerRetourEnClair(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "delegations.json")
	grants := []Grant{
		grantTest(avecID(UniqueGrantID(nil)), avecRole(AdminClasse), avecOUs(ouA), avecGroupes(groupe)),
		grantTest(avecID(UniqueGrantID(nil)), avecOperateur("bob"), avecRole(Helpdesk), avecSite("b.local")),
	}
	if err := SauvegarderGrants(grants, chemin); err != nil {
		t.Fatalf("SauvegarderGrants: %v", err)
	}

	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Version int     `json:"version"`
		Grants  []Grant `json:"grants"`
	}
	if err := json.Unmarshal(brut, &payload); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if payload.Version != 1 || payload.Grants[0].Operateur != "alice" {
		t.Errorf("payload = %+v", payload)
	}

	relus, err := ChargerGrants(chemin)
	if err != nil {
		t.Fatalf("ChargerGrants: %v", err)
	}
	if len(relus) != len(grants) {
		t.Fatalf("relus = %+v", relus)
	}
	for i := range grants {
		if relus[i].ID != grants[i].ID || relus[i].Role != grants[i].Role ||
			relus[i].Actif != grants[i].Actif || relus[i].Site != grants[i].Site {
			t.Errorf("délégué %d = %+v, attendu %+v", i, relus[i], grants[i])
		}
		if len(relus[i].OUs) != len(grants[i].OUs) || len(relus[i].Groupes) != len(grants[i].Groupes) {
			t.Errorf("portées %d = %+v", i, relus[i])
		}
	}
}

func TestFichierAbsentOuCorrompuDonneAucunDelegue(t *testing.T) {
	dir := t.TempDir()
	if grants, err := ChargerGrants(filepath.Join(dir, "absent.json")); err != nil || len(grants) != 0 {
		t.Errorf("absent: %v %v", grants, err)
	}
	casse := filepath.Join(dir, "delegations.json")
	if err := os.WriteFile(casse, []byte("{ pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if grants, _ := ChargerGrants(casse); len(grants) != 0 {
		t.Errorf("json cassé: %+v", grants)
	}
	if err := os.WriteFile(casse, []byte(`{"grants": "pas une liste"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if grants, _ := ChargerGrants(casse); len(grants) != 0 {
		t.Errorf("grants non-liste: %+v", grants)
	}
}

func TestRoleInconnuRepliLectureSeule(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "delegations.json")
	if err := os.WriteFile(chemin,
		[]byte(`{"version":1,"grants":[{"id":"g1","operateur":"alice","role":"roi"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	grants, err := ChargerGrants(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Role != LectureSeule || !grants[0].Actif {
		t.Errorf("grants = %+v", grants)
	}
}

func TestBuildPolicyLitLeFichier(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "delegations.json")
	if err := SauvegarderGrants([]Grant{grantTest(avecRole(AdminSite))}, chemin); err != nil {
		t.Fatal(err)
	}
	policy, err := BuildPolicy("alice", "lycee.local", chemin)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Role != AdminSite {
		t.Errorf("alice = %s", policy.Role)
	}
	bob, err := BuildPolicy("bob", "lycee.local", chemin)
	if err != nil {
		t.Fatal(err)
	}
	if bob.Role != LectureSeule {
		t.Errorf("bob = %s", bob.Role)
	}
}

func TestFindGrantEtIDsUniques(t *testing.T) {
	grants := []Grant{grantTest(avecID("g1")), grantTest(avecID("g2"))}
	if g := FindGrant(grants, "g2"); g == nil || g.Operateur != "alice" {
		t.Errorf("FindGrant = %+v", g)
	}
	if FindGrant(grants, "") != nil || FindGrant(grants, "inconnu") != nil {
		t.Error("FindGrant devrait retourner nil")
	}
	for i := 0; i < 50; i++ {
		id := UniqueGrantID(grants)
		if id == "g1" || id == "g2" {
			t.Errorf("identifiant déjà pris : %q", id)
		}
	}
	if UniqueGrantID(nil) == "" {
		t.Error("identifiant vide")
	}
}

func TestValidateExpliqueCeQuiManque(t *testing.T) {
	erreurs := grantTest(avecOperateur("   ")).Validate()
	if len(erreurs) != 1 || erreurs[0] != "Le compte de l'opérateur est obligatoire." {
		t.Errorf("erreurs = %v", erreurs)
	}
	if !contientListe(grantTest(avecRole(AdminClasse)).Validate(), "au moins une portée") {
		t.Error("le rôle scopé sans portée devrait être refusé")
	}
	if !contientListe(grantTest(avecOUs("pas-un-dn")).Validate(), "DN attendu") {
		t.Error("une portée sans « = » devrait être refusée")
	}
	if len(grantTest(avecRole(AdminSite), avecOUs(ouA)).Validate()) != 0 {
		t.Error("un délégué valide devrait être accepté")
	}
}

// contient indique si foin contient aiguille.
func contient(foin, aiguille string) bool {
	for i := 0; i+len(aiguille) <= len(foin); i++ {
		if foin[i:i+len(aiguille)] == aiguille {
			return true
		}
	}
	return false
}

// contientListe indique si une des chaînes contient « cible ».
func contientListe(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if contient(v, cible) {
			return true
		}
	}
	return false
}
