"""M32 — Génération de sauvegarde complète et vérification d'intégrité.

Une sauvegarde regroupe, dans une archive unique (ZIP clair ou enveloppe
chiffrée — voir ``format_archive.py``) :

- ``configuration`` — tous les ``*.json`` du dossier de configuration de
  l'application, plus l'instantané de la configuration en mémoire
  (``appconfig.json``) ;
- ``modeles`` — planches d'étiquettes et modèles de groupes ;
- ``certificats`` — certificats CA LDAPS présents sur le poste et celui
  référencé par ``ldaps_chemin_certificat_ca`` ;
- ``journal`` — journal d'actions SQLite, copié par l'API ``backup`` de
  SQLite (instantané cohérent même si la base est en cours d'écriture) ;
- ``coffre`` — coffre-fort des mots de passe (idem) et, **seulement si
  l'archive est chiffrée**, la clé locale ``secret.key`` nécessaire pour
  relire le coffre ailleurs ;
- ``rgpd`` — rapport RGPD fourni, généré ou retrouvé sur le disque ;
- ``meta`` — registre de migrations (version de schéma des données).

Sécurité :

- l'archive est **chiffrée par défaut** (clé locale) et avec un mot de passe
  si ``password`` est fourni → archive portable « 1-clic » sur une nouvelle
  machine ;
- ``chiffrer=False`` produit un ZIP clair **sans ``secret.key``** et renvoie
  une mise en garde explicite : configuration, clés API et journal y sont
  lisibles par qui peut lire le fichier ;
- chaque fichier est horodaté d'une empreinte SHA-256 dans le manifeste,
  l'empreinte globale du manifeste étant elle-même stockée dans l'enveloppe
  chiffrée : toute altération se détecte avant toute restauration.
"""

from __future__ import annotations

import json
import shutil
import sqlite3
import tempfile
import zipfile
from contextlib import closing
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any

from edusync_ad import __version__
from edusync_ad.core.audit import data_dir
from edusync_ad.core.config import AppConfig, config_dir
from edusync_ad.core.sauvegarde import format_archive as fmt
from edusync_ad.core.sauvegarde.migration import (
    NOM_COFFRE,
    NOM_JOURNAL,
    REGISTRE_NOM,
    lire_registre,
)

#: Ordre d'affichage des sections de l'archive.
SECTIONS = (
    "configuration",
    "journal",
    "coffre",
    "modeles",
    "certificats",
    "rgpd",
    "meta",
)

#: Fichiers de modèles traités par la section ``modeles`` (exclus de la
#: section ``configuration`` pour ne pas les dupliquer).
FICHIERS_MODELES = ("label_templates.json", "group_templates.json")
#: Instantané de la configuration en mémoire, toujours à jour.
NOM_APPCONFIG = "appconfig.json"
#: Extensions reconnues comme certificats.
EXTENSIONS_CERTIFICATS = (".pem", ".crt", ".cer")
#: Clé locale de chiffrement (``core/crypto.py``) — jamais en clair dans un
#: ZIP non chiffré.
NOM_CLE = "secret.key"
#: Rapport RGPD recherché sur le disque si aucun n'est fourni.
CHEMIN_RAPPORT_RGPD = "rapports/rapport_rgpd.json"

MISE_EN_GARDE_NON_CHIFFRE = (
    "Archive non chiffrée : configuration, clés API et journal accessibles en clair "
    "— ne la copiez ni sur support partagé ni hors poste de confiance. "
    "La clé du coffre (secret.key) est volontairement exclue."
)


@dataclass
class _Element:
    """Fichier à inclure dans l'archive."""

    section: str
    racine: str  # "config" | "donnees"
    relatif: str  # chemin relatif à la racine de restauration
    source: Path | None = None  # fichier à copier…
    contenu: bytes | None = None  # …ou contenu déjà en mémoire
    snapshot_sqlite: bool = False  # copie via l'API SQLite si besoin
    membre: str = field(init=False, default="")

    def __post_init__(self) -> None:
        self.membre = f"{fmt.PREFIXE_MEMBRE}/{self.section}/{self.relatif}"


# -- Collecte ----------------------------------------------------------------


def _snapshot_sqlite(source: Path, cible: Path) -> None:
    """Copie cohérente d'une base SQLite (API ``backup``), même ouverte."""
    with (
        closing(sqlite3.connect(source)) as connexion_source,
        closing(sqlite3.connect(cible)) as connexion_cible,
    ):
        connexion_source.backup(connexion_cible)
        connexion_cible.commit()


def _copier(source: Path, cible: Path) -> None:
    """Copie un fichier, en instantanotant les bases SQLite."""
    cible.parent.mkdir(parents=True, exist_ok=True)
    try:
        _snapshot_sqlite(source, cible)
    except sqlite3.Error:
        # Pas une base SQLite valide : copie bête plutôt que perte de données.
        shutil.copy2(source, cible)


def _ajouter(
    elements: list[_Element],
    vus: set[Path],
    *,
    section: str,
    racine: str,
    relatif: str,
    source: Path | None = None,
    contenu: bytes | None = None,
    snapshot_sqlite: bool = False,
) -> None:
    """Ajoute un élément en évitant les doublons (source déjà vue, même nom)."""
    if source is not None:
        clef = source.absolute()
        if clef in vus:
            return
        vus.add(clef)
    final = relatif
    indice = 2
    while any(e.section == section and e.relatif == final for e in elements):
        racine_part, _, suffixe = final.rpartition(".")
        base = racine_part if suffixe else final
        final = f"{base}_{indice}.{suffixe}" if suffixe else f"{base}_{indice}"
        indice += 1
    elements.append(
        _Element(
            section=section,
            racine=racine,
            relatif=final,
            source=source,
            contenu=contenu,
            snapshot_sqlite=snapshot_sqlite,
        )
    )


def _collecter(
    config: AppConfig,
    repertoire_config: Path,
    repertoire_donnees: Path,
    *,
    include_config: bool,
    include_journal: bool,
    include_coffre: bool,
    include_modeles: bool,
    include_certificats: bool,
    include_rgpd: bool,
    inclure_cle: bool,
    rapport_rgpd: dict[str, Any] | None,
    portal_service: Any | None,
) -> tuple[list[_Element], list[str]]:
    """Construit la liste des fichiers à archiver + les avertissements."""
    elements: list[_Element] = []
    avertissements: list[str] = []
    vus: set[Path] = set()

    if include_config:
        _ajouter(
            elements,
            vus,
            section="configuration",
            racine="config",
            relatif=NOM_APPCONFIG,
            contenu=json.dumps(config.to_dict(), indent=2, ensure_ascii=False).encode("utf-8"),
        )
        for fichier in sorted(repertoire_config.glob("*.json")):
            if fichier.name in FICHIERS_MODELES or fichier.name == NOM_APPCONFIG:
                continue
            if not fichier.is_file():
                continue
            _ajouter(
                elements,
                vus,
                section="configuration",
                racine="config",
                relatif=fichier.name,
                source=fichier,
            )

    if include_modeles:
        trouves = 0
        for nom in FICHIERS_MODELES:
            fichier = repertoire_config / nom
            if fichier.is_file():
                _ajouter(
                    elements,
                    vus,
                    section="modeles",
                    racine="config",
                    relatif=nom,
                    source=fichier,
                )
                trouves += 1
        if not trouves:
            avertissements.append("Aucun modèle/étiquette trouvé — section « modeles » omise.")

    if include_certificats:
        trouves = 0
        for extension in EXTENSIONS_CERTIFICATS:
            for fichier in sorted(repertoire_config.glob(f"*{extension}")):
                if not fichier.is_file():
                    continue
                _ajouter(
                    elements,
                    vus,
                    section="certificats",
                    racine="config",
                    relatif=f"certificats/{fichier.name}",
                    source=fichier,
                )
                trouves += 1
        chemin_ca = config.ldaps_chemin_certificat_ca
        if chemin_ca:
            ca = Path(chemin_ca)
            if ca.is_file():
                _ajouter(
                    elements,
                    vus,
                    section="certificats",
                    racine="config",
                    relatif=f"certificats/{ca.name}",
                    source=ca,
                )
                trouves += 1
            else:
                avertissements.append(f"Certificat CA introuvable : {chemin_ca}")
        if not trouves:
            avertissements.append("Aucun certificat LDAPS trouvé — section « certificats » omise.")

    if include_journal:
        journal = repertoire_donnees / NOM_JOURNAL
        if journal.is_file():
            _ajouter(
                elements,
                vus,
                section="journal",
                racine="data",
                relatif=NOM_JOURNAL,
                source=journal,
                snapshot_sqlite=True,
            )
        else:
            avertissements.append(f"Journal d'actions introuvable : {journal} — rien à archiver.")

    if include_coffre:
        coffre = repertoire_donnees / NOM_COFFRE
        if coffre.is_file():
            _ajouter(
                elements,
                vus,
                section="coffre",
                racine="data",
                relatif=NOM_COFFRE,
                source=coffre,
                snapshot_sqlite=True,
            )
        else:
            avertissements.append(f"Coffre MDP introuvable : {coffre} — rien à archiver.")
        cle = repertoire_config / NOM_CLE
        if cle.is_file():
            if inclure_cle:
                _ajouter(
                    elements,
                    vus,
                    section="coffre",
                    racine="config",
                    relatif=NOM_CLE,
                    source=cle,
                )
            else:
                avertissements.append(
                    f"{NOM_CLE} exclu de l'archive non chiffrée — le coffre restauré "
                    "ne sera pas lisible sur une autre machine."
                )

    if include_rgpd:
        rapport = _obtenir_rapport_rgpd(
            config, repertoire_config, repertoire_donnees, rapport_rgpd, portal_service,
            avertissements,
        )
        if rapport is not None:
            _ajouter(
                elements,
                vus,
                section="rgpd",
                racine="data",
                relatif=CHEMIN_RAPPORT_RGPD,
                contenu=json.dumps(rapport, indent=2, ensure_ascii=False).encode("utf-8"),
            )

    registre = repertoire_donnees / REGISTRE_NOM
    if registre.is_file():
        _ajouter(
            elements,
            vus,
            section="meta",
            racine="data",
            relatif=REGISTRE_NOM,
            source=registre,
        )

    return elements, avertissements


def _obtenir_rapport_rgpd(
    config: AppConfig,
    repertoire_config: Path,
    repertoire_donnees: Path,
    rapport_rgpd: dict[str, Any] | None,
    portal_service: Any | None,
    avertissements: list[str],
) -> dict[str, Any] | None:
    """Rapport RGPD : fourni > généré via le portail > dernier rapport disque."""
    if rapport_rgpd is not None:
        return rapport_rgpd
    if portal_service is not None:
        # Import paresseux : core/compliance tire core/portal (ldap3) — inutile
        # pour une sauvegarde qui n'a pas de portail à interroger.
        from edusync_ad.core.compliance.rgpd import generer_rapport_rgpd

        return generer_rapport_rgpd(config, portal_service)
    for candidat in (
        repertoire_donnees / CHEMIN_RAPPORT_RGPD,
        repertoire_config / "rapport_rgpd.json",
    ):
        if not candidat.is_file():
            continue
        try:
            charge = json.loads(candidat.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            avertissements.append(f"Rapport RGPD illisible ({candidat}) : {exc}")
            return None
        if isinstance(charge, dict):
            return charge
        avertissements.append(f"Rapport RGPD inattendu (objet JSON requis) : {candidat}")
        return None
    avertissements.append("Aucun rapport RGPD disponible — section « rgpd » omise.")
    return None


def _version_donnees(registre: Path, avertissements: list[str]) -> int | None:
    """Version de schéma des données à inscrire dans le manifeste."""
    try:
        return lire_registre(registre)["version"]
    except Exception as exc:  # registre corrompu : signalé, pas bloquant
        avertissements.append(f"Registre de migrations illisible ({registre}) : {exc}")
        return None


# -- Génération --------------------------------------------------------------


def _chemin_archive_unique(repertoire: Path, chiffre: bool) -> Path:
    suffixe = ".zip.aes" if chiffre else ".zip"
    base = datetime.now().strftime("%Y%m%d_%H%M%S")
    for indice in range(1000):
        nom = base if indice == 0 else f"{base}_{indice}"
        candidat = repertoire / f"edusync_backup_{nom}{suffixe}"
        if not candidat.exists():
            return candidat
    return repertoire / f"edusync_backup_{datetime.now().strftime('%Y%m%d_%H%M%S_%f')}{suffixe}"


def generer_backup_complet(
    config: AppConfig,
    *,
    repertoire_config: Path | None = None,
    repertoire_donnees: Path | None = None,
    repertoire_sortie: Path | None = None,
    include_config: bool = True,
    include_journal: bool = True,
    include_coffre: bool = True,
    include_modeles: bool = True,
    include_certificats: bool = True,
    include_rgpd: bool = True,
    chiffrer: bool = True,
    password: str | None = None,
    key_path: Path | None = None,
    rapport_rgpd: dict[str, Any] | None = None,
    portal_service: Any | None = None,
) -> dict[str, Any]:
    """Génère une sauvegarde complète d'EduSync AD.

    Args:
        config: Configuration en mémoire (instantanée dans l'archive).
        repertoire_config: Dossier de configuration source (défaut :
            ``config_dir()``).
        repertoire_donnees: Dossier de données source (défaut :
            ``data_dir()``).
        repertoire_sortie: Dossier d'écriture de l'archive (défaut :
            ``data_dir()/sauvegardes``).
        include_*: Sections à inclure (les absences sont signalées dans
            ``avertissements`` au lieu de faire échouer la sauvegarde).
        chiffrer: ``False`` → ZIP clair **sans** ``secret.key`` + mise en
            garde. Un ``password`` force toujours le chiffrement.
        password: Mot de passe → archive chiffrée **portable** (clé
            dérivée, ``secret.key`` incluse : restauration possible sur une
            autre machine). Sans mot de passe, l'archive est chiffrée avec la
            clé locale de la machine.
        key_path: Emplacement de la clé locale (défaut :
            ``config_dir()/secret.key``).
        rapport_rgpd: Rapport RGPD déjà construit à inclure.
        portal_service: Service du portail M27 pour générer le rapport RGPD.

    Returns:
        Dict : ``archive_chemin``, ``generation_date``, ``inclus``,
        ``nb_fichiers``, ``taille_octets``, ``empreinte_zip``,
        ``verification`` (chiffrement + empreinte + mise éventuelle en
        garde), ``config`` (options demandées), ``avertissements``.

    Raises:
        ArchiveError: si le répertoire de sortie est inaccessible, si la clé
            locale ne peut être créée/lue, si une section demandée est
            inconnue ou si un fichier collecté est illisible ou disparaît
            pendant la lecture.
    """
    repertoire_config = Path(repertoire_config) if repertoire_config else config_dir()
    repertoire_donnees = Path(repertoire_donnees) if repertoire_donnees else data_dir()
    repertoire_sortie = (
        Path(repertoire_sortie) if repertoire_sortie else data_dir() / "sauvegardes"
    )
    chiffrer_effectif = chiffrer or password is not None
    if chiffrer_effectif and password is None:
        # Clé locale créée AVANT la collecte : elle voyage avec le coffre.
        try:
            fmt.charger_cle_locale(key_path)
        except OSError as exc:
            raise fmt.ArchiveError(
                f"Clé locale inaccessible ({fmt.chemin_cle_locale(key_path)}) : {exc}"
            ) from exc

    elements, avertissements = _collecter(
        config,
        repertoire_config,
        repertoire_donnees,
        include_config=include_config,
        include_journal=include_journal,
        include_coffre=include_coffre,
        include_modeles=include_modeles,
        include_certificats=include_certificats,
        include_rgpd=include_rgpd,
        inclure_cle=chiffrer_effectif,
        rapport_rgpd=rapport_rgpd,
        portal_service=portal_service,
    )
    version_donnees = _version_donnees(repertoire_donnees / REGISTRE_NOM, avertissements)

    with tempfile.TemporaryDirectory(prefix="edusync-backup-") as dossier_temp:
        staging = Path(dossier_temp)
        sections: dict[str, list[dict[str, Any]]] = {}
        for element in elements:
            cible = staging / element.membre
            try:
                if element.contenu is not None:
                    cible.parent.mkdir(parents=True, exist_ok=True)
                    cible.write_bytes(element.contenu)
                else:
                    _copier(element.source, cible)  # type: ignore[arg-type]
                entree = {
                    "chemin": element.membre,
                    "section": element.section,
                    "racine": element.racine,
                    "relatif": element.relatif,
                    "sha256": fmt.empreinte_fichier(cible),
                    "taille": cible.stat().st_size,
                }
            except (OSError, sqlite3.Error) as exc:
                raise fmt.ArchiveError(
                    f"Collecte impossible ({element.section}/{element.relatif}) : {exc}"
                ) from exc
            sections.setdefault(element.section, []).append(entree)

        toutes = [entree for liste in sections.values() for entree in liste]
        manifeste = {
            "format": fmt.FORMAT_ARCHIVE,
            "format_version": fmt.VERSION_FORMAT_ARCHIVE,
            "version_application": __version__,
            "version_donnees": version_donnees,
            "generation_date": datetime.now().isoformat(timespec="seconds"),
            "options": {
                "include_config": include_config,
                "include_journal": include_journal,
                "include_coffre": include_coffre,
                "include_modeles": include_modeles,
                "include_certificats": include_certificats,
                "include_rgpd": include_rgpd,
                "chiffre": chiffrer_effectif,
            },
            "sections": sections,
            "empreinte_globale": fmt.calculer_empreinte_globale(toutes),
            "avertissements": avertissements,
        }
        try:
            (staging / fmt.MANIFESTE_NOM).write_text(
                json.dumps(manifeste, indent=2, ensure_ascii=False), encoding="utf-8"
            )
        except OSError as exc:
            raise fmt.ArchiveError(f"Écriture du manifeste impossible : {exc}") from exc

        zip_temp = staging / "archive.zip"
        try:
            with zipfile.ZipFile(zip_temp, "w", zipfile.ZIP_DEFLATED) as zf:
                zf.write(staging / fmt.MANIFESTE_NOM, fmt.MANIFESTE_NOM)
                for entree in toutes:
                    zf.write(staging / entree["chemin"], entree["chemin"])
            octets_zip = zip_temp.read_bytes()
        except (OSError, zipfile.BadZipFile) as exc:
            raise fmt.ArchiveError(f"Construction du ZIP impossible : {exc}") from exc

        try:
            repertoire_sortie.mkdir(parents=True, exist_ok=True)
        except OSError as exc:
            raise fmt.ArchiveError(
                f"Répertoire de sortie inaccessible ({repertoire_sortie}) : {exc}"
            ) from exc
        archive_chemin = _chemin_archive_unique(repertoire_sortie, chiffrer_effectif)
        infos = fmt.ecrire_archive(
            archive_chemin,
            octets_zip,
            chiffrer=chiffrer_effectif,
            mot_de_passe=password,
            key_path=key_path,
        )

    verification: dict[str, Any] = {
        "chiffre": infos["chiffre"],
        "algorithme": infos["algorithme"],
        "kdf": infos["kdf"],
        "source_cle": infos["cle"],
        "empreinte_zip": infos["empreinte_zip"],
    }
    if not infos["chiffre"]:
        verification["mise_en_garde"] = MISE_EN_GARDE_NON_CHIFFRE

    return {
        "archive_chemin": str(archive_chemin),
        "generation_date": datetime.now().isoformat(timespec="seconds"),
        "inclus": [section for section in SECTIONS if sections.get(section)],
        "nb_fichiers": len(toutes),
        "taille_octets": archive_chemin.stat().st_size,
        "empreinte_zip": infos["empreinte_zip"],
        "verification": verification,
        "config": {
            "inclure_config": include_config,
            "inclure_journal": include_journal,
            "inclure_coffre": include_coffre,
            "inclure_modeles": include_modeles,
            "inclure_certificats": include_certificats,
            "inclure_rgpd": include_rgpd,
            "chiffre": chiffrer_effectif,
        },
        "version_donnees": version_donnees,
        "avertissements": avertissements,
    }


# -- Vérification d'intégrité ------------------------------------------------


def verifier_integrite_backup(
    chemin_archive: str,
    *,
    mot_de_passe: str | None = None,
    key_path: Path | None = None,
) -> dict[str, Any]:
    """Vérifie l'intégrité d'une archive de sauvegarde existante.

    Contrôle sans clé (toujours possible) : structure de l'enveloppe,
    empreinte du contenu chiffré, puis — pour un ZIP clair — manifeste,
    présence et empreinte SHA-256 de chaque fichier, empreinte globale.
    Le déchiffrement complet (empreinte du ZIP + manifeste) demande le mot
    de passe, ou la clé locale pour une archive ``cle-locale``.

    Returns:
        Dict : ``valide`` (aucune altération détectée sur ce qui a pu être
        vérifié), ``verification`` (``complète`` | ``enveloppe`` |
        ``aucune``), ``chiffre``, ``erreur`` / ``details``,
        ``avertissements``, ``taille``, ``date_creation``, ``contient``,
        ``nb_fichiers``, ``empreinte_zip``, ``empreinte_globale``,
        ``format_version``, ``version_application``, ``version_donnees``.
    """
    resultat: dict[str, Any] = {
        "valide": False,
        "verification": "aucune",
        "chiffre": False,
        "erreur": None,
        "details": [],
        "avertissements": [],
        "taille": 0,
        "date_creation": "",
        "contient": [],
        "nb_fichiers": 0,
        "empreinte_zip": "",
        "empreinte_globale": "",
        "format_version": None,
        "version_application": "",
        "version_donnees": None,
    }

    archive = Path(chemin_archive)
    if not archive.is_file():
        resultat["erreur"] = f"Fichier introuvable : {chemin_archive}"
        resultat["details"] = [resultat["erreur"]]
        return resultat

    info = archive.stat()
    resultat["taille"] = info.st_size
    resultat["date_creation"] = datetime.fromtimestamp(info.st_ctime).isoformat(
        timespec="seconds"
    )
    try:
        contenu = archive.read_bytes()
    except OSError as exc:
        resultat["erreur"] = f"Impossible de lire l'archive : {exc}"
        resultat["details"] = [resultat["erreur"]]
        return resultat

    octets_zip: bytes | None = None
    if fmt.est_enveloppe(contenu):
        resultat["chiffre"] = True
        try:
            enveloppe = fmt.lire_enveloppe(contenu)
            enveloppe.verifier_corps()
        except fmt.ArchiveError as exc:
            resultat["erreur"] = str(exc)
            resultat["details"] = [str(exc)]
            return resultat
        resultat["empreinte_zip"] = enveloppe.empreinte_zip

        # Déchiffrement possible sans mot de passe pour une clé locale
        # présente ; sinon vérification limitée à l'enveloppe.
        peut_dechiffrer = True
        if enveloppe.kdf.get("algorithme") == fmt.KDF_MOT_DE_PASSE:
            peut_dechiffrer = mot_de_passe is not None
        elif fmt.lire_cle_locale(key_path) is None:
            peut_dechiffrer = False
            resultat["avertissements"].append(
                f"Clé locale introuvable ({fmt.chemin_cle_locale(key_path)}) : "
                "vérification limitée à l'enveloppe."
            )

        if peut_dechiffrer:
            try:
                octets_zip = fmt.ouvrir_archive(
                    archive, mot_de_passe=mot_de_passe, key_path=key_path
                )
            except fmt.ArchiveError as exc:
                resultat["erreur"] = str(exc)
                resultat["details"] = [str(exc)]
                return resultat
            resultat["verification"] = "complète"
        else:
            resultat["verification"] = "enveloppe"
            resultat["avertissements"].append(
                "Vérification complète impossible sans le mot de passe : seules "
                "l'enveloppe et l'empreinte du contenu chiffré ont été contrôlées."
            )
    elif contenu.startswith(fmt.MAGIC_ZIP):
        resultat["empreinte_zip"] = fmt.empreinte(contenu)
        octets_zip = contenu
        resultat["verification"] = "complète"
    else:
        resultat["erreur"] = "Format d'archive non reconnu (attendu : ZIP ou enveloppe EduSync)"
        resultat["details"] = [resultat["erreur"]]
        return resultat

    if octets_zip is None:
        # Enveloppe seule : structure + empreinte du contenu = conforme.
        resultat["valide"] = True
        return resultat

    details = fmt.verifier_manifeste(octets_zip)
    if details:
        resultat["erreur"] = " ; ".join(details)
        resultat["details"] = details
        return resultat

    manifeste = fmt.lire_manifeste(octets_zip)
    sections = manifeste.get("sections") or {}
    resultat["contient"] = sorted(sections)
    resultat["nb_fichiers"] = sum(len(liste) for liste in sections.values())
    resultat["empreinte_globale"] = manifeste.get("empreinte_globale", "")
    resultat["format_version"] = manifeste.get("format_version")
    resultat["version_application"] = manifeste.get("version_application", "")
    resultat["version_donnees"] = manifeste.get("version_donnees")
    resultat["valide"] = True
    return resultat
