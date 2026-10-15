# Module M4 — Réinitialisation de mot de passe

> **Objectif** : Par OU, groupe AD ou CSV → politiques différées (élèves/personnels) → forçage changement prochaine connexion → export CSV

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Reset par OU | `src/edusync_ad/core/mdp.py` | Tous users dans OU + sous-OU |
| Reset par groupe AD | `src/edusync_ad/core/mdp.py` | Membres directs + imbriqués |
| Reset par CSV | `src/edusync_ad/core/mdp.py` | Colonnes : `prenom;nom` (résolution prénom/nom) |
| Politiques MDP | `src/edusync_ad/core/politiques_mdp.py` | Élèves : 8 car, 90j, pas complexe / Personnels : 12 car, 180j, complexe |
| Génération MDP | `src/edusync_ad/core/mdp.py` | Aléatoire sécurisé, exclure ambigus, stocker dans coffre |
| Forçage changement | `src/edusync_ad/core/mdp.py` | `pwdLastSet = 0` → changement obligatoire prochaine connexion |
| Export CSV résultats | `src/edusync_ad/core/export.py` | Colonnes : identifiant, nouveau MDP, mail, OU |
| Mode simulation | `src/edusync_ad/core/simulation.py` | Preview MDP générés avant validation |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── password.go            # MODIFIER : Simulate + Execute
├── simulation.go          # EXISTE : Pattern réutilisé

internal/models/
├── password.go            # NOUVEAU : PasswordPolicy, ResetPlan, ResetResult
```

### UI (`internal/ui/`)
```
internal/ui/
├── password.go            # MODIFIER : Handlers passwordSimulate, passwordExecute
├── server.go              # MODIFIER : Routes /password/*
└── i18n.go                # MODIFIER : Clés fr/en MDP
```

### Templates (`web/templates/`)
```
web/templates/
├── password.html            # EXISTE (page sélection mode + options)
├── password_simulate.html   # NOUVEAU : Preview MDP générés (table)
├── password_execute.html    # NOUVEAU : Résultat + export CSV
└── partials/
    ├── password_policy_selector.html  # NOUVEAU (Élèves/Personnels/Personnalisé)
    ├── password_csv_import.html       # NOUVEAU
    ├── password_ou_selector.html      # NOUVEAU
    └── password_group_selector.html   # NOUVEAU
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── password.go              # NOUVEAU : reset (OU/groupe/CSV) simulate/execute (JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Page `password.html` | ✅ Gabarit + routes + i18n |
| Backend `password.go` | 🔨 Partiel (structure existante) |
| Simulation `passwordSimulate` | ⬜ TODO |
| Exécution `passwordExecute` | ⬜ TODO |
| Gabarit simulation | ⬜ Absent |
| Gabarit exécution | ⬜ Absent |
| Politiques MDP | ⚠️ Modèle existe, handlers TODO |
| Coffre MDP | ✅ `internal/crypto/vault.go` (existant) |
| Commande CLI | ⬜ Absente |
| i18n | ⚠️ Partiel |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/ad/password_test.go` + `internal/ui/password_test.go`
2. **Backend Simulate** : `SimulatePasswordReset(ctx, criteria, policy)` → `[]ResetPreview{UserDN, CurrentSAM, NewPassword, Policy, Email}`
   - Résolution critères (OU, groupe, CSV) → liste users
   - Génération MDP selon politique (élèves/personnels/custom)
   - Vérification politique domaine (complexité, historique, longueur)
3. **Backend Execute** : `ExecutePasswordReset(ctx, previews)` → `unicodePwd` AD + coffre + `pwdLastSet=0` + audit
   - Stockage coffre : `vault.Store("pwd:"+sam, newPassword)`
   - Forçage changement : `pwdLastSet = 0`
4. **Template simulation** : `password_simulate.html` — tableau avec colonnes : Prénom, Nom, Identifiant, Nouveau MDP, Politique, Action (editable)
5. **Template exécution** : `password_execute.html` — résultats + bouton « Exporter CSV »
6. **Routes** : `POST /password/simulate`, `POST /password/execute`
7. **Sélecteur politique** : Radio Élèves / Personnels / Personnalisé (champs longueur, complexité, expiry)
8. **i18n** : Clés `password.simulate.preview`, `password.execute.success`, `password.policy.eleves`, `password.policy.personnels`, `password.force_change` (fr+en)
9. **CLI** : `edusync-cli password reset --ou "OU=Eleves" --policy eleves --simulate`, `--execute`
10. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Reset 50 comptes élèves (OU) → simulation affiche MDP générés selon politique élèves
- [ ] Reset 20 comptes personnels (groupe) → politique personnels appliquée
- [ ] Reset par CSV (prénom/nom) → résolution + MDP générés
- [ ] MDP stockés dans coffre chiffré (`vault.Store`)
- [ ] `pwdLastSet = 0` appliqué → forçage changement prochaine connexion
- [ ] Export CSV résultats : identifiant, MDP, mail, OU
- [ ] Simulation modifiable en ligne (changer MDP généré manuellement)
- [ ] Audit journalisé : type `password_reset`, détails (nb comptes, politique, opérateur)
- [ ] CLI : simulate → JSON preview, execute → JSON résultats
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Password`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Password -v
go test ./internal/ui/... -run Password -v

# Test manuel
task ad:start
task dev
# UI : MDP → Sélection OU Élèves → Politique Élèves → Simulation → Exécution
# Vérifier : AD (unicodePwd), coffre (vault), pwdLastSet=0, journal, export CSV
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/mdp.py` : Logique reset + génération MDP
- `src/edusync_ad/core/politiques_mdp.py` : Politiques élèves/personnels
- `src/edusync_ad/plugins/mdp/` : Plugin UI PyQt6
- `tests/core/test_mdp.py` : Tests Python