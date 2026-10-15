# Phase 0 + Core v1.x (M1-M11) — Migration du cœur

> **Source** : `ROADMAP_GO_MIGRATION.md` §0 (État d'avancement), §4.1 (Inventaire Core), §6 (Roadmap Lot A)
> **Périmètre** : Fondations (T0-T2 équivalents Go) + Modules M1 à M11
> **Priorité** : 1 — Lot A (en cours)

---

## 1. Phase 0 — Fondations (Jours 1-14) — ✅ Livrée

| Jours | Livrable | Statut | Localisation |
|-------|----------|--------|--------------|
| 1-2 | Starter kit : repo Go, `go.mod`, `Taskfile.yml`, CI, `CLAUDE.md`/`AGENTS.md`, cross-build | ✅ | `go.mod`, `Taskfile.yml`, `.github/workflows/ci.yml`, `build/cross-build.sh` |
| 3-4 | Config chiffrée multi-profil, lecture fichiers Python | ✅ Testé | `internal/config` + fixture `testdata/config_python.json` |
| 3-4 | Crypto compatible Python (AES-256-GCM) | ✅ Testé | `internal/crypto`, vecteur interop Python → Go |
| 5-6 | **Core AD : pool connexions + TLS + SASL/GSSAPI** | ✅ | `internal/ad/pool.go` (pool, health check, LDAPS/StartTLS, AuthMethod simple/NTLM/SASL-GSSAPI + `gokrb5`) |
| 7-8 | Modèles domaine + audit SQLite | ✅ Testé | `internal/models`, `internal/audit` (schéma identique Python, append-only) |
| 9-10 | Testutil container Samba + fixtures | ✅ (hors campagne) | `internal/testutil/samba.go`, `Dockerfile.samba-ad`, `provision-ad.sh`, `docs/AD_TEST_ENV.md` |
| 11-12 | CLI squelette | ✅ Testé | `cmd/edusync-cli` : `version`, `paths`, `config show`, `audit list`, `connect` |
| 13-14 | Wails v3 + HTMX/Alpine/Tailwind embeddé | ✅ Testé | `cmd/edusync-ad`, `internal/ui`, `web/` — login, dashboard, thème clair/sombre, i18n fr/en |

**Décisions d'implémentation à conserver** (cf. `ROADMAP_GO_MIGRATION.md` §0) :
1. Tailwind compilé puis versionné (`web/static/css/tailwind.css`) — `task web:css` régénère
2. Aucun service Wails lié au JS — UI rendue côté serveur (HTMX)
3. CLI sans CGO (`modernc.org/sqlite`) → cross-compile trivial
4. Parité Python ↔ Go exigée par les tests (config JSON, chiffrement, schéma SQLite, chemins)

---

## 2. Core v1.x (M1 → M11) — Lot A : Câblage bout en bout

> **Méthode par module** : backend existant → handlers HTTP réels → gabarit → clés i18n fr/en → commande CLI → `task lint && task test` verts → commit.

### État actuel (constat code 14/10/2026)

| Module | Réel dans le code | Manque pour « fini » |
|--------|-------------------|----------------------|
| Login / dashboard / thème / i18n | ✅ Fonctionnel et testé | — |
| Connexion AD (M9) | ✅ Pool livré | ⚠️ `BindPassword` en dur vide (`server.go:438`, TODO : config chiffrée) ; erreurs connexion à afficher proprement dans l'UI |
| Création comptes (M1) | 🔨 Page + import réel (`usersImport`) | `usersSimulate` / `usersExecute` = TODO ; gabarits `users_simulate.html`, `users_execute.html` absents |
| Réinitialisation MDP (M4) | 🔨 Gabarit `password.html` + routes | `passwordSimulate` / `passwordExecute` = TODO ; 2 gabarits absents |
| Migration (M2) | 🔨 Gabarit `migration.html` + routes | `migrationPlan` / `migrationSimulate` / `migrationExecute` = TODO ; 3 gabarits absents |
| Explorateur AD (M5) | 🔨 Routes + i18n | Handlers `pool.Search` / `pool.BuildTree` = TODO ; 3 gabarits absents |
| Export CSV/PDF (M6) | 🔨 Routes + i18n | Handlers = **stubs** (CSV/PDF factices) ; gabarit `export.html` absent |
| Journal d'actions (M7) | 🔨 Page 50 dernières entrées | Filtres (`auditFilter`) = TODO ; gabarits `audit.html` + `audit_results.html` absents |
| **Départs (M3)** | ⬜ **Module absent** | Backend `internal/ad/depart.go`, route, gabarit, CLI, i18n (`models.DepartRow` existe) |
| **Paramètres (M8)** | ⬜ **Page absente** | 4 onglets (comptes, mots de passe, apparence, délégation), routes, gabarit, i18n |
| Simulation (M11) | 🔨 Pattern `simulation.go` prêt | Câbler `Validate → Simulate → Execute → Audit` sur tous modules ci-dessus |
| Mise à jour (M10) | ⬜ Absente en Go | Auto-update + vérification `.sha256` (chantier alpha, §6 P2) |

---

## 3. Travail détaillé par module (Lot A)

### A1 — Connexion AD (M9) — **Priorité immédiate**
- [ ] Câbler `BindPassword` depuis la config chiffrée (`server.go:438`)
- [ ] Afficher les erreurs de connexion proprement dans l'UI (toast HTMX)
- [ ] Avertissement explicite repli LDAP 389 (si LDAPS échoue)
- **Test** : `task ad:start` → connexion LDAPS réussie → mot de passe lu depuis `config.json` chiffré

### A2 — Création de comptes (M1)
- [ ] Implémenter `usersSimulate` : validation CSV, détection doublons, preview comptes à créer
- [ ] Implémenter `usersExecute` : écriture AD via pool, gestion groupes auto, export étiquettes CSV
- [ ] Créer gabarits : `users_simulate.html`, `users_execute.html`
- [ ] Clés i18n fr/en pour tous les messages
- [ ] Commande CLI : `edusync-cli users import --csv ... --simulate` / `--execute`
- **Test** : Import CSV 50 comptes → simulation OK → exécution → vérif AD + journal

### A3 — Réinitialisation MDP (M4)
- [ ] Implémenter `passwordSimulate` : sélection OU/groupe/CSV, politiques élèves/personnels, preview MDP générés
- [ ] Implémenter `passwordExecute` : reset AD, forçage changement prochaine connexion, export CSV
- [ ] Créer gabarits : `password_simulate.html`, `password_execute.html`
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli password reset --ou ... --policy eleves --simulate`
- **Test** : Reset 20 comptes élèves → simulation → exécution → vérif AD + journal

### A4 — Migration / Restructuration (M2)
- [ ] Implémenter `migrationPlan` : depuis CSV (prénom/nom/équipe) → résolution OU cible + groupes
- [ ] Implémenter `migrationSimulate` : dry-run déplacement AD + groupes, détection conflits
- [ ] Implémenter `migrationExecute` : exécution réelle, rollback partiel si erreur
- [ ] Créer 3 gabarits : `migration_plan.html`, `migration_simulate.html`, `migration_execute.html`
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli migration plan --csv ...`, `migrate --simulate`, `migrate --execute`
- **Test** : Migration 5 classes → simulation (dry-run) → exécution → vérif AD + groupes + journal

### A5 — Explorateur AD (M5)
- [ ] Implémenter `pool.Search` : recherche paginée utilisateurs/groupes/OUs, filtres
- [ ] Implémenter `pool.BuildTree` : construction arborescence OUs + groupes (cache)
- [ ] Créer 3 gabarits : `explorer.html` (page), `explorer_tree.html` (arborescence HTMX), `explorer_results.html` (panneau central)
- [ ] Clic droit complet : modif, move, MDP, groupes, suppr (via HTMX)
- [ ] Recherche temps réel (debounce Alpine)
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli explorer search --query ...`, `explorer tree --ou ...`
- **Test** : Navigation 5000+ objets, recherche, clic droit actions

### A6 — Export CSV / Étiquettes PDF (M6)
- [ ] Créer gabarit `export.html` : sélection OU ± sous-OU, champs cochables (incl. MDP mémorisés)
- [ ] Implémenter vrais handlers `ExportCSV` / `ExportLabelsPDF` (remplacer stubs)
- [ ] PDF Avery L7160/L7163, QR codes, couleurs pastel (package `label` existant)
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli export csv --ou ... --fields ...`, `export labels --format L7160`
- **Test** : Export 500 comptes CSV + PDF étiquettes → vérif format

### A7 — Journal d'actions (M7)
- [ ] Créer gabarits : `audit.html` (page filtres), `audit_results.html` (tableau HTMX)
- [ ] Implémenter `auditFilter` : filtres date/type/résultat/domaine/opérateur, pagination
- [ ] Export CSV des résultats filtrés
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli audit list --since ... --type ... --domain ... --json`
- **Test** : Filtres combinés, export CSV, pagination

### A8 — **Départs (M3) — Module à créer**
- [ ] Créer `internal/ad/depart.go` :
  - Désactivation immédiate (disable account)
  - Archivage (move vers OU Archive + désactivation)
  - Purge planifiée idempotente (délai configurable, défaut 30j) — table `pending_deletions`
- [ ] Routes UI : `GET /depart`, `POST /depart/simulate`, `POST /depart/execute`
- [ ] Gabarit `depart.html` : sélection OU/groupe/CSV, mode (désactiver/archiver/purger), délai
- [ ] Clés i18n fr/en
- [ ] Commande CLI : `edusync-cli depart disable --ou ...`, `depart archive --csv ...`, `depart purge --dry-run`
- [ ] Réutiliser `models.DepartRow` (existe déjà)
- **Test** : Départ 10 comptes → simulation → exécution → vérif AD (disable/move) + journal + purge différée

### A9 — **Paramètres globaux (M8) — Page à créer**
- [ ] 4 onglets :
  1. **Comptes** : format identifiant, domaine mail, groupes par défaut, comportement départs
  2. **Mots de passe** : politiques élèves/personnels (longueur, complexité, historique), coffre MDP
  3. **Apparence** : thème (clair/sombre/auto), langue (fr/en), densité UI
  4. **Délégation** (reportée Lot C / M26) : placeholder ou onglet masqué
- [ ] Routes : `GET /settings`, `POST /settings` (validation + sauvegarde config chiffrée)
- [ ] Gabarit `settings.html` avec navigation par onglets (Alpine)
- [ ] Clés i18n fr/en (toutes nouvelles clés)
- **Test** : Modification chaque onglet → sauvegarde → rechargement → persistence config.json chiffré

### A10 — Simulation généralisée (M11)
- [ ] Pattern `Validate → Simulate → Execute → Audit` sur **tous** modules A2-A8
- [ ] Middleware/handler commun pour la prévisualisation modifiable
- [ ] Aucune écriture AD sans passage par simulation
- [ ] Clés i18n : messages simulation, boutons « Modifier », « Confirmer », « Annuler »
- **Test** : Chaque module force simulation avant exécution ; annulation possible

### A11 — CLI Core
- [ ] Commandes JSON pour chaque module (import, reset, migration, départs, explorer, export, audit)
- [ ] Sortie structurée : `--json`, `--quiet`, `--verbose`
- [ ] Codes de sortie appropriés (0=succès, 1=erreur validation, 2=erreur AD, 3=erreur config)
- **Test** : Chaque commande CLI testée indépendamment + enchaînement

### A12 — Progression / Annulation (équivalent T0)
- [ ] Barre de progression HTMX + Alpine sur opérations de masse (import, migration, reset, départs)
- [ ] `context.Context` annulable propagé aux handlers AD
- [ ] Bouton « Annuler » fonctionnel (arrêt propre, rollback partiel si possible)
- [ ] Rapport d'erreur détaillé en cas d'échec partiel
- **Test** : Import 1500 comptes → annulation à 50% → état cohérent + journal

### A13 — Revue parity Core (Checklist §4.1)
- [ ] Vérifier chaque ligne du tableau §4.1 = ✅ **dans le code** (pas dans les intentions)
- [ ] i18n fr/en sur **toutes** les clés ajoutées (grep `internal/ui/i18n.go`)
- [ ] `gofmt -l cmd internal web` → vide
- [ ] `go vet ./...` → vert
- [ ] `go test ./...` → vert (y compris `internal/ad` qui était WIP)
- [ ] `.venv/bin/pytest -q` → vert
- [ ] Mettre à jour `ROADMAP_GO_MIGRATION.md` (statuts modules) + `CLAUDE.md` (état migration)

---

## 4. Commandes de validation Lot A

```bash
# Après chaque module (A1-A12) :
task lint && task test && task test:python

# Validation complète Lot A :
gofmt -l cmd internal web
go vet ./...
go test ./... -v
.venv/bin/pytest -q

# Test manuel contre AD de test :
task ad:start
# Lancer l'app : task dev
# Parcourir chaque module : Login → Création → Migration → Départs → MDP → Explorateur → Export → Journal → Paramètres
# Vérifier : simulation → exécution → journal à chaque écriture AD
```

---

## 5. Fiches modules correspondantes

Voir `docs/modules/` :
- `M01-creation-comptes.md`
- `M02-migration.md`
- `M03-departs.md`
- `M04-reset-mot-de-passe.md`
- `M05-explorateur-ad.md`
- `M06-export.md`
- `M07-journal-actions.md`
- `M08-parametres.md`
- `M09-connexion-ldaps.md`
- `M10-mise-a-jour.md`
- `M11-mode-simulation.md`

Chaque fiche contient : objectif, fonctionnalités Python, fichiers Python ref, fichiers Go à créer/modifier, avancement, étapes restantes, critères validation, commandes test.

---

## 6. Prochaine étape après Lot A

→ **Lot B** : `docs/migration/02-modules-avances.md` (M12-M24 + Cache T2)
→ **Lot C** : `docs/migration/03-modules-v4.md` (M25-M32 + Coffre)
→ **Alpha** : `docs/migration/04-alpha.md`
→ **Tests** : `docs/migration/05-tests.md`