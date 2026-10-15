# Python v1.x — Fonctionnalités disponibles (Référence parité Go)

> **Version** : 1.17.1 (production)
> **Source** : `src/edusync_ad/`, `docs/ROADMAP_python_v1.md`
> **Objectif** : Documenter le périmètre fonctionnel exact à reproduire en Go (zéro perte)

---

## Core v1.x (M1 → M11) — Déployé

| Module | Fonctionnalités clés | Fichiers Python |
|--------|---------------------|-----------------|
| **M1 — Création comptes / Arrivées** | Import CSV (encodage/séparateur auto), génération identifiants/MDP/mail, résolution doublons, vérification AD existants, export CSV étiquettes, simulation | `core/arrivees.py`, `core/identifiants.py`, `core/mdp.py`, `core/mail.py`, `core/doublons.py`, `core/simulation.py`, `plugins/arrivees/` |
| **M2 — Migration / Restructuration** | Via CSV (prénom/nom/équipe) ou UI (sélection OU), déplacement AD + groupes classe auto, résolution prénom/nom, simulation | `core/migration.py`, `core/groupes.py`, `core/ad_ops.py`, `core/simulation.py`, `plugins/migration/` |
| **M3 — Gestion départs** | Désactivation immédiate ou archivage (move OU Archive) + purge planifiée (délai configurable 30j), résolution prénom/nom, table `pending_deletions` | `core/departs.py`, `core/pending_deletions.py`, `plugins/departs/` |
| **M4 — Réinitialisation MDP** | Par OU, groupe AD, CSV, politiques différées (élèves/personnels), forçage changement prochaine connexion, export CSV, simulation | `core/mdp.py`, `core/politiques_mdp.py`, `core/simulation.py`, `plugins/mdp/` |
| **M5 — Explorateur AD** | Arborescence OUs/groupes, panneau central unifié (users+groups+sous-OU), clic droit complet (modif, move, MDP, groupes, suppr), recherche temps réel, pagination | `core/explorateur.py`, `core/recherche.py`, `plugins/explorateur/` |
| **M6 — Export CSV / Étiquettes PDF** | Sélection OU ± sous-OU, champs cochables (incl. MDP mémorisés), PDF Avery L7160/L7163, QR codes, couleurs pastel, trombinoscope | `core/export.py`, `core/etiquettes.py`, `core/coffre.py`, `plugins/export/` |
| **M7 — Journal d'actions** | Filtres date/type/résultat/domaine/opérateur, export CSV, SQLite append-only, table `actions` + `pending_deletions` | `core/journal.py`, `core/export.py`, `plugins/journal/` |
| **M8 — Paramètres globaux** | 4 onglets : Comptes (format ID, mail, groupes, départs), Mots de passe (politiques élèves/personnels, coffre), Apparence (thème, langue), Délégation (M26) | `core/config.py`, `core/politiques_mdp.py`, `plugins/parametres/` |
| **M9 — Connexion LDAPS/LDAP** | LDAPS prioritaire (636), repli LDAP 389 + avertissement, CA custom, validation certificat, mémorisation chiffrée (domaine/user), MDP jamais stocké | `core/connexion.py`, `core/config.py`, `plugins/connexion/` |
| **M10 — Mise à jour intégrée** | Vérification GitHub Releases, téléchargement, vérification SHA256, installation (InnoSetup/Flatpak), redémarrage auto | `core/updater.py`, `packaging/installer.iss`, `packaging/edusync_ad.spec`, `plugins/updater/` |
| **M11 — Mode simulation** | Toute écriture AD = prévisualisation modifiable (Validate→Simulate→Execute→Audit), pattern universel sur tous modules | `core/simulation.py`, `plugins/*/` |

---

## Fondations techniques v2.0 — Livrées

| Fondation | Description | Fichiers Python |
|-----------|-------------|-----------------|
| **T0 — Couche LDAP Asynchrone** | Queue de jobs (`Job{id, coro, progress_cb, done_cb}`), `QThreadPool` worker, signaux Qt `progress/finished/error/cancelled` | `core/ldap_async.py`, `core/job_queue.py` |
| **T1 — Système de plugins** | Chargeur via `entry_points`, interface `IModule{id, name, version, widget, requires_ad, permissions, on_load, on_unload}` | `core/plugins.py`, `plugins/*/plugin.py` |
| **T2 — Cache AD SQLite** | Cache OU/groupes/users sync background, invalidation TTL + `uSNChanged` polling, recherche offline | `core/cache_ad.py`, `core/usn_sync.py` |

---

## Modules avancés (M12 → M24) — Livrés

| Module | Fonctionnalités | Fichiers Python |
|--------|----------------|-----------------|
| **M12 — Photos d'identité** | Import manuel/masse/nommage auto, cadrage 3:4 ≤ 200 Ko, vignettes explorateur + étiquettes | `core/photos.py`, `plugins/photos/` |
| **M13 — Profils utilisateurs** | Itinérant/local/obligatoire par OU/groupe, variables `%USERNAME%` `%SAM%` `%OU%` | `core/profil.py`, `plugins/profil/` |
| **M14 — Quotas FSRM** | Quota par OU/groupe, application auto, rapport CSV | `core/quotas.py`, `plugins/quotas/` |
| **M15 — Scripts logon/logoff** | Éditeur bat/powershell/vbs, variables, déploiement `scriptPath`/GPO | `core/scripts_logon.py`, `plugins/scripts_logon/` |
| **M16 — Heures connexion** | Grille 24h×7j, préréglages, attribut `logonHours` | `core/heures_connexion.py`, `plugins/heures_connexion/` |
| **M17 — Dossiers personnels** | Création auto `\\srv\homes\%USERNAME%`, droits NTFS, lettre lecteur | `core/dossier_perso.py`, `plugins/dossier_perso/` |
| **M18 — Espaces partagés classe** | Partage SMB + NTFS à création groupe, sync membres ↔ droits | `core/espaces_partages.py`, `plugins/espaces_partages/` |
| **M19 — Microsoft 365 / Entra ID** | App registration, users cloud + licences, groupes/Teams, photos, sync MDP (PHS/fédération) | `core/m365.py`, `plugins/m365/` |
| **M20 — Exchange** | Boîtes aux lettres, aliases/proxy, policies, quotas (On-prem + Online) | `core/exchange.py`, `plugins/exchange/` |
| **M21 — RDS / Bureau à distance** | Collections, RemoteApp, UPD/FSLogix | `core/rds.py`, `plugins/rds/` |
| **M22 — Import/Export avancés** | Import LDAP (OU/filtre), export LDIF/vCard, publipostage HTML, import GEP | `core/import_export.py`, `plugins/import_export/` |
| **M23 — Modèles de groupes** | Modèles classe/personnel/services, instanciation 1-clic, duplication/import | `core/modeles_groupes.py`, `plugins/modeles_groupes/` |
| **M24 — Étiquettes avancées** | Éditeur WYSIWYG, badges/cartes, trombinoscope A4/A3, envoi mail | `core/label_studio.py`, `plugins/label_studio/` |

---

## Modules v4.0 (M25 → M32) — Livrés (sauf M32 en cours)

| Module | Fonctionnalités | Fichiers Python |
|--------|----------------|-----------------|
| **M25 — Multisite** | Multi-domaines/forêts, `domaines.json` chiffré, sélecteur site, journal par domaine | `core/multisite.py`, `plugins/multisite/` |
| **M26 — RBAC (Délégation)** | 5 rôles, portées OU/groupe, garde-fou écriture AD, journal par opérateur, `delegations.json` | `core/rbac.py`, `plugins/delegation/` |
| **M27 — Portail auto-service** | Reset MDP code mail (PBKDF2 usage unique), consultation identifiant/mail, demande création compte + validation admin, serveur stdlib (`http.server`/`smtplib`/`sqlite3`) | `core/portal.py`, `core/portal_server.py`, `plugins/portal/` |
| **M28 — API REST + Webhooks** | OpenAPI, webhooks (créé/migré/départ/MDP), sync bidir SIS (PRONOTE, etc.) | `core/api.py`, `core/webhooks.py`, `core/sis_sync.py`, `plugins/api/` |
| **M29 — IA / Assistant** | Détection anomalies (doublons, orphelins, OU vides, groupes sans membres), nettoyage fin année, identifiants intelligents, analyse logs → recommandations sécurité | `core/ia.py`, `core/ia_anomalies.py`, `core/ia_cleanup.py`, `core/ia_idgen.py`, `core/ia_logs.py`, `plugins/ia/` |
| **M30 — Conformité & Sécurité** | Rapport RGPD + droit à l'oubli (purge coffre, anonymisation journal), MFA admin, session unique, PSO, signature exports | `core/compliance.py`, `core/rgpd.py`, `core/mfa.py`, `core/pso.py`, `core/signature.py`, `plugins/compliance/` |
| **M31 — DFS / IIS / WebDAV / FTP** | Espaces DFS, publication WebDAV/IIS, FTP isolé (priorité basse) | `core/dfs.py`, `core/webdav.py`, `core/ftp.py`, `plugins/dfs/` |
| **M32 — Sauvegarde / Restauration** | Backup ZIP chiffré (config, journal, coffre, modèles, certificats), restore 1-clic, migrations vN→vN+1 | `core/sauvegarde.py`, `plugins/sauvegarde/` |

---

## Formats de données partagés (Compatibilité Go)

| Format | Spécification | Validation |
|--------|---------------|------------|
| `config.json` | JSON UTF-8, clés `snake_case` françaises, indenté 2, `ensure_ascii=False` | `AppConfig.to_dict()` / `from_dict()` |
| `secret.key` | 32 octets bruts, permissions 0600 | Lecture/écriture `platformdirs` |
| Chiffrement | `base64(nonce(12) ‖ AES-256-GCM(ciphertext, tag))` | Clé 32 octets depuis `secret.key` |
| `journal.db` | SQLite, tables `actions` + `pending_deletions`, mêmes colonnes, `ALTER TABLE` identiques | Schéma testé aller-retour |

**Chemins `platformdirs`** :
- Linux : `~/.config/EduSyncAD`, `~/.local/share/EduSyncAD`, `~/.cache/EduSyncAD`
- macOS : `~/Library/Application Support/EduSyncAD`, `~/Library/Caches/EduSyncAD`
- Windows : `%APPDATA%\EduSyncAD`, `%LOCALAPPDATA%\EduSyncAD`

---

## Tests Python (Référence pour Go)

| Suite | Nombre tests | Couverture |
|-------|--------------|------------|
| `tests/core/` | ~757 tests | M1-M22, fondations T0-T2 |
| Manquants | 0 tests | M26 (RBAC), M27 (Portail), M28 (API), M29 (IA), M30 (Conformité), M31 (DFS), M32 (Sauvegarde) |

---

## Build & Distribution Python (Production actuelle)

| Cible | Méthode | Artefact |
|-------|---------|----------|
| Windows | PyInstaller + InnoSetup | `EduSyncAD-Setup.exe` |
| Linux | Flatpak | `EduSyncAD-linux.flatpak` |
| Sources | `pip install -e ".[dev]"` | Développement |

---

## Prochaine étape pour la migration Go

> Voir `docs/migration/01-core.md` (Lot A), `docs/migration/02-modules-avances.md` (Lot B), `docs/migration/03-modules-v4.md` (Lot C)

Chaque module Go doit reproduire **exactement** les fonctionnalités listées ci-dessus, avec :
- Formats de données identiques (`config.json`, `secret.key`, `journal.db`)
- Même comportement en simulation/erreur/réseau coupé
- Tests d'interopérabilité (Python produit → Go lit, Go produit → Python lit)