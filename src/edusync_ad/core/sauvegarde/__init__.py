"""M32 — Sauvegarde / Restauration / Migration appli.

Ce module fournit les fonctionnalités de sauvegarde, de restauration et de
migration des données :

- **Backup complet** (configuration, journal d'actions SQLite, coffre MDP,
  modèles/étiquettes, certificats, rapport RGPD) → archive ZIP chiffrée
  AES-256-GCM (mot de passe → portable ; sinon clé locale de la machine) ;
- **Restore 1-clic** sur une nouvelle machine : déchiffrement, vérification
  d'intégrité (empreintes SHA-256), extraction vers la destination demandée,
  migration des données héritées, rapports d'erreurs précis ;
- **Migration vN → vN+1** : registre de migrations (``data/migrations.json``),
  table ordonnée ``MIGRATIONS``, application séquentielle idempotente,
  refus des versions inconnues et des rétrogradations.

Architecture : stdlib uniquement (``zipfile``, ``sqlite3``, ``hashlib``,
``json``…) + ``edusync_ad.core.crypto`` pour le chiffrement. Voir
``format_archive.py`` pour le format de l'archive.

Évolution de l'API (implémentation de la coquille initiale)
-----------------------------------------------------------

Les quatre points d'entrée historiques sont inchangés ; leurs paramètres et
leurs retours ont été précisés (rien d'autre dans le dépôt ne les appelait) :

- ``generer_backup_complet(config, *, ...)`` — écrit **réellement** l'archive
  (la version de coquille renvoyait des tailles estimées sans rien écrire).
  Nouveaux arguments : ``repertoire_config`` / ``repertoire_donnees`` /
  ``repertoire_sortie`` (défauts = dossiers natifs), ``include_certificats``
  et ``include_rgpd``, ``chiffrer`` (défaut ``True``), ``key_path``,
  ``rapport_rgpd`` / ``portal_service``. Retour : ``taille_estimee_octets``
  est remplacé par ``taille_octets`` **réelle** (+ ``nb_fichiers``,
  ``empreinte_zip``, ``avertissements``, ``version_donnees``).
- ``verifier_integrite_backup(chemin_archive, *, mot_de_passe=None,
  key_path=None)`` — vérification réelle (manifeste + empreintes SHA-256) au
  lieu d'une liste devinée ; le mot de passe ou la clé locale sont requis
  pour déchiffrer et vérifier au-delà de l'enveloppe (``verification``:
  ``complète`` | ``enveloppe`` | ``aucune``).
- ``restorer_dans_nouvelle_machine(chemin_archive, mot_de_passe=None,
  chemin_destination=None, *, key_path=None, appliquer_migrations=True)`` —
  restauration réelle (la coquille se contentait d'ouvrir le ZIP). La clé
  d'accueil ``elements_restores`` remplace ``elements_restaurés``
  (accents retirés pour rester ASCII-clefs, comme le reste du module) ;
  ``reussite`` reste la clé de succès.
- ``migrer_donnees_vn_vn1(repertoire_config=None, repertoire_donnees=None,
  *, chemin_registre=None, version_cible=None, mode_test=False)`` — les trois
  paramètres objets de la coquille (``config_actuel``, ``config_cible``,
  ``portal_service_actuel``), jamais utilisés, sont remplacés par des chemins
  explicites ; le rapport conserve ``reussite`` / ``elements_migres`` /
  ``elements_nouveaux`` / ``erreurs`` et ajoute ``version_depart`` /
  ``version_arrivee`` / ``migrations_appliquees`` / ``rapport``.

Les erreurs de bas niveau sont levées sous forme d'``ArchiveError`` et de
``MigrationError`` (messages français) ; ``restorer_dans_nouvelle_machine``
et ``verifier_integrite_backup`` consignent leurs refus dans leur résultat
(``erreurs`` / ``erreur``) au lieu de lever, tandis que
``generer_backup_complet`` peut lever ``ArchiveError`` si l'écriture de
l'archive échoue.
"""

from __future__ import annotations

from edusync_ad.core.sauvegarde.backup import generer_backup_complet, verifier_integrite_backup
from edusync_ad.core.sauvegarde.format_archive import ArchiveError
from edusync_ad.core.sauvegarde.migration import (
    MIGRATIONS,
    MigrationError,
    VERSION_DONNEES_COURANTE,
    detecter_version_donnees,
    migrer_donnees_vn_vn1,
)
from edusync_ad.core.sauvegarde.restore import restorer_dans_nouvelle_machine

__all__ = [
    "ArchiveError",
    "MigrationError",
    "MIGRATIONS",
    "VERSION_DONNEES_COURANTE",
    "detecter_version_donnees",
    "generer_backup_complet",
    "migrer_donnees_vn_vn1",
    "restorer_dans_nouvelle_machine",
    "verifier_integrite_backup",
]
