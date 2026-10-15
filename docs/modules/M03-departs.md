# Module M3 — Gestion des départs

> **Objectif** : Désactivation immédiate ou archivage + purge planifiée (délai configurable, 30j par défaut) → résolution par prénom/nom

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Désactivation immédiate | `src/edusync_ad/core/departs.py` | `userAccountControl` |= 2 (ACCOUNTDISABLE) |
| Archivage (move vers OU Archive) | `src/edusync_ad/core/departs.py` | Déplacement vers OU `Archive` + désactivation |
| Purge planifiée | `src/edusync_ad/core/departs.py` | Table `pending_deletions` : DN, date_purge, raison → job quotidien |
| Délai configurable | `src/edusync_ad/core/config.py` | Paramètre `depart_purge_delay_days` (défaut 30) |
| Résolution par prénom/nom | `src/edusync_ad/core/departs.py` | Identique migration : `givenName`+`sn` → DN |
| Import CSV départs | `src/edusync_ad/core/departs.py` | Colonnes : `prenom;nom;action` (disable/archive/purge) |
| Mode simulation | `src/edusync_ad/core/simulation.py` | Preview comptes à désactiver/archiver/purger |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── depart.go              # NOUVEAU : Disable, Archive, Purge
├── simulation.go          # EXISTE : Pattern réutilisé

internal/models/
├── depart.go              # EXISTE : DepartRow (déjà défini)
```

### UI (`internal/ui/`)
```
internal/ui/
├── departs.go             # NOUVEAU : Handlers departList, departSimulate, departExecute
├── server.go              # MODIFIER : Routes /departs/*
└── i18n.go                # MODIFIER : Clés fr/en départs
```

### Templates (`web/templates/`)
```
web/templates/
├── departs.html           # NOUVEAU : Page principale (sélection mode CSV/OUI/prénom-nom)
├── depart_simulate.html   # NOUVEAU : Preview actions
├── depart_execute.html    # NOUVEAU : Résultat
└── partials/
    ├── depart_import_form.html    # NOUVEAU
    ├── depart_ou_selector.html    # NOUVEAU
    └── depart_result.html         # NOUVEAU
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── departs.go             # NOUVEAU : list, disable, archive, purge (JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Modèle `DepartRow` | ✅ Existe dans `internal/models/` |
| Backend `depart.go` | ⬜ **Module absent** |
| Page UI | ⬜ Absente |
| Routes | ⬜ Absentes |
| Gabarits | ⬜ Absents |
| CLI | ⬜ Absente |
| i18n | ⬜ Absent |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/ad/depart_test.go` + `internal/ui/departs_test.go`
2. **Backend** : `internal/ad/depart.go` :
   - `DisableUsers(ctx, userDNs)` → désactivation AD
   - `ArchiveUsers(ctx, userDNs, archiveOUDN)` → move + désactivation
   - `SchedulePurge(ctx, userDNs, delayDays)` → insert `pending_deletions`
   - `RunPurge(ctx)` → supprimer comptes `pending_deletions` où date_purge ≤ now (idempotent)
3. **Template principal** : `departs.html` — 3 onglets : CSV / OU / Prénom-Nom, mode (désactiver/archiver/purger), délai
4. **Template simulation** : `depart_simulate.html` — preview (comptes trouvés, action, OU cible)
5. **Template exécution** : `depart_execute.html` — résultats + lien purge différée
6. **Routes** : `GET /departs`, `POST /departs/simulate`, `POST /departs/execute`, `POST /departs/purge`
6. **i18n** : Clés `departs.title`, `departs.mode.disable`, `departs.mode.archive`, `departs.mode.purge`, `departs.delay`, `departs.simulate.preview` (fr+en)
7. **CLI** : `edusync-cli departs list`, `departs disable --csv file.csv`, `departs archive --ou ...`, `departs purge --dry-run`
8. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] CSV 10 départs → simulation affiche comptes trouvés + action + OU cible
- [ ] Comptes introuvables → marqués « Ignoré » avec raison
- [ ] Désactivation → compte disabled AD, audit journalisé
- [ ] Archivage → compte moved vers OU Archive + disabled, audit
- [ ] Purge planifiée → entrée `pending_deletions` avec date = now + 30j, audit
- [ ] Job purge (CLI `purge`) → supprime comptes dus, audit, idempotent (relance = OK)
- [ ] CLI : `list` (pending_deletions), `disable`, `archive`, `purge --dry-run`
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Depart`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Depart -v
go test ./internal/ui/... -run Departs -v

# Test manuel
task ad:start
task dev
# UI : Départs → Import CSV exemples/departs.csv
# Vérifier : simulation → exécution → vérif AD (disabled/moved) → journal
# Purge : edusync-cli departs purge --dry-run (puis --execute)
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/departs.py` : Logique disable/archive/purge
- `src/edusync_ad/core/pending_deletions.py` : Table purge planifiée
- `src/edusync_ad/plugins/departs/` : Plugin UI PyQt6
- `tests/core/test_departs.py` : Tests Python