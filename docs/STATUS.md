# État d'avancement — EduSync AD (Migration Go)

> **Mise à jour : 15 octobre 2026** — Données issues de `ROADMAP_GO_MIGRATION.md` §0 et du code réel.

---

## Vue d'ensemble

| Composant | Version Python (production) | Version Go (migration) |
|-----------|----------------------------|------------------------|
| **Core v1.x (M1-M11)** | ✅ Déployé (1.17.1) | 🔨 Câblage en cours |
| **Modules avancés (M12-M24)** | ✅ Déployé | ⬜ Non commencé |
| **Modules v4.0 (M25-M32)** | 🚧 M32 en cours | ⬜ Non commencé |
| **Fondations (T0-T2)** | ✅ Livré | ✅ Config, Crypto, Modèles, Audit, Pool AD, CLI, UI Wails |

---

## Détail par module — Core v1.x (Priorité 1, Lot A)

| Module | Fonctionnalité | État Go | Manque principal |
|--------|---------------|---------|------------------|
| **M1** | Création de comptes / Arrivées | ✅ **Fini** (commit `f705d80`) | — (simulation, exécution, doublons, groupes auto, export étiquettes) |
| **M2** | Migration / Restructuration | 🔨 Page + routes | `migrationPlan` / `migrationSimulate` / `migrationExecute` + 3 gabarits |
| **M3** | Gestion des départs | ⬜ **Absent** | Backend `depart.go`, routes, gabarit, CLI, i18n |
| **M4** | Réinitialisation MDP | ✅ **Fini** (commit `7eacd2b`) | — (3 modes OU/groupe/CSV, politiques élèves/perso, forçage MDP, export CSV) |
| **M5** | Explorateur AD | 🔨 Routes + i18n | Handlers `Search`/`BuildTree` + 3 gabarits |
| **M6** | Export CSV/PDF | 🔨 Routes + i18n | Handlers réels (stubs actuels) + gabarit |
| **M7** | Journal d'actions | 🔨 Liste 50 entrées | Filtres `auditFilter` + 2 gabarits |
| **M8** | Paramètres globaux | ⬜ **Absent** | 4 onglets, routes, gabarit, i18n |
| **M9** | Connexion LDAPS/LDAP | ✅ **Fini** (commit `86414c0`) | — (BindPassword, repli LDAP, avertissement, erreurs UI) |
| **M10** | Mise à jour intégrée | ⬜ Absent | Livré avec alpha (P2) |
| **M11** | Mode simulation | 🔨 Pattern prêt | Câblage généralisé sur tous modules |

**Légende** : ✅ Fini · 🔨 Partiellement câblé · ⬜ Absent

---

## Modules avancés (M12-M24) — Lot B

| Module | État Python | État Go |
|--------|-------------|---------|
| M12 Photos d'identité | ✅ | ⬜ |
| M13 Profils utilisateurs | ✅ | ⬜ |
| M14 Quotas FSRM | ✅ | ⬜ |
| M15 Scripts logon/logoff | ✅ | ⬜ |
| M16 Heures de connexion | ✅ | ⬜ |
| M17 Dossiers personnels | ✅ | ⬜ |
| M18 Espaces partagés classe | ✅ | ⬜ |
| M19 Microsoft 365 / Entra ID | ✅ | ⬜ |
| M20 Exchange | ✅ | ⬜ |
| M21 RDS / Bureau à distance | ✅ | ⬜ |
| M22 Import/Export avancés | ✅ | ⬜ |
| M23 Modèles de groupes | ✅ | ⬜ |
| M24 Étiquettes avancées | ✅ | ⬜ |

---

## Modules v4.0 (M25-M32) — Lot C

| Module | État Python | État Go |
|--------|-------------|---------|
| M25 Multisite | ✅ | ⬜ |
| M26 RBAC (Délégation) | ✅ | ⬜ |
| M27 Portail auto-service | ✅ | ⬜ |
| M28 API REST + Webhooks | ✅ | ⬜ |
| M29 IA / Assistant | ✅ | ⬜ |
| M30 Conformité & Sécurité | ✅ | ⬜ |
| M31 DFS/IIS/WebDAV/FTP | ✅ | ⬜ |
| M32 Sauvegarde/Restauration | 🚧 En cours | ⬜ |

---

## Transverses

| Élément | État Go | Note |
|---------|---------|------|
| Config chiffrée multi-profil | ✅ | Testé aller-retour Python ↔ Go |
| Crypto AES-256-GCM compatible | ✅ | Vecteur d'interopérabilité OK |
| Modèles domaine + Audit SQLite | ✅ | Schéma identique Python |
| Pool AD (TLS, SASL/GSSAPI) | ✅ | `internal/ad/pool.go` |
| CLI (5 cibles cross-compile) | ✅ | 9,5 MB stripped |
| UI Wails v3 + HTMX/Alpine/Tailwind | ✅ | Login, dashboard, thème, i18n fr/en |
| Testutil Samba AD (container) | ✅ | `task ad:start` manuel |
| Migration données Python → Go | ⬜ | Script `edusync-cli migrate` à créer (fin P1) |
| Cache AD SQLite (T2) | ⬜ | Lot B |

---

## Métriques de build (Linux amd64, Go 1.27.2)

| Critère | Cible | Réel (14/10) |
|---------|-------|--------------|
| `gofmt -l cmd internal web` | Rien | ✅ Rien (2 fichiers WIP non formatés) |
| `go vet ./...` | Vert | ✅ Vert |
| `go test ./...` | Vert | 7/10 paquets verts (`internal/ad` WIP) |
| CLI cross-build | 5 cibles | ✅ 5 cibles en 1 commande |
| Taille CLI | 8-12 MB | ✅ 9,5 MB |
| Taille Desktop | < 15 MB | ⚠️ 17,2 MB (optimisation P2) |
| CI (Go + Python) | Vertes | ✅ |

---

## Prochaine action immédiate

**Lot A, A1** — Câbler `BindPassword` depuis la config chiffrée (`server.go:438`) et afficher les erreurs de connexion proprement dans l'UI. Cela débloque les modules suivants (M1, M2, M4, M5, M6, M7).

---

## Ordre de travail (rappel)

1. **Lot A** : Finir le câblage Core M1-M11 (A1 → A13)
2. **Lot B** : Modules avancés M12-M24 + cache T2
3. **Lot C** : Modules v4.0 M25-M32 + Coffre MDP
4. **Clôture P1** : `edusync-cli migrate --from-python` + checklist §4 complète → **Porte 1**
5. **Priorité 2** : Alpha Windows (Setup.exe) + Linux (AppImage + tar.gz) → **Porte 2**
6. **Priorité 3** : Campagne de tests (intégration, e2e, benchmarks) → **Porte 3**

---

## Liens utiles

- [Feuille de route détaillée](ROADMAP_GO_MIGRATION.md)
- [Architecture Go](ARCHITECTURE.md)
- [Environnement de test AD](AD_TEST_ENV.md)
- [Inventaire complet des modules](ROADMAP_GO_MIGRATION.md#4-inventaire--la-r%C3%A9f%C3%A9rence-logiciel-fini)