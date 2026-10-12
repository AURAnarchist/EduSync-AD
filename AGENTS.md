# AGENTS.md — Instructions pour les agents

Ce document décrit **comment travailler** dans ce dépôt. Le **quoi** (périmètre,
priorités, statuts) est dans `ROADMAP_GO_MIGRATION.md` et
`docs/ROADMAP_python_v1.md`. Le contexte détaillé (commandes, patterns, règles
d'or) est dans `CLAUDE.md`.

## Boucle de travail

```
spec → test d'abord → implémentation → gofmt/vet/test → lint CI → revue → commit
```

1. **Comprendre** : lire `CLAUDE.md`, la roadmap, puis le code existant du
   périmètre concerné (au moins les paquets `internal/*` voisins).
2. **Tester d'abord** : écrire le ou les tests qui échouent aujourd'hui.
3. **Implémenter** jusqu'à ce qu'ils passent — sans toucher à autre chose.
4. **Vérifier** avant de déclarer :
   ```bash
   task lint && task test && task test:python
   ```
5. **Rapporter** : fichiers créés/modifiés, décisions, commandes exécutées et
   résultats chiffrés, limites connues.

## Définition de « terminé »

Un chantier est terminé uniquement si **tous** ces points sont vrais :

- [ ] `gofmt -l cmd internal web` ne retourne rien ;
- [ ] `go vet ./...` passe ;
- [ ] `go test ./...` est vert ;
- [ ] `.venv/bin/pytest -q` est vert (si du Python a été touché) ;
- [ ] les nouveaux comportements ont des tests **qui échoueraient sans eux** ;
- [ ] les clés i18n ajoutées existent en français **et** en anglais ;
- [ ] les fichiers de configuration produits sont relus par la version
      Python (`config.json`, `secret.key`, `journal.db`) et inversement ;
- [ ] la roadmap reflète le nouveau statut ;
- [ ] aucune donnée sensible n'est écrite en clair, aucun secret commité.

## Interdits

- Supprimer ou modifier une entrée du journal d'actions (append-only).
- Casser le format `config.json` / `secret.key` / `journal.db` partagé avec
  Python sans script de migration **et** test aller-retour.
- Ajouter une dépendance sans justification écrite dans le commit.
- Désactiver un test pour faire passer la CI.
- Écrire du code non couvert « pour aller plus vite » — la boucle IA en
  dépend (les tests sont son oracle).
- Réécrire l'UI en framework JavaScript : HTMX + Alpine, HTML rendu côté serveur.

## Conventions de code

**Go** : commentaires en français, doc sur tout symbole exporté, erreurs
wrappées (`fmt.Errorf("paquet: contexte: %w", err)`, sentinelles `Err…`),
pas de `panic` hors initialisation, chemins de test dans `t.TempDir()`,
`context.Context` en premier paramètre dès qu'une annulation est possible.

**Python** : `from __future__ import annotations`, typage complet, docstrings
françaises, stdlib d'abord, conventions identiques aux modules voisins.

**Commits** : impératif en anglais ou français cohérent avec l'historique,
format `type: message` (`feat:`, `fix:`, `test:`, `docs:`, `chore:`),
un chantier = un commit. **Ne jamais commité** sans avoir lancé la
vérification du « Définition de terminé ».

## Revue

Avant merge, vérifier spécifiquement :

1. les frontières de sécurité (session, redirections, fichiers de clé,
   validation des entrées de formulaire) ;
2. la parité Python ↔ Go (JSON, crypto, schéma SQLite, chemins) ;
3. le comportement en échec (réseau coupé, fichier absent, corrompu) ;
4. la lisibilité pour un relecteur humain **et** pour une IA (code explicite,
   variables nommées, pas d'astuce) ;
5. que la roadmap et `CLAUDE.md` reflètent encore la réalité du code.
