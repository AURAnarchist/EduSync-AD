"""M32 — Restauration d'une sauvegarde (« 1-clic » sur une nouvelle machine).

Le parcours de restauration, dans l'ordre :

1. **lecture** de l'archive (déchiffrement AES-256-GCM si enveloppe) —
   mot de passe, clé locale ou format/version inconnus = refus précis ;
2. **intégrité** : manifeste, empreinte SHA-256 de chaque fichier, empreinte
   globale, chemins hostiles (``..``, absolu) — le moindre écart arrête la
   restauration *avant* toute écriture ;
3. **compatibilité** : une archive ou un registre de la destination portant
   une version de schéma supérieure à celle supportée est refusée (une
   application plus ancienne ne doit pas écraser des données plus récentes) ;
4. **extraction** vers les racines demandées : ``<destination>/config`` et
   ``<destination>/data`` ; sans destination, les répertoires natifs de
   l'application (restauration sur la machine courante) ;
5. **migration** des données héritées vers la version de schéma courante
   (idempotente : restaurer deux fois n'altère rien).

Chaque erreur est reportée fichier par fichier dans ``erreurs`` ;
``elements_restores`` liste ce qui a effectivement été écrit.
"""

from __future__ import annotations

import io
import json
import os
import stat
import zipfile
import zlib
from pathlib import Path
from typing import Any

from edusync_ad.core.audit import data_dir
from edusync_ad.core.config import config_dir
from edusync_ad.core.sauvegarde import format_archive as fmt
from edusync_ad.core.sauvegarde.backup import NOM_CLE
from edusync_ad.core.sauvegarde.migration import (
    NOM_COFFRE,
    NOM_CONFIG,
    VERSION_DONNEES_COURANTE,
    MigrationError,
    lire_registre,
    migrer_donnees_vn_vn1,
    registre_path,
)


def _racines(chemin_destination: str | None) -> tuple[Path, Path, list[str]]:
    """Racines de restauration + avertissements associés."""
    if chemin_destination:
        racine = Path(chemin_destination)
        return racine / "config", racine / "data", []
    return (
        config_dir(),
        data_dir(),
        [
            "Destination non précisée : restauration dans les répertoires natifs "
            "de l'application (configuration et données actuelles écrasées)."
        ],
    )


def _verifier_versions(
    manifeste: dict[str, Any], racine_donnees: Path, erreurs: list[str]
) -> bool:
    """Refuse une archive ou une destination d'une version de schéma
    supérieure à celle supportée (avant toute écriture)."""
    version_archive = manifeste.get("version_donnees")
    if isinstance(version_archive, int) and version_archive > VERSION_DONNEES_COURANTE:
        erreurs.append(
            f"Archive créée avec la version de données {version_archive} "
            f"(maximum supporté : {VERSION_DONNEES_COURANTE}) — application à mettre à jour."
        )
        return False
    registre = registre_path(racine_donnees)
    if registre.is_file():
        try:
            version_dest = lire_registre(registre)["version"]
        except MigrationError as exc:
            erreurs.append(
                f"Destination non restaurable — {exc}. Réparer ou supprimer ce fichier "
                "puis relancer la restauration."
            )
            return False
        if version_dest > VERSION_DONNEES_COURANTE:
            erreurs.append(
                f"Destination portant la version de données {version_dest} "
                f"(maximum supporté : {VERSION_DONNEES_COURANTE}) — restauration refusée "
                "pour ne pas écraser des données plus récentes."
            )
            return False
    return True


def restorer_dans_nouvelle_machine(
    chemin_archive: str,
    mot_de_passe: str | None = None,
    chemin_destination: str | None = None,
    *,
    key_path: Path | None = None,
    appliquer_migrations: bool = True,
) -> dict[str, Any]:
    """Restaure une sauvegarde EduSync AD.

    Args:
        chemin_archive: Fichier de sauvegarde (ZIP clair ou enveloppe
            ``.zip.aes``).
        mot_de_passe: Mot de passe de l'archive portable (optionnel pour un
            ZIP clair ; une archive ``cle-locale`` utilise la clé de la
            machine, indiquée par ``key_path``).
        chemin_destination: Dossier recevant ``config/`` et ``data/``.
            Absent → répertoires natifs de l'application.
        key_path: Emplacement de la clé locale (défaut :
            ``config_dir()/secret.key``).
        appliquer_migrations: Lance la migration vN → vN+1 des données
            restaurées (par défaut, désactivable pour un diagnostic).

    Returns:
        Dict : ``reussite``, ``elements_restores``, ``erreurs``,
        ``avertissements``, ``destination``, ``integrite``, ``nb_fichiers``,
        ``version_donnees``, ``migrations``. Tout refus attendu (fichier
        absent, mot de passe faux, archive altérée ou construite pour
        écrire hors destination, version inconnue) est consigné dans
        ``erreurs`` sans lever d'exception.
    """
    resultat: dict[str, Any] = {
        "reussite": False,
        "elements_restores": [],
        "erreurs": [],
        "avertissements": [],
        "destination": str(chemin_destination) if chemin_destination else "natif",
        "integrite": {"valide": False, "verification": "aucune", "details": []},
        "nb_fichiers": 0,
        "version_donnees": None,
        "migrations": None,
    }
    erreurs: list[str] = resultat["erreurs"]
    avertissements: list[str] = resultat["avertissements"]

    archive = Path(chemin_archive)
    if not archive.is_file():
        erreurs.append(f"Archive introuvable : {chemin_archive}")
        return resultat

    # 1. Lecture / déchiffrement (refus d'un format ou d'une version inconnus).
    try:
        octets_zip = fmt.ouvrir_archive(archive, mot_de_passe=mot_de_passe, key_path=key_path)
    except fmt.ArchiveError as exc:
        erreurs.append(str(exc))
        resultat["integrite"] = {"valide": False, "verification": "aucune", "details": [str(exc)]}
        return resultat

    # 2. Intégrité complète : rien n'est écrit tant que tout n'est pas conforme.
    #    (Le ZIP est déjà déchiffré et vérifié par l'enveloppe ci-dessus — le
    #    manifeste et chaque empreinte sont donc contrôlés ici directement.)
    details = fmt.verifier_manifeste(octets_zip)
    if details:
        erreurs.append(
            "Archive refusée — intégrité non vérifiée : " + " ; ".join(details)
        )
        resultat["integrite"] = {
            "valide": False,
            "verification": "complète",
            "details": details,
        }
        return resultat
    resultat["integrite"] = {"valide": True, "verification": "complète", "details": []}

    try:
        manifeste = fmt.lire_manifeste(octets_zip)
    except fmt.ArchiveError as exc:
        erreurs.append(f"Manifeste illisible : {exc}")
        return resultat

    racine_config, racine_donnees, avis_racines = _racines(chemin_destination)
    avertissements.extend(avis_racines)

    # 3. Compatibilité de version avant toute écriture.
    if not _verifier_versions(manifeste, racine_donnees, erreurs):
        return resultat

    # 4. Extraction.
    entrees = [
        entree
        for liste in (manifeste.get("sections") or {}).values()
        if isinstance(liste, list)
        for entree in liste
        if isinstance(entree, dict)
    ]
    racines = {"config": racine_config, "data": racine_donnees}
    try:
        zipfile_context = zipfile.ZipFile(io.BytesIO(octets_zip))
    except zipfile.BadZipFile as exc:
        erreurs.append(f"Archive ZIP illisible : {exc}")
        return resultat

    with zipfile_context as zf:
        for entree in entrees:
            base = racines.get(entree.get("racine"))  # validé par verifier_manifeste
            if base is None:
                erreurs.append(
                    f"Racine de restauration inconnue : {entree.get('racine')!r}"
                )
                continue
            cible = base / entree["relatif"]
            # Double garde anti « zip-slip », au-delà du contrôle du manifeste.
            if not cible.resolve().is_relative_to(base.resolve()):
                erreurs.append(f"Chemin de sortie hors destination refusé : {entree['relatif']}")
                continue
            try:
                donnees = zf.read(entree["chemin"])
                cible.parent.mkdir(parents=True, exist_ok=True)
                cible.write_bytes(donnees)
                if entree["relatif"] == NOM_CLE:
                    os.chmod(cible, stat.S_IRUSR | stat.S_IWUSR)
            except (OSError, KeyError, zipfile.BadZipFile, RuntimeError, zlib.error) as exc:
                erreurs.append(f"Restauration impossible de {entree['relatif']} : {exc}")
                continue
            resultat["elements_restores"].append(f"{entree['racine']}/{entree['relatif']}")
            resultat["nb_fichiers"] += 1

    # 5. Avertissements précis (coffre sans clé, certificat CA à repositionner).
    _alerter(racine_config, entrees, avertissements)

    # 6. Migration des données héritées vers la version courante.
    if appliquer_migrations and not erreurs:
        try:
            migrations = migrer_donnees_vn_vn1(racine_config, racine_donnees)
        except Exception as exc:  # défaut inattendu : signalé, jamais perdu
            migrations = None
            erreurs.append(f"Migration des données impossible : {exc}")
        else:
            resultat["migrations"] = migrations
            if not migrations["reussite"]:
                erreurs.append(
                    "Migration des données échouée : " + " ; ".join(migrations["erreurs"])
                )
    resultat["version_donnees"] = manifeste.get("version_donnees")
    resultat["reussite"] = not erreurs
    return resultat


def _alerter(racine_config: Path, entrees: list[dict[str, Any]], avertissements: list[str]) -> None:
    """Pointeurs d'attention post-restauration (lisibilité du coffre, CA)."""
    cles = {str(entree.get("relatif")) for entree in entrees}
    coffre_restaur = any(relatif.endswith(NOM_COFFRE) for relatif in cles)
    if coffre_restaur and not any(relatif.endswith(NOM_CLE) for relatif in cles):
        avertissements.append(
            "Coffre MDP restauré sans sa clé de chiffrement : les mots de passe "
            "ne seront pas lisibles depuis cette machine."
        )
    config = racine_config / NOM_CONFIG
    if config.is_file():
        try:
            donnees = json.loads(config.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return
        chemin_ca = donnees.get("ldaps_chemin_certificat_ca") if isinstance(donnees, dict) else ""
        if chemin_ca and not Path(chemin_ca).is_file():
            avertissements.append(
                f"Certificat CA référencé introuvable ({chemin_ca}) : repositionner le "
                "fichier restauré et mettre à jour ldaps_chemin_certificat_ca."
            )
