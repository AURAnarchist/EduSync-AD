# Python v1.x — État réel des modules (Référence parité Go)

> **Mise à jour** : 12 octobre 2026
> **Source** : Vérifié dans le code `src/edusync_ad/`, pas dans les intentions
> **Version production** : 1.17.1

---

## Vue d'ensemble

| Lot | Modules | État |
|-----|---------|------|
| **Fondations v2.0** | T0 LDAP asynchrone, T1 Système plugins, T2 Cache AD SQLite | ✅ Livré |
| **v2.0** | M12 → M18 (photos, profils, quotas, scripts, heures, dossiers perso, espaces classe) | ✅ Livré |
| **v2.5** | M19 → M22 (M365, Exchange, RDS, imports/exports avancés) | ✅ Livré |
| **v3.0** | M23 Modèles groupes ✅ — M24 Étiquettes avancées ✅ — M25 Multisite ✅ | ✅ Livré |
| **v3.5** | M26 RBAC ✅ — M27 Portail auto-service ✅ — M28 API REST + Webhooks ✅ | ✅ Livré — ⚠️ **Tests manquants** |
| **v4.0** | M29 IA ✅ — M30 Conformité ✅ — M31 DFS ✅ — M32 Sauvegarde 🚧 | 🚧 Partiel — **M32 en cours**, tests M29-M32 manquants |

---

## Détail par module — Core v1.x (M1-M11)

| Module | Statut | Description | Tests |
|--------|--------|-------------|-------|
| **M1 — Création comptes** | ✅ Fait | Import CSV, génération identifiants/MDP/mail, résolution doublons, vérif AD, export étiquettes, simulation | ✅ Couverts |
| **M2 — Migration classe** | ✅ Fait | CSV ou UI sélection OU, groupes classe auto, move AD, simulation | ✅ Couverts |
| **M3 — Gestion départs** | ✅ Fait | Désactivation/archivage + purge planifiée (30j), résolution prénom/nom | ✅ Couverts |
| **M4 — Réinitialisation MDP** | ✅ Fait | OU/groupe/CSV, politiques élèves/personnels, forçage changement, export | ✅ Couverts |
| **M5 — Explorateur AD** | ✅ Fait | Arborescence OUs+groupes, panneau central, clic droit complet, recherche temps réel | ✅ Couverts |
| **M6 — Export CSV/PDF** | ✅ Fait | Sélection OU±sous-OU, champs cochables, MDP mémorisés, PDF Avery L7160/L7163, QR, couleurs | ✅ Couverts |
| **M7 — Journal actions** | ✅ Fait | Filtres date/type/résultat/domaine/opérateur, export CSV, SQLite append-only | ✅ Couverts |
| **M8 — Paramètres globaux** | ✅ Fait | 4 onglets : Comptes, MDP, Apparence, Délégation (M26) | ✅ Couverts |
| **M9 — Connexion LDAPS** | ✅ Fait | LDAPS prioritaire, repli LDAP + avertissement, CA custom, mémorisation chiffrée | ✅ Couverts |
| **M10 — Mise à jour** | ✅ Fait | Vérification, téléchargement, installation, redémarrage, SHA256 | ✅ Couverts |
| **M11 — Mode simulation** | ✅ Fait | Toute écriture AD = prévisualisation modifiable | ✅ Couverts |

---

## Modules avancés (M12-M24)

| Module | Statut | Description | Tests |
|--------|--------|-------------|-------|
| M12 Photos | ✅ Fait | Import manuel/masse/nommage, cadrage 3:4, vignettes explorateur+étiquettes | ✅ |
| M13 Profils | ✅ Fait | Itinérant/local/obligatoire par OU/groupe, variables `%USERNAME%` `%SAM%` `%OU%` | ✅ |
| M14 Quotas | ✅ Fait | Quota par OU/groupe, application auto, rapport CSV | ✅ |
| M15 Scripts | ✅ Fait | Éditeur bat/ps1/vbs, variables, déploiement `scriptPath`/GPO | ✅ |
| M16 Heures | ✅ Fait | Grille 24h×7j, préréglages, attribut `logonHours` | ✅ |
| M17 Homedirs | ✅ Fait | Création `\\srv\homes\%USERNAME%`, droits NTFS, lettre lecteur | ✅ |
| M18 Partages | ✅ Fait | Partage SMB+NTFS auto à création groupe, sync membres↔droits | ✅ |
| M19 M365 | ✅ Fait | Graph API, users cloud+licences, groupes/Teams, photos, sync MDP | ✅ |
| M20 Exchange | ✅ Fait | Boîtes aux lettres, aliases, policies, quotas (On-prem/Online) | ✅ |
| M21 RDS | ✅ Fait | Collections, RemoteApp, UPD/FSLogix | ✅ |
| M22 Import/Export | ✅ Fait | Import LDAP, export LDIF/vCard, publipostage HTML, import GEP | ✅ |
| M23 Modèles groupes | ✅ Fait | Modèles classe/personnel/services, instanciation 1-clic, duplication/import | ✅ |
| M24 Label Studio | ✅ Fait | WYSIWYG, badges/cartes, trombinoscope A4/A3, envoi mail | ✅ |

---

## Modules v4.0 (M25-M32)

| Module | Statut | Description | Tests |
|--------|--------|-------------|-------|
| M25 Multisite | ✅ Fait | Multi-domaines/forêts, `domaines.json` chiffré, sélecteur, journal par domaine | ✅ |
| M26 RBAC | ✅ Fait | 5 rôles, portées OU/groupe, garde-fou écriture AD, journal par opérateur | ⚠️ **Manquants** |
| M27 Portail | ✅ Fait | Reset MDP code mail, consultation identifiant/mail, demande création compte, serveur stdlib | ⚠️ **Manquants** |
| M28 API REST | ✅ Fait | OpenAPI, webhooks, sync bidir SIS | ⚠️ **Manquants** |
| M29 IA | ✅ Fait | Anomalies, nettoyage fin année, identifiants intelligents, analyse logs | ⚠️ **Manquants** |
| M30 Conformité | ✅ Fait | RGPD, droit oubli, MFA, session unique, PSO, signature exports | ⚠️ **Manquants** |
| M31 DFS/IIS | ✅ Fait | DFS, WebDAV/IIS, FTP isolé (priorité basse) | ⚠️ **Manquants** |
| M32 Sauvegarde | 🚧 **En cours** | Backup ZIP chiffré, restore 1-clic, migrations vN→vN+1 | ❌ **Manquants** |

---

## Dette technique identifiée

| Domaine | Description | Impact migration Go |
|---------|-------------|---------------------|
| **Tests M26-M32** | 0 tests pour RBAC, Portail, API, IA, Conformité, DFS, Sauvegarde | Doit être comblé en Go (Priorité 3) |
| **Numérotation versions** | Releases publiées en `1.x` (1.17.1) vs jalons `v2.0`→`v4.0` | Documenter dans release notes Go |
| **Migration Go** | Réécriture complète indépendante | Voir `ROADMAP_GO_MIGRATION.md` |

---

## Fichiers de configuration (référence compatibilité)

| Fichier | Format | Chemin (platformdirs) | Validation Go |
|---------|--------|----------------------|---------------|
| `config.json` | JSON UTF-8, clés fr `snake_case`, indent 2 | `~/.config/EduSyncAD/config.json` | ✅ Test aller-retour |
| `secret.key` | 32 octets bruts, 0600 | `~/.config/EduSyncAD/secret.key` | ✅ Vecteur interop |
| `journal.db` | SQLite, tables `actions` + `pending_deletions` | `~/.local/share/EduSyncAD/journal.db` | ✅ Schéma identique |

---

## Commandes de référence Python

```bash
# Développement
python -m venv .venv && source .venv/bin/activate
pip install -e ".[dev]"
python src/edusync_ad/app.py

# Tests
pytest                    # ~757 tests
pytest tests/core/        # Tests unitaires core
pytest -k "migration"     # Filtre par module

# Build Windows
pip install pyinstaller cairosvg pillow
python tools/generate_icon.py
pyinstaller packaging/edusync_ad.spec
# → dist/EduSyncAD/EduSyncAD.exe

# Build Linux Flatpak
flatpak-builder build-dir packaging/org.edusync.EduSyncAD.yml
```

---

## Points d'attention pour la parité Go

1. **Chiffrement** : Format `base64(nonce(12) ‖ AES-256-GCM)` identique — vecteur test dans `internal/crypto`
2. **Config JSON** : Clés françaises `snake_case` — test `internal/config` lit fixture Python
3. **Journal SQLite** : Schéma `actions` + `pending_deletions` identique — test `internal/audit`
4. **Chemins** : `platformdirs` identiques — test `internal/config`
5. **Simulation** : Pattern `Validate→Simulate→Execute→Audit` sur **tous** modules
6. **LDAPS** : Priorité 636, repli 389 + avertissement, CA custom
7. **Mots de passe** : Jamais stockés en clair, coffre AES-256 local uniquement