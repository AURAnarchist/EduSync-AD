# Migration Python → Go — Résumé

> **Version** : 2.0 — 15 octobre 2026
> **Objectif** : Réécrire **toutes** les fonctionnalités d'EduSync-AD en Go (backend + UI desktop Wails) **sans perte de fonctionnalité**, publier une **release alpha Windows + Linux**, puis déclencher la **campagne de tests**.

---

## Les 3 priorités dans l'ordre (décision du 14/10/2026)

| Priorité | Objectif | État |
|----------|----------|------|
| **1. Finir le logiciel en Go** | Migration complète : Core v1.x (M1-M11) + Modules avancés (M12-M24) + Modules v4.0 (M25-M32) | 🔨 En cours (Lot A) |
| **2. Sortir l'alpha** | Windows `EduSyncAD-Setup.exe` + Linux `AppImage` + `.tar.gz` | ⬜ Après P1 |
| **3. Campagne de tests** | Intégration Samba, e2e Playwright, benchmarks vs Python, couverture > 90% | ⬜ Après alpha |

> **Règle** : La boucle d'hygiène `task lint && task test && task test:python` reste obligatoire à **chaque livraison** (voir `AGENTS.md`). Les phases de tests dédiées (campagne complète) sont décalées **après l'alpha**.

---

## Phases détaillées

| Phase | Fichier | Contenu |
|-------|---------|---------|
| **0. Fondations** | `01-core.md` | Starter kit, config, crypto, modèles, audit, pool AD, CLI, UI Wails, testutil Samba |
| **1. Core v1.x (M1-M11)** | `01-core.md` (suite) | Câblage bout en bout : création, migration, départs, MDP, explorateur, export, journal, paramètres, simulation |
| **2. Modules avancés (M12-M24)** | `02-modules-avances.md` | Photos, profils, quotas, scripts, heures, dossiers, partages, M365, Exchange, RDS, imports avancés, modèles groupes, Label Studio |
| **3. Modules v4.0 (M25-M32)** | `03-modules-v4.md` | Multisite, RBAC, portail auto-service, API REST, IA, conformité, DFS, sauvegarde, coffre MDP |
| **4. Alpha & Packaging** | `04-alpha.md` | `release-go.yml`, InnoSetup Windows, AppImage Linux, M10 auto-update, checklist pré-alpha |
| **5. Tests post-alpha** | `05-tests.md` | Couverture unitaire, intégration Samba, e2e, benchmarks, charge, sécurité, beta → stable 1.0 |

> **Détails par module** : voir `docs/modules/MXX-*.md` (une fiche par module).

---

## Ordre d'implémentation des modules (Priorité 1)

### Lot A — Core v1.x (M1-M11) — *En cours*

| # | Module | Travail principal |
|---|--------|-------------------|
| A1 | Connexion AD (M9) | `BindPassword` depuis config chiffrée, erreurs UI, avertissement repli LDAP |
| A2 | Création comptes (M1) | `usersSimulate`/`Execute` + 2 gabarits, doublons, export étiquettes |
| A3 | Réinit MDP (M4) | `passwordSimulate`/`Execute` + 2 gabarits, politiques élèves/personnels |
| A4 | Migration (M2) | `migrationPlan`/`Simulate`/`Execute` + 3 gabarits, groupes auto |
| A5 | Explorateur (M5) | 3 gabarits, `pool.Search`/`BuildTree` |
| A6 | Export (M6) | Gabarit `export.html`, vrais handlers CSV/PDF, sélection OU ± sous-OU |
| A7 | Journal (M7) | 2 gabarits, filtres `auditFilter`, export CSV |
| A8 | **Départs (M3)** | Créer `internal/ad/depart.go` + routes + gabarit + CLI + i18n |
| A9 | **Paramètres (M8)** | Créer page 4 onglets + routes + gabarit + i18n |
| A10 | Simulation (M11) | Généraliser `Validate→Simulate→Execute→Audit` sur A2-A8 |
| A11 | CLI Core | Commandes JSON pour chaque module |
| A12 | Progression/Annulation | Barre progression + `context.Context` annulable (équivalent T0) |
| A13 | Revue parity Core | Checklist §4.1 = ✅ vérifiée dans le code, i18n fr+en |

### Lot B — Modules avancés (M12-M24) + Cache T2

| # | Modules | Points clés |
|---|---------|-------------|
| B1 | M13, M16, M15 | Attributs AD classiques (profil, heures, scripts) |
| B2 | M17, M14, M18 | Partages/NTFS/FSRM — prérequis serveur Windows documentés |
| B3 | **T2 Cache AD** | Cache SQLite OU/groupes/users, invalidation TTL + `uSNChanged` |
| B4 | M12 Photos | Import masse + cadrage, vignettes explorateur, intégration étiquettes |
| B5 | M22 Imports/Exports | Import LDAP, export LDIF/vCard, publipostage HTML |
| B6 | M19 M365, M20 Exchange | Graph + On-prem — tests manuels documentés |
| B7 | M21 RDS | Collections, RemoteApp, UPD/FSLogix |
| B8 | M24 Label Studio + M23 Modèles groupes | WYSIWYG, trombinoscope A4/A3, instanciation 1-clic |

### Lot C — Modules v4.0 (M25-M32) + Coffre

| # | Modules | Points clés |
|---|---------|-------------|
| C1 | M25 Multisite | Modèle `Site`, config chiffrée par site, sélecteur header, pool par site, `site_id` journal |
| C2 | M26 RBAC | 5 rôles, portées OU/groupe, **garde-fou sur chaque écriture AD**, journal par opérateur |
| C3 | Coffre MDP + M32 Sauvegarde | AES-256 local, backup ZIP chiffré, restore 1-clic, migrations vN→vN+1 |
| C4 | M27 Portail auto-service | Serveur web stdlib, code mail, PBKDF2 usage unique, RBAC, audit `utilisateur="portail"` |
| C5 | M28 API REST + Webhooks | OpenAPI, webhooks SIS, sync bidirectionnelle |
| C6 | M29 IA + M30 Conformité | Anomalies, nettoyage fin d'année, RGPD/MFA/PSO/signature |
| C7 | M31 DFS/IIS/WebDAV/FTP | Priorité basse, livré en dernier |

---

## Critères d'acceptation (3 portes)

### Porte 1 — « Logiciel fini » (fin Priorité 1)

- [ ] Inventaire §4 complet = ✅ vérifié dans le code (M1→M32, T2, coffre, transverses)
- [ ] Chaque écriture AD passe par `Validate → Simulate → Execute → Audit`
- [ ] `gofmt`, `go vet`, `go test ./...`, `pytest -q` tous verts
- [ ] Toutes les clés i18n ajoutées existent en français **et** anglais
- [ ] `edusync-cli migrate --from-python` livré
- [ ] Roadmap + `CLAUDE.md` reflètent le code

### Porte 2 — « Alpha publiée » (fin Priorité 2)

- [ ] `release-go.yml` (tags `v0.*`) produit : `Setup.exe`, `AppImage`, `.tar.gz` + `.sha256`
- [ ] `prerelease: true`, notes de version (périmètre, limites, installation, appel testeurs)
- [ ] Checklist P2.5 : démarrage Windows réel, install/désinstall, relecture config Python, LDAPS, thème+i18n
- [ ] `release.yml` (Python, tags `v1.*`) **intact**
- [ ] M10 auto-update validé contre l'alpha

### Porte 3 — « Stable 1.0 » (fin Priorité 3)

- [ ] Zéro perte fonctionnelle : feature parity vérifiée par les tests
- [ ] Tests verts en CI : unit + intégration (Samba) + e2e (Playwright) ; couverture > 90%
- [ ] Perf : démarrage < 100 ms ; import 1500 comptes < 30 s ; navigation fluide 10k+ objets ; RAM 30-50 MB
- [ ] Binaires Windows + Linux < 15 MB, générés en une commande
- [ ] Migration données : utilisateur Python passe à Go sans reconfiguration manuelle
- [ ] Documentation à jour (guide utilisateur, AD test, architecture, prompts IA)
- [ ] Benchmarks publiés (comparatif Python)

---

## Risques principaux et mitigations

| Risque | Prob. | Mitigation |
|--------|-------|------------|
| Périmètre complet avant alpha (20 modules Lot B+C) | Moyenne | Lots A/B/C avec checklist visible ; Python 1.17.1 reste en production |
| Volume portage (plugins Python) | Haute | Un module = une méthode (backend→UI→CLI→lint/test) ; prompts IA ; revue fin de lot |
| `release.yml` Python cassé par tags Go | Moyenne | Workflows **séparés** : `release.yml` (`v1.*`) intact, `release-go.yml` (`v0.*`) |
| WebKitGTK Linux (Wails) | Moyenne | Runner ubuntu + `libwebkit2gtk-4.1-dev`, AppImage bundlée, test manuel P2.5 |
| Samba AD ≠ Windows AD | Faible | Tests ciblés (schema, GPO, PSO, trusts) en P3, documentation écarts |
| Migration config/vault | Faible | Script testé sur configs réelles, backup auto `.bak`, rollback documenté |

---

## Commandes de référence

```bash
# Développement
task dev              # App desktop (Wails v3)
task test             # Tests Go
task test:python      # Tests Python
task lint             # gofmt (lecture) + go vet
task fmt              # gofmt -w
task build:all        # Cross-build → dist/ (5 cibles CLI)

# AD de test
task ad:start         # Démarrer Samba AD (docs/AD_TEST_ENV.md)
task ad:reset         # Reset domaine
task ad:shell         # Shell dans le container

# Migration données (à créer fin P1)
edusync-cli migrate --from-python --config-dir ... --vault-dir ...
```

---

## Contexte pour l'IA

- `CLAUDE.md` — Architecture, règles d'or, conventions, prompts réutilisables
- `AGENTS.md` — Boucle de travail : spec → test d'abord → implémentation → gofmt/vet/test → lint CI → revue → commit
- `docs/IA_PROMPTS.md` — Bibliothèque de prompts (nouveau module, optimisation, multisite, etc.)

---

## Documents de référence

| Document | Rôle |
|----------|------|
| `ROADMAP_GO_MIGRATION.md` | Feuille de route officielle complète (à transformer progressivement) |
| `docs/STATUS.md` | Vue rapide de l'avancement réel (ce qu'il faut consulter en premier) |
| `docs/modules/` | Une fiche par module : objectif, Python ref, Go à faire, avancement, validation |
| `docs/python/` | Documentation de la version Python actuelle (référence parité) |
| `docs/ARCHITECTURE.md` | Architecture Go cible (ce qui existe dans le code) |
| `docs/AD_TEST_ENV.md` | Environnement de test AD (Samba en container) |