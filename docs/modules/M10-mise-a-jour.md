# Module M10 — Mise à jour intégrée

> **Objectif** : Vérification, téléchargement, installation, redémarrage auto, somme de contrôle SHA256

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Vérification version | `src/edusync_ad/core/updater.py` | GitHub Releases API, comparaison semver |
| Téléchargement | `src/edusync_ad/core/updater.py` | Stream download, progression, résumé |
| Vérification SHA256 | `src/edusync_ad/core/updater.py` | Fichier `.sha256` associé à l'artefact |
| Installation Windows | `src/edusync_ad/core/updater.py` | InnoSetup `/VERYSILENT`, redémarrage app |
| Installation Linux | `src/edusync_ad/core/updater.py` | Remplacement binaire + systemd/user service restart |
| Redémarrage auto | `src/edusync_ad/core/updater.py` | `exec` nouveau processus, nettoyage ancien |
| Notification UI | `src/edusync_ad/plugins/updater/` | Bandeau « Mise à jour disponible » + bouton |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/updater/
├── updater.go             # NOUVEAU : Check, Download, Verify, Install, Restart
├── github.go              # NOUVEAU : GitHub Releases API client
├── installer.go           # NOUVEAU : Windows (InnoSetup) / Linux (binaire/AppImage)
└── checksum.go            # NOUVEAU : SHA256 verification
```

### UI (`internal/ui/`)
```
internal/ui/
├── updater.go             # NOUVEAU : Handlers check, download, install, status
├── server.go              # MODIFIER : Routes /updater/*
└── i18n.go                # MODIFIER : Clés fr/en mise à jour
```

### Templates (`web/templates/`)
```
web/templates/
├── updater.html                 # NOUVEAU : Page mise à jour (statut + actions)
└── partials/
    ├── updater_banner.html          # NOUVEAU (bandeau header « Mise à jour dispo »)
    ├── updater_progress.html        # NOUVEAU (barre progression téléchargement/install)
    └── updater_restart_modal.html   # NOUVEAU (confirmation redémarrage)
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── updater.go               # NOUVEAU : check, download, install (JSON)
```

### Build / Packaging
```
build/inno/
├── installer.iss            # MODIFIER : Accepter flag `/UPDATE` pour mode mise à jour
build/appimage/
├── build.sh                 # MODIFIER : Générer artefact mise à jour compatible
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Mise à jour Go | ⬜ **Absente** (chantier P2.4) |
| Python updater | ✅ Référence complète |
| Release Go workflow | ⬜ `release-go.yml` à créer (P2.1) |
| Artefacts alpha | ⬜ P2.2/P2.3 |
| UI bandeau maj | ✅ Partiel (placeholder dans header) |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre — P2.4)

1. **Test d'abord** : `internal/updater/updater_test.go` + `internal/ui/updater_test.go`
2. **GitHub client** : `internal/updater/github.go` — `GetLatestRelease(tagPrefix "v0.")` → `Release{Version, Assets[], SHA256s}`
3. **Updater core** : `internal/updater/updater.go` — `CheckForUpdate() → UpdateInfo`, `Download(asset, progressCb)`, `VerifySHA256(file, expected)`, `Install(file)`, `Restart()`
4. **Installateur Windows** : `internal/updater/installer.go` — détecte Setup.exe → lance `Setup.exe /VERYSILENT /CLOSEAPPLICATIONS /RESTARTAPPLICATIONS`
5. **Installateur Linux** : `internal/updater/installer.go` — backup binaire → écrit nouveau → `chmod +x` → `exec` nouveau
6. **Handler check** : `GET /updater/check` → JSON `{available, version, notes, size}`
7. **Handler download** : `POST /updater/download` → stream + progression HTMX (Server-Sent Events ou polling)
8. **Handler install** : `POST /updater/install` → lance installateur → modal redémarrage
9. **Bandeau header** : `updater_banner.html` — HTMX poll `/updater/check` (intervalle 1h), affichage si dispo
10. **Routes** : `GET /updater/check`, `POST /updater/download`, `POST /updater/install`, `GET /updater/status`
11. **i18n** : Clés `updater.check`, `updater.available`, `updater.downloading`, `updater.installing`, `updater.restart`, `updater.sha256_failed` (fr+en)
12. **CLI** : `edusync-cli updater check --json`, `updater download`, `updater install`
13. **Validation alpha** : Publier `v0.1.0-alpha.2` → vérifier détection + installation + redémarrage
14. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation (P2.5 ④)

- [ ] Détection release `v0.*` sur GitHub Releases
- [ ] Téléchargement artefact correspondant (Windows/Linux) + progression UI
- [ ] Vérification SHA256 → échec si corrompu
- [ ] Installation Windows : Setup.exe `/VERYSILENT` → app redémarrée, version mise à jour
- [ ] Installation Linux : binaire remplacé → `exec` nouveau processus → version mise à jour
- [ ] Bandeau header : apparaît si maj dispo, clic → page updater
- [ ] Aucune régression config/secret/journal pendant maj
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/updater/... ./internal/ui/... -run Updater`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/updater/... -v
go test ./internal/ui/... -run Updater -v

# Test manuel (alpha)
# 1. Publier v0.1.0-alpha.2
# 2. Lancer v0.1.0-alpha.1
# 3. Vérifier bandeau → page updater → download → install → restart → version alpha.2
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/updater.py` : Logique complète (check, download, verify, install, restart)
- `src/edusync_ad/plugins/updater/` : Plugin UI PyQt6 (bandeau, progression)
- `packaging/installer.iss` : InnoSetup script (référence flags `/UPDATE`)
- `tests/core/test_updater.py` : Tests Python