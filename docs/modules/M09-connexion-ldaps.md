# Module M9 — Connexion LDAPS / LDAP

> **Objectif** : LDAPS prioritaire (port 636) → repli LDAP (389) avec avertissement → CA custom → mémorisation chiffrée (domaine + utilisateur)

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Connexion LDAPS | `src/edusync_ad/core/connexion.py` | Port 636, validation certificat, CA custom (fichier `.pem`/`.crt`) |
| Repli LDAP | `src/edusync_ad/core/connexion.py` | Port 389 + StartTLS, avertissement explicite « connexion non chiffrée » |
| CA custom | `src/edusync_ad/plugins/connexion/` | Sélecteur fichier, affichage infos cert (OpenSSL) |
| Mémorisation | `src/edusync_ad/core/config.py` | Domaine + utilisateur chiffrés AES-256 (`config.json`), MDP **jamais** stocké |
| Test connexion | `src/edusync_ad/core/connexion.py` | DNS → TCP → TLS → Bind, feedback temps réel |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/ad/
├── pool.go                # EXISTE : Pool connexions, TLS, SASL/GSSAPI, health check
├── connection.go          # EXISTE : Connection, AuthMethod (Simple/NTLM/SASL)

internal/config/
├── config.go              # EXISTE : AppConfig, Load/Save (domaine/user chiffrés)
```

### UI (`internal/ui/`)
```
internal/ui/
├── login.go               # MODIFIER : Handlers login, testConnection, saveConfig
├── server.go              # MODIFIER : Routes /login, /connect, /logout
└── i18n.go                # MODIFIER : Clés fr/en connexion
```

### Templates (`web/templates/`)
```
web/templates/
├── login.html               # EXISTE (fonctionnel)
└── partials/
    ├── login_form.html            # EXISTE
    ├── login_ca_selector.html     # NOUVEAU (input file + aperçu cert)
    └── login_ldap_warning.html    # NOUVEAU (toast avertissement repli LDAP)
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── connect.go               # EXISTE : connect (DNS+TCP+TLS test)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Pool AD (TLS, SASL/GSSAPI) | ✅ Livré (`internal/ad/pool.go`) |
| Page login | ✅ Fonctionnelle + i18n |
| Connexion LDAPS | ✅ Pool gère TLS |
| **BindPassword depuis config** | ⬜ **TODO** (`server.go:438` en dur vide) |
| Avertissement repli LDAP | ⬜ Absent |
| CA custom (UI sélecteur) | ⬜ Partiel (backend OK, UI TODO) |
| Erreurs connexion UI | ⚠️ Basiques (à améliorer) |
| Mémorisation config | ✅ Config chiffrée existante |
| CLI `connect` | ✅ Existant |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/ad/pool_test.go` (BindPassword) + `internal/ui/login_test.go`
2. **BindPassword** : `server.go:438` → lire `config.ADConfig.BindPassword` (déchiffré via `crypto.Decrypt`)
3. **Avertissement repli** : Si LDAPS échoue → tentative LDAP 389 + StartTLS → toast HTMX `⚠️ Connexion non chiffrée (LDAP 389)`
4. **CA custom UI** : `login_ca_selector.html` — `<input type="file" accept=".pem,.crt">` + aperçu infos cert
5. **Erreurs connexion** : Mapper erreurs LDAP → messages i18n (invalid credentials, server down, cert invalid, bind failed)
6. **Test connexion** : Bouton « Test » → `POST /connect/test` → feedback temps réel (DNS, TCP, TLS, Bind)
7. **i18n** : Clés `login.ldaps_failed`, `login.ldap_fallback_warning`, `login.ca_selector`, `login.test.dns`, `login.test.tcp`, `login.test.tls`, `login.test.bind` (fr+en)
8. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Connexion LDAPS avec config mémorisée → `BindPassword` lu depuis config chiffré
- [ ] Certificat invalide → erreur claire + option « Continuer quand même » (InsecureSkipVerify)
- [ ] CA custom sélectionné → connexion LDAPS valide avec CA fourni
- [ ] LDAPS indisponible → repli LDAP 389 + StartTLS → avertissement visible persistant
- [ ] Erreur bind : message clair (identifiants invalides, compte verrouillé, serveur inaccessible)
- [ ] Test connexion : étapes DNS/TCP/TLS/Bind affichées avec temps
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/ad/... ./internal/ui/... -run Login`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/ad/... -run Pool -v
go test ./internal/ui/... -run Login -v

# Test manuel
task ad:start
task dev
# UI : Page login → Remplir domaines → Test connexion → Vérifier étapes
# UI : LDAPS OK → Mémoriser → Recharger → Connexion auto
# UI : Certificat invalide → Option ignorer
# UI : LDAPS down → Repli LDAP → Avertissement
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/connexion.py` : Logique LDAPS/LDAP/StartTLS/CA
- `src/edusync_ad/plugins/connexion/` : Plugin UI PyQt6
- `tests/core/test_connexion.py` : Tests Python