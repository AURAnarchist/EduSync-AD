# EduSync AD — Contexte pour l'IA

> Fichier de contexte : à lire **avant** toute modification. Conventions,
> commandes, règles et prompts réutilisables.
> Roadmap : `ROADMAP_GO_MIGRATION.md` (migration Python → Go) et
> `docs/ROADMAP_python_v1.md` (périmètre fonctionnel).

## Vue d'ensemble

Le dépôt contient **deux implémentations** de la même application :

| Stack | État | Où |
|---|---|---|
| **Python / PyQt6** | publiée (1.17.1), version de production | `src/edusync_ad/`, `tests/core/` |
| **Go / Wails v3** | migration en cours — Phase 0 livrée, câblage du Core en cours | `cmd/`, `internal/`, `web/` |

La cible est une réécriture **sans perte fonctionnelle** en Go, testable et
pilotable par l'IA. Tant que la migration n'est pas terminée, les deux stacks
doivent rester utilisables et leurs tests verts.

## Commandes

```bash
task --list            # toutes les commandes
task dev               # lance l'application desktop (Wails v3)
task test              # tests Go
task test:python       # tests Python (.venv/bin/pytest)
task lint              # gofmt (lecture seule) + go vet
task fmt               # gofmt -w
task build             # binaires locaux dans bin/
task build:all         # cross-build → dist/ (5 cibles CLI + desktop)
task web:css           # recompile Tailwind (résultat versionné)
task ad:start          # AD de test Samba en container (docs/AD_TEST_ENV.md)
task cli -- paths --json   # CLI Go
```

Sans `task` : `go test ./...`, `go vet ./...`, `gofmt -l cmd internal web`.

## Règles d'or (ne jamais enfreindre)

1. **Zéro perte fonctionnalité** — chaque module Python a un équivalent Go
   avant de déclarer la migration terminée.
2. **`Validate() → Simulate() → Execute() → Audit()`** pour toute écriture AD.
   Le mode simulation n'est pas une option : c'est le chemin par défaut.
3. **LDAPS en priorité** (636) ; le repli LDAP (389) doit afficher un
   avertissement explicite.
4. **Journal append-only** : `internal/audit` n'expose ni suppression ni mise
   à jour, et le schéma reste identique à Python.
5. **Aucun secret en clair** : mots de passe AD jamais persistés, coffre
   chiffré AES-256, fichiers de clé en 0600.
6. **Compatibilité stricte Python ↔ Go** :
   - clés JSON en `snake_case` **français** (identiques à `AppConfig.to_dict()`) ;
   - format de chiffrement identique : `base64(nonce(12) ‖ AES-256-GCM)` avec
     clé brute de 32 octets dans `secret.key` (`internal/crypto`) ;
   - chemins identiques à `platformdirs` (`internal/config`) ;
   - schéma SQLite du journal identique (`internal/audit`).
7. **stdlib d'abord** : une dépendance Go doit être justifiée (aujourd'hui :
   `modernc.org/sqlite` sans CGO, `wails/v3` pour l'UI).
8. **Tests d'abord** : écrire le test avant l'implémentation, et le rendre
   impossible à satisfaire sans la vraie implémentation (pas de test de
   présence de commentaire).
9. **Rien de vert n'est laissé rouge** : `gofmt` + `go vet` + `go test ./...`
   passent avant tout commit.

## Architecture Go

```
cmd/edusync-ad/      application desktop (Wails v3, UI HTML+HTMX embarquée)
cmd/edusync-cli/     CLI pour scripts, CI et IA (sorties JSON)
internal/models/     types de domaine (reflet des dataclasses Python)
internal/config/     config.json, chemins platformdirs, écriture atomique
internal/crypto/     AES-256-GCM compatible Python (secret.key)
internal/audit/      journal SQLite append-only, schéma partagé avec Python
internal/ui/         serveur HTTP, templates, i18n fr/en, sessions
web/                 templates HTMX + assets (embeddés via //go:embed)
tests/ (Python)      suite de la version publiée
```

### Ajouter une page (HTMX)

1. `web/templates/<page>.html` avec `{{define "content"}} … {{end}}` ;
2. route dans `internal/ui/server.go` → `routes()` (`GET /x`, `POST /x`) ;
3. clés i18n dans `internal/ui/i18n.go` (**français ET anglais**) ;
4. modèle dans `PageData` si besoin ;
5. test d'intégration HTTP dans `internal/ui/server_test.go`
   (`demander(...)` simule un formulaire ou une requête HTMX).

N'oubliez pas `task web:css` si de nouvelles classes Tailwind sont utilisées.

### Ajouter une entrée au journal

```go
entree := nouvelleEntree("action", compte, resultat, detail)
entree.Utilisateur = operateur   // ou vide → contexte de session
entree.Domaine     = domaine
_ = s.journal.Record(entree)     // jamais de Delete ni d'UPDATE
```

## Tests

- **Go** : `go test ./...` — paquets `internal/*`, `cmd/*` (pas de réseau,
  pas de container requis, chemins redirigés dans `t.TempDir()`).
- **Python** : `.venv/bin/pytest -q` (~843 tests).
- **AD réel** : `task ad:start` puis `AD_TEST=1 go test ./internal/ad/...`
  (container Samba AD DC, `docs/AD_TEST_ENV.md`) — socle livré
  (`internal/testutil`, `tests/integration/`) ; la campagne complète est
  déclenchée **après l'alpha** (voir la priorité 3 de la roadmap).
- **Benchmarks** : `go test -bench=. ./internal/...`.

## État de la migration (mise à jour à chaque livraison)

Voir la section « État d'avancement » (§0) de `ROADMAP_GO_MIGRATION.md`
(version 3.0, 15 octobre 2026).

En résumé : **Phase 0 livrée** (config, crypto, modèles, audit, pool AD
TLS/SASL, CLI, UI Wails/HTMX, testutil Samba) + **A1/M9 livré**
(connexion : `BindPassword`, repli LDAPS→LDAP, erreurs fr/en). Stratégie
décidée le 15/10/2026 : **1) finir le Core M1→M11** — câblage UI, les
backends existent déjà dans `internal/ad/` (voir le tableau « constat
réel dans le code » du §0) —, **2) sortir l'alpha 1 dès que M11 est
prêt**, puis **un module avancé livré = une release** (M12→M32),
**3) campagne de tests intégrée aux releases**. La boucle
`task lint && task test` reste obligatoire à chaque livraison.

## Prompts réutilisables

### Nouveau module

```
Crée le module « X » pour EduSync-AD en Go.
1. Lis internal/ et cmd/edusync-cli/ pour les patterns (config, audit, i18n, UI).
2. Écris D'ABORD les tests : internal/<x>/<x>_test.go + internal/ui/<page>_test.go.
3. Implémente en suivant Validate → Simulate → Execute → Audit.
4. Ajoute les routes UI + clés i18n fr/en + gabarit web/templates/<x>.html.
5. Ajoute la commande CLI associée (sortie JSON).
6. gofmt, go vet, go test, task lint : tout vert.
7. Mets à jour ROADMAP_GO_MIGRATION.md (statut du module).
```

### Correctif de bug

```
Reproduis d'abord (test qui échoue), corrige, fais repasser go test ./...
et .venv/bin/pytest -q. Aucune API publique ne change sans mise à jour des
appelsants et de la doc.
```

### Performance

```
go test -bench=BenchmarkX -cpuprofile=cpu.prof ./internal/...
Identifie les 3 goulots, propose les patches, re-benchmark, publie l'avant/après.
```
