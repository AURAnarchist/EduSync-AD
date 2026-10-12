"""M32 — Migration des données vN → vN+1.

Le module maintient un **registre de migrations**
(``<données>/migrations.json``) qui mémorise la version de schéma des
données locales et la liste des migrations déjà appliquées. Au lancement :

1. la **version courante** est détectée depuis le registre (registre absent
   = version initiale : données héritées ou installation neuve) ;
2. les migrations **séquentielles** comprises entre cette version et la
   version cible sont appliquées dans l'ordre de la table ``MIGRATIONS`` —
   chacune n'agit que si ses données sont concernées (idempotence de fait)
   et n'est validée dans le registre qu'une fois réussie, si bien qu'une
   interruption reprend au prochain lancement ;
3. relancer la migration quand tout est à jour est un **no-op**
   (``deja_migre: True``).

Refus explicites (sécurité) :

- version de données **supérieure** à la version supportée (archive ou
  registre venu d'une application plus récente) ;
- **rétrogradation** demandée (``version_cible`` < version courante) ;
- registre illisible ou table de migrations incohérente.

Migrations actuelles :

- ``0001_config_format_identifiant`` (v1→v2) : ``config.json`` historique
  avec un seul champ ``identifiant_format`` → distinction
  ``identifiant_format_eleve`` / ``identifiant_format_personnel`` ;
- ``0002_journal_colonnes_audit`` (v2→v3) : colonnes ``utilisateur`` /
  ``domaine`` (journal multisite/opérateur) et ``delai_jours`` ajoutées
  comme le fait ``core/audit.py`` ;
- ``0003_coffre_sam_minuscules`` (v3→v4) : sam du coffre normalisés en
  minuscules (recherche insensible à la casse dans ``PasswordVault``),
  doublons fusionnés sur la dernière entrée.
"""

from __future__ import annotations

import json
import sqlite3
import tempfile
from contextlib import closing
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

from edusync_ad.core.audit import data_dir
from edusync_ad.core.config import config_dir

#: Format du registre.
FORMAT_REGISTRE = "edusync-migrations"
#: Nom du fichier de registre, dans le dossier de données.
REGISTRE_NOM = "migrations.json"
#: Version portée par les données qui précèdent l'introduction du registre.
VERSION_INITIALE_DONNEES = 1
#: Version de schéma supportée par cette version de l'application.
VERSION_DONNEES_COURANTE = 4

#: Noms des fichiers de données de l'application — source unique pour
#: ``backup.py`` / ``restore.py`` ; voir ``core/audit.py`` et
#: ``core/password_vault.py`` (``default_db_path`` / ``default_vault_path``).
NOM_JOURNAL = "journal.db"
NOM_COFFRE = "password_vault.sqlite3"
NOM_CONFIG = "config.json"


class MigrationError(Exception):
    """Erreur de migration de données (message déjà en français)."""


# -- Registre ----------------------------------------------------------------


def registre_path(repertoire_donnees: Path | None = None) -> Path:
    """Emplacement du registre de migrations (dossier ``data`` par défaut)."""
    return (repertoire_donnees if repertoire_donnees is not None else data_dir()) / REGISTRE_NOM


def lire_registre(chemin: Path | None = None) -> dict[str, Any]:
    """Charge le registre de migrations.

    Un registre absent signale la **version initiale** : les données sont
    héritées d'une version antérieure au registre (ou l'installation est
    neuve — les migrations idempotentes n'y trouvent rien à changer).

    Raises:
        MigrationError: registre illisible ou invalide (jamais réparé
            silencieusement : le refus explicite vaut mieux qu'une
            remise à zéro hasardeuse).
    """
    chemin = chemin if chemin is not None else registre_path()
    if not chemin.exists():
        return {
            "format": FORMAT_REGISTRE,
            "version": VERSION_INITIALE_DONNEES,
            "historique": [],
        }
    try:
        brut = json.loads(chemin.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise MigrationError(f"Registre de migrations illisible ({chemin}) : {exc}") from exc
    if not isinstance(brut, dict):
        raise MigrationError(f"Registre de migrations invalide ({chemin}) : objet attendu")
    if brut.get("format") not in (None, FORMAT_REGISTRE):
        raise MigrationError(
            f"Registre de migrations de format inconnu : {brut.get('format')!r}"
        )
    version = brut.get("version")
    if not isinstance(version, int) or version < VERSION_INITIALE_DONNEES:
        raise MigrationError(f"Registre de migrations invalide : version {version!r}")
    historique = brut.get("historique")
    if not isinstance(historique, list):
        raise MigrationError("Registre de migrations invalide : historique absent")
    return {"format": FORMAT_REGISTRE, "version": version, "historique": historique}


def ecrire_registre(registre: dict[str, Any], chemin: Path | None = None) -> None:
    """Écrit le registre de façon atomique (écriture temporaire + remplacement)."""
    chemin = chemin if chemin is not None else registre_path()
    chemin.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        "w", encoding="utf-8", dir=chemin.parent, delete=False, suffix=".tmp"
    ) as fichier:
        json.dump(registre, fichier, indent=2, ensure_ascii=False)
        fichier.write("\n")
        temp = Path(fichier.name)
    temp.replace(chemin)


def detecter_version_donnees(chemin: Path | None = None) -> int:
    """Version de schéma des données courantes (1 si registre absent)."""
    return int(lire_registre(chemin)["version"])


# -- Table des migrations ----------------------------------------------------


@dataclass(frozen=True)
class ContexteMigration:
    """Fichiers sur lesquels une migration opère."""

    repertoire_config: Path
    repertoire_donnees: Path


@dataclass
class ResultatMigration:
    """Bilan d'une migration : éléments transformés / éléments créés."""

    migres: list[str] = field(default_factory=list)
    nouveaux: list[str] = field(default_factory=list)


@dataclass(frozen=True)
class Migration:
    """Une étape vN → vN+1. ``version`` est la version **atteinte**."""

    identifiant: str
    version: int
    description: str
    cibles: tuple[str, ...]
    appliquer: Callable[[ContexteMigration], ResultatMigration]


def _migrer_format_identifiant(contexte: ContexteMigration) -> ResultatMigration:
    """``identifiant_format`` (unique) → ``identifiant_format_eleve`` +
    ``identifiant_format_personnel`` (M2 — politiques distinctes)."""
    chemin = contexte.repertoire_config / NOM_CONFIG
    if not chemin.is_file():
        return ResultatMigration(migres=[f"{NOM_CONFIG} absent — rien à migrer"])
    try:
        donnees = json.loads(chemin.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise MigrationError(f"{NOM_CONFIG} illisible : {exc}") from exc
    if not isinstance(donnees, dict):
        raise MigrationError(f"{NOM_CONFIG} invalide (objet JSON attendu)")

    historique = donnees.get("identifiant_format")
    resultat = ResultatMigration()
    if not isinstance(historique, str) or not historique:
        resultat.migres.append(f"{NOM_CONFIG} déjà au format courant")
        return resultat

    for champ in ("identifiant_format_eleve", "identifiant_format_personnel"):
        if not isinstance(donnees.get(champ), str) or not donnees.get(champ):
            donnees[champ] = historique
            resultat.nouveaux.append(f"config.{champ}")
    del donnees["identifiant_format"]
    resultat.migres.append(
        f"{NOM_CONFIG} : champ historique « identifiant_format » scindé en "
        "identifiant_format_eleve / identifiant_format_personnel"
    )
    try:
        chemin.write_text(
            json.dumps(donnees, indent=2, ensure_ascii=False), encoding="utf-8"
        )
    except OSError as exc:
        raise MigrationError(f"Écriture impossible ({chemin}) : {exc}") from exc
    return resultat


def _colonnes_manquantes(connexion: sqlite3.Connection, table: str) -> list[tuple[str, str]]:
    """Colonnes à ajouter pour aligner un journal hérité sur le schéma actuel."""
    try:
        existantes = {
            ligne["name"]
            for ligne in connexion.execute(f"PRAGMA table_info({table})")
        }
    except sqlite3.Error as exc:
        raise MigrationError(f"Table {table} illisible : {exc}") from exc
    attendues = {"utilisateur": "TEXT", "domaine": "TEXT"} if table == "actions" else {
        "delai_jours": "INTEGER"
    }
    return [(nom, type_sql) for nom, type_sql in attendues.items() if nom not in existantes]


def _migrer_journal(contexte: ContexteMigration) -> ResultatMigration:
    """Colonnes d'audit absentes d'un journal SQLite hérité (M25/M26)."""
    chemin = contexte.repertoire_donnees / NOM_JOURNAL
    if not chemin.is_file():
        return ResultatMigration(migres=[f"{NOM_JOURNAL} absent — rien à migrer"])
    resultat = ResultatMigration()
    try:
        with closing(sqlite3.connect(chemin)) as connexion:
            connexion.row_factory = sqlite3.Row
            tables = {
                ligne["name"]
                for ligne in connexion.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table'"
                )
            }
            for table in ("actions", "pending_deletions"):
                if table not in tables:
                    resultat.migres.append(f"{NOM_JOURNAL} : table {table} absente")
                    continue
                for nom, type_sql in _colonnes_manquantes(connexion, table):
                    connexion.execute(f"ALTER TABLE {table} ADD COLUMN {nom} {type_sql}")
                    resultat.nouveaux.append(f"{table}.{nom}")
                connexion.commit()
    except sqlite3.Error as exc:
        raise MigrationError(f"{NOM_JOURNAL} illisible : {exc}") from exc
    if not resultat.nouveaux and not resultat.migres:
        resultat.migres.append(f"{NOM_JOURNAL} déjà au schéma courant")
    return resultat


def _migrer_coffre(contexte: ContexteMigration) -> ResultatMigration:
    """Normalisation des sam du coffre en minuscules (recherche insensible à
    la casse) avec fusion des doublons hérités."""
    chemin = contexte.repertoire_donnees / NOM_COFFRE
    if not chemin.is_file():
        return ResultatMigration(migres=[f"{NOM_COFFRE} absent — rien à migrer"])
    resultat = ResultatMigration()
    try:
        with closing(sqlite3.connect(chemin)) as connexion:
            connexion.row_factory = sqlite3.Row
            tables = {
                ligne["name"]
                for ligne in connexion.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table'"
                )
            }
            if "mots_de_passe" not in tables:
                return ResultatMigration(
                    migres=[f"{NOM_COFFRE} : table mots_de_passe absente"]
                )
            lignes = connexion.execute(
                "SELECT rowid, sam, secret, horodatage FROM mots_de_passe ORDER BY rowid"
            ).fetchall()
            # Dernière entrée gagne sur un doublon hérité (ordre rowid croissant).
            regroupes: dict[str, tuple[str, str]] = {}
            casse = 0
            for ligne in lignes:
                sam = ligne["sam"] or ""
                if sam != sam.lower():
                    casse += 1
                regroupes[sam.lower()] = (ligne["secret"], ligne["horodatage"])
            if casse == 0 and len(regroupes) == len(lignes):
                resultat.migres.append(f"{NOM_COFFRE} déjà normalisé")
                return resultat
            connexion.execute("DELETE FROM mots_de_passe")
            connexion.executemany(
                "INSERT INTO mots_de_passe (sam, secret, horodatage) VALUES (?, ?, ?)",
                [(sam, secret, horodatage) for sam, (secret, horodatage) in regroupes.items()],
            )
            connexion.commit()
    except sqlite3.Error as exc:
        raise MigrationError(f"{NOM_COFFRE} illisible : {exc}") from exc
    resultat.nouveaux.append(f"{NOM_COFFRE}.sam normalisés en minuscules")
    resultat.migres.append(
        f"{NOM_COFFRE} : {casse} entrée(s) recasée(s), "
        f"{len(lignes) - len(regroupes)} doublon(s) fusionné(s)"
    )
    return resultat


#: Table des migrations, dans l'ordre d'application — les ``version`` doivent
#: être contiguës à partir de ``VERSION_INITIALE_DONNEES + 1`` (contrôlé par
#: :func:`migrer_donnees_vn_vn1`).
MIGRATIONS: tuple[Migration, ...] = (
    Migration(
        identifiant="0001_config_format_identifiant",
        version=2,
        description="Scission du champ unique identifiant_format (élèves / personnels)",
        cibles=("config",),
        appliquer=_migrer_format_identifiant,
    ),
    Migration(
        identifiant="0002_journal_colonnes_audit",
        version=3,
        description="Ajout des colonnes d'audit utilisateur / domaine / delai_jours",
        cibles=("journal",),
        appliquer=_migrer_journal,
    ),
    Migration(
        identifiant="0003_coffre_sam_minuscules",
        version=4,
        description="Normalisation des sam du coffre MDP en minuscules",
        cibles=("coffre",),
        appliquer=_migrer_coffre,
    ),
)


# -- Exécution ---------------------------------------------------------------


def _rapport_vierge(version_cible: int) -> dict[str, Any]:
    return {
        "reussite": False,
        "version_depart": None,
        "version_arrivee": None,
        "version_cible": version_cible,
        "deja_migre": False,
        "elements_migres": [],
        "elements_nouveaux": [],
        "migrations_appliquees": [],
        "migrations_en_attente": [],
        "rapport": [],
        "avertissements": [],
        "erreurs": [],
    }


def migrer_donnees_vn_vn1(
    repertoire_config: Path | None = None,
    repertoire_donnees: Path | None = None,
    *,
    chemin_registre: Path | None = None,
    version_cible: int | None = None,
    mode_test: bool = False,
) -> dict[str, Any]:
    """Migre les données locales vers la version de schéma suivante (vN → vN+1).

    Applique **séquentiellement** toutes les migrations en attente entre la
    version détectée et la version cible, puis enregistre chacune d'elles
    dans le registre : un second appel est sans effet (``deja_migre``).

    Args:
        repertoire_config: Dossier de configuration (défaut : ``config_dir()``).
        repertoire_donnees: Dossier de données (défaut : ``data_dir()``).
        chemin_registre: Registre explicite (défaut : ``<données>/migrations.json``).
        version_cible: Plafond à atteindre (défaut : version courante de
            l'application). Une valeur supérieure ou inférieure à la version
            supportée est refusée.
        mode_test: ``True`` → rapport détaillé sans rien modifier.

    Returns:
        Dict : ``reussite``, ``version_depart`` / ``version_arrivee`` /
        ``version_cible``, ``deja_migre``, ``elements_migres``,
        ``elements_nouveaux``, ``migrations_appliquees`` /
        ``migrations_en_attente``, ``rapport`` (une ligne par étape),
        ``avertissements``, ``erreurs``. Aucun refus attendu ne lève :
        ``erreurs`` décrit précisément le motif (version inconnue,
        rétrogradation, registre illisible, échec d'une étape).
    """
    config_rep = Path(repertoire_config) if repertoire_config else config_dir()
    data_rep = Path(repertoire_donnees) if repertoire_donnees else data_dir()
    registre_chemin = (
        Path(chemin_registre) if chemin_registre else data_rep / REGISTRE_NOM
    )
    cible = version_cible if version_cible is not None else VERSION_DONNEES_COURANTE
    rapport = _rapport_vierge(cible)

    try:
        registre = lire_registre(registre_chemin)
    except MigrationError as exc:
        rapport["erreurs"].append(str(exc))
        return rapport
    depart = int(registre["version"])
    rapport["version_depart"] = depart

    if depart > VERSION_DONNEES_COURANTE:
        rapport["erreurs"].append(
            f"Version de données inconnue : {depart} (version maximale supportée : "
            f"{VERSION_DONNEES_COURANTE}) — migration refusée, application à mettre à jour."
        )
        return rapport
    if cible > VERSION_DONNEES_COURANTE:
        rapport["erreurs"].append(
            f"Version cible inconnue : {cible} (version maximale supportée : "
            f"{VERSION_DONNEES_COURANTE})."
        )
        return rapport
    if cible < depart:
        rapport["erreurs"].append(
            f"Rétrogradation refusée : version cible {cible} inférieure à la version "
            f"courante {depart}."
        )
        return rapport

    # Contrôle de cohérence de la table (dév. : jamais atteint en production).
    for indice, migration in enumerate(
        MIGRATIONS, start=VERSION_INITIALE_DONNEES + 1
    ):
        if migration.version != indice:
            rapport["erreurs"].append(
                f"Table de migrations incohérente : {migration.identifiant} vise la "
                f"version {migration.version}, attendu {indice}."
            )
            return rapport

    en_attente = [m for m in MIGRATIONS if depart < m.version <= cible]
    rapport["migrations_en_attente"] = [m.identifiant for m in en_attente]
    if not en_attente:
        rapport["reussite"] = True
        rapport["deja_migre"] = True
        rapport["version_arrivee"] = depart
        return rapport

    if mode_test:
        rapport["reussite"] = True
        rapport["version_arrivee"] = depart
        rapport["rapport"] = [
            {
                "identifiant": m.identifiant,
                "version": m.version,
                "description": m.description,
                "cibles": list(m.cibles),
                "statut": "à appliquer",
                "elements": [],
            }
            for m in en_attente
        ]
        rapport["avertissements"].append("Mode test : aucune donnée modifiée.")
        return rapport

    contexte = ContexteMigration(repertoire_config=config_rep, repertoire_donnees=data_rep)
    version_courante = depart
    interrompue = False
    for migration in en_attente:
        entree: dict[str, Any] = {
            "identifiant": migration.identifiant,
            "version": migration.version,
            "description": migration.description,
            "cibles": list(migration.cibles),
            "statut": "appliquée",
            "elements": [],
        }
        try:
            resultat = migration.appliquer(contexte)
        except Exception as exc:  # étape défaillante : on s'arrête, le reste
            entree["statut"] = "échec"  # reprendra au prochain lancement
            entree["erreur"] = str(exc)
            rapport["rapport"].append(entree)
            rapport["erreurs"].append(f"Échec de {migration.identifiant} : {exc}")
            interrompue = True
            break

        entree["elements"] = [*resultat.migres, *resultat.nouveaux]
        rapport["elements_migres"].extend(resultat.migres)
        rapport["elements_nouveaux"].extend(resultat.nouveaux)
        rapport["migrations_appliquees"].append(migration.identifiant)
        rapport["rapport"].append(entree)

        version_courante = migration.version
        registre["version"] = version_courante
        registre["historique"].append(
            {
                "identifiant": migration.identifiant,
                "version": migration.version,
                "horodatage": datetime.now(timezone.utc).isoformat(timespec="seconds"),
                "elements": entree["elements"],
            }
        )
        try:
            ecrire_registre(registre, registre_chemin)
        except OSError as exc:
            rapport["erreurs"].append(
                f"Registre de migrations non écrit ({registre_chemin}) : {exc}"
            )
            interrompue = True
            break

    rapport["version_arrivee"] = version_courante
    rapport["migrations_en_attente"] = [
        m.identifiant for m in MIGRATIONS if version_courante < m.version <= cible
    ]
    rapport["reussite"] = not interrompue
    if not interrompue:
        rapport["deja_migre"] = False
    return rapport
