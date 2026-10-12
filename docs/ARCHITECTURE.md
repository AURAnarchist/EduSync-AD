# Architecture — EduSync AD (version Go)

> Cible de la migration décrite dans `ROADMAP_GO_MIGRATION.md`.
> Ce document décrit **ce qui existe**. Toute divergence avec le code est un
> bug à corriger ici ou dans le code.

## 1. Principes

1. **Une seule opération AD = `Validate() → Simulate() → Execute() → Audit()`**.
   Le mode simulation est le chemin par défaut, pas une option ajoutée après
   coup : aucune écriture AD ne peut contourner la prévisualisation.
2. **Le cœur est UI-agnostic** : `internal/*` ne dépend d'aucune interface.
   Le desktop (Wails), la CLI et l'API (à venir) partagent les mêmes paquets.
3. **Compatibilité de données avec Python** : tant que la migration n'est pas
   terminée, `config.json`, `secret.key` et `journal.db` restent lisibles et
   écrits par les deux implémentations. Des tests aller-retour le garantissent.
4. **Journal append-only** : traçabilité non réfutable, aucune API de
   suppression.
5. **Zéro dépendance inutile** : stdlib d'abord ; `modernc.org/sqlite` (sans
   CGO) et `wails/v3` sont les seules exceptions actuelles.

## 2. Flux d'une requête UI

```
Navigateur (WebView Wails)
   │  fetch / HTML + HTMX
   ▼
cmd/edusync-ad  ──►  internal/ui (http.Handler)
                        │  résout la route, la session, la langue
                        │  construit PageData
                        ▼
                   web/templates/*.html  (html/template, //go:embed)
                        │  {{tr .Langue "clé"}}  → internal/ui/i18n.go
                        ▼
                   HTML (fragments HTMX pour les erreurs 422)
   │
   ▼
internal/audit  ←  chaque action significative est journalisée
internal/config ←  thème / langue persistés dans config.json
```

Points remarquables :

- **Tout fonctionne sans JavaScript** : les formulaires HTML purs sont
  utilisables ; HTMX n'apporte que les erreurs en ligne et les redirections
  `HX-Redirect`.
- **Deux mécanismes d'état** : la session d'authentification (cookie
  `edusync_sid`, en mémoire, 12 h) et les préférences d'affichage (cookies +
  `config.json`, lisibles par Python).
- **Sécurité** : cookie `HttpOnly` + `SameSite=Strict`, redirections réduites
  au chemin local (pas d'open redirect), échappement systématique
  (`html/template`), saisie de formulaire validée côté serveur.

## 3. Paquets

| Paquet | Responsabilité | Dépendances |
|---|---|---|
| `internal/models` | Types de domaine (User, Group, OU, policies, entrée de journal) | — |
| `internal/config` | `config.json`, chemins `platformdirs`, écriture atomique | `models` |
| `internal/crypto` | AES-256-GCM, `secret.key` 0600, format identique à Python | — |
| `internal/audit` | SQLite append-only, schéma identique à Python, filtres | `models`, `config` |
| `internal/ui` | Routes, sessions, i18n, rendu des gabarits | `audit`, `config`, `models`, `web` |
| `web` | Gabarits HTML + assets embarqués (`//go:embed`) | — |
| `internal/ad` | *à venir* : pool de connexions LDAP/LDAPS/GSSAPI | — |
| `cmd/edusync-ad` | Fenêtre Wails, montage du serveur d'UI | `internal/ui` |
| `cmd/edusync-cli` | Scripts, CI, IA — sorties JSON | `audit`, `config` |

## 4. Formats de données partagés avec Python

| Donnée | Format | Garantie |
|---|---|---|
| `config.json` | JSON UTF-8, clés `snake_case` françaises, indenté 2, `ensure_ascii=False` | `internal/config` : test aller-retour sur un fichier produit par Python |
| `secret.key` | 32 octets bruts, permissions 0600 | `internal/crypto` : vecteur d'interopérabilité Python → Go |
| Chiffrement | `base64(nonce(12) ‖ AES-256-GCM)` | idem |
| `journal.db` | SQLite, tables `actions` + `pending_deletions`, mêmes colonnes et mêmes `ALTER TABLE` | `internal/audit` : test de schéma |

Chemin (équivalent `platformdirs`) :

- Linux : `~/.config/EduSyncAD`, `~/.local/share/EduSyncAD`, `~/.cache/EduSyncAD`
- macOS : `~/Library/Application Support/EduSyncAD`, `~/Library/Caches/EduSyncAD`
- Windows : `%APPDATA%\EduSyncAD`, `%LOCALAPPDATA%\EduSyncAD`

## 5. Concurrence (état actuel)

- Un `http.Handler` par app : les requêtes arrivent en goroutines ; l'accès à
  la table des sessions est protégé par `sync.Mutex`.
- SQLite est ouvert sur **une seule connexion** (`SetMaxOpenConns(1)`) avec
  `busy_timeout(5000)` : identique au modèle `sqlite3.connect` de Python et
  compatible avec une application Python encore ouverte sur le même fichier.
- Le pool de connexions AD (LRU, santé, retry/backoff) n'existe pas encore —
  c'est l'étape des jours 5-6 de la Phase 0.

## 6. Points d'extension

| Besoin | Où intervenir |
|---|---|
| Nouvelle page | `web/templates/x.html` + route dans `internal/ui` + clés i18n |
| Nouvelle commande CLI | `cmd/edusync-cli/<fichier>.go` + `run()` |
| Nouvelle action AD | `internal/ad` (à venir) avec `Validate → Simulate → Execute → Audit` |
| Module Phase 1 | paquet dédié + page + commande CLI + tests d'abord |
| Appels Go ⇄ JS | `wails3 generate bindings` (volontairement absent du starter kit) |

## 7. Tests

- **Unitaires** (`go test ./...`) : aucune dépendance externe, chemins
  redirigés vers `t.TempDir()`.
- **HTTP** (`internal/ui`) : `httptest` sur le vrai `http.Handler`, en
  navigation normale **et** en requêtes HTMX.
- **Intégration AD** (à venir) : `//go:build integration` +
  `internal/testutil` contre le container Samba (`docs/AD_TEST_ENV.md`).
- **Python** (`.venv/bin/pytest`) : la version publiée reste testée à chaque
  commit (`ci.yml`).
