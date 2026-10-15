# Modules avancés (M12 → M24) + Cache AD (T2) — Lot B

> **Source** : `ROADMAP_GO_MIGRATION.md` §4.2, §6 « Lot B »
> **Périmètre** : 13 modules avancés + Cache AD SQLite (T2)
> **Priorité** : 1 — Après Lot A (Core M1-M11 fini)
> **Dépendance** : Lot A terminé (pool AD, simulation, audit, CLI, UI patterns)

---

## Ordre d'implémentation (Lots B1 → B8)

| # | Modules | Points clés | Dépendances |
|---|---------|-------------|-------------|
| B1 | M13 Profils, M16 Heures, M15 Scripts | Attributs AD classiques (profilePath, logonHours, scriptPath), appliqués en masse à la création/migration | Pool AD, simulation, modèles User |
| B2 | M17 Homedirs, M14 Quotas, M18 Espaces classe | Partages SMB/NTFS/FSRM — prérequis serveur Windows côté client, **documenter** | Pool AD, modèles Group/OU, simulation |
| B3 | **T2 Cache AD** | Cache SQLite OU/groupes/users, invalidation TTL + `uSNChanged` — **conditionne fluidité explorateur** | SQLite, pool AD, modèles |
| B4 | M12 Photos | Import masse + cadrage 3:4 ≤ 200 Ko, vignettes explorateur + étiquettes (intègre B8) | Cache AD, label package, simulation |
| B5 | M22 Imports/Exports avancés | Import LDAP (OU/filtre), export LDIF/vCard, publipostage HTML, import GEP | CSV/label packages, pool AD |
| B6 | M19 M365, M20 Exchange | Microsoft Graph + On-prem/Online — **tests manuels documentés** (pas d'AD test cloud) | API package, crypto, config |
| B7 | M21 RDS | Collections, RemoteApp, UPD/FSLogix | Pool AD, modèles |
| B8 | M24 Label Studio + M23 Modèles groupes | Éditeur WYSIWYG, trombinoscope A4/A3, instanciation 1-clic | Label package, cache AD, modèles |

---

## B1 — M13 Profils, M16 Heures, M15 Scripts

### M13 — Profils utilisateurs (itinérant / local / obligatoire)
- **Objectif** : Configurer `profilePath` (itinérant `\\srv\profil\%USERNAME%`), local, obligatoire (`.man`) par OU ou groupe
- **Python ref** : `src/edusync_ad/core/profil.py`, plugin `profil`
- **Go à créer** :
  - `internal/ad/profile.go` : `ApplyProfile(userDN, ProfileConfig)` + simulation
  - `internal/models/profile.go` : `ProfileConfig{Type, PathTemplate, Variables}`
  - Routes UI + gabarit `profile.html` (configuration par OU/groupe)
  - CLI : `edusync-cli profile apply --ou ... --template ...`
- **Variables supportées** : `%USERNAME%`, `%SAM%`, `%OU%`, `%FULLNAME%`
- **Validation** : Appliqué lors création (M1) + migration (M2)

### M16 — Heures de connexion (logonHours)
- **Objectif** : Grille 24h × 7j par utilisateur ou masse (OU/groupe), préréglages
- **Python ref** : `src/edusync_ad/core/heures_connexion.py`
- **Go à créer** :
  - `internal/ad/logonhours.go` : `ParseGrid/BuildAttribute/Apply(userDN, schedule)`
  - `internal/models/logonhours.go` : `Schedule{Days[7]Hours[24]bool}`, préréglages
  - UI : éditeur visuel grille (Alpine), préréglages « Heures cours », « Heures admin »
  - CLI : `edusync-cli logonhours set --ou ... --preset cours`
- **Attribut AD** : `logonHours` (octet 21 bytes, bit par heure)

### M15 — Scripts logon/logoff (GPO-like)
- **Objectif** : Éditeur scripts (bat/powershell/vbs) par OU/groupe, variables, déploiement `scriptPath`/GPO
- **Python ref** : `src/edusync_ad/core/scripts_logon.py`
- **Go à créer** :
  - `internal/ad/script.go` : `DeployScript(ouDN, ScriptConfig)` — écriture `scriptPath` + GPO liaison optionnelle
  - `internal/models/script.go` : `ScriptConfig{Type, Content, Variables, TargetOU}`
  - UI : éditeur Monaco/textarea + preview variables
  - CLI : `edusync-cli script deploy --ou ... --file ... --type logon`

---

## B2 — M17 Homedirs, M14 Quotas, M18 Espaces partagés

### M17 — Dossiers personnels (Home Directory)
- **Objectif** : Création auto `\\srv\homes\%USERNAME%`, droits NTFS (Utilisateur:Modification, Admins:Total, SYSTEM), lettre lecteur configurable
- **Python ref** : `src/edusync_ad/core/dossier_perso.py`
- **Go à créer** :
  - `internal/ad/homedirectory.go` : `CreateHomeDir(userDN, HomeConfig)` — appel PowerShell distant ou SMB + `icacls`/`Set-Acl`
  - `internal/models/homedirectory.go` : `HomeConfig{Server, Share, Letter, QuotaRef}`
  - **Note** : Exécution côté serveur Windows requise — documenter prérequis (WinRM/PowerShell Remoting)
  - CLI : `edusync-cli homedir create --user ... --server SRV --share homes`

### M14 — Quotas de disque (FSRM)
- **Objectif** : Quota par OU/groupe (taille, seuils alerte, blocage dur), application auto à la création dossier, rapport CSV
- **Python ref** : `src/edusync_ad/core/quotas.py`
- **Go à créer** :
  - `internal/ad/quota.go` : `ApplyQuota(path, QuotaConfig)` — FSRM via PowerShell/WinRM
  - `internal/models/quota.go` : `QuotaConfig{SizeLimit, Thresholds[], HardLimit, Template}`
  - UI : configuration quotas par OU/groupe, rapport export CSV
  - **Note** : FSRM = File Server Resource Manager (Windows Server) — documenter

### M18 — Espaces partagés par classe/groupe
- **Objectif** : Création auto partage SMB + NTFS à la création groupe classe, modèle `\\srv\classes\%GROUP%` — profs (R/W), élèves (R), admins (Total), sync membres ↔ droits
- **Python ref** : `src/edusync_ad/core/espaces_partages.py`
- **Go à créer** :
  - `internal/ad/sharedspace.go` : `CreateSharedSpace(groupDN, ShareConfig)`, `SyncPermissions(groupDN)`
  - `internal/models/sharedspace.go` : `ShareConfig{Server, ShareRoot, PermissionsTemplate}`
  - UI : déclencheur auto à la création groupe (M1/M2) + sync manuelle
  - **Note** : SMB/NTFS distant — documenter prérequis Windows

---

## B3 — T2 Cache AD SQLite (Critique pour explorateur)

> **Objectif** : Cache local OU/groupes/users synchronisé en background, invalidation TTL + `uSNChanged` polling, recherche offline → explorateur instantané sur gros domaines (>5k objets).

### Architecture
```
internal/cache/
├── cache.go           # Cache struct, méthodes Get/Set/Invalidate
├── syncer.go          # Background sync (goroutine + ticker), uSNChanged polling
├── models.go          # CachedOU, CachedGroup, CachedUser (subset champs utiles UI)
└── cache_test.go      # Tests : sync, invalidation, recherche offline
```

### Fonctionnement
1. **Démarrage** : Chargement cache depuis SQLite → UI utilisable immédiatement
2. **Sync initial** : Goroutine `syncer.FullSync()` (pagination LDAP, batch INSERT)
3. **Maintien** : Toutes les `SyncInterval` (configurable, défaut 5 min) :
   - Lecture `uSNChanged` depuis `rootDSE`
   - Si changé : `syncer.IncrementalSync(sinceUSN)` → mise à jour objets modifiés
4. **Invalidation manuelle** : Après toute écriture AD (M1, M2, M3, M4) → `cache.Invalidate(ouDN)` + `syncer.TriggerSync()`
5. **Recherche UI** : `cache.Search(query, filters)` → résultats instantanés, fallback LDAP si cache vide

### Schéma SQLite (`cache.db`)
```sql
CREATE TABLE cached_ous (
    dn TEXT PRIMARY KEY, name TEXT, parent_dn TEXT, guid TEXT, usn_changed BIGINT, updated_at TIMESTAMP
);
CREATE TABLE cached_groups (
    dn TEXT PRIMARY KEY, name TEXT, ou_dn TEXT, guid TEXT, members_count INT, usn_changed BIGINT, updated_at TIMESTAMP
);
CREATE TABLE cached_users (
    dn TEXT PRIMARY KEY, sam TEXT, given_name TEXT, sn TEXT, ou_dn TEXT, guid TEXT, enabled BOOL, usn_changed BIGINT, updated_at TIMESTAMP
);
CREATE INDEX idx_cached_users_ou ON cached_users(ou_dn);
CREATE INDEX idx_cached_groups_ou ON cached_groups(ou_dn);
```

### Intégration
- `internal/ad/pool.go` : `pool.Search()` → d'abord `cache.Search()`, sinon LDAP + maj cache
- `internal/ad/explorer.go` : `BuildTree()` → `cache.GetTree()`
- Écritures AD (M1-M4) → `cache.Invalidate(affectedOUs)` + trigger sync

---

## B4 — M12 Photos d'identité

- **Objectif** : Import manuel/masse/nommage auto, cadrage 3:4 ≤ 200 Ko, vignettes explorateur + étiquettes
- **Python ref** : `src/edusync_ad/core/photos.py`, plugin `photos`
- **Go à créer** :
  - `internal/ad/photo.go` : `ImportPhoto(userDN, imageBytes)`, `Thumbnail(userDN, size)`, `DeletePhoto(userDN)`
  - `internal/photo/` : `processor.go` (resize/crop 3:4, max 200 Ko, JPEG quality), `naming.go` (conventions `prenom_nom.jpg`, `samaccountname.jpg`)
  - UI : dropzone import, preview, cadrage manuel (Canvas), vignettes dans explorateur (cache)
  - Intégration étiquettes (B8) : photo dans PDF trombinoscope
  - CLI : `edusync-cli photo import --csv mapping.csv --dir photos/`, `photo thumbnail --user ...`

---

## B5 — M22 Import/Export avancés

| Sous-module | Fonctionnalité | Go à créer |
|-------------|----------------|------------|
| Import LDAP | Depuis OU, groupe, filtre LDAP personnalisé | `internal/csv/ldap_import.go` |
| Export LDIF | Paramétrable, pour annuaires LDAP tiers | `internal/csv/ldif_export.go` |
| Export vCard | Cartes de visite (RFC 6350) | `internal/csv/vcard_export.go` |
| Publipostage HTML | Chartes, conventions, docs personnalisés | `internal/label/mailmerge.go` (templates Go `html/template`) |
| Import GEP | Via outil tiers fourni (Base Education Nationale) | `internal/csv/gep_import.go` (format documenté) |

---

## B6 — M19 Microsoft 365 / Entra ID, M20 Exchange

> **Note** : Pas d'AD de test cloud → tests manuels documentés, mocks pour CI.

### M19 — Microsoft 365 / Entra ID (Hybride)
- App registration (certificat/secret), Microsoft Graph SDK Go (`msgraph-sdk-go`)
- Création users cloud + licences (packs A1/A3/A5)
- Groupes distribution/sécurité/Teams auto depuis groupes AD primaires
- Sync mot de passe (PHS) ou fédération (ADFS/PTA) — config seulement
- Photos users → cloud

### M20 — Exchange (On-prem / Online)
- Création boîtes aux lettres users/groupes (EWS / Graph / PowerShell distant)
- Aliases / adresses proxy supplémentaires
- Politiques d'adresses (Email Address Policies)
- Quotas boîte, rétention, archivage

---

## B7 — M21 RDS / Bureau à distance

- Collections de sessions / applications RemoteApp
- Publication apps par groupe AD
- Profils utilisateurs RDS (disques profil UPD / FSLogix)
- **Go** : `internal/ad/rds.go` — PowerShell distant / WMI

---

## B8 — M24 Label Studio + M23 Modèles de groupes

### M24 — Étiquettes avancées (Label Studio)
- **Objectif** : Éditeur WYSIWYG (positionnement champs, police, couleur, QR code, photo), modèles pré-enregistrés (Avery L7160, L7163, badges, cartes), trombinoscope grille photos + noms export PDF A4/A3, envoi étiquette individuelle par mail
- **Python ref** : `src/edusync_ad/core/label_studio.py`, plugin `label_studio`
- **Go à créer** :
  - `internal/label/studio.go` : `Template{Fields[], PageSize, Grid}`, `RenderPDF(template, data)` (package `label` existant + `github.com/signintech/gopdf`)
  - UI : éditeur Canvas/HTML5 (Alpine + Fabric.js ou konva.js — évaluer taille), sauvegarde templates JSON
  - Trombinoscope : grille responsive, export PDF multi-pages
  - Envoi mail étiquette individuelle (SMTP config depuis paramètres)

### M23 — Modèles de groupes (Templates)
- **Objectif** : Modèles « Classe élève », « Classe prof », « Personnel admin », « Service technique »… instanciation 1-clic
- **Python ref** : `src/edusync_ad/core/modeles_groupes.py`
- **Go à créer** :
  - `internal/models/grouptemplate.go` : `GroupTemplate{Name, OUParent, AutoGroups[], Scripts, Quotas, HomeDir, SharedSpace, Profile, LogonHours, PasswordPolicy, M365Licenses}`
  - `internal/ad/grouptemplate.go` : `Instantiate(template, className)` → crée OU + groupes + applique tous réglages
  - UI : liste modèles, bouton « Instancier » → saisie nom → exécution simulation → audit
  - Import/Export modèles (JSON/YAML)
  - CLI : `edusync-cli template list`, `template instantiate --name "Classe_6e_A" --model classe_eleve`

---

## Critères de validation Lot B

- [ ] Chaque module M12-M24 : backend → UI → CLI → i18n fr/en → tests
- [ ] **T2 Cache AD** : explorateur fluide sur 10k+ objets (test manuel `task ad:start` + 5000 users injectés)
- [ ] `gofmt`, `go vet`, `go test ./...`, `pytest -q` verts après chaque module
- [ ] Roadmap mise à jour (statuts §4.2)
- [ ] Documentation prérequis Windows (WinRM, FSRM, SMB) pour M14, M17, M18, M21

---

## Prochaine étape

→ **Lot C** : `docs/migration/03-modules-v4.md` (M25-M32 + Coffre MDP)