# Lot C — Modules v4.0 (M25-M32) + Coffre MDP

> **Période** : Après Lot B (Modules avancés finis)
> **Source** : `ROADMAP_GO_MIGRATION.md` §4.3, §6 « Lot C »

---

## Vue d'ensemble

| Module | Fonctionnalités Python (référence) | État Go |
|--------|-----------------------------------|---------|
| **M25** | Multisite : plusieurs domaines/forêts, `domaines.json` chiffré, sélecteur, journal par domaine | ⬜ |
| **M26** | RBAC (Délégation) : 5 rôles, portées OU/groupe, garde-fou sur chaque écriture AD, journal par opérateur | ⬜ |
| **M27** | Portail auto-service : réinit MDP par code mail, consultation identifiant/mail, demande création compte + validation admin | ⬜ |
| **M28** | API REST + Webhooks : OpenAPI, webhooks SIS, sync bidirectionnelle | ⬜ |
| **M29** | IA / Assistant : détection anomalies, nettoyage fin d'année, identifiants intelligents, analyse logs | ⬜ |
| **M30** | Conformité & Sécurité : rapport RGPD, droit à l'oubli, MFA admin, session unique, PSO, signature exports | ⬜ |
| **M31** | DFS/IIS/WebDAV/FTP : espaces DFS, publication WebDAV/IIS, FTP isolé (priorité basse) | ⬜ |
| **M32** | Sauvegarde/Restauration : backup ZIP chiffré, restore 1-clic, migrations vN→vN+1 | ⬜ |
| **Coffre MDP** | AES-256 local (machines admin), jamais sur l'AD | ⬜ |

---

## Ordre d'implémentation (C1 → C7)

### C1 — M25 Multisite (Fondation pour M26, M27, M28)

**Modèle de données** :
```go
type Site struct {
    ID           string
    Name         string
    Domain       string        // FQDN
    Controller   string        // DC hostname/IP
    CredsEncrypted []byte      // Chiffré avec clé dérivée master key
    BaseDN       string
    TLSConfig    *TLSConfig
    IsDefault    bool
}
```

**Changements transverses** :
- Config : `config.json` → `sites[]` chiffré (clé dérivée master key)
- Header UI : sélecteur de site (Alpine store `currentSite`)
- Pool AD : `map[siteID]*ConnectionPool` (LRU, max 5 simultanées par site)
- Journal : colonne `site_id` + filtre UI
- CLI : `--site <id>` sur toutes commandes AD

**Fichiers** :
- `internal/multisite/site.go`, `internal/multisite/config.go`
- `internal/config/sites.go` (extension config)
- `internal/ui/sites.go` (handlers + middleware site context)
- `web/templates/partials/site_selector.html` (header)
- `web/templates/sites.html` (gestion sites)
- `cmd/edusync-cli/sites.go`

---

### C2 — M26 RBAC (Délégation d'administration)

**5 Rôles** :
| Rôle | Portée | Description |
|------|--------|-------------|
| Super-admin | Tous sites, toutes OU | Accès total, gestion sites, RBAC, audit global |
| Admin site | 1 site, toutes OU | Gestion complète sur son site |
| Admin classe | 1 site, OU/groupe spécifiques | Création/migration/reset sur ses OU/groupes |
| Helpdesk | 1 site, OU/groupe spécifiques | Reset MDP, consultation, départs |
| Lecture seule | 1 site, OU/groupe spécifiques | Consultation uniquement |

**Architecture** :
- `internal/rbac/` : `Evaluator`, `Policy`, `Role`, `Scope`
- **Garde-fou** : Middleware sur **chaque** handler écriture AD → `rbac.Check(ctx, action, targetOU, targetGroup)`
- Journal : enrichir `AuditEntry` avec `Operator`, `Role`, `SiteID`
- Config : `delegations.json` (clair, signé) : `Operator, Role, SiteID, Scopes[], Active`
- UI : Onglet « Délégation » dans Paramètres (réservé Super-admin) — matrice opérateurs × rôles × portées

**Fichiers** :
- `internal/rbac/evaluator.go`, `internal/rbac/policy.go`, `internal/rbac/middleware.go`
- `internal/audit/entry.go` (ajout champs RBAC)
- `internal/ui/rbac.go` (handlers UI délégation)
- `web/templates/settings_delegation.html`
- `cmd/edusync-cli/rbac.go`

---

### C3 — Coffre MDP + M32 Sauvegarde/Restauration

#### Coffre MDP (AES-256 local)
- Format identique Python : `base64(nonce(12) || AES-256-GCM(ciphertext, tag))`
- Clé 32 octets dans `secret.key` (0600, `platformdirs`)
- API : `vault.Store(key, value)`, `vault.Retrieve(key)`, `vault.Delete(key)`, `vault.List()`
- UI : Onglet « Coffre » dans Paramètres (M8) — accès Super-admin / Admin site

#### M32 Sauvegarde / Restauration / Migration
| Fonctionnalité | Détail |
|---------------|--------|
| **Backup** | ZIP chiffré (AES-256-GCM) : `config.json` + `secret.key` + `journal.db` + modèles + templates + certificats + `domaines.json` (si multisite) |
| **Restore** | 1-clic : arrêt app → déchiffre → écrit fichiers → redémarre |
| **Migration vN→vN+1** | Scripts de migration versionnés dans `internal/migrate/versions/` : `v1_to_v2.go`, etc. Appliqués auto au 1er démarrage après mise à jour |

**Fichiers** :
- `internal/vault/vault.go` (existant ? vérifier)
- `internal/backup/backup.go`, `internal/backup/restore.go`, `internal/backup/migrate.go`
- `internal/migrate/versions/v1_to_v2.go`, etc.
- `web/templates/backup.html` (backup/restore)
- `web/templates/settings_vault.html`
- `cmd/edusync-cli/backup.go`, `vault.go`

---

### C4 — M27 Portail Auto-service (Serveur web stdlib)

**Architecture** : Serveur HTTP Go stdlib (`net/http`), **aucune dépendance externe**.
- Port configurable, TLS optionnel (certificat auto-signé ou fourni)
- Géré depuis l'onglet Paramètres (démarrage/arrêt, statut)
- RBAC : permissions `reset_password`, `create_user` (portail)

**Fonctionnalités** :
1. **Réinit MDP self-service** : Code par courriel (PBKDF2, usage unique, anti-énumération), définition nouveau MDP (politique élève/personnel), forçage changement prochaine connexion
2. **Consultation identifiant/mail** : Même vérification par code
3. **Demande création compte** : Pré-inscription → validation admin (création compte + envoi identifiants)

**Sécurité** :
- Rate limiting par IP + par compte
- Codes à usage unique (stockage SQLite `portal_codes` : hash PBKDF2, expiry, used flag)
- Audit : `utilisateur="portail"`, `domaine=site`, `type=portal_reset|portal_lookup|portal_request`
- CSRF tokens, headers sécurisés, SameSite=Strict

**Fichiers** :
- `internal/portal/server.go` (stdlib HTTP)
- `internal/portal/handlers.go` (reset, lookup, request)
- `internal/portal/codes.go` (PBKDF2, stockage SQLite)
- `internal/portal/mail.go` (SMTP stdlib)
- `web/portal/templates/` (pages HTML pures, pas HTMX — accessibles sans JS)
- `cmd/edusync-cli/portal.go` (start/stop/status)

---

### C5 — M28 API REST + Webhooks

**Spécification** : OpenAPI 3.1 (fichier `docs/api/openapi.yaml` + Swagger UI embeddé)
- Auth : API Key (header `X-API-Key`) + scopes (lecture/écriture par module)
- Versioning : `/api/v1/`

**Endpoints principaux** :
| Ressource | Endpoints |
|-----------|-----------|
| Utilisateurs | `GET/POST /users`, `GET/PATCH/DELETE /users/{id}`, `POST /users/{id}/reset-password` |
| Groupes | `GET/POST /groups`, `GET/PATCH/DELETE /groups/{id}`, `POST /groups/{id}/members` |
| Migrations | `POST /migrations/plan`, `POST /migrations/simulate`, `POST /migrations/execute` |
| Départs | `POST /departs/simulate`, `POST /departs/execute` |
| Audit | `GET /audit` (filtres, pagination) |
| Sites | `GET /sites`, `GET /sites/{id}` |

**Webhooks** :
- Événements : `user.created`, `user.migrated`, `user.departed`, `user.password_reset`, `group.created`, `ou.created`
- Configuration : URL cible, secret HMAC (signature `X-Webhook-Signature`), retry/backoff
- Destinations : Teams, Slack, Mail, SI (SIS), webhook générique

**Sync bidirectionnelle SIS** : Mapping champs EduSync ↔ SIS (PRONOTE, etc.), planification (cron), résolution conflits.

**Fichiers** :
- `internal/api/openapi.yaml` (spec)
- `internal/api/server.go` (chi router ou stdlib)
- `internal/api/handlers/` (par ressource)
- `internal/api/webhooks.go` (dispatcher, retry, signature)
- `internal/api/sis_sync.go` (bidirectionnel)
- `web/api/swagger.html` (Swagger UI embeddé)
- `cmd/edusync-cli/api.go` (start/stop, generate client)

---

### C6 — M29 IA / Assistant + M30 Conformité & Sécurité

#### M29 IA / Assistant
| Fonction | Implémentation |
|----------|----------------|
| Détection anomalies | Règles heuristiques (pas de ML externe) : doublons non résolus, comptes orphelins (pas de groupe, OU par défaut), OU vides, groupes sans membres |
| Nettoyage fin d'année | Suggestions : archiver classes sorties, purger comptes départs > 30j, réviser groupes périmés |
| Identifiants intelligents | Génération évitant homonymes (check AD existant), respect charte établissement (configurable) |
| Analyse logs → sécurité | Règles : connexions échecs répétés, modifications inhabituelles, comptes dormants, privilèges excessifs |

**UI** : Tableau de bord « Assistant » avec cartes d'alertes + actions 1-clic (simulation → exécution).

**Fichiers** : `internal/ai/analyzer.go`, `internal/ai/suggestions.go`, `internal/ai/idgen.go`, `web/templates/ai_dashboard.html`

#### M30 Conformité & Sécurité
| Fonction | Détail |
|----------|--------|
| Rapport RGPD | Données personnelles stockées (config, journal, coffre, modèles), export JSON/CSV, droit à l'oubli (purge coffre MDP utilisateur, anonymisation journal) |
| MFA Admin | TOTP (RFC 6238) — `github.com/pquerna/otp` — obligatoire pour Super-admin, optionnel autres |
| Session unique | Un seul onglet/app par utilisateur (token session + verrouillage) |
| PSO (Fine-Grained Password Policies) | Lecture/écriture `msDS-PasswordSettings` via AD, application aux groupes |
| Signature numérique exports | Horodatage RFC 3161 + certificat (optionnel), non-répudiation CSV/PDF |

**Fichiers** : `internal/compliance/rgpd.go`, `internal/compliance/mfa.go`, `internal/compliance/pso.go`, `internal/compliance/signature.go`, `web/templates/compliance.html`

---

### C7 — M31 DFS/IIS/WebDAV/FTP (Priorité basse)

**Livré en dernier** — seulement si temps disponible avant alpha.

| Fonction | Backend |
|----------|---------|
| Espaces DFS | `internal/dfs/namespace.go` : création dossiers DFS, réplication multi-serveurs |
| Publication WebDAV/IIS | `internal/webdav/publish.go` : configuration auto dossiers perso/partagés |
| FTP isolé AD | `internal/ftp/isolated.go` — héritage, priorité basse |

**Note** : Ces modules nécessitent des serveurs Windows cibles spécifiques. Documenter les prérequis explicitement.

---

## Fichiers à créer (Lot C - aperçu)

```
internal/multisite/          # C1
internal/rbac/               # C2
internal/vault/              # C3 (coffre)
internal/backup/             # C3 (sauvegarde)
internal/migrate/            # C3 (migrations vN→vN+1)
internal/portal/             # C4 (stdlib HTTP)
internal/api/                # C5
internal/ai/                 # C6 (M29)
internal/compliance/         # C6 (M30)
internal/dfs/                # C7
internal/webdav/             # C7
internal/ftp/                # C7

cmd/edusync-cli/
├── sites.go
├── rbac.go
├── vault.go
├── backup.go
├── portal.go
├── api.go
├── ai.go
├── compliance.go
├── dfs.go

web/templates/
├── sites.html
├── settings_delegation.html
├── settings_vault.html
├── backup.html
├── portal_config.html
├── api_docs.html (Swagger UI)
├── ai_dashboard.html
├── compliance.html
├── dfs.html
└── partials/
    ├── site_selector.html
    ├── rbac_matrix.html
    ├── vault_entry.html
    └── ai_alert_card.html

web/portal/templates/        # C4 (HTML pur, pas HTMX)
├── login.html
├── reset_password.html
├── lookup.html
├── request_account.html
└── success.html
```

---

## Critères de validation Lot C

- [ ] **Multisite (C1)** : 2 containers Samba (EDUSYNC.LOCAL + ANNEXE.LOCAL) → switch site, opérations croisées, journal filtré par site
- [ ] **RBAC (C2)** : Garde-fou effectif sur **chaque** écriture AD (test : Helpdesk tente création hors portée → 403 + audit)
- [ ] **Coffre + Sauvegarde (C3)** : Backup ZIP chiffré → restore 1-clic sur machine vierge → config + journal + coffre intacts
- [ ] **Portail (C4)** : Serveur stdlib démarre/arrête depuis UI, reset MDP par code mail fonctionne, audit `utilisateur="portail"`
- [ ] **API (C5)** : OpenAPI valide, webhooks signés reçus par cible test, sync bidirectionnelle SIS documentée
- [ ] **IA + Conformité (C6)** : Alertes pertinentes, rapport RGPD complet, MFA TOTP fonctionnel, PSO lecture/écriture, exports signés
- [ ] `task lint && task test && task test:python` verts
- [ ] Roadmap mise à jour (statuts §4.3)

---

## Dépendances techniques à évaluer

| Module | Dépendance Go candidate | Justification |
|--------|------------------------|---------------|
| M27 Portail | Aucune (stdlib) | Exigence : zéro dépendance externe |
| M28 API | `github.com/go-chi/chi/v5` ou stdlib | Router léger + middlewares |
| M28 Webhooks | Aucune (stdlib HMAC/SHA256) | Signature HMAC, retry exponentiel |
| M29 IA | Aucune (heuristiques pures) | Pas de ML externe — règles métier |
| M30 MFA | `github.com/pquerna/otp` | TOTP RFC 6238, standard |
| M30 Signature | `github.com/certifi/gocertifi` + `crypto/x509` | Horodatage RFC 3161 |
| M31 DFS/IIS | WMI/CIM ou WinRM | Gestion serveur Windows distant |

> **Règle** : Toute nouvelle dépendance = justification écrite dans le commit.

---

## Prochaine étape après Lot C

→ **Clôture Priorité 1** : `edusync-cli migrate --from-python` + Checklist §4 complète → **Porte 1**
→ **Priorité 2** : Alpha Windows + Linux — voir `04-alpha.md`