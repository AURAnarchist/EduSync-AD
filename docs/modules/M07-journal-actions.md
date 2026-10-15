# Module M7 — Journal d'actions

> **Objectif** : Filtres date/type/résultat/domaine/opérateur → pagination → export CSV → stockage SQLite append-only

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Liste 50 dernières | `src/edusync_ad/core/journal.py` | Page d'accueil dashboard |
| Filtres combinés | `src/edusync_ad/core/journal.py` | Date (depuis/jusqu'à), type action, résultat (succès/échec), domaine (M25), opérateur (M26) |
| Pagination | `src/edusync_ad/core/journal.py` | 50/page, navigation |
| Export CSV | `src/edusync_ad/core/export.py` | Résultats filtrés → CSV |
| Append-only SQLite | `src/edusync_ad/core/journal.py` | Table `actions` + `pending_deletions`, pas de DELETE/UPDATE |
| Détails entrée | `src/edusync_ad/plugins/journal/` | Modal : timestamp, user, action, target, résultat, détails JSON |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/audit/
├── audit.go               # EXISTE : Record, Filter, Export (compléter Filter)
├── models.go              # EXISTE : AuditEntry, AuditFilter

internal/models/
├── audit.go               # EXISTE : Déjà défini
```

### UI (`internal/ui/`)
```
internal/ui/
├── audit.go               # NOUVEAU : Handlers auditList, auditFilter, auditExport
├── server.go              # MODIFIER : Routes /audit/*
└── i18n.go                # MODIFIER : Clés fr/en journal
```

### Templates (`web/templates/`)
```
web/templates/
├── audit.html               # NOUVEAU : Page journal (filtres + tableau HTMX)
├── audit_results.html       # NOUVEAU : Tableau résultats (partial HTMX)
└── partials/
    ├── audit_filter_form.html    # NOUVEAU (formulaire filtres)
    ├── audit_row.html            # NOUVEAU (ligne tableau)
    └── audit_detail_modal.html   # NOUVEAU (modal détails)
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── audit.go                 # MODIFIER : Étendre (filtres, export, JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Backend `internal/audit` | ✅ Record, schema, liste 50 entrées |
| Filtres `auditFilter` | ⬜ **TODO** |
| Export CSV filtré | ⬜ TODO |
| Page `audit.html` | ⬜ **Absente** |
| Partial `audit_results.html` | ⬜ Absent |
| Routes filtres/export | ⬜ Absentes |
| CLI étendu | ⬜ Partiel (list seulement) |
| i18n | ⚠️ Partiel (liste seulement) |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/audit/filter_test.go` + `internal/ui/audit_test.go`
2. **Backend Filter** : `Filter(ctx, filter)` → paginated `[]AuditEntry` + total
   - Filtres : `Since`, `Until`, `ActionTypes[]`, `Results[]`, `Domains[]`, `Operators[]`, `TextSearch` (détails)
   - Index SQLite : `CREATE INDEX idx_audit_date ON actions(timestamp)`, `idx_audit_type`, `idx_audit_domain`, `idx_audit_operator`
3. **Backend Export** : `ExportCSV(ctx, filter)` → streaming writer
4. **Template filtres** : `audit_filter_form.html` — date picker, multi-select types/résultats/domaines/opérateurs, recherche texte
5. **Template principal** : `audit.html` — filtres (gauche/repliable) + résultats (droite, HTMX)
6. **Template résultats** : `audit_results.html` — tableau paginé HTMX, colonnes : Date, Type, Opérateur, Cible, Résultat, Domaine, Détail (clic → modal)
7. **Modal détails** : `audit_detail_modal.html` — JSON formaté, liens vers objets AD si résolvables
6. **Routes** : `GET /audit`, `GET /audit/results` (HTMX), `POST /audit/filter`, `GET /audit/export`
7. **CLI** : `edusync-cli audit list --since "2026-01-01" --type create --result success --domain EDUSYNC.LOCAL --operator admin --json`, `audit export --output file.csv`
8. **i18n** : Clés `audit.title`, `audit.filter.date`, `audit.filter.type`, `audit.filter.result`, `audit.filter.domain`, `audit.filter.operator`, `audit.export`, `audit.details` (fr+en)
9. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Filtres combinés : date + type + résultat + domaine + opérateur + texte → résultats corrects
- [ ] Pagination 50/page fonctionne (navigation + total)
- [ ] Export CSV résultats filtrés → fichier valide, colonnes complètes
- [ ] Modal détails : JSON lisible, timestamp formaté, opérateur, domaine, résultat
- [ ] Performance : filtre sur 10k entrées < 200ms (index SQLite)
- [ ] Append-only : aucune API DELETE/UPDATE sur table `actions`
- [ ] CLI : filtres + export + JSON sorties structurées
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/audit/... ./internal/ui/... -run Audit`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/audit/... -run Filter -v
go test ./internal/ui/... -run Audit -v

# Test manuel
task ad:start
task dev
# UI : Journal → Filtres combinés → Résultats → Export CSV
# Vérifier : performance, pagination, modal détails
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/journal.py` : Logique filtres, export, append-only
- `src/edusync_ad/plugins/journal/` : Plugin UI PyQt6
- `tests/core/test_journal.py` : Tests Python