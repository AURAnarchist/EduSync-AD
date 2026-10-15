# Module M2 — Migration / Restructuration

> **Objectif** : Via CSV (prénom/nom/équipe) ou interface (sélection OU) → déplacement AD + groupes auto → résolution par prénom/nom

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Import CSV migration | `src/edusync_ad/core/migration.py` | Colonnes : `prenom;nom;equipe` (équipe = classe/service cible) |
| Sélection OU interface | `src/edusync_ad/plugins/migration/` | Arborescence OU, sélection multiple, drag-drop |
| Plan de migration | `src/edusync_ad/core/migration.py` | Résolution prénom/nom → DN source → OU cible + groupes |
| Groupes de classe auto | `src/edusync_ad/core/groupes.py` | Création `Classe_X_Y`, ajout membres, gestion professeurs |
| Simulation (dry-run) | `src/edusync_ad/core/simulation.py` | Preview déplacements + groupes avant validation |
| Exécution | `src/edusync_ad/core/migration.py` | Move AD + groupes + audit |
| Résolution conflits | `src/edusync_ad/core/migration.py` | Homonymes, comptes introuvables, OU cible inexistante |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── migration.go         # MODIFIER : Plan, Simulate, Execute
├── group.go             # EXISTE : CRUD groupes (réutiliser pour groupes auto)

internal/models/
├── migration.go         # NOUVEAU : MigrationPlan, MigrationRow, SimulatedMove
```

### UI (`internal/ui/`)
```
internal/ui/
├── migration.go         # MODIFIER : Handlers migrationPlan, migrationSimulate, migrationExecute
├── server.go            # MODIFIER : Routes /migration/*
└── i18n.go              # MODIFIER : Clés fr/en migration
```

### Templates (`web/templates/`)
```
web/templates/
├── migration.html              # EXISTE (page sélection mode CSV/OU)
├── migration_plan.html         # NOUVEAU : Plan depuis CSV (table editable)
├── migration_simulate.html     # NOUVEAU : Preview déplacements + groupes
├── migration_execute.html      # NOUVEAU : Résultat exécution
└── partials/
    ├── migration_csv_import.html    # NOUVEAU
    ├── migration_ou_selector.html   # NOUVEAU (arborescence HTMX)
    ├── migration_plan_table.html    # NOUVEAU
    └── migration_result.html        # NOUVEAU
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── migration.go         # NOUVEAU : plan, simulate, execute (JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Page migration (`migration.html`) | ✅ Existe (sélection mode CSV/OU) |
| Routes de base | ✅ Existent |
| i18n de base | ✅ Existe |
| Plan depuis CSV (`migrationPlan`) | ⬜ TODO |
| Simulation (`migrationSimulate`) | ⬜ TODO |
| Exécution (`migrationExecute`) | ⬜ TODO |
| Groupes auto (création + membres) | ⬜ TODO |
| Gabarit plan | ⬜ Absent |
| Gabarit simulation | ⬜ Absent |
| Gabarit exécution | ⬜ Absent |
| Sélecteur OU arborescence | ⬜ Absent |
| Commande CLI | ⬜ Absente |
| i18n complet | ⚠️ Partiel |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/ad/migration_test.go` + `internal/ui/migration_test.go`
2. **Backend Plan** : `BuildMigrationPlan(ctx, csvData)` → `MigrationPlan{Rows[], TargetOU, Groups[]}`
   - Résolution prénom/nom → recherche AD (samAccountName, userPrincipalName, mail)
   - Détection homonymes → proposition résolution
   - Détermination OU cible depuis `equipe` (mapping config) ou sélection UI
3. **Backend Simulate** : `SimulateMigration(ctx, plan)` → `SimulatedMove{User, FromOU, ToOU, Groups[], Conflicts[]}`
   - Dry-run complet : move AD + création groupes + ajout membres
   - Détection conflits : OU cible inaccessible, groupes existants, permissions
4. **Backend Execute** : `ExecuteMigration(ctx, simulated)` → écriture AD pool + audit
   - Transaction-like : move user → créer groupes → ajouter membres → rollback partiel si erreur
5. **Templates** : 3 gabarits + partials (table editable HTMX, arborescence OU via `explorer_tree` partial)
6. **Routes** : `POST /migration/plan`, `POST /migration/simulate`, `POST /migration/execute`
7. **i18n** : Clés `migration.plan.title`, `migration.simulate.preview`, `migration.execute.result`, `migration.groups.auto`, `migration.conflict.homonym` (fr + en)
8. **CLI** : `edusync-cli migration plan --csv file.csv`, `migrate --simulate`, `migrate --execute`
9. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Import CSV 5 classes (25 élèves/classe) → plan affiche tous les moves + groupes à créer
- [ ] Homonymes détectés → proposition résolution (numérotation, initiale prénom)
- [ ] Comptes introuvables → marqués « Ignoré » avec raison
- [ ] Simulation → preview exacte (move + groupes créés + membres ajoutés) modifiable
- [ ] Exécution → comptes déplacés, groupes créés/ajoutés, audit journalisé
- [ ] Sélection OU interface → arborescence fonctionnelle, sélection multiple
- [ ] CLI fonctionne : `plan`, `simulate`, `execute` avec sorties JSON
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Migration`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Migration -v
go test ./internal/ui/... -run Migration -v

# Test manuel
task ad:start
task dev
# UI : Migration → Import CSV exemples/migration_restruct.csv
# Vérifier : plan → simulation → exécution → vérif AD (move + groupes) → journal
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/migration.py` : Logique plan/simulation/exécution
- `src/edusync_ad/core/groupes.py` : Groupes de classe auto
- `src/edusync_ad/plugins/migration/` : Plugin UI PyQt6
- `tests/core/test_migration.py` : Tests Python