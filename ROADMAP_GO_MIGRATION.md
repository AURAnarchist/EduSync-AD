# 🗺️ Feuille de route — Migration EduSync-AD : Python/PyQt6 → Go/Wails

> **Version** : 1.2 — 14 octobre 2026
> **Objectif** : Réécrire entièrement EduSync-AD en Go (backend + UI desktop via Wails) **sans perte de fonctionnalité**, avec une architecture testable et "IA-ready" (l'IA peut tester, détecter les erreurs et patcher).
> **Timeline** : ~14 semaines, qualité > vitesse.
>
> **⚠️ Priorité revue le 14 octobre 2026** (décision utilisateur) :
> **1) finir le logiciel en Go** (tous les modules de l'inventaire §4),
> **2) sortir une release alpha**, **3) ensuite seulement**, campagne de
> tests (unitaires, intégration container Samba, e2e). Les tests ne sont
> pas supprimés — ils sont **décalés après l'alpha**. L'installation
> testcontainers/integration est livrée en parallèle mais n'est plus le
> jalon bloquant.

---

## 0. État d'avancement (mise à jour : 14 octobre 2026)

> Statut vérifié **dans le code et les tests**, pas dans les intentions.

### Phase 0 (semaines 1-2)

| Jours | Livrable | Statut | Où |
|---|---|---|---|
| 1-2 | Starter kit : repo Go, `go.mod`, `Taskfile.yml`, CI, `CLAUDE.md`/`AGENTS.md`, cross-build | ✅ Livré | `go.mod`, `Taskfile.yml`, `.github/workflows/ci.yml`, `build/cross-build.sh` |
| 3-4 | Config chiffrée multi-profil, lecture des fichiers Python | ✅ Livré (testé) | `internal/config` (+ fixture `testdata/config_python.json` généré par Python) |
| 3-4 | Crypto compatible Python (AES-256-GCM) | ✅ Livré (testé) | `internal/crypto`, avec vecteur d'interopérabilité Python → Go |
| 5-6 | **Core AD : pool de connexions + TLS + SASL/GSSAPI** | ✅ Livré | `internal/ad/pool.go` (pool, health check, LDAPS/StartTLS, `AuthMethod` simple/NTLM/SASL-GSSAPI + client `gokrb5`) |
| 7-8 | Modèles domaine + audit SQLite | ✅ Livré (testé) | `internal/models`, `internal/audit` (schéma identique à Python, append-only) |
| 9-10 | Testutil container Samba + fixtures | ✅ Livré (hors campagne de tests) | `internal/testutil/samba.go` (testcontainers-go), `Dockerfile.samba-ad`, `provision-ad.sh`, `docs/AD_TEST_ENV.md` |
| 11-12 | CLI squelette | ✅ Livré (testé) | `cmd/edusync-cli` : `version`, `paths`, `config show`, `audit list`, `connect` (DNS + TCP + TLS) |
| 13-14 | Wails v3 + HTMX/Alpine/Tailwind embeddé | ✅ Livré (testé) | `cmd/edusync-ad`, `internal/ui`, `web/` — login, dashboard, thème clair/sombre, i18n fr/en + gabarits `users`, `password`, `migration` |

> Workflow `.github/workflows/integration.yml` ajouté (jobs
> `integration` / `integration-multisite`, portée `AD_TEST=1`) — exécution
> décalée à la campagne de tests post-alpha.

### Phase 1 (amorcée côté UI — semaine 3)

| Module | Avancement constaté | Où |
|---|---|---|
| Réinitialisation MDP | 🔨 Gabarit HTMX + routes + i18n fr/en livrés (logique AD à brancher) | `web/templates/password.html`, `internal/ui/server.go`, `internal/ui/i18n.go` |
| Migration / restructuration | 🔨 Gabarit HTMX + routes + i18n fr/en livrés (logique AD à brancher) | `web/templates/migration.html`, `internal/ui/server.go`, `internal/ui/i18n.go` |
| Création de comptes / Arrivées | ⬜ Backend `internal/ad/user.go` prêt (CRUD + Simulate), handler + UI à brancher | `internal/ad/user.go`, `web/templates/users.html` |
| Gestion des départs | ⬜ À implémenter | — |
| Explorateur AD | ⬜ Backend `internal/ad/explorer.go` prêt, UI à brancher | `internal/ad/explorer.go` |
| Export CSV/PDF | ⬜ Backend `internal/ad/export.go` prêt (Avery, QR, trombinoscope), UI à brancher | `internal/ad/export.go` |
| Journal d'actions | ⬜ Backend `internal/audit` prêt, UI à brancher | `internal/audit` |

### Mesures constatées (Linux amd64 — Go 1.27.2, Wails v3.0.0-beta.28, WebKitGTK 2.54.1)

| Critère | Cible | Réel |
|---|---|---|
| Tests Go | — | **65 tests, 6 paquets, tous verts** (`task test`) |
| CLI cross-compilée | toutes cibles | **5 cibles** produites en une commande (`./build/cross-build.sh cli`) |
| Taille CLI (`-s -w`) | 8-12 MB | **9,5 MB** ✅ |
| Taille app desktop (`-s -w`) | < 15 MB | **17,2 MB** ⚠️ (glue cgo/WebKit — optimisation à venir) |
| Démarrage de l'app | < 100 ms | à mesurer (démarrage validé sous Xvfb, smoke test) |
| CI | vertes | `ci.yml` : job Go (fmt/vet/test/build/smoke) + job Python (pytest) |

### Décisions d'implémentation (à conserver)

1. **Tailwind compilé puis versionné** (`web/static/css/tailwind.css`) : aucun
   build obligatoire pour un contributeur ni pour l'IA ; `task web:css`
   régénère après modification des gabarits. Source unique :
   `web/static/css/input.css` (`source(none)` → seuls `web/templates` et
   `web/static/js` sont scannés).
2. **Aucun service Wails lié au JS pour l'instant** : l'UI est intégralement
   rendue côté serveur (HTMX), ce qui supprime une couche à maintenir. Les
   bindings (`wails3 generate bindings`) arriveront avec les modules Phase 1
   qui en ont réellement besoin.
3. **CLI sans cgo** : `internal/*` reste pur Go (`modernc.org/sqlite`), d'où
   la compilation croisée sur 5 cibles sans outil système.
4. **Parité Python ↔ Go exigée par les tests** : format de config JSON,
   format de chiffrement, schéma SQLite et chemins platformdirs sont couverts
   par des tests aller-retour (données produites par Python lues par Go).

### Reste de la Phase 0

- `internal/ad` : pool de connexions (LRU, santé, retry/backoff), LDAPS,
  CA custom, SASL/GSSAPI — **étape bloquante pour les Phases 1+**.
- `internal/testutil` + tests d'intégration `-tags=integration` contre le
  container Samba (une fois livré).
- Workflow `integration.yml` (tests AD réels) à ajouter.

---

## Table des matières

1. [Contexte et motivations](#1-contexte-et-motivations)
2. [Décisions validées](#2-décisions-validées)
3. [Architecture cible](#3-architecture-cible)
4. [Inventaire des fonctionnalités à migrer](#4-inventaire-des-fonctionnalités-à-migrer)
5. [Environnement de test AD](#5-environnement-de-test-ad)
6. [Roadmap par phases](#6-roadmap-par-phases)
7. [Stratégie de tests "IA-ready"](#7-stratégie-de-tests-ia-ready)
8. [Migration des données existantes](#8-migration-des-données-existantes)
9. [Packaging et cross-build](#9-packaging-et-cross-build)
10. [Prompts IA prêts à l'emploi](#10-prompts-ia-prêts-à-lemploi)
11. [Risques et mitigations](#11-risques-et-mitigations)
12. [Critères d'acceptation global](#12-critères-dacceptation-global)

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
| OS de dev principal | **Linux** | Cross-compile Go natif, containers Docker/Podman, Wails dev mode |
| Environnement de test AD | **Samba AD DC en container** (`testcontainers-go`) | Vrai AD (LDAP, LDAPS, Kerberos, DNS, GPO, schema) — se rapproche d'un Windows Server, démarre en 30 s, isolé, reproductible, gratuit |
| Timeline | **~14 semaines, qualité > vitesse** | Pas de deadline dure, focus robustesse + IA-ready |
| Ordre des modules | **Core v1.x d'abord** (création, migration, départs, MDP, explorateur, export, audit, simulation) → puis v4.0 (multisite, RBAC, portail, API, IA, RGPD) | Fondations solides, valeur immédiate, zéro perte fonctionnelle |
| Frontend | **HTMX 2.0 + Alpine.js + Tailwind CSS** (embedded via `//go:embed`) | Zéro build JS, HTML-first (l'IA génère du HTML), < 50 KB JS, reactif, parfait avec Wails, pas de node_modules |
| SQLite | `modernc.org/sqlite` (pure Go, **sans CGO**) | Garde le cross-compile trivial (`CGO_ENABLED=0`) |
| Licence | MIT (identique à l'existant) | Compatibilité |
| Tests d'intégration | `testcontainers-go` + image Samba AD maison | Un vrai AD dans chaque test |

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
│   │   ├── login.html
│   │   ├── dashboard.html
│   │   ├── modules/           # Un fichier HTML par module
│   │   └── partials/          # Composants réutilisables (table, form, tree, toast, modal)
│   └── static/
│       ├── css/tailwind.css
│       ├── js/alpine.stores.js
│       └── assets/
├── build/
│   ├── cross-build.sh         # Matrice GOOS/GOARCH + ldflags
│   ├── appimage/              # AppImage Linux
│   └── inno/                  # InnoSetup Windows (.iss)
├── .github/workflows/
│   ├── ci.yml                 # Test, lint, build multi-arch
│   ├── integration.yml        # Tests AD réels (container Samba)
│   └── release.yml            # Tag → build → artefacts → release notes
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

## 4. Inventaire des fonctionnalités à migrer

> **Règle : zéro perte fonctionnelle.** Chaque ligne ci-dessous doit exister en Go avant de déclarer la migration terminée.

### Phase Core v1.x

| Module | Fonctionnalités | Statut |
|---|---|---|
| **Connexion AD** | LDAPS par défaut (636), fallback LDAP explicite avec avertissement, certificat CA custom, pool de connexions, Kerberos/GSSAPI, auto-discovery DC | ✅ `internal/ad/pool.go` (auto-discovery : partiel) |
| **Création de comptes / Arrivées** | Import CSV (encodage + séparateur auto), génération identifiants/MDP/mail, résolution doublons (incrément), vérification AD existants, export CSV étiquettes | 🔨 Backend user prêt, handler/UI à brancher |
| **Migration (restructuration)** | Via CSV (prénom/nom/équipe) ou UI (sélection OU), déplacement AD + groupes auto, résolution par prénom/nom | 🔨 Backend prêt (`migration.go`), UI à brancher |
| **Gestion des départs** | Désactivation immédiate ou archivage + purge planifiée (délai configurable, 30 j par défaut) | ⬜ |
| **Réinitialisation MDP** | Par OU, groupe AD ou CSV, politiques différées (élèves/personnels), forçage changement à la prochaine connexion | 🔨 Backend prêt (`password.go`), UI à brancher |
| **Explorateur AD** | Arborescence OUs/groupes, panneau central unifié, clic droit complet (modif, move, MDP, groupes, suppr), recherche temps réel | 🔨 Backend prêt (`explorer.go`), UI à brancher |
| **Export (CSV / Étiquettes PDF)** | Sélection OU ± sous-OU, champs cochables (incl. MDP mémorisés), PDF Avery L7160/L7163, QR codes, couleurs pastel | 🔨 Backend prêt (`export.go`), UI à brancher |
| **Journal d'actions** | Filtres date/type/résultat/domaine/opérateur, export CSV, SQLite append-only | 🔨 Backend prêt (`internal/audit`), UI à brancher |
| **Mode simulation** | Toute écriture AD = prévisualisation modifiable avant validation | 🔨 Pattern `simulation.go` prêt, câblage UI à faire |

### Phase v4.0 (livrée en Python)

| Module | Fonctionnalités | Statut |
|---|---|---|
| **Profils / Modèles** | Profils de config réutilisables, modèles groupes/OU | ⬜ |
| **Étiquettes avancées** | Éditeur WYSIWYG, badges/cartes, trombinoscope photo A4/A3, envoi étiquette par mail | ⬜ |
| **Multisite** | Plusieurs domaines/forêts, profils chiffrés, sélecteur de site, journal par domaine | ⬜ |
| **RBAC (délégation)** | 5 rôles, portées par OU/groupe, garde-fou sur chaque écriture AD, journal filtrable par opérateur | ⬜ |
| **Portail auto-service** | Réinit MDP par code mail, consultation identifiant/mail, demande création compte avec validation admin (serveur web stdlib) | ⬜ |
| **API REST + Webhooks** | Intégration SIS (PRONOTE), notifications (Teams, Slack, mail, SIG), sync bidirectionnelle | ⬜ |
| **IA / Assistant** | Détection anomalies (doublons, orphelins), suggestion nettoyage fin d'année, identifiants "intelligents", analyse logs → recommandations sécurité | ⬜ |
| **Conformité RGPD** | Rapport, droit à l'oubli, chiffrement exports | ⬜ |
| **Sécurité avancée** | MFA admin, session unique, granularité MDP (PSO), signature numérique exports | ⬜ |
| **Sauvegarde & restauration** | Backup ZIP, restore 1-clic, migration vN→vN+1 | ⬜ |
| **Coffre MDP** | AES-256 local (machines admin), jamais sur l'AD | ⬜ |
| **Mise à jour intégrée** | Vérification, téléchargement, installation, redémarrage auto, somme de contrôle | ⬜ |
| **Home dirs / Quotas / Logon hours / Logon scripts / Class spaces / M365 / Exchange / RDS / Advanced IO / Group templates / Label Studio** | Modules v4.x existants (voir `pyproject.toml` entry-points) | ⬜ |
| **Portails modules complémentaires** | `advanced_io`, `templates`, `rds`, `exchange`, `m365`, `class_spaces`, `logon_scripts`, `logon_hours`, `quotas`, `homedirs`, `profiles`, `label_studio` | ⬜ |

### Fonctionnalités transverses

| Fonctionnalité | Statut |
|---|---|
| Configuration chiffrée (domaine, utilisateur) via platformdirs + AES | ✅ |
| Thème dark/light, i18n fr/en | ✅ |
| Historique/mode démo VM (protocole de test) | ⬜ |

---

## 5. Environnement de test AD

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
Les tests d'intégration couvrent explicitement les points où Samba ≠ Windows :
- Schema étendu (attributs personnalisés).
- GPO (lecture/écriture basique).
- Fine-grained password policies (PSO).
- LDAPS + certificat CA custom.
- Kerberos/GSSAPI (keytab, SPN).
- Recherche paginée, contrôle de référence (paged results, server-side sort).
- Fallback LDAP (389) avec avertissement.

---

## 6. Roadmap par phases

> **Ordre révisé (14 oct. 2026)** : priorité = **finir le logiciel Go**
> (Phases 1 → 2 → 3 partiels) puis **release alpha**, la campagne de
> tests complète (§7) intervenant **après l'alpha**. Les tests ne
> bloquent plus l'avancement des modules : chaque module est livré avec
> ses tests existants verts (`task test`), la montée en puissance des
> tests (intégration Samba, e2e Playwright, charge) est un chantier
> séparé post-alpha.

### Phase 0 : Fondations (Semaines 1-2) — ✅ livrée

| Jour | Livrable | Critère de passage |
|---|---|---|
| 1-2 | **Starter kit complet** (repo, CI, Taskfile, structure, CLAUDE.md) | `task dev` lance Wails en dev, `task test` passe, `task build:all` sort les binaires multi-cibles |
| 3-4 | **Config chiffrée multi-profil** (lecture/écriture compatible Python) | Importe config Python existante, chiffre/déchiffre, switch de profil |
| 5-6 | **Core AD : pool de connexions + TLS + SASL/GSSAPI** | Connexion LDAPS + Kerberos OK, pool géré, retry/backoff, health check |
| 7-8 | **Modèles domaine + audit SQLite** | User/Group/OU/Config/AuditEntry sérialisables, migrations SQL, index |
| 9-10 | **Testutil container Samba + fixtures** | `go test -tags=integration ./internal/ad/...` passe à 100 % |
| 11-12 | **CLI squelette** (`edusync-cli connect`, `edusync-cli test-ad`) | Connexion AD réelle, liste OU/users, sortie JSON |
| 13-14 | **Wails v3 + HTMX/Alpine/Tailwind embed** | `wails dev` → login fonctionnel, thème dark/light, i18n fr/en |

**Sortie Phase 0** : base technique solide, AD de test automatisé, cycle dev < 2 s (hot reload Wails).

### Phase 1 : Modules core v1.x (Semaines 3-7)

*Méthode par module (révisée) : implémentation → branchement UI HTMX →
bindings CLI → `task lint && task test` verts → commit. Les tests
d'intégration dédiés (container Samba) sont écrits dans la campagne
post-alpha, sauf si un test existant couvre déjà le cas.*

| Semaine | Module | Points clés (validation manuelle + `task test`) |
|---|---|---|
| **3** | **Création de comptes** | Import CSV (encodage/séparateur auto), doublons gérés, politiques MDP, export étiquettes |
| **4** | **Migration / restructuration** | Déplacement cross-OU, groupes auto, dry-run exact |
| **5** | **Gestion des départs** | Archivage + purge planifiée idempotente |
| **6** | **Réinitialisation MDP** | Politiques appliquées, MDP conformes ANSSI, forçage changement |
| **7** | **Explorateur AD** | Navigation fluide, recherche temps réel, actions atomiques |

**En parallèle (semaines 5-7)** : **Export CSV/PDF**, **Journal d'actions**, **Mode simulation** (transverses à tous les modules).

### Phase 2 : Modules avancés (Semaines 8-11)

| Semaine | Module | Points clés |
|---|---|---|
| **8** | **Profils / Modèles / Étiquettes avancées** | Label Studio WYSIWYG, badges/cartes, trombinoscope A4/A3, envoi mail, QR codes |
| **9** | **Multisite** | 2 domaines simultanés, sélecteur de site, journal par domaine, pools séparés |
| **10** | **RBAC** | 5 rôles, portées OU/groupe, garde-fou sur chaque écriture, journal par opérateur |
| **11** | **Portail auto-service + API REST** | Serveur stdlib, réinit par code mail, validation admin, OpenAPI, webhooks |

### Phase 3 : IA / Conformité / Finitions (Semaines 12-14)

| Semaine | Module | Points clés |
|---|---|---|
| **12** | **IA Assistant** | Détection doublons/orphelins/MDP faibles, nettoyage fin d'année, analyse logs |
| **13** | **Conformité RGPD + sécurité** | Rapport RGPD, droit à l'oubli, MFA TOTP, session unique, PSO, signature, backup/restore, migration vN→vN+1 |
| **14** | **Polish + packaging + docs** | Matrice cross-build, AppImage, InnoSetup, signatures, release notes auto, guide utilisateur, tests charge (5k users), benchmarks vs Python |

---

## 7. Stratégie de tests "IA-ready"

> **Reportée après la release alpha** (décision du 14 oct. 2026). Le
> socle existe déjà : `internal/testutil/samba.go`, tests
> `-tags=integration` dans `tests/integration/`, workflow
> `integration.yml`. La campagne complète (couverture par module, e2e
> Playwright, charge) est un chantier post-alpha dédié.

```
tests/
├── unit/                  # go test ./internal/... (rapide, mocks)
│   ├── ad/  config/  crypto/  csv/  label/  rbac/  multisite/
├── integration/           # go test -tags=integration ./... (container Samba)
│   ├── ad_connection_test.go
│   ├── user_crud_test.go
│   ├── migration_test.go
│   ├── password_reset_test.go
│   ├── explorer_test.go
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

**Script fourni** : `edusync-cli migrate --from-python --config-dir ... --vault-dir ...`
→ lit tout, valide, écrit au format Go, sauvegarde l'original en `.bak`.

---

## 9. Packaging et cross-build

### Une commande par cible

```bash
# Depuis Linux (OS de dev principal)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/edusync-ad.exe ./cmd/edusync-ad
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/edusync-ad          ./cmd/edusync-ad
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/edusync-ad-arm64    ./cmd/edusync-ad
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/edusync-ad-mac      ./cmd/edusync-ad
```

- **`CGO_ENABLED=0`** → cross-compile 100 % natif, aucun outil système requis.
- **Wails** : `wails build -platform windows/amd64,linux/amd64,darwin/arm64` gère le backend Go + le bundle frontend automatiquement.

### Cibles de distribution

| Cible | Méthode | Notes |
|---|---|---|
| **Windows** | `go build` → `.exe` → **InnoSetup** (`build/inno/`) → `EduSyncAD-Setup.exe` | WebView2 présent sur Win 10/11 ; installateur peut le télécharger si absent. Signature code (certificat) optionnelle. |
| **Linux (générique)** | Binaire + `.desktop` + icône, ou `.tar.gz` | Remplace l'ancien Flatpak quand possible |
| **Linux (AppImage)** | `build/appimage/` | Bundlé WebKitGTK → un fichier unique, zéro dépendance |
| **Linux (développeurs)** | `go build` direct | Un seul fichier exécutable |
| **macOS** | `GOOS=darwin` (Wails) | Secondaire |

> **Attention WebKitGTK (Linux/Wails)** : `libwebkit2gtk-4.1` est une dépendance système. Solution principale = **AppImage** (bundle). Fallback = Flatpak léger uniquement pour WebKitGTK.

---

## 10. Prompts IA prêts à l'emploi

### Prompt 1 : nouveau module complet

```
Crée le module "X" pour EduSync-AD Go.

1. Lis internal/ad/ pour comprendre les patterns (pool, simulation, audit).
2. Écris D'ABORD les tests d'intégration dans tests/integration/x_test.go
   - Utilise testutil.NewSambaADContainer()
   - Couvre : succès, erreurs, simulation, edge cases
3. Implémente internal/x/handler.go + types dans internal/models/
   - Suit le pattern : Validate() → Simulate() → Execute() → Audit()
4. Ajoute les bindings Wails dans cmd/edusync-ad/main.go (wails.Bind).
5. Crée les templates HTMX dans web/templates/modules/x.html
   - Utilise les partials : table, form, modal, toast, tree
   - Stores Alpine pour l'état client
6. Ajoute les commandes CLI dans cmd/edusync-cli/x.go
7. Lance : go test -tags=integration ./tests/integration/... -run TestX
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
6. Tests : 2 containers Samba (EDUSYNC.LOCAL + ANNEXE.LOCAL),
   switch de site, opérations croisées.
```

*(Voir `docs/IA_PROMPTS.md` pour la bibliothèque complète.)*

---

## 11. Risques et mitigations

| Risque | Prob. | Impact | Mitigation |
|---|---|---|---|
| WebKitGTK Linux (Wails) | Moyenne | Build AppImage complexe | Script AppImage inclus, testé en CI, fallback Flatpak |
| Samba AD ≠ Windows AD | Faible | Différences schema/GPO | Tests ciblés (schema étendu, GPO, PSO, trusts), documentation des écarts |
| Kerberos/GSSAPI Linux | Moyenne | Config keytab/SPN | `gokrb5` + docs, fallback NTLM si Kerberos KO, tests des deux voies |
| Migration config/vault | Faible | Perte de données | Script testé sur configs réelles, backup auto, rollback documenté |
| Courbe Go + Wails + HTMX | Moyenne | Vitesse initiale | Starter kit complet, patterns documentés, prompts IA, pair programming |
| SQLite sans CGO (modernc) | Faible | Perf SQL | Benchmarks intégrés ; fallback CGO local si nécessaire (perte de cross-compile) |
| Dépendance WebView2/WebKit | Moyenne | Installateur Windows | WebView2 redistributable, détection + téléchargement auto |

---

## 12. Critères d'acceptation global

> **Jalon alpha (avant ces critères complets)** : la **release alpha** est
> éligible dès que l'inventaire Phase Core v1.x (§4) est câblé de bout en
> bout (UI + CLI), que `task lint && task test && task test:python` sont
> verts et que `release.yml` produit les binaires multi-cibles. Les
> critères ci-dessous (intégration/e2e en CI, benchmarks, couverture)
> conditionnent la **release stable 1.0**, pas l'alpha.

La migration est terminée quand **tous** ces critères sont réunis :

- [ ] **Zéro perte fonctionnelle** : chaque module de l'inventaire (§4) fonctionne en Go avec feature parity vérifiée.
- [ ] **Tests verts** : unit + integration (container Samba) + e2e (Playwright) en CI à chaque commit.
- [ ] **Perf** : démarrage < 100 ms ; import 1500 comptes < 30 s ; navigation explorateur fluide sur 10k+ objets ; RAM au repos 30-50 MB.
- [ ] **Binaire** : Windows `.exe` + Linux (+ AppImage) < 15 MB, générés par une commande.
- [ ] **Migration données** : un utilisateur Python existant passe à Go sans reconfiguration manuelle (script de migration validé).
- [ ] **Documentation** : guide utilisateur mis à jour, guide de l'AD de test, architecture, prompts IA.
- [ ] **Benchmarks** : résultats comparés à la version Python publiés (avant/après).
- [ ] **Mode simulation** : toute écriture AD passe par `Validate → Simulate → Execute → Audit`.

---

## Prochaines étapes

**Priorité 1 — finir le logiciel Go (jusqu'à l'alpha) :**

1. **Semaine 3** : module **Création de comptes / Arrivées**
   (handler HTTP + bindings + `web/templates/users.html`) :
   import CSV, génération identifiants/MDP/mail, doublons, étiquettes.
2. **Semaines 4-7** : modules **Migration**, **Départs**, **Reset MDP**,
   **Explorateur** — backend souvent déjà prêt, reste le câblage
   UI + CLI.
3. **Transverses** : Export CSV/PDF, Journal, Simulation branchés sur
   l'UI.
4. **Release alpha** : `release.yml` sur tag `v0.x.0-alpha`, binaires
   multi-cibles, notes de version (« feature parity Phase Core v1.x »).

**Priorité 2 — après l'alpha :**

5. Campagne de tests (§7) : intégration Samba complète (`AD_TEST=1`),
   e2e Playwright, benchmarks vs Python, couverture.
6. Phase 2 (multisite, RBAC, portail, API) puis Phase 3 (IA, RGPD,
   packaging).
7. Aligner le budget binaire : l'app desktop pèse 17,2 MB (cible
   15 MB) — à traiter au packaging.

> Historique : la Phase 0 (hors pool AD et testutil) a été livrée le
> 12 octobre 2026 — starter kit, config, crypto, modèles, audit, CLI, UI.
> Les 13 octobre 2026, les gabarits UI « reset MDP » et « migration »
> (routes + i18n fr/en) ont amorcé la Phase 1 côté interface.
> Le 14 octobre 2026 : pool AD (TLS/SASL-GSSAPI), `internal/testutil`
> (testcontainers Samba) et le workflow `integration.yml` ont été livrés,
> et la priorité a été revue : **logiciel d'abord, alpha, tests ensuite**.
