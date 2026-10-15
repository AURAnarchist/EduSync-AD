# Python v1.x — Tests manquants (Gaps à combler en Go)

> **Objectif** : Identifier les zones non testées en Python pour s'assurer qu'elles **le soient en Go** (Priorité 3)
> **Règle** : Zéro perte fonctionnelle = tests Go sur 100% du périmètre, y compris ce que Python n'a pas testé

---

## Gap critique — Modules v3.5/v4.0 sans tests

| Module | Fonctionnalités non testées | Priorité Go |
|--------|----------------------------|-------------|
| **M26 — RBAC** | 5 rôles, portées OU/groupe, garde-fou écriture AD, journal par opérateur, matrice délégation | 🔴 **Critique** — Sécurité |
| **M27 — Portail auto-service** | Reset MDP code mail (PBKDF2 usage unique, anti-énumération), consultation identifiant/mail, demande création compte + validation admin, serveur stdlib, rate limiting, audit `utilisateur="portail"` | 🔴 **Critique** — Sécurité + Exposition web |
| **M28 — API REST + Webhooks** | OpenAPI endpoints, auth API Key + scopes, webhooks HMAC + retry, sync bidir SIS | 🔴 **Critique** — Intégration |
| **M29 — IA / Assistant** | Détection anomalies (doublons, orphelins, OU vides, groupes vides), suggestions nettoyage fin année, identifiants intelligents (anti-homonymes), analyse logs → recommandations sécurité | 🟠 **Haute** — Différenciation |
| **M30 — Conformité & Sécurité** | Rapport RGPD (données stockées, droit oubli), MFA TOTP admin, session unique, PSO lecture/écriture, signature exports RFC3161 | 🔴 **Critique** — Conformité |
| **M31 — DFS/IIS/WebDAV/FTP** | Espaces DFS, publication WebDAV/IIS, FTP isolé | 🟢 **Basse** — Priorité basse |
| **M32 — Sauvegarde** | Backup ZIP chiffré (config+secret+journal+modèles+certificats), restore 1-clic, migrations vN→vN+1 | 🔴 **Critique** — Récupération |

---

## Gap modéré — Couverture partielle sur modules avancés

| Module | Zones testées | Zones non testées / faibles |
|--------|---------------|----------------------------|
| M12 Photos | Import unitaire | Import masse, cadrage, vignettes, intégration étiquettes |
| M13 Profils | Application profil | Variables `%USERNAME%` `%SAM%` `%OU%` dans tous contextes |
| M14 Quotas | Configuration | Application auto à création dossier, rapport CSV |
| M15 Scripts | Éditeur | Déploiement GPO, variables runtime |
| M16 Heures | Grille UI | Application masse OU/groupe, attribut `logonHours` bitmap |
| M17 Homedirs | Création | Droits NTFS exacts, quotas FSRM liés (M14) |
| M18 Partages | Création | Sync membres↔droits en continu |
| M19 M365 | Connexion Graph | Création users cloud, licences, groupes Teams, sync photos |
| M20 Exchange | Connexion | Boîtes aux lettres, aliases, policies, quotas |
| M21 RDS | Collections | RemoteApp publication, UPD/FSLogix |
| M22 Import/Export | Export CSV | Import LDAP, export LDIF/vCard, publipostage, import GEP |
| M23 Modèles groupes | Structure | Instanciation 1-clic complète, duplication/import |
| M24 Label Studio | Templates | WYSIWYG éditeur, trombinoscope PDF, envoi mail |

---

## Gap fondations (T0-T2)

| Fondation | Testé | Manquant |
|-----------|-------|----------|
| **T0 LDAP Async** | Queue jobs, progression, annulation | Charge haute (1500 users), race conditions, timeout réseau |
| **T1 Plugins** | Chargement entry_points | Hot-reload (hors périmètre), conflits dépendances, activation/désactivation runtime |
| **T2 Cache AD** | Structure cache | Invalidation `uSNChanged` incrémentielle, cohérence multi-thread, TTL expiré |

---

## Scénarios d'erreur non testés (à couvrir en Go)

| Scénario | Modules concernés | Description |
|----------|-------------------|-------------|
| Réseau coupé pendant opération masse | M1, M2, M3, M4, M6 | Rollback partiel, état cohérent, reprise |
| AD inaccessible (timeout/refus) | Tous | Messages clairs, pas de crash, retry/backoff |
| Certificat LDAPS invalide | M9 | Option « Continuer quand même », fallback LDAP |
| Verrouillage compte (bad pwd count) | M4, M27 | Détection, message, déverrouillage |
| Quotas disque dépassés | M14, M17 | Erreur création dossier, notification |
| Permissions insuffisantes AD | Tous | Garde-fou RBAC (M26), message précis |
| Fichier CSV corrompu/encodage | M1, M2, M3, M4, M6 | Détection encodage, séparateur, lignes invalides |
| Doublons non résolus | M1, M2 | Détection, suggestions, import partiel |
| Conflits migration (OU cible verrouillée) | M2 | Détection, rapport, rollback |
| Purge départs interrompu | M3 | Idempotence, reprise |
| Coffre MDP corrompu | M4, M6, M8 | Détection, récupération, alerte |
| Migration config vN→vN+1 échouée | M32 | Rollback, sauvegarde `.bak`, message |

---

## Tests de performance (benchmarks cibles Go)

| Opération | Cible Go | Référence Python | Méthode test |
|-----------|----------|------------------|--------------|
| Démarrage à froid | < 100 ms | 2-5 s | `BenchmarkStartup` |
| Import CSV 1500 comptes | < 30 s | ~45 s | `BenchmarkCSVImport` (pool LDAP tuning) |
| Migration 500 comptes | < 20 s | ~35 s | `BenchmarkMigration` |
| Recherche explorateur 10k objets | < 200 ms | ~2 s | `BenchmarkExplorerSearch` (cache T2) |
| RAM au repos | 30-50 MB | 80-150 MB | `BenchmarkMemory` |
| Taille binaire CLI | 8-12 MB | 80-200 MB | `task build:all` |
| Taille binaire Desktop | < 15 MB | 80-200 MB | `task build:desktop` |

---

## Tests d'intégration AD requis (Priorité 3)

| Test | Description | Données test |
|------|-------------|--------------|
| Connexion LDAPS/StartTLS/SASL | Pool, health check, fallback, auth methods | 2 domaines Samba (EDUSYNC.LOCAL, ANNEXE.LOCAL) |
| CRUD utilisateurs complets | Create, Read, Update, Delete, Enable/Disable, Move, Unlock, Groups | 50 comptes pré-existants + nouveaux |
| Migration restructuration | Plan CSV→plan, Simulate (dry-run), Execute (move+groupes), rollback | CSV 5 classes × 25 élèves |
| Gestion départs | Disable, Archive, Purge (idempotent), pending_deletions | 10 départs variés |
| Reset MDP | OU/groupe/CSV, politiques élèves/personnels, force change, coffre | 20 reset |
| Explorateur | Search paginée, filtres, BuildTree, cache hit/miss, 10k objets | Injection 5000 users + 100 groupes |
| Journal | Record, Filtres combinés, Export CSV, Append-only | 1000 entrées |
| Multisite | 2 containers, switch site, opérations croisées, journal filtré | EDUSYNC.LOCAL + ANNEXE.LOCAL |
| RBAC | Helpdesk hors portée → 403, Admin site plein → 200, Audit operator/role | 5 users test, rôles variés |
| Portail | Code mail PBKDF2, usage unique, anti-énumération, rate limit, audit | 5 demandes reset, 3 lookups |
| API REST | OpenAPI endpoints, webhooks HMAC, retry, SIS sync | Mock SIS endpoint |
| Backup/Restore | ZIP chiffré → restore machine vierge → config/secret/journal intacts | Config Python + Go |
| Migration Python→Go | `edusync-cli migrate --from-python` : lit Python, valide, écrit Go, backup `.bak` | Fixtures Python réelles |

---

## Couverture cible Go (Priorité 3)

| Type | Cible | Outils |
|------|-------|--------|
| Unitaires | > 90% sur `internal/...` | `go test -cover ./internal/...` |
| Intégration AD | 100% scénarios ci-dessus | `AD_TEST=1 go test ./tests/integration/...` |
| E2E Playwright | 100% modules M1-M32 | `npx playwright test tests/e2e/` |
| Race detector | 0 data races | `go test -race ./...` |
| Static analysis | 0 issues | `golangci-lint run`, `staticcheck ./...` |
| Fuzzing | Tous handlers HTTP | `go-fuzz` sur formulaires |

---

## Checklist parité tests Python → Go

- [ ] Chaque test Python dans `tests/core/` a un équivalent Go
- [ ] **Tous** les gaps ci-dessus ont des tests Go **qui échoueraient sans implémentation**
- [ ] Tests d'interopérabilité : Python produit → Go lit, Go produit → Python lit (config, secret, journal)
- [ ] Benchmarks publiés : Go vs Python (démarrage, import, migration, RAM, taille binaire)
- [ ] Race detector vert sur toute la codebase
- [ ] Static analysis propre
- [ ] Fuzzing sur entrées formulaires (validation, injection, XSS)

---

## Références

- `tests/core/` — Suite tests Python (~757 tests)
- `ROADMAP_GO_MIGRATION.md` §7 — Stratégie tests IA-ready
- `docs/migration/05-tests.md` — Campagne tests Priorité 3 détaillée