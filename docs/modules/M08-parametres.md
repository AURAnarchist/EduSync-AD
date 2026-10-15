# Module M8 — Paramètres globaux

> **Objectif** : 4 onglets — Comptes, Mots de passe, Apparence, Délégation (M26) → persistance config.json chiffré

---

## Fonctionnalités Python (référence v1.17.1)

| Onglet | Fonctionnalités | Fichier Python |
|--------|----------------|----------------|
| **Comptes** | Format identifiant, domaine mail, groupes par défaut, comportement départs (OU archive, délai purge) | `src/edusync_ad/plugins/parametres/comptes.py` |
| **Mots de passe** | Politiques élèves/personnels (longueur, complexité, historique, âge), coffre MDP, génération auto | `src/edusync_ad/plugins/parametres/mdp.py` |
| **Apparence** | Thème (clair/sombre/auto), langue (fr/en), densité UI (compact/normal), animations | `src/edusync_ad/plugins/parametres/apparence.py` |
| **Délégation** | RBAC (M26) : matrice opérateurs × rôles × portées, garde-fou écriture AD | `src/edusync_ad/plugins/parametres/delegation.py` |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/config/
├── settings.go            # NOUVEAU : Structs Settings{Accounts, Passwords, Appearance, Delegation}
├── config.go              # MODIFIER : Load/Save settings (merge avec config existante)

internal/models/
├── settings.go            # NOUVEAU : AccountSettings, PasswordPolicy, AppearanceSettings, DelegationSettings
```

### UI (`internal/ui/`)
```
internal/ui/
├── settings.go            # NOUVEAU : Handlers settingsGet, settingsPost (validation + save)
├── server.go              # MODIFIER : Routes /settings/*
└── i18n.go                # MODIFIER : Clés fr/en paramètres
```

### Templates (`web/templates/`)
```
web/templates/
├── settings.html                # NOUVEAU : Page 4 onglets (Alpine tabs)
├── settings_accounts.html       # NOUVEAU : Onglet Comptes (partial HTMX)
├── settings_passwords.html      # NOUVEAU : Onglet Mots de passe (partial)
├── settings_appearance.html     # NOUVEAU : Onglet Apparence (partial)
├── settings_delegation.html     # NOUVEAU : Onglet Délégation (placeholder M26)
└── partials/
    ├── settings_tab_nav.html          # NOUVEAU (navigation onglets)
    ├── settings_password_policy.html  # NOUVEAU (éditeur politique)
    └── settings_theme_selector.html   # NOUVEAU
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── settings.go              # NOUVEAU : show, set (JSON)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Thème/Langue (header) | ✅ Partiel (cookies + config.json) |
| Page `settings.html` | ⬜ **Absente** |
| Modèles settings | ⬜ Absents |
| Handlers GET/POST | ⬜ Absents |
| 4 onglets | ⬜ Absents |
| Persistance config.json | ⚠️ Partiel (theme/lang seulement) |
| CLI | ⬜ Absente |
| i18n | ⬜ Absent |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/config/settings_test.go` + `internal/ui/settings_test.go`
2. **Modèles** : `internal/models/settings.go` — structs typés pour chaque onglet
3. **Config** : `internal/config/settings.go` — `LoadSettings()`, `SaveSettings()` (écriture atomique, merge)
4. **Handler GET** : `settingsGet` → charge settings + rend `settings.html` avec onglet actif
5. **Handler POST** : `settingsPost` → validation → `SaveSettings()` → toast succès → rechargement
6. **Template principal** : `settings.html` — Alpine tabs (x-data="tabs"), partials par onglet
7. **Onglet Comptes** : `settings_accounts.html` — format identifiant (select), domaine mail, groupes défaut (multi-select), OU archive, délai purge
8. **Onglet MDP** : `settings_passwords.html` — 2 politiques (élèves/personnels) : longueur, complexité, historique, expiry, coffre activé
9. **Onglet Apparence** : `settings_appearance.html` — thème (radio), langue (select), densité, animations
10. **Onglet Délégation** : `settings_delegation.html` — **Placeholder** : « Disponible dans M26 (RBAC) » + lien doc
11. **Routes** : `GET /settings`, `POST /settings`, `GET /settings/tab/:name` (HTMX)
12. **i18n** : Clés `settings.title`, `settings.tab.accounts`, `settings.tab.passwords`, `settings.tab.appearance`, `settings.tab.delegation`, `settings.accounts.id_format`, `settings.passwords.policy.eleves`, `settings.appearance.theme` (fr+en)
13. **CLI** : `edusync-cli settings show --json`, `settings set --accounts.id_format "prenom.nom" --appearance.theme dark`
14. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Chaque onglet : formulaire valide → sauvegarde → rechargement → valeurs persistées
- [ ] Config.json chiffré mis à jour (test : ouvrir avec Python 1.17.1 → valeurs lues)
- [ ] Thème appliqué immédiatement (CSS variable swap via HTMX `hx-swap-oob`)
- [ ] Langue changée → rechargement page → i18n mis à jour
- [ ] Politiques MDP : validation (longueur min, complexité regex, historique > 0)
- [ ] Onglet Délégation : message clair « M26 RBAC » + lien vers doc
- [ ] CLI : show/set JSON
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/config/... ./internal/ui/... -run Settings`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/config/... -run Settings -v
go test ./internal/ui/... -run Settings -v

# Test manuel
task ad:start
task dev
# UI : Paramètres → Chaque onglet → Modifier → Sauvegarder → Vérifier config.json + rechargement
# Python : Lire config.json avec 1.17.1 → valeurs cohérentes
```

---

## Fichiers Python de référence

- `src/edusync_ad/plugins/parametres/` : 4 plugins (comptes, mdp, apparence, delegation)
- `src/edusync_ad/core/config.py` : AppConfig, sauvegarde atomique
- `tests/core/test_parametres.py` : Tests Python