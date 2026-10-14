# 🗺️ Feuille de route — Migration EduSync-AD : Python/PyQt6 → Go/Wails

> **Version** : 2.0 — 14 octobre 2026
> **Objectif** : réécrire **toutes** les fonctionnalités d'EduSync-AD en Go
> (backend + UI desktop Wails) **sans perte de fonctionnalité**, publier une
> **release alpha Windows + Linux**, puis déclencher la **campagne de tests**.
>
> **Priorités ordonnées (décision utilisateur, 14 octobre 2026)** :
>
> 1. **Finir le logiciel en Go** — migration complète (Core v1.x **et** tous
>    les modules avancés, inventaire §4), **sans lancer les phases de test**
>    dédiées.
> 2. **Sortir une alpha** — version **Windows** (`EduSyncAD-Setup.exe`) et
>    version **Linux** (AppImage + `.tar.gz`).
> 3. **Ensuite seulement**, la campagne de tests (unitaires par module,
>    intégration container Samba, e2e, benchmarks vs Python).
>
> Ce qui **ne change pas** : la boucle d'hygiène reste obligatoire à chaque
> livraison (`task lint && task test`, `AGENTS.md`). Ce qui est décalé **après
> l'alpha**, ce sont les *phases de test dédiées* — campagne d'intégration AD,
> e2e, benchmarks, couverture. Aucun test existant n'est supprimé ni
> désactivé.

---

## Table des matières

1. [État d'avancement](#0-état-davancement-mise-à-jour--14-octobre-2026)
2. [Contexte et motivations](#1-contexte-et-motivations)
3. [Décisions validées](#2-décisions-validées)
4. [Architecture cible](#3-architecture-cible)
5. [Inventaire — la référence « logiciel fini »](#4-inventaire--la-référence-logiciel-fini)
6. [Environnement de test AD](#5-environnement-de-test-ad)
7. [Roadmap : les trois priorités dans l'ordre](#6-roadmap--les-trois-priorités-dans-lordre)
8. [Stratégie de tests « IA-ready » (priorité 3)](#7-stratégie-de-tests-ia-ready--priorité-3)
9. [Migration des données existantes](#8-migration-des-données-existantes)
10. [Packaging et cross-build](#9-packaging-et-cross-build)
11. [Prompts IA prêts à l'emploi](#10-prompts-ia-prêts-à-lemploi)
12. [Risques et mitigations](#11-risques-et-mitigations)
13. [Critères d'acceptation (3 portes)](#12-critères-dacceptation--3-portes)

---

## 0. État d'avancement (mise à jour : 14 octobre 2026)

> Statut vérifié **dans le code et les tests**, pas dans les intentions.

### Phase 0 (semaines 1-2) — ✅ livrée

| Jours | Livrable | Statut | Où |
|---|---|---|---|
| 1-2 | Starter kit : repo Go, `go.mod`, `Taskfile.yml`, CI, `CLAUDE.md`/`AGENTS.md`, cross-build | ✅ Livré | `go.mod`, `Taskfile.yml`, `.github/workflows/ci.yml`, `build/cross-build.sh` |
| 3-4 | Config chiffrée multi-profil, lecture des fichiers Python | ✅ Livré (testé) | `internal/config` (+ fixture `testdata/config_python.json` généré par Python) |
| 3-4 | Crypto compatible Python (AES-256-GCM) | ✅ Livré (testé) | `internal/crypto`, avec vecteur d'interopérabilité Python → Go |
| 5-6 | **Core AD : pool de connexions + TLS + SASL/GSSAPI** | ✅ Livré | `internal/ad/pool.go` (pool, health check, LDAPS/StartTLS, `AuthMethod` simple/NTLM/SASL-GSSAPI + client `gokrb5`) |
| 7-8 | Modèles domaine + audit SQLite | ✅ Livré (testé) | `internal/models`, `internal/audit` (schéma identique à Python, append-only) |
| 9-10 | Testutil container Samba + fixtures | ✅ Livré (hors campagne de tests) | `internal/testutil/samba.go` (testcontainers-go), `Dockerfile.samba-ad`, `provision-ad.sh`, `docs/AD_TEST_ENV.md` |
| 11-12 | CLI squelette | ✅ Livré (testé) | `cmd/edusync-cli` : `version`, `paths`, `config show`, `audit list`, `connect` (DNS + TCP + TLS) |
| 13-14 | Wails v3 + HTMX/Alpine/Tailwind embeddé | ✅ Livré (testé) | `cmd/edusync-ad`, `internal/ui`, `web/` — login, dashboard, thème clair/sombre, i18n fr/en |

### Priorité 1 en cours — câblage du Core (constat réel dans le code, 14/10)

| Module | Réel dans le code | Manque pour le dire « fini » |
|---|---|---|
| Login / dashboard / thème / i18n | ✅ Fonctionnel et testé | — |
| Connexion AD (pool) | ✅ Pool livré | ⚠️ `BindPassword` en dur vide (`server.go:438`, TODO : config chiffrée) ; erreurs de connexion à afficher proprement dans l'UI |
| Création de comptes (M1) | 🔨 Page + import réel (`usersImport`) | `usersSimulate` / `usersExecute` = TODO ; gabarits `users_simulate.html`, `users_execute.html` absents |
| Réinitialisation MDP (M4) | 🔨 Gabarit `password.html` + routes | `passwordSimulate` / `passwordExecute` = TODO ; 2 gabarits absents |
| Migration / restructuration (M2) | 🔨 Gabarit `migration.html` + routes | `migrationPlan` / `migrationSimulate` / `migrationExecute` = TODO ; 3 gabarits absents |
| Explorateur AD (M5) | 🔨 Routes + i18n | Handlers `pool.Search` / `pool.BuildTree` = TODO ; 3 gabarits absents (`explorer.html`, `explorer_results.html`, `explorer_tree.html`) |
| Export CSV/PDF (M6) | 🔨 Routes + i18n | Handlers = **stubs** (CSV/PDF factices) ; gabarit `export.html` absent |
| Journal d'actions (M7) | 🔨 Page avec 50 dernières entrées | Filtres (`auditFilter`) = TODO ; gabarit `audit.html` + `audit_results.html` absents |
| Gestion des départs (M3) | ⬜ **Module absent** | Backend `internal/ad/depart.go`, route, gabarit, CLI, i18n (`models.DepartRow` existe déjà) |
| Paramètres globaux (M8) | ⬜ **Page absente** | 4 onglets (comptes, mots de passe, apparence, délégation), routes, gabarit, i18n |
| Mode simulation (M11) | 🔨 Pattern `simulation.go` prêt | Câler `Validate → Simulate → Execute → Audit` sur tous les modules ci-dessus |
| Mise à jour intégrée (M10) | ⬜ Absente en Go | Auto-update + vérification `.sha256` (livrée avec le chantier alpha, §6 P2) |

### Mesures constatées (Linux amd64 — Go 1.27.2, Wails v3.0.0-beta.29, WebKitGTK 2.54.1)

| Critère | Cible | Réel |
|---|---|---|
| `gofmt -l cmd internal web` | rien | **rien** sur le code livré ⚠️ (2 fichiers WIP non formattés : `arrivals*.go`) |
| `go vet ./...` | vert | **vert** ✅ |
| Tests Go | — | **7 paquets verts** sur 10 testés ; `internal/ad` en chantier (WIP module Arrivées, 2 tests rouges le 14/10) |
| CLI cross-compilée | toutes cibles | **5 cibles** produites en une commande (`./build/cross-build.sh cli`) |
| Taille CLI (`-s -w`) | 8-12 MB | **9,5 MB** ✅ |
| Taille app desktop (`-s -w`) | < 15 MB | **17,2 MB** ⚠️ (glue cgo/WebKit — optimisation au chantier alpha) |
| CI | vertes | `ci.yml` : job Go (fmt/vet/test/build/smoke) + job Python (pytest) |
| Release | alpha Win + Linux | ⬜ `release.yml` construit **encore la version Python** (PyInstaller + Flatpak) — à réécrire en `release-go.yml` (§6 P2) |

### Décisions d'implémentation (à conserver)

1. **Tailwind compilé puis versionné** (`web/static/css/tailwind.css`) : aucun
   build obligatoire pour un contributeur ni pour l'IA ; `task web:css`
   régénère après modification des gabarits. Source unique :
   `web/static/css/input.css` (`source(none)` → seuls `web/templates` et
   `web/static/js` sont scannés).
2. **Aucun service Wails lié au JS pour l'instant** : l'UI est intégralement
   rendue côté serveur (HTMX), ce qui supprime une couche à maintenir. Les
   bindings (`wails3 generate bindings`) arriveront avec les modules qui en
   ont réellement besoin.
3. **CLI sans cgo** : `internal/*` reste pur Go (`modernc.org/sqlite`), d'où
   la compilation croisée sur 5 cibles sans outil système.
4. **Parité Python ↔ Go exigée par les tests** : format de config JSON,
   format de chiffrement, schéma SQLite et chemins platformdirs sont couverts
   par des tests aller-retour (données produites par Python lues par Go).

---

## 1. Contexte et motivations

### Problème actuel
- Application **Python 3.11+ / PyQt6** devenue **lourde et lente** :
  - Démarrage froid 2-5 s (interpréteur + Qt + imports).
  - Binaire PyInstaller 80-200 MB.
  - RAM 80-150 MB au repos.
  - `ldap3` (pure Python) lent sur grosses OU ; GIL bloque les opérations CPU.
  - UI qui freeze sur les opérations longues (import 1500 comptes).
  - Packaging lourd (PyInstaller/Flatpak).

### Cible
- **Binaire unique 8-12 MB**, démarrage **< 100 ms**, RAM **30-50 MB**.
- **LDAP natif et concurrent** (go-ldap v3, goroutines).
- **Cross-compile en une commande** : Windows, Linux, macOS, amd64/arm64.
- **Tests en millisecondes** (pas de runtime Python à charger).
- **Workflow IA** : spec → IA génère tests → IA implémente → tests verts → review → merge.

### Pourquoi Go (et pas Rust ou autre)
| Critère | Go | Rust | Rester en Python |
|---|---|---|---|
| Perf LDAP | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| Courbe d'apprentissage | 1-2 semaines | 2-4 mois | connu |
| Démarrage binaire | < 100 ms | < 50 ms | 1-3 s |
| Taille binaire | 8-12 MB | 3-8 MB | 80-150 MB |
| Cross-compile | trivial (`GOOS`/`GOARCH`) | simple | lourd |
| Lisibilité pour IA | excellente (verbose, explicite) | moyenne (lifetimes) | bonne |
| Tests rapides | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ |
| Écosystème AD | go-ldap v3, gokrb5 | ldap3-rs | ldap3 |

---

## 2. Décisions validées

| Sujet | Décision | Rationale |
|---|---|---|
| **Ordre des priorités** | **1) finir le logiciel en Go → 2) alpha Windows + Linux → 3) campagne de tests** | Décision utilisateur du 14/10/2026 ; les tests dédiés ne bloquent plus l'avancement, ils suivent l'alpha |
| **Périmètre avant alpha** | **Migration complète** : Core v1.x (M1-M11) **et** modules avancés (M12-M32, §4) | Décision utilisateur du 14/10/2026 : « la migration complète du logiciel de Python vers Go avec toutes les fonctionnalités, mais sans lancer les phases de test » |
| **Formats alpha** | Windows **`EduSyncAD-Setup.exe`** (InnoSetup) + Linux **AppImage** + **`.tar.gz`** | Décision utilisateur du 14/10/2026 |
| Workflow de release | `release-go.yml` dédié aux tags **`v0.*`** ; `release.yml` (Python) inchangé pour les tags **`v1.*`** | Ne jamais casser le canal de release de la version Python publiée (1.17.1) |
| OS de dev principal | **Linux** | Cross-compile Go natif, containers Docker/Podman, Wails dev mode |
| Environnement de test AD | **Samba AD DC en container** (`testcontainers-go`) | Vrai AD (LDAP, LDAPS, Kerberos, DNS, GPO, schema) — se rapproche d'un Windows Server, démarre en 30 s, isolé, reproductible, gratuit |
| Timeline | **Qualité > vitesse, pas de deadline dure** | Lots livrables et vérifiables plutôt qu'une date ; la Python 1.17.1 reste en production pendant la migration |
| Ordre des modules | **Core v1.x d'abord** (création, migration, départs, MDP, explorateur, export, audit, simulation) → puis modules avancés (M12-M24) → puis v4.0 (M25-M32) | Fondations solides, valeur immédiate, zéro perte fonctionnelle |
| Frontend | **HTMX 2.0 + Alpine.js + Tailwind CSS** (embedded via `//go:embed`) | Zéro build JS, HTML-first (l'IA génère du HTML), < 50 KB JS, reactif, parfait avec Wails, pas de node_modules |
| SQLite | `modernc.org/sqlite` (pure Go, **sans CGO**) | Garde le cross-compile trivial (`CGO_ENABLED=0`) |
| Licence | MIT (identique à l'existant) | Compatibilité |
| Tests d'intégration | `testcontainers-go` + image Samba AD maison | Un vrai AD dans chaque test (socle déjà livré, campagne en priorité 3) |

---

## 3. Architecture cible

```
edusync-ad/
├── cmd/
│   ├── edusync-ad/            # App desktop (Wails v3)
│   ├── edusync-cli/           # CLI complète (scripts, CI, IA)
│   └── edusync-server/        # API REST + Webhooks (optionnel, même core)
├── internal/
│   ├── ad/                    # ⭐ Cœur métier LDAP/AD
│   │   ├── connection.go      # Pool, TLS, SASL/GSSAPI, découverte DC
│   │   ├── user.go            # CRUD, MDP, enable/disable, move, groupes
│   │   ├── group.go           # CRUD, membres, groupes imbriqués
│   │   ├── ou.go              # CRUD, arborescence, liens GPO
│   │   ├── depart.go          # Désactivation, archivage, purge planifiée
│   │   ├── migration.go       # Logique restructuration (CSV + UI)
│   │   ├── password.go        # Politiques de reset, génération, historique
│   │   ├── explorer.go        # Recherche paginée, cache, constructeur d'arbre
│   │   └── simulation.go      # Mode dry-run avec diff preview
│   ├── config/                # Multi-profil, chiffré (AES-GCM), platformdirs
│   ├── crypto/                # Vault MDP, certs, config TLS, signatures
│   ├── csv/                   # Import/export, validateurs, modèles
│   ├── models/                # Types domaine (User, Group, OU, Config, AuditEntry, Site)
│   ├── audit/                 # Journal SQLite (append-only, indexé, rétention)
│   ├── label/                 # Génération PDF (Avery L7160/L7163, QR, trombinoscope)
│   ├── multisite/             # Multi-domaine/forêt, profils chiffrés, sélecteur
│   ├── rbac/                  # 5 rôles, portées OU/groupe, garde-fous écriture
│   ├── portal/                # Self-service élève/parent (code mail, validation)
│   ├── api/                   # REST + Webhooks, spec OpenAPI
│   ├── ai/                    # Détection anomalies, suggestions, analyse logs
│   ├── compliance/            # RGPD, backup/restore, MFA, PSO, signature
│   └── testutil/              # Container Samba, fixtures, helpers
├── web/                       # Frontend embed (//go:embed)
│   ├── templates/
│   │   ├── layout.html        # Base + config HTMX + stores Alpine
│   │   ├── login.html / dashboard.html / <module>.html
│   │   └── partials/          # Composants réutilisables (table, form, tree, toast, modal)
│   └── static/
│       ├── css/tailwind.css
│       ├── js/alpine.stores.js
│       └── assets/
├── build/
│   ├── cross-build.sh         # Matrice GOOS/GOARCH + ldflags
│   ├── appimage/              # AppImage Linux
│   └── inno/                  # InnoSetup Windows (.iss, porté depuis packaging/)
├── .github/workflows/
│   ├── ci.yml                 # Test, lint, build multi-arch
│   ├── integration.yml        # Tests AD réels (container Samba) — exécution en priorité 3
│   ├── release.yml            # Tags v1.* → release Python (inchangé)
│   └── release-go.yml         # Tags v0.* → alpha Go (§6 P2)
├── Taskfile.yml               # Commandes dev unifiées (task setup/dev/test/build)
├── go.mod / go.sum
├── CLAUDE.md                  # Contexte IA : patterns, règles, prompts
├── AGENTS.md                  # Instructions agents (test, implémenter, review)
├── Dockerfile.samba-ad        # Image AD de test
├── provision-ad.sh            # Provisioning domaine de test
└── docs/
    ├── ARCHITECTURE.md
    ├── AD_TEST_ENV.md
    ├── MIGRATION_GUIDE.md     # Guide de migration des données Python → Go
    └── IA_PROMPTS.md
```

### Principes d'architecture

1. **Pattern universel par opération AD** : `Validate() → Simulate() → Execute() → Audit()` — le mode simulation est natif, pas une option ajoutée après coup.
2. **Le core Go est UI-agnostic** : `cmd/edusync-ad` (Wails), `cmd/edusync-cli` (scripts) et `cmd/edusync-server` (API) partagent `internal/*`.
3. **Connexions poolées par site** (LRU, max simultanées, health check, retry/backoff).
4. **Toute écriture AD passe par RBAC** (garde-fou) + **audit** (append-only SQLite).
5. **Frontend embarqué** : `//go:embed web/` → zéro fichier externe, un seul binaire.

---

## 4. Inventaire — la référence « logiciel fini »

> **Règle : zéro perte fonctionnelle.** Chaque ligne ci-dessous doit exister
> en Go avant de déclarer la migration terminée. Le périmètre = **toutes les
> fonctionnalités de la version Python publiée** (M1 → M32, `docs/ROADMAP_python_v1.md`)
> + les fondations T0-T2. Statuts : ✅ livré / 🔨 partiellement câblé / ⬜ absent.

### 4.1 Core v1.x (M1 → M11) — Priorité 1, Lot A

| Module | Fonctionnalités | Statut Go (14/10) |
|---|---|---|
| **M1 — Création de comptes / Arrivées** | Import CSV (encodage + séparateur auto), génération identifiants/MDP/mail, résolution doublons, vérification AD existants, export CSV étiquettes | 🔨 import réel, `simulate`/`execute` TODO |
| **M2 — Migration (restructuration)** | Via CSV (prénom/nom/équipe) ou UI (sélection OU), déplacement AD + groupes auto, résolution par prénom/nom | 🔨 page + routes, plan/simulate/execute TODO |
| **M3 — Gestion des départs** | Désactivation immédiate ou archivage + purge planifiée (délai configurable, 30 j par défaut) | ⬜ **absent** (`models.DepartRow` prêt) |
| **M4 — Réinitialisation MDP** | Par OU, groupe AD ou CSV, politiques différées (élèves/personnels), forçage changement à la prochaine connexion | 🔨 gabarit + routes, simulate/execute TODO |
| **M5 — Explorateur AD** | Arborescence OUs/groupes, panneau central unifié, clic droit complet, recherche temps réel | 🔨 backend `explorer.go` prêt, handlers/gabarits TODO |
| **M6 — Export (CSV / Étiquettes PDF)** | Sélection OU ± sous-OU, champs cochables (incl. MDP mémorisés), PDF Avery L7160/L7163, QR codes, couleurs pastel | 🔨 backend `export.go` prêt, handlers = stubs, gabarit absent |
| **M7 — Journal d'actions** | Filtres date/type/résultat/domaine/opérateur, export CSV, SQLite append-only | 🔨 liste OK, filtres TODO, gabarit absent |
| **M8 — Paramètres globaux** | 4 onglets : Comptes, Mots de passe, Apparence, Délégation | ⬜ **absent** (thème/langue partiel via header) |
| **M9 — Connexion LDAPS/LDAP** | LDAPS prioritaire, repli LDAP avec avertissement, CA custom, mémorisation chiffrée | 🔨 pool livré, `BindPassword` à câbler (TODO) |
| **M10 — Mise à jour intégrée** | Vérification, téléchargement, installation, redémarrage auto, somme de contrôle | ⬜ absent en Go (livré avec le chantier alpha, §6 P2) |
| **M11 — Mode simulation** | Toute écriture AD = prévisualisation modifiable avant validation | 🔨 pattern `simulation.go` prêt, câblage général à faire |

### 4.2 Modules avancés (M12 → M24) — Priorité 1, Lot B

| Module | Fonctionnalités | Statut Go |
|---|---|---|
| **M12 — Photos d'identité** | Import manuel/masse/nommage auto, cadrage 3:4 ≤ 200 Ko, vignettes explorateur + étiquettes | ⬜ |
| **M13 — Profils utilisateurs** | Itinérant/local/obligatoire par OU ou groupe, variables `%USERNAME%` `%SAM%` `%OU%` | ⬜ |
| **M14 — Quotas de disque (FSRM)** | Quota par OU/groupe, application auto, rapport CSV | ⬜ |
| **M15 — Scripts logon/logoff** | Éditeur bat/powershell/vbs, variables, déploiement `scriptPath`/GPO | ⬜ |
| **M16 — Heures de connexion** | Grille 24h × 7j, préréglages, attribut `logonHours` | ⬜ |
| **M17 — Dossiers personnels** | Création auto `\\srv\homes\%USERNAME%`, droits NTFS, lettre lecteur | ⬜ |
| **M18 — Espaces partagés par classe** | Partage SMB + NTFS à la création du groupe de classe, sync membres ↔ droits | ⬜ |
| **M19 — Microsoft 365 / Entra ID** | App registration, users cloud + licences, groupes/Teams, photos | ⬜ |
| **M20 — Exchange** | Boîtes aux lettres, aliases/proxy, policies, quotas | ⬜ |
| **M21 — RDS / Bureau à distance** | Collections, RemoteApp, UPD/FSLogix | ⬜ |
| **M22 — Import/Export avancés** | Import LDAP (OU/filtre), export LDIF/vCard, publipostage HTML, import GEP | ⬜ |
| **M23 — Modèles de groupes** | Modèles classe/personnel/services, instanciation 1-clic, duplication/import | ⬜ |
| **M24 — Étiquettes avancées (Label Studio)** | Éditeur WYSIWYG, badges/cartes, trombinoscope photo A4/A3, envoi par mail | ⬜ |

### 4.3 Modules v4.0 (M25 → M32) — Priorité 1, Lot C

| Module | Fonctionnalités | Statut Go |
|---|---|---|
| **M25 — Multisite** | Plusieurs domaines/forêts, `domaines.json` chiffré, sélecteur, journal par domaine | ⬜ |
| **M26 — RBAC (délégation)** | 5 rôles, portées par OU/groupe, garde-fou sur chaque écriture AD, journal par opérateur | ⬜ |
| **M27 — Portail auto-service** | Réinit MDP par code mail, consultation identifiant/mail, demande création compte + validation admin | ⬜ |
| **M28 — API REST + Webhooks** | OpenAPI, webhooks (compte créé/migré/départ/MDP), sync bidirectionnelle SIS | ⬜ |
| **M29 — IA / Assistant** | Détection anomalies, nettoyage fin d'année, identifiants intelligents, analyse logs | ⬜ |
| **M30 — Conformité & sécurité** | Rapport RGPD + droit à l'oubli, MFA admin, session unique, PSO, signature exports | ⬜ |
| **M31 — DFS / IIS / WebDAV / FTP** | Espaces DFS, publication WebDAV/IIS, FTP isolé (priorité basse) | ⬜ |
| **M32 — Sauvegarde / Restauration** | Backup ZIP chiffré, restore 1-clic, migration vN→vN+1 | ⬜ |
| **Coffre MDP** | AES-256 local (machines admin), jamais sur l'AD | ⬜ |

### 4.4 Fondations Python (T0-T2) — équivalents Go

| Fondation Python | Équivalent Go | Décision |
|---|---|---|
| **T0 — couche LDAP asynchrone** | Goroutines + `context.Context` (natif) | Pas de chantier de portage **mais** : barre de progression, annulation et rapports d'erreur à câbler dans l'UI pour les opérations de masse (Lot A) |
| **T1 — système de plugins** (`entry_points`) | Modules `internal/*` câblés, activation par configuration | **Hot-reload hors périmètre** : écart documenté dans les notes alpha ; revue en priorité 3 si un vrai besoin de chargement à chaud émerge |
| **T2 — cache AD SQLite** | Cache OU/groupes/users, invalidation TTL + `uSNChanged` | À implémenter (Lot B) — conditionne la fluidité de l'explorateur sur gros domaines |

### 4.5 Transverses

| Fonctionnalité | Statut Go |
|---|---|
| Configuration chiffrée (domaine, utilisateur) via platformdirs + AES | ✅ |
| Thème dark/light, i18n fr/en | ✅ (toutes les clés des nouveaux modules en **français ET anglais**) |
| Migration des données Python (`edusync-cli migrate --from-python`) | ⬜ script à livrer en fin de Priorité 1 (§8) |
| Historique / mode démo VM (protocole de test) | ⬜ |

---

## 5. Environnement de test AD

> Socle **déjà livré** (Phase 0) mais **exploitation en priorité 3**. Il sert
> aussi de banc de test **manuel** ponctuel pendant les priorités 1 et 2
> (une connexion LDAPS réelle de bout en bout avant de publier l'alpha).

### Choix : Samba AD DC en container

On se rapproche d'un **Windows Server AD** avec un vrai Active Directory Samba 4 (LDAPS, Kerberos, DNS, GPO, schema étendu) provisionné automatiquement.

**Fourni dans le starter kit :**

1. **`Dockerfile.samba-ad`** — Ubuntu 24.04 + Samba AD + Winbind + Kerberos + BIND9 + ldap-utils.
2. **`provision-ad.sh`** — Au premier démarrage :
   - Provisionne le domaine `EDUSYNC.LOCAL` (realm identique).
   - Compte `Administrator` / `TestPass123`.
   - Structure OU : `Eleves`, `Personnels`, `Groupes`, `Services`, `Archive`.
   - ~50 utilisateurs de test (élèves + personnels, classes, accents français).
   - Groupes de test (classes, services, délégations RBAC).
   - GPO basiques + fine-grained password policies.
   - Certificat auto-signé pour LDAPS (port 636).
   - Démarre les services, loggue "Samba AD DC ready".

3. **`internal/testutil/samba.go`** — Helper Go :
   ```go
   func NewSambaADContainer(ctx context.Context, t *testing.T) (testcontainers.Container, *ADConfig)
   ```
   - Lève le container, expose 389/636/88/53, attend le log "ready".
   - Retourne un `*ADConfig` (domaine, controller, LDAPS, BaseDN, creds).
   - Arrêt + nettoyage automatique à la fin du test (`t.Cleanup`).

4. **`docs/AD_TEST_ENV.md`** — Comment lancer l'AD de test :
   - `task ad:start` (démarrage manuel pour test manuel/UI).
   - `task ad:reset` (reset propre du domaine).
   - `task ad:shell` (ldapsearch / samba-tool dans le container).
   - Ports mappés, identifiants, troubleshooting.

5. **Deux domaines** pour les tests multisite : `EDUSYNC.LOCAL` + `ANNEXE.LOCAL` (containers multiples).

### Validation "proche Windows Server"
Les tests d'intégration (priorité 3) couvrent explicitement les points où Samba ≠ Windows :
- Schema étendu (attributs personnalisés).
- GPO (lecture/écriture basique).
- Fine-grained password policies (PSO).
- LDAPS + certificat CA custom.
- Kerberos/GSSAPI (keytab, SPN).
- Recherche paginée, contrôle de référence (paged results, server-side sort).
- Fallback LDAP (389) avec avertissement.

---

## 6. Roadmap — les trois priorités dans l'ordre

> **Ordre arrêté le 14 octobre 2026** : **P1 logiciel Go complet → P2 alpha
> Windows + Linux → P3 campagne de tests.**
>
> - Chaque livraison de la P1 passe `task lint && task test && task test:python`
>   (boucle d'hygiène, non négociable — `AGENTS.md`).
> - **Aucune campagne de tests dédiée ne bloque un lot de la P1** : tests
>   d'intégration Samba, e2e, benchmarks et objectifs de couverture sont
>   regroupés en P3, après l'alpha.
> - La **version Python 1.17.1 reste en production** pendant toute la
>   migration : zéro rupture pour les utilisateurs.

### Priorité 1 — Finir le logiciel en Go

#### Lot A — Câblage de bout en bout du Core (M1 → M11)

*Méthode par module : backend existant → handlers HTTP réels → gabarit →
clés i18n fr/en → commande CLI → `task lint && task test` verts → commit.*

| # | Module | Travail réellement à faire (constat §0) |
|---|---|---|
| A1 | Connexion AD (M9) | Câbler `BindPassword` depuis la config chiffrée (TODO `server.go:438`), afficher les erreurs de connexion, avertissement repli LDAP 389 |
| A2 | Création de comptes (M1) | Implémenter `usersSimulate` / `usersExecute` (TODO) + 2 gabarits, doublons, export étiquettes depuis la page |
| A3 | Réinitialisation MDP (M4) | `passwordSimulate` / `passwordExecute` + 2 gabarits, politiques élèves/personnels, forçage changement |
| A4 | Migration (M2) | `migrationPlan` (depuis CSV), `migrationSimulate`, `migrationExecute` + 3 gabarits, groupes auto, dry-run exact |
| A5 | Explorateur (M5) | 3 gabarits (`explorer.html`, `explorer_results.html`, `explorer_tree.html`), `pool.Search`, `pool.BuildTree` (TODO) |
| A6 | Export (M6) | Gabarit `export.html`, vrais `ExportCSV` / `ExportLabelsPDF` (stubs actuels), sélection OU ± sous-OU, champs cochables |
| A7 | Journal (M7) | Gabarits `audit.html` / `audit_results.html`, filtrage réel (`auditFilter` TODO), export CSV |
| A8 | **Départs (M3)** | **Module à créer** : `internal/ad/depart.go` (désactivation / archivage + purge planifiée idempotente), routes, gabarit, CLI, i18n (`models.DepartRow` existe) |
| A9 | **Paramètres (M8)** | **Page à créer** : 4 onglets (comptes, mots de passe, apparence, délégation reportée en Lot C), routes, gabarit, i18n |
| A10 | Simulation (M11) | Généraliser `Validate → Simulate → Execute → Audit` sur A2-A8 : toute écriture AD passe par une prévisualisation |
| A11 | CLI Core | Commandes JSON de chaque module (import, reset, migration, départs, explorer, export, audit) |
| A12 | Progression / annulation | Barre de progression + `context.Context` annulable sur les opérations de masse (équivalent T0, §4.4) |
| A13 | Revue parity Core | Checklist §4.1 = ✅ vérifiée **dans le code**, i18n fr+en sur toutes les clés ajoutées |

**Sortie Lot A** : depuis la page de login jusqu'au journal, chaque module
Core fonctionne de bout en bout (UI + CLI) contre l'AD de test
(`task ad:start`, vérification **manuellement**, pas une campagne).

#### Lot B — Modules avancés (M12 → M24) + cache (T2)

| # | Module(s) | Points clés |
|---|---|---|
| B1 | M13 profils, M16 heures, M15 scripts | Attributs AD classiques, appliqués en masse à la création/migration |
| B2 | M17 homedirs, M14 quotas, M18 espaces de classe | Partages/NTFS/FSRM — prérequis serveur Windows côté client, gardez-le documenté |
| B3 | T2 cache AD | Cache SQLite OU/groupes/users, invalidation TTL + `uSNChanged` — fluidité explorateur |
| B4 | M12 photos | Import masse + cadrage, vignettes explorateur, intégration étiquettes (B8) |
| B5 | M22 imports/exports avancés | Import LDAP, export LDIF/vCard, publipostage HTML |
| B6 | M19 M365, M20 Exchange | Graph + On-prem/Online — tests manuels documentés (pas d'AD de test cloud) |
| B7 | M21 RDS | Collections, RemoteApp, UPD/FSLogix |
| B8 | M24 Label Studio + M23 modèles de groupes | Éditeur WYSIWYG, trombinoscope A4/A3, instanciation 1-clic |

#### Lot C — Modules v4.0 (M25 → M32) + coffre

| # | Module(s) | Points clés |
|---|---|---|
| C1 | M25 multisite | Modèle `Site`, config chiffrée par site, sélecteur header, pool par site, colonne `site_id` au journal |
| C2 | M26 RBAC | 5 rôles, portées OU/groupe, **garde-fou sur chaque écriture AD**, journal par opérateur |
| C3 | Coffre MDP + M32 sauvegarde | AES-256 local, backup ZIP chiffré, restore 1-clic, migrations vN→vN+1 |
| C4 | M27 portail auto-service | Serveur web stdlib (Go stdlib), code mail, PBKDF2 usage unique, RBAC, audit `utilisateur="portail"` |
| C5 | M28 API REST + webhooks | OpenAPI, webhooks SIS, sync bidirectionnelle |
| C6 | M29 IA + M30 conformité/sécurité | Anomalies, nettoyage fin d'année, RGPD/MFA/PSO/signature |
| C7 | M31 DFS/IIS/WebDAV/FTP | Priorité basse, livré en dernier |

#### Clôture de la Priorité 1 — « logiciel fini » (Porte 1, §12)

1. **`edusync-cli migrate --from-python`** : lit config/coffre/journal
   Python, valide, écrit au format Go, sauvegarde l'original en `.bak` (§8).
2. Checklist **§4 complète = ✅** vérifiée dans le code (M1 → M32, T2,
   transverses) — pas dans les intentions.
3. `gofmt -l`, `go vet`, `go test ./...`, `.venv/bin/pytest -q` tous verts.
4. Roadmap + `CLAUDE.md` reflètent le code.

### Priorité 2 — Release alpha Windows + Linux (≈ 1 à 2 semaines)

> **Format validé** : Windows **`EduSyncAD-Setup.exe`** (InnoSetup) ;
> Linux **AppImage** + **`.tar.gz`** (binaire brut). Chaque artefact avec
> son `.sha256`.

| # | Travail | Détail |
|---|---|---|
| P2.1 | **Nouveau workflow `release-go.yml`** | Déclenché sur les tags **`v0.*`**, `prerelease: true`. `release.yml` (Python, PyInstaller + Flatpak) reste en place pour les tags `v1.*` — **ne pas le modifier** |
| P2.2 | Job **Windows** | Runner `windows-latest` (Go + WebView2 + Inno Setup 6 déjà présents) → `wails build` → portage de `packaging/installer.iss` vers `build/inno/` (cible le binaire Go, icônes `assets/`) → `EduSyncAD-Setup.exe` + `.sha256` |
| P2.3 | Job **Linux** | Runner `ubuntu-latest` + `libwebkit2gtk-4.1-dev` → `build/appimage/` (WebKitGTK bundlé) → `EduSyncAD-linux.AppImage` + `edusync-ad-linux-amd64.tar.gz` + `.sha256` |
| P2.4 | **M10 — mise à jour intégrée** | L'updater Go vérifie le `.sha256` de la release, télécharge, installe, redémarre (modèle = `core/updater.py`) — validé contre la première alpha |
| P2.5 | **Checklist pré-alpha** (vérifications **manuelles**, pas une campagne) | ① `task lint && task test && task test:python` verts ② app démarre sous Xvfb (smoke `ci.yml`) ③ app **démarre sur Windows 10/11** réel ④ installation + désinstallation propres ⑤ relecture d'une config Python existante sans reconfiguration ⑥ connexion LDAPS réelle contre `task ad:start` ⑦ thème + i18n fr/en sur les modules livrés |
| P2.6 | **Notes de version alpha** | Périmètre couvert (§4), limites connues, installation (Setup.exe / `chmod +x` AppImage), appel à testeurs, avertissement « logiciel en cours de migration, la 1.17.1 reste la version de production » |
| P2.7 | **Publication** | Tag `v0.1.0-alpha.1` → `release-go.yml` → 3 artefacts + 3 `.sha256` en release **pré-release** GitHub |

**Sortie P2** : une alpha téléchargeable et installable sur **Windows** et
**Linux**, avec notes de version et sommes de contrôle.

### Priorité 3 — Campagne de tests (après l'alpha)

> Le socle existe déjà : `internal/testutil/samba.go`, `tests/integration/`,
> workflow `integration.yml`, smoke `ci.yml`. Détail §7.

| # | Chantier | Contenu |
|---|---|---|
| P3.1 | Couverture unitaire par module | Un test **qui échouerait sans l'implémentation** par fonctionnalité de §4 (M1 → M32) ; cible > 90 % |
| P3.2 | Intégration container Samba | `AD_TEST=1` + `integration.yml` : connexion, CRUD users, migration, reset MDP, explorateur, journal, multisite (2 containers), RBAC, départs/purge |
| P3.3 | E2E Playwright | Contre l'app Wails : login → chaque module → simulation → exécution → audit |
| P3.4 | Benchmarks vs Python | Import 1500 comptes, démarrage, RAM, navigation 10k objets — avant/après publiés |
| P3.5 | Charge + sécurité | 5k utilisateurs, gros CSV, fuzz des formulaires, vérif « aucun secret en clair » |
| P3.6 | Correctifs → beta → stable | Releases `v0.2.0-beta.x` (corrections issues alpha) puis **Porte 3** = version stable 1.0 (§12) |

---

## 7. Stratatégie de tests « IA-ready » — priorité 3

> **Déclenchée après la release alpha** (décision du 14 oct. 2026). Pendant
> les priorités 1 et 2, seule s'applique la boucle d'hygiène : `task lint &&
> task test` verts à chaque livraison, tests existants maintenus.

```
tests/
├── unit/                  # go test ./internal/... (rapide, mocks)
│   ├── ad/  config/  crypto/  csv/  label/  rbac/  multisite/
├── integration/           # AD_TEST=1 go test ./... (container Samba)
│   ├── ad_connection_test.go
│   ├── user_crud_test.go
│   ├── migration_test.go
│   ├── depart_test.go
│   ├── password_reset_test.go
│   ├── explorer_test.go
│   ├── audit_test.go
│   ├── multisite_test.go
│   └── rbac_test.go
├── e2e/                   # Playwright contre Wails (dev ou build)
│   ├── login.spec.ts  create_accounts.spec.ts  migration.spec.ts  ad_explorer.spec.ts
├── bench/                 # go test -bench=. -benchtime=10s
│   ├── ldap_pool_bench_test.go  csv_import_bench_test.go  migration_bench_test.go
└── fixtures/              # CSV exemples, exports LDIF, configs
    ├── eleves_1500.csv  personnels_200.csv  migration_restruct.csv
```

### Boucle IA (loop)

1. Tu donnes la spec d'un module → l'IA génère **les tests d'intégration d'abord**.
2. L'IA implémente jusqu'à ce que `go test` soit vert.
3. `task lint` (golangci-lint, staticcheck) + `task test` + `task build` en CI.
4. Tests E2E Playwright sur le binaire/Wails.
5. Erreurs → l'IA patche → boucle jusqu'à vert.
6. Review humaine → merge.

**Outillage pour l'IA :** `go vet`, `staticcheck`, `golangci-lint`, `go test -race`, benchmarks avec profils CPU/mém. Code Go verbose et explicite = lisible et corrigeable par l'IA.

**Fichiers de contexte IA** : `CLAUDE.md` + `AGENTS.md` (patterns, règles, prompts réutilisables).

---

## 8. Migration des données existantes

> **Zéro perte** : les utilisateurs Python existants doivent pouvoir passer à Go sans réconfiguration.

| Donnée Python | Stratégie Go | Compatibilité |
|---|---|---|
| Config chiffrée (platformdirs + AES) | Même platformdirs, même algo (AES-GCM), clé dérivée PBKDF2 | Lecture directe → réécriture Go au premier save |
| Coffre MDP (AES-256) | Même format, `golang.org/x/crypto` | Lecture directe |
| Journal audit SQLite | Même schéma append-only, même fichier | Réutilisation directe (ALTER TABLE si nouvelles colonnes) |
| Profils / Modèles | JSON/YAML → mêmes structs Go | Import transparent |
| Templates étiquettes | Fichiers compatibles | Migration 1:1 |

- **Script fourni** : `edusync-cli migrate --from-python --config-dir ... --vault-dir ...`
  → lit tout, valide, écrit au format Go, sauvegarde l'original en `.bak`.
  **Livraison** : fin de la Priorité 1 (clôture §6). **Validation aller-retour
  détaillée** (les fichiers Go relus par Python et inversement) : priorité 3.
- Déjà acquis : les tests `internal/config`, `internal/crypto`,
  `internal/audit` lisent des fixtures produites par Python.

---

## 9. Packaging et cross-build

### Une commande par cible

```bash
# Depuis Linux (OS de dev principal)
./build/cross-build.sh cli       # 5 cibles CLI, CGO_ENABLED=0
./build/cross-build.sh desktop   # détecte webkit2gtk / mingw, signale ce qui manque
```

- **CLI 100 % pur Go** (`CGO_ENABLED=0`) → cross-compile partout, sans outil système.
- **Desktop = cgo obligatoire** : Linux exige `libwebkit2gtk-4.1-dev`, Windows
  `mingw-w64` **ou** un runner natif Windows (solution retenue pour
  l'alpha, cf. `release-go.yml`). `cross-build.sh` **signale explicitement**
  les cibles non produites (jamais de « succès » silencieux).

### Cibles de distribution

| Cible | Méthode | Échéance |
|---|---|---|
| **Windows** | Runner `windows-latest` → `wails build` → **InnoSetup** (`build/inno/`) → `EduSyncAD-Setup.exe` + `.sha256` | **Alpha (P2)** |
| **Linux (AppImage)** | Runner ubuntu + webkit2gtk → `build/appimage/` → `EduSyncAD-linux.AppImage` + `.sha256` | **Alpha (P2)** |
| **Linux (binaire)** | `edusync-ad-linux-amd64.tar.gz` (binaire + `.desktop` + icône) | **Alpha (P2)** |
| **Linux (développeurs)** | `go build` direct | ✅ disponible |
| **macOS** | `GOOS=darwin` (nécessite un runner macOS) | Hors alpha (secondaire) |

> **Attention WebKitGTK (Linux/Wails)** : `libwebkit2gtk-4.1` est une
> dépendance système. Solution retenue = **AppImage** (bundle). Fallback =
> Flatpak léger uniquement pour WebKitGTK.
>
> **Taille desktop** : 17,2 MB aujourd'hui (cible < 15 MB) — optimisation
> (`-trimpath`, stripping du glue Wails) au chantier P2.

---

## 10. Prompts IA prêts à l'emploi

> **Pendant la Priorité 1** : les étapes « tests d'intégration d'abord » et
> `AD_TEST=1` deviennent « tests unitaires du paquet
> d'abord » (`internal/<x>/<x>_test.go`) — la forme intégration complète
> s'applique en priorité 3.

### Prompt 1 : nouveau module complet

```
Crée le module "X" pour EduSync-AD Go.

1. Lis internal/ad/ pour comprendre les patterns (pool, simulation, audit).
2. Écris D'ABORD les tests : internal/<x>/<x>_test.go + internal/ui/<page>_test.go
   (en priorité 3 : aussi tests/integration/x_test.go avec
   testutil.NewSambaADContainer()).
3. Implémente internal/<x>/handler.go + types dans internal/models/
   - Suit le pattern : Validate() → Simulate() → Execute() → Audit()
4. Crée les templates HTMX dans web/templates/<x>.html
   - Utilise les partials : table, form, modal, toast, tree
   - Stores Alpine pour l'état client
5. Ajoute les routes UI dans internal/ui/server.go + clés i18n fr/en.
6. Ajoute les commandes CLI dans cmd/edusync-cli/ (sortie JSON).
7. Lance : gofmt, go vet, go test ./... — tout vert.
8. Si échec → analyse, patch, relance. Si succès → commit.
```

### Prompt 2 : optimisation perf

```
L'import CSV de 1500 users prend 45 s. Profile avec
`go test -bench=BenchmarkCSVImport -cpuprofile=cpu.prof`.
Identifie les 3 goulots principaux. Propose des patches : tuning du pool
LDAP, modify LDAP en batch, prepared statements SQLite.
Implémente, re-benchmark, valide < 20 s.
```

### Prompt 3 : feature complexe (multisite)

```
Ajoute le support multisite (multi-domaine/forêt).
1. Modèle Site{Name, Domain, Controller, CredsEncrypted, BaseDN, TLSConfig}.
2. Config chiffrée par site (clé dérivée de la master key).
3. Sélecteur de site dans le header (Alpine store currentSite).
4. Pool de connexions par site (LRU, max 5 simultanées).
5. Journal audit : colonne site_id, filtre UI.
6. Tests (priorité 3) : 2 containers Samba (EDUSYNC.LOCAL + ANNEXE.LOCAL),
   switch de site, opérations croisées.
```

*(Voir `docs/IA_PROMPTS.md` pour la bibliothèque complète.)*

---

## 11. Risques et mitigations

| Risque | Prob. | Impact | Mitigation |
|---|---|---|---|
| **Périmètre complet avant alpha** (M12-M32 = 20 modules) | Moyenne | Alpha très retardée | Livraison par **lots A/B/C** avec checklist §4 visible ; Python 1.17.1 reste en production ; chaque module suit le même pattern éprouvé (§10) |
| **Volume du portage** (20 plugins Python) | Haute | Dilution de la qualité | Un module = une méthode (backend → UI → CLI → lint/test verts) ; prompts IA ; revue à la fin de chaque lot |
| **`release.yml` Python cassé par les tags Go** | Moyenne | Perte du canal release Python | Workflows **séparés** : `release.yml` (`v1.*`) intact, `release-go.yml` (`v0.*`) ; validation sur un tag de test pré-alpha |
| WebKitGTK Linux (Wails) | Moyenne | Build AppImage complexe | Runner ubuntu avec `libwebkit2gtk-4.1-dev`, script AppImage maison, test d'installation manuel (P2.5) |
| Dépendance WebView2 (Windows) | Faible | App non démarable | WebView2 présent sur Win 10/11 ; l'installateur InnoSetup détecte et télécharge le redistribuable si absent |
| Samba AD ≠ Windows AD | Faible | Différences schema/GPO | Tests ciblés (schema étendu, GPO, PSO, trusts) en priorité 3, documentation des écarts |
| Kerberos/GSSAPI Linux | Moyenne | Config keytab/SPN | `gokrb5` + docs, fallback NTLM si Kerberos KO, tests des deux voies en P3 |
| Migration config/vault | Faible | Perte de données | Script testé sur configs réelles (déjà : fixtures Python lues par Go), backup auto `.bak`, rollback documenté |
| SQLite sans CGO (modernc) | Faible | Perf SQL | Benchmarks en priorité 3 ; fallback CGO local si nécessaire (perte de cross-compile) |
| Taille desktop 17,2 MB | Faible | Cible 15 MB dépassée | Optimisation au chantier P2 (§9) |
| Courbe Go + Wails + HTMX | Moyenne | Vitesse initiale | Starter kit complet, patterns documentés, prompts IA, pair programming |

---

## 12. Critères d'acceptation — 3 portes

### Porte 1 — « Logiciel fini » (fin de la Priorité 1)

- [ ] **Inventaire §4 complet = ✅** vérifié dans le code : M1 → M32,
      fondations T2, coffre, transverses — feature parity avec la Python 1.17.1.
- [ ] Chaque écriture AD passe par `Validate → Simulate → Execute → Audit`.
- [ ] `gofmt -l cmd internal web` vide ; `go vet ./...` vert ;
      `go test ./...` vert ; `.venv/bin/pytest -q` vert.
- [ ] Toutes les clés i18n ajoutées existent en français **et** en anglais.
- [ ] `edusync-cli migrate --from-python` livré (§8).
- [ ] Roadmap + `CLAUDE.md` reflètent le code.

### Porte 2 — « Alpha publiée » (fin de la Priorité 2)

- [ ] `release-go.yml` déclenché par un tag `v0.1.0-alpha.1` produit :
      `EduSyncAD-Setup.exe`, `EduSyncAD-linux.AppImage`,
      `edusync-ad-linux-amd64.tar.gz` + leurs `.sha256`.
- [ ] `prerelease: true`, notes de version (périmètre §4, limites connues,
      installation, appel à testeurs).
- [ ] Checklist P2.5 passée (démarrage Windows réel, install/désinstall,
      relecture config Python, LDAPS contre l'AD de test, thème + i18n).
- [ ] `release.yml` (Python, tags `v1.*`) **intact et vérifié**.
- [ ] M10 (mise à jour intégrée) valide contre la release alpha.

### Porte 3 — « Stable 1.0 » (fin de la Priorité 3)

- [ ] **Zéro perte fonctionnelle** : feature parity vérifiée par les tests.
- [ ] **Tests verts en CI** : unit + intégration (container Samba) + e2e
      (Playwright) à chaque commit ; couverture > 90 %.
- [ ] **Perf** : démarrage < 100 ms ; import 1500 comptes < 30 s ; navigation
      explorateur fluide sur 10k+ objets ; RAM au repos 30-50 MB.
- [ ] **Binaire** : Windows + Linux (+ AppImage) < 15 MB, générés en une
      commande.
- [ ] **Migration données** : un utilisateur Python existant passe à Go sans
      reconfiguration manuelle (aller-retour Python ↔ Go validé).
- [ ] **Documentation** : guide utilisateur, guide AD de test, architecture,
      prompts IA à jour.
- [ ] **Benchmarks** : résultats comparés à la version Python publiés.

---

## Prochaines étapes

**Priorité 1 — finir le logiciel Go :**

1. **Lot A, A1** : câbler `BindPassword` depuis la config chiffrée + erreurs
   de connexion dans l'UI (débloque les modules suivants).
2. **Lot A, A2-A7** : finir le câblage des modules existants (handlers TODO
   + 13 gabarits manquants), module par module, `task lint && task test`
   verts à chaque fois.
3. **Lot A, A8-A9** : créer les modules **Départs** et **Paramètres**
   (backend + UI + CLI + i18n fr/en).
4. **Lot A, A10-A13** : simulation généralisée, CLI Core, progression /
   annulation, revue parity §4.1.
5. **Lot B** puis **Lot C** : modules avancés (M12-M24 + cache T2) puis
   v4.0 (M25-M32 + coffre).
6. **Clôture P1** : `edusync-cli migrate --from-python`, checklist §4
   complète → **Porte 1**.

**Priorité 2 — alpha Windows + Linux :**

7. `release-go.yml` (tags `v0.*`) + `build/inno/` + `build/appimage/` ;
   M10 auto-update ; checklist P2.5 ; tag `v0.1.0-alpha.1` → **Porte 2**.

**Priorité 3 — après l'alpha :**

8. Campagne de tests (§7) : intégration Samba (`AD_TEST=1`), e2e
   Playwright, couverture par module, benchmarks vs Python, charge.
9. Correctifs → `v0.2.0-beta.x` → **Porte 3** = stable 1.0.

---

> **Historique**
> - 12 octobre 2026 — Phase 0 livrée : starter kit, config, crypto, modèles,
>   audit, CLI, UI.
> - 13 octobre 2026 — gabarits UI « reset MDP » et « migration » (routes +
>   i18n fr/en) : amorçage de la Phase 1 côté interface.
> - 14 octobre 2026 — pool AD (TLS/SASL-GSSAPI), `internal/testutil`
>   (testcontainers Samba) et le workflow `integration.yml` livrés ;
>   **v2.0 de la feuille de route** : réécriture selon la décision
>   utilisateur **logiciel complet d'abord → alpha Windows + Linux →
>   campagne de tests ensuite** ; constats de code du câblage Core intégrés
>   au §0, périmètre alpha étendu à la migration complète (§4).
