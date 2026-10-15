# Module M5 — Explorateur AD

> **Objectif** : Arborescence OUs/groupes, panneau central unifié (utilisateurs + groupes + sous-OU), clic droit complet, recherche temps réel

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Arborescence OUs | `src/edusync_ad/core/explorateur.py` | TreeView OUs + groupes, lazy loading |
| Panneau central | `src/edusync_ad/core/explorateur.py` | Liste unifiée users + groups + sous-OUs |
| Recherche temps réel | `src/edusync_ad/core/recherche.py` | Debounce 300ms, filtres (type, OU, attributs) |
| Clic droit complet | `src/edusync_ad/plugins/explorateur/` | Modifier, Déplacer, MDP, Groupes, Supprimer |
| Pagination | `src/edusync_ad/core/explorateur.py` | 100 entrées/page, navigation |
| Colonnes configurables | `src/edusync_ad/core/explorateur.py` | Afficher/masquer : DN, mail, téléphone, etc. |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── explorer.go            # MODIFIER : Search, BuildTree (TODO → implémenter)
├── user.go                # EXISTE : CRUD (réutiliser pour actions clic droit)
├── group.go               # EXISTE : CRUD groupes
├── ou.go                  # EXISTE : CRUD OUs
├── cache.go               # NOUVEAU (T2) : Cache SQLite pour fluidité

internal/models/
├── explorer.go            # NOUVEAU : ExplorerNode, SearchResult, TreeOptions
```

### UI (`internal/ui/`)
```
internal/ui/
├── explorer.go            # NOUVEAU : Handlers explorerTree, explorerSearch, explorerActions
├── server.go              # MODIFIER : Routes /explorer/*
└── i18n.go                # MODIFIER : Clés fr/en explorateur
```

### Templates (`web/templates/`)
```
web/templates/
├── explorer.html              # NOUVEAU : Page principale (split view HTMX)
├── explorer_tree.html         # NOUVEAU : Arborescence OU/groupes (partial HTMX)
├── explorer_results.html      # NOUVEAU : Panneau central (partial HTMX)
├── explorer_detail.html       # NOUVEAU : Détail objet (modal HTMX)
└── partials/
    ├── explorer_tree_node.html     # NOUVEAU (récursif)
    ├── explorer_result_row.html    # NOUVEAU
    ├── explorer_context_menu.html  # NOUVEAU (clic droit Alpine)
    └── explorer_search.html        # NOUVEAU (barre recherche)
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── explorer.go              # NOUVEAU : search, tree, detail (JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Routes de base | ✅ Existent |
| i18n de base | ✅ Existe |
| Backend `explorer.go` | 🔨 Squelette (Search/BuildTree = TODO) |
| Gabarits | ⬜ **3 gabarits absents** |
| Handlers UI | ⬜ Absents |
| Cache AD (T2) | ⬜ Lot B |
| Clic droit actions | ⬜ À faire (réutiliser M1/M2/M3/M4) |
| Commande CLI | ⬜ Absente |
| i18n complet | ⚠️ Partiel |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/ad/explorer_test.go` + `internal/ui/explorer_test.go`
2. **Backend Search** : `pool.Search(ctx, filter, page, pageSize)` → pagination LDAP + cache fallback
3. **Backend BuildTree** : `pool.BuildTree(ctx, rootOUDN, depth)` → arborescence OUs + count groupes/users
4. **Template principal** : `explorer.html` — layout split (arbre gauche 30%, résultats droite 70%)
5. **Template arbre** : `explorer_tree.html` — récursif HTMX, lazy load enfants au clic
6. **Template résultats** : `explorer_results.html` — tableau paginé, tri, filtres temps réel (Alpine debounce)
7. **Template détail** : `explorer_detail.html` — modal HTMX au clic sur ligne
8. **Menu contextuel** : `explorer_context_menu.html` — actions selon type (user/group/ou)
   - User : Modifier, Déplacer, Reset MDP, Groupes, Désactiver, Supprimer
   - Groupe : Modifier, Membres, Déplacer, Supprimer
   - OU : Nouvelle OU, Déplacer, Supprimer, Propriétés
9. **Actions clic droit** : Routes réutilisant M1 (création), M2 (move), M4 (MDP), M3 (départs)
10. **Recherche** : `GET /explorer/search?q=...&type=user|group|ou&ou=...` → HTMX swap résultats
11. **Routes** : `GET /explorer`, `GET /explorer/tree`, `GET /explorer/search`, `GET /explorer/detail/:dn`
12. **i18n** : Clés `explorer.title`, `explorer.search`, `explorer.tree`, `explorer.context.*` (fr+en)
13. **CLI** : `edusync-cli explorer search --query "martin" --type user`, `explorer tree --ou "DC=..."`, `explorer detail --dn "CN=..."`
14. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Arborescence charge < 500ms (cache T2) / < 2s (LDAP direct)
- [ ] Navigation fluide 10k+ objets (pagination + cache)
- [ ] Recherche temps réel (debounce 300ms) → résultats < 200ms
- [ ] Clic droit : toutes actions fonctionnent (simulation → exécution → audit)
- [ ] Pagination résultats (100/page) + tri colonnes
- [ ] Détail objet modal : attributs complets, actions rapides
- [ ] CLI : search/tree/detail sorties JSON structurées
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Explorer`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Explorer -v
go test ./internal/ui/... -run Explorer -v

# Test manuel
task ad:start
task dev
# UI : Explorateur → navigation arbre → recherche → clic droit actions
# Vérifier : fluidité, pagination, dry-run actions
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/explorateur.py` : Logique arbre + panneau central
- `src/edusync_ad/core/recherche.py` : Recherche temps réel
- `src/edusync_ad/plugins/explorateur/` : Plugin UI PyQt6
- `tests/core/test_explorateur.py` : Tests Python