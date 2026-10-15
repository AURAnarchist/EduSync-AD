# Module M1 — Création de comptes / Arrivées

> **Objectif** : Import CSV → génération identifiants/MDP/mail auto → résolution doublons → vérification AD existants → export CSV étiquettes

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Import CSV (encodage + séparateur auto) | `src/edusync_ad/core/arrivees.py` | Détection `;` / `,`, UTF-8 / ISO-8859-1 |
| Génération identifiants | `src/edusync_ad/core/identifiants.py` | Format configurable (ex: `prenom.nom`, `pnom`, `prenom.nom2`) |
| Génération MDP | `src/edusync_ad/core/mdp.py` | Politique élèves/personnels, complexité, exclusion caractères ambigus |
| Génération mail | `src/edusync_ad/core/mail.py` | Domaine configurable, résolution conflits |
| Résolution doublons | `src/edusync_ad/core/doublons.py` | Par prénom/nom, suggestions auto (numérotation, seconde initiale) |
| Vérification AD existants | `src/edusync_ad/core/arrivees.py` | Recherche par `userPrincipalName`, `samAccountName`, `mail` |
| Export CSV étiquettes | `src/edusync_ad/core/export.py` | Prêt à imprimer (colonnes : identifiant, MDP, mail, classe) |
| Mode simulation | `src/edusync_ad/core/simulation.py` | Preview modifiable avant validation |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── user.go              # MODIFIER : Add Simulate/Execute pour création
├── simulation.go        # EXISTE : Utiliser pattern Validate→Simulate→Execute→Audit

internal/models/
├── arrival.go           # NOUVEAU : ArrivalRow, ImportResult, SimulatedUser
```

### UI (`internal/ui/`)
```
internal/ui/
├── arrivals.go          # NOUVEAU : Handlers usersImport, usersSimulate, usersExecute
├── server.go            # MODIFIER : Routes /arrivals/*
└── i18n.go              # MODIFIER : Clés fr/en arrivées
```

### Templates (`web/templates/`)
```
web/templates/
├── arrivals.html           # EXISTE (page import)
├── users_simulate.html     # NOUVEAU : Preview comptes à créer (table editable HTMX)
├── users_execute.html      # NOUVEAU : Résultat exécution + export étiquettes
└── partials/
    ├── arrival_import_form.html   # EXISTE (réutiliser)
    ├── arrival_simulate_table.html # NOUVEAU
    └── arrival_execute_result.html # NOUVEAU
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── arrivals.go          # NOUVEAU : import, simulate, execute (JSON output)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Page import CSV (`arrivals.html`) | ✅ Fonctionnelle |
| Import réel (`usersImport`) | ✅ Parsing + validation |
| Simulation (`usersSimulate`) | ⬜ TODO |
| Exécution (`usersExecute`) | ⬜ TODO |
| Gabarit simulation | ⬜ Absent |
| Gabarit exécution | ⬜ Absent |
| Résolution doublons | ⬜ TODO (backend) |
| Export étiquettes CSV | ⬜ TODO |
| Commande CLI | ⬜ Absente |
| i18n fr/en | ⚠️ Partiel (page import seulement) |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : Écrire `internal/ad/user_simulate_test.go` + `internal/ui/arrivals_test.go`
2. **Backend** : Implémenter `SimulateCreateUsers(ctx, csvData)` → `[]SimulatedUser` (doublons, AD existants, MDP générés)
3. **Backend** : Implémenter `ExecuteCreateUsers(ctx, simulatedUsers)` → écriture AD pool + audit + retour résultats
4. **Template** : `users_simulate.html` — tableau HTMX editable (colonnes : prénom, nom, classe, identifiant, MDP, mail, action)
5. **Template** : `users_execute.html` — résultat (créés, erreurs, ignorés) + bouton « Exporter étiquettes CSV »
6. **Routes** : `POST /arrivals/simulate`, `POST /arrivals/execute` dans `internal/ui/arrivals.go`
7. **i18n** : Clés `arrivals.simulate.title`, `arrivals.execute.success`, `arrivals.duplicate`, `arrivals.export_labels` (fr + en)
8. **CLI** : `edusync-cli arrivals import --csv file.csv --simulate`, `--execute`, `--output json`
9. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Import CSV 50 comptes → simulation affiche tous les comptes avec identifiants/MDP/mail générés
- [ ] Doublons détectés (même prénom/nom) → proposition résolution (numérotation, initiale)
- [ ] Comptes existants AD détectés → marqués « Ignoré » avec raison
- [ ] Simulation modifiable en ligne (HTMX) → identifiant/MDP/mail éditables
- [ ] Exécution → comptes créés dans AD, groupes par défaut appliqués, MDP dans coffre, audit journalisé
- [ ] Export CSV étiquettes depuis page exécution (colonnes : identifiant, MDP, mail, classe)
- [ ] CLI fonctionne : `import --simulate` → JSON preview, `import --execute` → JSON résultats
- [ ] i18n fr/en complet sur toutes clés ajoutées
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Arrival`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run SimulateCreateUsers -v
go test ./internal/ui/... -run Arrivals -v

# Test manuel avec AD test
task ad:start
task dev
# UI : http://localhost:34115 → Arrivées → Import CSV exemples/eleves.csv
# Vérifier : simulation → modification → exécution → étiquettes → journal
```

---

## Fichiers Python de référence (pour parité)

- `src/edusync_ad/core/arrivees.py` : Logique principale import/création
- `src/edusync_ad/core/identifiants.py` : Génération identifiants
- `src/edusync_ad/core/mdp.py` : Génération MDP + politiques
- `src/edusync_ad/core/mail.py` : Génération mail
- `src/edusync_ad/core/doublons.py` : Résolution doublons
- `src/edusync_ad/core/simulation.py` : Pattern simulation
- `src/edusync_ad/plugins/arrivees/` : Plugin UI PyQt6
- `tests/core/test_arrivees.py` : Tests Python (référence comportements)