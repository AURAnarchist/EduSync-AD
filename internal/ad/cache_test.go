package ad

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// cacheTest crée un cache vide dans un dossier temporaire.
func cacheTest(t *testing.T) *ADCache {
	t.Helper()
	c, err := NewADCache(filepath.Join(t.TempDir(), NomFichierCache), 0)
	if err != nil {
		t.Fatalf("NewADCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// marquerSynchronise ecrit une date de dernière synchro (secondes epoch).
func marquerSynchronise(t *testing.T, c *ADCache, quand time.Time) {
	t.Helper()
	if err := c.sauverMeta("last_sync", strconv.FormatFloat(float64(quand.Unix()), 'f', -1, 64)); err != nil {
		t.Fatalf("sauverMeta last_sync: %v", err)
	}
	if err := c.chargerEtat(); err != nil {
		t.Fatalf("chargerEtat: %v", err)
	}
}

func TestCacheSchemaEtStats(t *testing.T) {
	c := cacheTest(t)

	if s := c.Stats(); s.OUsCount != 0 || s.GroupsCount != 0 || s.UsersCount != 0 {
		t.Errorf("cache neuf non vide : %+v", s)
	}
	if !c.IsStale() {
		t.Error("un cache jamais synchronisé doit être périmé")
	}
}

func TestCacheRemplaceEtRequetes(t *testing.T) {
	c := cacheTest(t)
	base := "DC=lycee,DC=local"
	classe := "OU=Classes," + base
	sixieme := "OU=6emeA," + classe

	ous := []CacheOU{
		{DN: classe, Name: "Classes", ParentDN: base, Depth: 0, USNChanged: 10},
		{DN: sixieme, Name: "6emeA", ParentDN: classe, Depth: 1, USNChanged: 11},
	}
	groupes := []CacheGroup{
		{DN: "CN=6emeA," + classe, CN: "6emeA", OUDN: classe, GroupType: -2147483646, MemberCount: 2, USNChanged: 12},
	}
	utilisateurs := []CacheUser{
		{DN: "CN=Alice Martin," + sixieme, SAM: "amartin", CN: "Alice Martin", GivenName: "Alice", SN: "Martin", Mail: "alice@lycee.local", OUDN: sixieme, UAC: 512, USNChanged: 20},
		{DN: "CN=Bob Bernard," + sixieme, SAM: "bbernard", CN: "Bob Bernard", OUDN: sixieme, UAC: 514, USNChanged: 21},
	}

	if err := c.remplacerOUs(ous); err != nil {
		t.Fatalf("remplacerOUs: %v", err)
	}
	if err := c.remplacerGroupes(groupes); err != nil {
		t.Fatalf("remplacerGroupes: %v", err)
	}
	if err := c.remplacerUtilisateurs(utilisateurs); err != nil {
		t.Fatalf("remplacerUtilisateurs: %v", err)
	}

	toutes, err := c.GetOUs("")
	if err != nil || len(toutes) != 2 {
		t.Fatalf("GetOUs = %d (%v), attendu 2", len(toutes), err)
	}
	if toutes[0].Name != "Classes" || toutes[1].Name != "6emeA" {
		t.Errorf("ordre GetOUs = %s, %s", toutes[0].Name, toutes[1].Name)
	}

	enfants, err := c.GetOUs(base)
	if err != nil || len(enfants) != 1 || enfants[0].Name != "Classes" {
		t.Fatalf("GetOUs(parent) = %+v (%v)", enfants, err)
	}

	grp, err := c.GetGroups(classe)
	if err != nil || len(grp) != 1 || grp[0].CN != "6emeA" {
		t.Fatalf("GetGroups = %+v (%v)", grp, err)
	}
	if grp[0].MemberCount != 2 {
		t.Errorf("MemberCount = %d, attendu 2", grp[0].MemberCount)
	}

	users, err := c.GetUsersInOU(sixieme)
	if err != nil || len(users) != 2 {
		t.Fatalf("GetUsersInOU = %d (%v), attendu 2", len(users), err)
	}
	for _, u := range users {
		if u.SAM == "bbernard" && !u.Desactive {
			t.Error("bbernard devrait être désactivé (uac 514)")
		}
		if u.SAM == "amartin" && u.Desactive {
			t.Error("amartin ne devrait pas être désactivé (uac 512)")
		}
	}

	recherche, err := c.SearchUsers("ali", 10)
	if err != nil || len(recherche) != 1 || recherche[0].SAM != "amartin" {
		t.Fatalf("SearchUsers = %+v (%v)", recherche, err)
	}

	rechercheOU, err := c.SearchOUs("6eme", 10)
	if err != nil || len(rechercheOU) != 1 || rechercheOU[0].Name != "6emeA" {
		t.Fatalf("SearchOUs = %+v (%v)", rechercheOU, err)
	}

	children, err := c.GetOUChildren(base)
	if err != nil {
		t.Fatalf("GetOUChildren: %v", err)
	}
	if len(children) != 1 || children[0].Kind != "ou" {
		t.Errorf("GetOUChildren(base) = %+v", children)
	}
}

func TestCacheUpsert(t *testing.T) {
	c := cacheTest(t)
	base := "DC=lycee,DC=local"

	if err := c.remplacerUtilisateurs([]CacheUser{
		{DN: "CN=Alice," + base, SAM: "amartin", CN: "Alice", UAC: 512, USNChanged: 5},
	}); err != nil {
		t.Fatalf("remplacerUtilisateurs: %v", err)
	}

	if err := c.upsertUtilisateurs([]CacheUser{
		{DN: "CN=Alice," + base, SAM: "amartin", CN: "Alice Martin", UAC: 514, USNChanged: 9},
		{DN: "CN=Bob," + base, SAM: "bbernard", CN: "Bob", UAC: 512, USNChanged: 9},
	}); err != nil {
		t.Fatalf("upsertUtilisateurs: %v", err)
	}

	users, err := c.SearchUsers("", 10)
	if err != nil || len(users) != 2 {
		t.Fatalf("SearchUsers = %d (%v), attendu 2", len(users), err)
	}
	for _, u := range users {
		if u.SAM == "amartin" {
			if u.CN != "Alice Martin" {
				t.Errorf("CN non mis à jour : %q", u.CN)
			}
			if !u.Desactive {
				t.Error("amartin devrait être désactivé après upsert")
			}
		}
	}
}

func TestCacheTTLEtPersistance(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), NomFichierCache)
	c, err := NewADCache(chemin, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewADCache: %v", err)
	}

	if err := c.remplacerOUs([]CacheOU{{DN: "OU=A,DC=x", Name: "A"}}); err != nil {
		t.Fatalf("remplacerOUs: %v", err)
	}
	if err := c.sauverMeta("last_usn_changed", "42"); err != nil {
		t.Fatalf("sauverMeta: %v", err)
	}
	// Synchro vieille de 10 minutes → périmé malgré un TTL court.
	marquerSynchronise(t, c, time.Now().Add(-10*time.Minute))
	if !c.IsStale() {
		t.Error("le cache datant de 10 min doit être périmé")
	}
	_ = c.Close()

	c2, err := NewADCache(chemin, 0)
	if err != nil {
		t.Fatalf("réouverture: %v", err)
	}
	defer func() { _ = c2.Close() }()
	if s := c2.Stats(); s.LastUSNChanged != 42 {
		t.Errorf("LastUSNChanged = %d, attendu 42", s.LastUSNChanged)
	}
	if s := c2.Stats(); s.OUsCount != 1 {
		t.Errorf("OUsCount = %d, attendu 1", s.OUsCount)
	}
}

func TestCacheFraisNonStale(t *testing.T) {
	c := cacheTest(t)
	marquerSynchronise(t, c, time.Now())
	if c.IsStale() {
		t.Error("un cache synchronisé à l'instant ne doit pas être périmé")
	}

	// Une date non numérique laisse le cache périmé (repli sûr).
	if err := c.sauverMeta("last_sync", "pas-un-nombre"); err != nil {
		t.Fatalf("sauverMeta: %v", err)
	}
	if err := c.chargerEtat(); err != nil {
		t.Fatalf("chargerEtat: %v", err)
	}
	if !c.IsStale() {
		t.Error("une date non numérique doit laisser le cache périmé")
	}
}

func TestEnfantsExplorateurEtGetSubTree(t *testing.T) {
	c := cacheTest(t)
	base := "DC=lycee,DC=local"

	if err := c.remplacerOUs([]CacheOU{
		{DN: "OU=Eleves," + base, Name: "Eleves", ParentDN: base, Depth: 0},
		{DN: "OU=Profs," + base, Name: "Profs", ParentDN: base, Depth: 0},
	}); err != nil {
		t.Fatalf("remplacerOUs: %v", err)
	}
	if err := c.remplacerGroupes([]CacheGroup{
		{DN: "CN=Direction," + base, CN: "Direction", OUDN: base},
	}); err != nil {
		t.Fatalf("remplacerGroupes: %v", err)
	}
	marquerSynchronise(t, c, time.Now())

	nodes, err := c.EnfantsExplorateur(base, true)
	if err != nil {
		t.Fatalf("EnfantsExplorateur: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("EnfantsExplorateur = %d nœuds, attendu 3", len(nodes))
	}

	// Intégration : GetSubTree doit servir depuis le cache sans connexion LDAP.
	pool, err := NewPoolNoValidate(Config{
		Domain:       "lycee.local",
		Controller:   "dc.lycee.local",
		BaseDN:       base,
		BindDN:       "CN=admin,DC=lycee,DC=local",
		BindPassword: "secret",
	})
	if err != nil {
		t.Fatalf("NewPoolNoValidate: %v", err)
	}
	pool.SetADCache(c)

	arbre, err := pool.GetSubTree(context.Background(), base, true)
	if err != nil {
		t.Fatalf("GetSubTree via cache: %v", err)
	}
	if len(arbre) != 3 {
		t.Errorf("GetSubTree = %d nœuds, attendu 3", len(arbre))
	}
}
