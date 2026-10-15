# Priorité 2 — Release Alpha Windows + Linux

> **Période** : Après Priorité 1 (logiciel Go complet, Porte 1 validée)
> **Durée estimée** : 1 à 2 semaines
> **Source** : `ROADMAP_GO_MIGRATION.md` §6 « Priorité 2 »

---

## Objectif

Produire une **alpha téléchargeable et installable** sur **Windows** et **Linux** :
- Windows : `EduSyncAD-Setup.exe` (InnoSetup) + `.sha256`
- Linux : `EduSyncAD-linux.AppImage` + `edusync-ad-linux-amd64.tar.gz` + `.sha256`
- Artefacts signés `prerelease: true` sur GitHub Releases (tag `v0.1.0-alpha.1`)

---

## Format validé (décision 14/10/2026)

| OS | Format | Méthode |
|----|--------|---------|
| Windows | `EduSyncAD-Setup.exe` | Runner `windows-latest` → `wails build` → InnoSetup 6 (`build/inno/`) |
| Linux | `EduSyncAD-linux.AppImage` | Runner `ubuntu-latest` + `libwebkit2gtk-4.1-dev` → `build/appimage/` (WebKitGTK bundlé) |
| Linux | `edusync-ad-linux-amd64.tar.gz` | Binaire brut + `.desktop` + icône |

Chaque artefact accompagné de son `.sha256`.

---

## Travail détaillé (P2.1 → P2.7)

### P2.1 — Nouveau workflow `release-go.yml`
- [ ] Créer `.github/workflows/release-go.yml` déclenché sur tags `v0.*`
- [ ] `prerelease: true` obligatoire
- [ ] **Ne pas modifier** `release.yml` (Python, tags `v1.*`) — rester intact pour la 1.17.1 en production
- [ ] Validation sur tag de test pré-alpha avant release officielle

### P2.2 — Job Windows (InnoSetup)
- [ ] Runner `windows-latest` (Go + WebView2 + Inno Setup 6 déjà présents)
- [ ] Portage `packaging/installer.iss` → `build/inno/installer.iss` (cible binaire Go, icônes `assets/`)
- [ ] Détection WebView2 : installer redistribuable si absent
- [ ] Sortie : `EduSyncAD-Setup.exe` + `EduSyncAD-Setup.exe.sha256`
- [ ] Test installation + désinstallation propre (paramètres Windows)

### P2.3 — Job Linux (AppImage + tar.gz)
- [ ] Runner `ubuntu-latest` + `libwebkit2gtk-4.1-dev`
- [ ] Script `build/appimage/build.sh` :
  - `wails build` → binaire
  - Bundle WebKitGTK + dépendances dans AppImage (`appimagetool` ou script maison)
  - Générer `.desktop`, icône, `AppRun`
- [ ] Sortie : `EduSyncAD-linux.AppImage` + `.sha256`
- [ ] Sortie : `edusync-ad-linux-amd64.tar.gz` (binaire + `.desktop` + icône) + `.sha256`

### P2.4 — M10 Mise à jour intégrée (Auto-update)
- [ ] Implémenter updater Go (modèle `core/updater.py` Python) :
  - Vérification release GitHub (tag `v0.*`)
  - Téléchargement artefact correspondant (Windows/Linux) + vérification `.sha256`
  - Installation silencieuse (Windows : InnoSetup `/VERYSILENT` ; Linux : remplacement binaire + restart)
  - Redémarrage auto app
- [ ] Valider contre la première alpha (boucle update alpha→alpha+1)

### P2.5 — Checklist pré-alpha (Vérifications **manuelles**)

| # | Vérification | Critère de réussite |
|---|--------------|---------------------|
| ① | `task lint && task test && task test:python` | Tous verts |
| ② | App démarre sous Xvfb (smoke `ci.yml`) | Pas de crash, UI accessible |
| ③ | **App démarre sur Windows 10/11 réel** | WebView2 OK, LDAPS connecté, UI complète |
| ④ | Installation + désinstallation propres | Setup.exe : entrées Menu Démarrer/Bureau, uninstall via Paramètres |
| ⑤ | Relecture config Python existante sans reconfiguration | `config.json`, `secret.key`, `journal.db` lus, réécrits au format Go |
| ⑥ | Connexion LDAPS réelle contre `task ad:start` | LDAPS 636 OK, fallback LDAP 389 avec avertissement |
| ⑦ | Thème + i18n fr/en sur modules livrés | Basculement thème/langue persistant, pas de clé manquante |

> **Important** : Ce sont des vérifications **manuelles**, pas une campagne de tests automatisés (campagne = Priorité 3).

### P2.6 — Notes de version alpha
Contenu obligatoire :
- Périmètre couvert (référence §4 `ROADMAP_GO_MIGRATION.md` : M1-M32, T2, Coffre, Transverses)
- **Limites connues** (ex: modules non testés en conditions réelles, perf non optimisées, WebKitGTK Linux)
- Installation : `Setup.exe` (Windows), `chmod +x AppImage` ou `tar.gz` (Linux)
- Appel à testeurs : remontée issues GitHub avec logs
- **Avertissement visible** : *« Logiciel en cours de migration, la version 1.17.1 (Python) reste la version de production »*

### P2.7 — Publication
- [ ] Tag `v0.1.0-alpha.1` → déclenche `release-go.yml`
- [ ] 3 artefacts + 3 `.sha256` en release **pré-release** GitHub
- [ ] Vérifier `release.yml` (Python) **inchangé** sur tag `v1.17.1` existant

---

## Optimisations cibles (chantier P2)

| Métrique | Actuel (14/10) | Cible Alpha | Action |
|----------|----------------|-------------|--------|
| Taille Desktop | 17,2 MB | < 15 MB | `-trimpath`, `-ldflags="-s -w"`, stripping glue Wails, UPX optionnel |
| Démarrage à froid | ~200 ms | < 100 ms | Profilage `pprof`, lazy load templates, pré-chargement pool AD |

---

## Dépendances système (documentation alpha)

### Windows
- WebView2 Runtime (installé par défaut Win 10 1809+ / Win 11, sinon téléchargé par l'installateur)
- Pas de Python, pas de .NET

### Linux (AppImage)
- `libwebkit2gtk-4.1-0` (bundlé dans AppImage)
- `libayatana-appindicator3-1` (tray icon, optionnel)
- `xdg-utils`, `desktop-file-utils` (intégration bureau)

### Linux (tar.gz)
- `libwebkit2gtk-4.1-0` **requis sur le système**
- `libayatana-appindicator3-1` (optionnel)

> Documenter dans les notes de version : « AppImage recommandée sur Linux (WebKitGTK inclus) »

---

## Checklist technique release-go.yml

```yaml
# .github/workflows/release-go.yml (structure cible)
name: Release Go Alpha
on:
  push:
    tags: ['v0.*']
permissions:
  contents: write
jobs:
  windows:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Build Wails
        run: task build:windows
      - name: InnoSetup
        run: ./build/inno/build.ps1
      - name: SHA256
        run: certutil -hashfile dist/EduSyncAD-Setup.exe SHA256 > dist/EduSyncAD-Setup.exe.sha256
      - name: Upload
        uses: softprops/action-gh-release@v1
        with: { files: dist/*.exe, dist/*.sha256, prerelease: true }
  linux:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Install WebKitGTK
        run: sudo apt-get update && sudo apt-get install -y libwebkit2gtk-4.1-dev
      - name: Build Wails
        run: task build:linux
      - name: Build AppImage
        run: ./build/appimage/build.sh
      - name: Create tar.gz
        run: tar -czf dist/edusync-ad-linux-amd64.tar.gz -C dist/linux_amd64 .
      - name: SHA256
        run: sha256sum dist/*.AppImage dist/*.tar.gz > dist/checksums.sha256
      - name: Upload
        uses: softprops/action-gh-release@v1
        with: { files: dist/*.AppImage dist/*.tar.gz dist/*.sha256, prerelease: true }
```

---

## Validation post-publication

- [ ] Télécharger `Setup.exe` sur Windows 10/11 vierge → installer → lancer → connecter AD test → parcourir modules Core
- [ ] Télécharger `AppImage` sur Ubuntu 22.04/24.04 vierge → `chmod +x` → lancer → connecter AD test
- [ ] Vérifier auto-update : publier `v0.1.0-alpha.2` → vérifier détection + installation + redémarrage
- [ ] Confirmer `release.yml` (Python) non impacté : tag `v1.17.2` → build Python OK

---

## Prochaine étape après alpha

→ **Priorité 3** : Campagne de tests — voir `05-tests.md`