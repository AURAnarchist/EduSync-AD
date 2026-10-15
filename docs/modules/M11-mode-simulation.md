# Module M11 — Mode simulation

> **Objectif** : Toute écriture AD passe par prévisualisation modifiable → `Validate → Simulate → Execute → Audit` (pattern universel)

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Pattern universel | `src/edusync_ad/core/simulation.py` | `SimulationEngine` : Validate → Simulate → Execute → Audit |
| Prévisualisation modifiable | `src/edusync_ad/plugins/*/` | Chaque module : tableau résultats editable avant validation |
| Diff AD | `src/edusync_ad/core/simulation.py` | Comparaison état actuel vs prévu (création, modification, suppression) |
| Validation entrée | `src/edusync_ad/core/simulation.py` | Schémas par module, erreurs bloquantes vs avertissements |
| Annulation | `src/edusync_ad/core/simulation.py` | Bouton « Annuler » à toute étape, pas d'écriture AD |
| Journalisation | `src/edusync_ad/core/journal.py` | Type `simulation` + `execution` liés par `simulation_id` |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── simulation.go            # EXISTE : Pattern de base (SimulationResult, Diff, Preview)
│                            # MODIFIER : Étendre pour tous modules (M1-M8)

internal/models/
├── simulation.go            # NOUVEAU : SimulationContext, ValidationResult, ExecutionResult
```

### UI (`internal/ui/`)
```
internal/ui/
├── simulation.go            # NOUVEAU : Middleware/handlers communs simulation
├── server.go                # MODIFIER : Routes simulation génériques
└── i18n.go                  # MODIFIER : Clés fr/en simulation communes
```

### Templates (`web/templates/`)
```
web/templates/
└── partials/
    ├── simulation_preview_table.html  # NOUVEAU (tableau editable générique)
    ├── simulation_diff_row.html       # NOUVEAU (ligne diff : actuel → prévu)
    ├── simulation_actions.html        # NOUVEAU (boutons Modifier/Confirmer/Annuler)
    └── simulation_progress.html       # NOUVEAU (barre exécution + annulation)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Pattern `simulation.go` | ✅ Existe (types de base) |
| Intégration M1 (Arrivées) | ⬜ TODO (A2) |
| Intégration M2 (Migration) | ⬜ TODO (A4) |
| Intégration M3 (Départs) | ⬜ TODO (A8) |
| Intégration M4 (MDP) | ⬜ TODO (A3) |
| Intégration M5 (Explorateur) | ⬜ Actions clic droit |
| Intégration M6 (Export) | ⬜ N/A (pas d'écriture AD) |
| Intégration M7 (Journal) | ⬜ N/A (lecture seule) |
| Intégration M8 (Paramètres) | ⚠️ Sauvegarde config = écriture (simulation simple) |
| Middleware commun | ⬜ Absent |
| Partials génériques | ⬜ Absents |
| i18n communes | ⚠️ Partiel |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre — A10)

1. **Test d'abord** : `internal/ad/simulation_integration_test.go` (teste pattern sur M1-M4)
2. **Modèles unifiés** : `internal/models/simulation.go` — `SimulationContext{Module, Operator, Criteria, Previews[]}`, `ValidationResult{Valid, Errors[], Warnings[]}`, `ExecutionResult{Success, Affected[], Errors[], AuditIDs[]}`
3. **Middleware simulation** : `internal/ui/simulation.go` — `SimulationMiddleware(next)` :
   - Intercepte POST d'écriture AD
   - Si header `X-Simulation: true` → lance `Simulate()` → retourne preview HTMX
   - Sinon → attend confirmation → lance `Execute()` → audit
4. **Partials génériques** :
   - `simulation_preview_table.html` — colonnes dynamiques selon module, cellules editables (HTMX `hx-post="/simulation/update-cell"`)
   - `simulation_diff_row.html` — diff visuel (rouge/vert) : actuel → prévu
   - `simulation_actions.html` — « Modifier » (retour preview), « Confirmer » (execute), « Annuler »
   - `simulation_progress.html` — barre progression exécution + bouton « Annuler » (context cancellation)
5. **Intégration modules** (A2, A3, A4, A8) :
   - Chaque handler écriture AD : `Validate()` → si invalide → 422 + erreurs
   - `Simulate()` → retourne `SimulationResult{Previews[], Diffs[]}`
   - UI affiche preview via partials génériques
   - Utilisateur modifie/confirme → `Execute()` → audit
6. **Annulation exécution** : `context.WithCancel` propagé → bouton « Annuler » → `cancel()` → rollback partiel si possible
7. **Journalisation liée** : `AuditEntry.SimulationID` → lier simulation + exécution
8. **i18n communes** : `simulation.preview`, `simulation.diff.added`, `simulation.diff.modified`, `simulation.diff.removed`, `simulation.action.modify`, `simulation.action.confirm`, `simulation.action.cancel`, `simulation.execute.progress`, `simulation.execute.cancelled` (fr+en)
9. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] **Tous** modules M1-M8 : écriture AD → simulation obligatoire (pas de bypass possible)
- [ ] Preview modifiable : cellules editables, modifications reprises à l'exécution
- [ ] Diff visuel clair : ajouts (vert), modifications (jaune), suppressions (rouge)
- [ ] Boutons : Modifier (retour preview), Confirmer (execute), Annuler (retour liste)
- [ ] Progression exécution : barre temps réel, bouton Annuler fonctionnel
- [ ] Annulation : stoppe exécution, rollback partiel, audit `cancelled`
- [ ] Journal : entrée `simulation` + entrée `execution` avec même `SimulationID`
- [ ] i18n fr/en sur toutes clés communes
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Simulation`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Simulation -v
go test ./internal/ui/... -run Simulation -v

# Test manuel
task ad:start
task dev
# UI : Arrivées → Import CSV → Simulation (modifiable) → Confirmer → Exécution → Journal
# Vérifier : diff affiché, modification preview possible, annulation marche
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/simulation.py` : SimulationEngine, pattern universel
- `src/edusync_ad/plugins/*/` : Chaque plugin implémente Validate/Simulate/Execute
- `tests/core/test_simulation.py` : Tests Python