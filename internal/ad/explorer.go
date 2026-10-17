// Package ad fournit l'explorateur AD : recherche unifiée, arborescence OU/groupes,
// cache pour navigation fluide sur gros domaines (10k+ objets).
package ad

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// ExplorerNode représente un nœud de l'arborescence (OU ou Groupe).
type ExplorerNode struct {
	DN          string
	Name        string
	Type        string // "ou" | "group" | "container"
	Children    []*ExplorerNode
	Expanded    bool
	Selected    bool
	ParentDN    string
	HasChildren bool // pour lazy loading
}

// SearchResultEntry représente un résultat de recherche unifiée.
type SearchResultEntry struct {
	DN             string
	ObjectType     string // "user" | "group" | "ou" | "computer" | "contact"
	DisplayName    string
	SAMAccountName string
	OU             string
	Enabled        bool
	Email          string
	Description    string
}

// SearchFilter décrit les critères de recherche.
type SearchFilter struct {
	Query           string   // texte libre (prénom, nom, identifiant, email)
	ObjectTypes     []string // ["user", "group", "ou"] - vide = tous
	BaseDN          string   // OU de départ (vide = BaseDN du pool)
	PageSize        int
	Cookie          string // pour pagination
	IncludeDisabled bool
}

// SearchResult contient les résultats paginés.
type SearchResult struct {
	Entries []SearchResultEntry
	Cookie  string // pour page suivante
	Total   int    // estimation si dispo
}

// ExplorerCache met en cache l'arborescence et les recherches récentes.
type ExplorerCache struct {
	mu           sync.RWMutex
	tree         *ExplorerNode
	treeExpiry   time.Time
	searchCache  map[string]*SearchResult
	searchExpiry map[string]time.Time
	ttl          time.Duration
	maxEntries   int
}

// NewExplorerCache crée un cache avec TTL configurable.
func NewExplorerCache(ttl time.Duration, maxEntries int) *ExplorerCache {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	if maxEntries == 0 {
		maxEntries = 100
	}
	return &ExplorerCache{
		searchCache:  make(map[string]*SearchResult),
		searchExpiry: make(map[string]time.Time),
		ttl:          ttl,
		maxEntries:   maxEntries,
	}
}

// GetTree retourne l'arbre en cache ou nil s'expiré.
func (c *ExplorerCache) GetTree() *ExplorerNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Now().Before(c.treeExpiry) {
		return c.tree
	}
	return nil
}

// SetTree stocke l'arbre en cache.
func (c *ExplorerCache) SetTree(tree *ExplorerNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tree = tree
	c.treeExpiry = time.Now().Add(c.ttl)
}

// GetSearch retourne un résultat de recherche en cache.
func (c *ExplorerCache) GetSearch(key string) *SearchResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if exp, ok := c.searchExpiry[key]; ok && time.Now().Before(exp) {
		return c.searchCache[key]
	}
	return nil
}

// SetSearch stocke un résultat de recherche.
func (c *ExplorerCache) SetSearch(key string, result *SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Éviction simple si trop d'entrées
	if len(c.searchCache) >= c.maxEntries {
		// Supprimer les plus anciens (naïf mais efficace pour petite taille)
		oldest := ""
		oldestTime := time.Now()
		for k, t := range c.searchExpiry {
			if t.Before(oldestTime) {
				oldest = k
				oldestTime = t
			}
		}
		if oldest != "" {
			delete(c.searchCache, oldest)
			delete(c.searchExpiry, oldest)
		}
	}

	c.searchCache[key] = result
	c.searchExpiry[key] = time.Now().Add(c.ttl)
}

// Invalidate invalide le cache (ex: après écriture AD).
func (c *ExplorerCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tree = nil
	c.searchCache = make(map[string]*SearchResult)
	c.searchExpiry = make(map[string]time.Time)
}

// Pool étendu avec explorer.
func (p *Pool) getCache() *ExplorerCache {
	if p.cache == nil {
		p.cache = NewExplorerCache(5*time.Minute, 100)
	}
	return p.cache
}

// BuildTree construit l'arborescence complète des OU et groupes (lazy loading possible).
func (p *Pool) BuildTree(ctx context.Context, baseDN string, maxDepth int, includeGroups bool) (*ExplorerNode, error) {
	// Vérifier le cache
	if cached := p.getCache().GetTree(); cached != nil {
		return cached, nil
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	root := &ExplorerNode{
		DN:       baseDN,
		Name:     "Racine",
		Type:     "container",
		Children: []*ExplorerNode{},
		Expanded: true,
	}

	// Construire récursivement
	if err := p.buildTreeRecursive(ctx, conn, root, 0, maxDepth, includeGroups); err != nil {
		return nil, err
	}

	p.getCache().SetTree(root)
	return root, nil
}

func (p *Pool) buildTreeRecursive(ctx context.Context, conn *ldap.Conn, parent *ExplorerNode, depth, maxDepth int, includeGroups bool) error {
	if depth >= maxDepth {
		return nil
	}

	// Rechercher les OU enfants
	filter := "(objectClass=organizationalUnit)"
	req := ldap.NewSearchRequest(parent.DN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false, filter,
		[]string{"dn", "ou", "description"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return fmt.Errorf("recherche OU sous %s: %w", parent.DN, err)
	}

	for _, entry := range res.Entries {
		ouName := entry.GetAttributeValue("ou")
		child := &ExplorerNode{
			DN:          entry.DN,
			Name:        ouName,
			Type:        "ou",
			Children:    []*ExplorerNode{},
			Expanded:    false,
			ParentDN:    parent.DN,
			HasChildren: true, // on suppose qu'il peut y avoir des enfants
		}
		parent.Children = append(parent.Children, child)

		// Récursif pour les sous-OU
		if err := p.buildTreeRecursive(ctx, conn, child, depth+1, maxDepth, includeGroups); err != nil {
			return err
		}
	}

	if includeGroups {
		// Groupes directement sous cette OU (pas récursif pour les groupes)
		filter := "(objectClass=group)"
		req := ldap.NewSearchRequest(parent.DN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false, filter,
			[]string{"dn", "cn", "description", "groupType"}, nil)
		res, err := conn.Search(req)
		if err != nil {
			return fmt.Errorf("recherche groupes sous %s: %w", parent.DN, err)
		}
		for _, entry := range res.Entries {
			child := &ExplorerNode{
				DN:       entry.DN,
				Name:     entry.GetAttributeValue("cn"),
				Type:     "group",
				Children: nil,
				Expanded: false,
				ParentDN: parent.DN,
			}
			parent.Children = append(parent.Children, child)
		}
	}

	return nil
}

// Search unifiée : utilisateurs, groupes, OU dans une même requête.
func (p *Pool) Search(ctx context.Context, filter SearchFilter) (*SearchResult, error) {
	// Clé de cache
	cacheKey := fmt.Sprintf("%s|%s|%d|%s|%v",
		filter.Query, strings.Join(filter.ObjectTypes, ","), filter.PageSize, filter.BaseDN, filter.IncludeDisabled)
	if filter.Cookie == "" {
		if cached := p.getCache().GetSearch(cacheKey); cached != nil {
			return cached, nil
		}
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	baseDN := filter.BaseDN
	if baseDN == "" {
		baseDN = p.cfg.BaseDN
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	// Construire le filtre LDAP
	ldapFilter := p.buildSearchFilter(filter)

	var allEntries []SearchResultEntry
	var cookie string

	// Pagination manuelle : on boucle jusqu'à avoir assez de résultats ou pas de cookie
	for {
		req := ldap.NewSearchRequest(baseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, ldapFilter,
			[]string{"dn", "objectClass", "cn", "displayName", "sAMAccountName", "ou", "userAccountControl", "mail", "description"}, nil)
		// Contrôle de pagination : première page sans cookie, pages suivantes
		// avec le cookie retourné par la requête précédente.
		if pageSize > 0 {
			ctrl := ldap.NewControlPaging(uint32(pageSize))
			if filter.Cookie != "" {
				ctrl.Cookie = []byte(filter.Cookie)
			}
			req.Controls = []ldap.Control{ctrl}
		}

		res, err := conn.Search(req)
		if err != nil {
			return nil, fmt.Errorf("recherche: %w", err)
		}

		for _, entry := range res.Entries {
			e := p.entryToSearchResult(entry)
			if e != nil {
				allEntries = append(allEntries, *e)
			}
		}

		// Extraire le cookie de pagination
		for _, ctrl := range res.Controls {
			if paging, ok := ctrl.(*ldap.ControlPaging); ok {
				cookie = string(paging.Cookie)
				break
			}
		}

		if cookie == "" || len(allEntries) >= pageSize {
			break
		}
		filter.Cookie = cookie // pour itération suivante
	}

	result := &SearchResult{
		Entries: allEntries,
		Cookie:  cookie,
		Total:   len(allEntries), // estimation locale
	}

	if filter.Cookie == "" { // pas de cache si pagination explicite
		p.getCache().SetSearch(cacheKey, result)
	}

	return result, nil
}

// buildSearchFilter construit le filtre LDAP à partir de SearchFilter.
func (p *Pool) buildSearchFilter(f SearchFilter) string {
	var parts []string

	// Types d'objets
	if len(f.ObjectTypes) > 0 {
		var typeFilters []string
		for _, t := range f.ObjectTypes {
			switch t {
			case "user":
				typeFilters = append(typeFilters, "(&(objectClass=user)(objectCategory=person))")
			case "group":
				typeFilters = append(typeFilters, "(objectClass=group)")
			case "ou":
				typeFilters = append(typeFilters, "(objectClass=organizationalUnit)")
			case "computer":
				typeFilters = append(typeFilters, "(objectClass=computer)")
			case "contact":
				typeFilters = append(typeFilters, "(objectClass=contact)")
			}
		}
		if len(typeFilters) == 1 {
			parts = append(parts, typeFilters[0])
		} else if len(typeFilters) > 1 {
			parts = append(parts, "(|"+strings.Join(typeFilters, "")+")")
		}
	} else {
		// Par défaut : users + groups + OU
		parts = append(parts, "(|(objectClass=user)(objectClass=group)(objectClass=organizationalUnit))")
	}

	// Désactivés
	if !f.IncludeDisabled {
		parts = append(parts, "(!(userAccountControl:1.2.840.113556.1.4.803:=2))")
	}

	// Texte libre
	if f.Query != "" {
		escaped := ldap.EscapeFilter(f.Query)
		parts = append(parts, fmt.Sprintf("(|(cn=*%s*)(sAMAccountName=*%s*)(displayName=*%s*)(mail=*%s*)(givenName=*%s*)(sn=*%s*))",
			escaped, escaped, escaped, escaped, escaped, escaped))
	}

	return "(&" + strings.Join(parts, "") + ")"
}

// entryToSearchResult convertit une entrée LDAP en SearchResultEntry.
func (p *Pool) entryToSearchResult(e *ldap.Entry) *SearchResultEntry {
	objClasses := e.GetAttributeValues("objectClass")
	objType := "unknown"
	for _, oc := range objClasses {
		switch strings.ToLower(oc) {
		case "user":
			objType = "user"
		case "group":
			objType = "group"
		case "organizationalunit":
			objType = "ou"
		case "computer":
			objType = "computer"
		case "contact":
			objType = "contact"
		}
	}

	uac := 512
	if v := e.GetAttributeValue("userAccountControl"); v != "" {
		fmt.Sscanf(v, "%d", &uac)
	}

	return &SearchResultEntry{
		DN:             e.DN,
		ObjectType:     objType,
		DisplayName:    e.GetAttributeValue("displayName"),
		SAMAccountName: e.GetAttributeValue("sAMAccountName"),
		OU:             parentDN(e.DN),
		Enabled:        (uac & 2) == 0,
		Email:          e.GetAttributeValue("mail"),
		Description:    e.GetAttributeValue("description"),
	}
}

// GetSubTree retourne les enfants d'un nœud (lazy loading pour l'UI).
func (p *Pool) GetSubTree(ctx context.Context, parentDN string, includeGroups bool) ([]*ExplorerNode, error) {
	// Cache AD SQLite (T2) : sert l'arborescence sans requête LDAP si frais.
	if p.cacheAD != nil && !p.cacheAD.IsStale() {
		if nodes, err := p.cacheAD.EnfantsExplorateur(parentDN, includeGroups); err == nil && len(nodes) > 0 {
			return nodes, nil
		}
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	var nodes []*ExplorerNode

	// OU enfants
	filter := "(objectClass=organizationalUnit)"
	req := ldap.NewSearchRequest(parentDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false, filter,
		[]string{"dn", "ou", "description"}, nil)
	res, err := conn.Search(req)
	if err == nil {
		for _, entry := range res.Entries {
			nodes = append(nodes, &ExplorerNode{
				DN:          entry.DN,
				Name:        entry.GetAttributeValue("ou"),
				Type:        "ou",
				Children:    nil,
				Expanded:    false,
				ParentDN:    parentDN,
				HasChildren: true,
			})
		}
	}

	if includeGroups {
		filter := "(objectClass=group)"
		req := ldap.NewSearchRequest(parentDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false, filter,
			[]string{"dn", "cn", "description"}, nil)
		res, err := conn.Search(req)
		if err == nil {
			for _, entry := range res.Entries {
				nodes = append(nodes, &ExplorerNode{
					DN:       entry.DN,
					Name:     entry.GetAttributeValue("cn"),
					Type:     "group",
					Children: nil,
					Expanded: false,
					ParentDN: parentDN,
				})
			}
		}
	}

	return nodes, nil
}

// InvalidateCache invalide le cache explorateur (à appeler après toute écriture AD).
func (p *Pool) InvalidateCache() {
	if p.cache != nil {
		p.getCache().Invalidate()
	}
}
