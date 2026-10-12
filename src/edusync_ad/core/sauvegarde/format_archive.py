"""M32 — Format des archives de sauvegarde (enveloppe chiffrée + manifeste).

Une sauvegarde EduSync AD est l'une des deux formes suivantes, identifiée par
ses **octets magiques** (jamais par l'extension de fichier, non fiable) :

1. **ZIP clair** — commence par ``PK\\x03\\x04`` : lisible avec n'importe quel
   outil d'archives, mais **sans aucune protection au repos**. Le fichier
   ``secret.key`` n'y est jamais écrit (voir ``backup.py``).
2. **Enveloppe chiffrée** — première ligne ``EDUSYNC-ARCHIVE``, deuxième
   ligne : en-tête JSON en clair, reste du fichier : jeton AES-256-GCM.

L'en-tête reste volontairement lisible et ne contient que des métadonnées
nécessaires à la vérification **sans clé** :

- ``empreinte_contenu`` : SHA-256 du jeton chiffré → toute altération du
  fichier chiffré est détectée *avant* tout déchiffrement ;
- ``empreinte_zip`` : SHA-256 du ZIP déchiffré → l'en-tête lui-même n'étant
  pas authentifié par GCM, toute modification de l'en-tête est détectée
  dès le déchiffrement (les deux empreintes se croisent) ;
- ``kdf`` : paramètres de dérivation de la clé de déchiffrement.

Le chiffrement réutilise **exclusivement** ``core/crypto.py``
(``encrypt_str``/``decrypt_str``, AES-256-GCM avec nonce aléatoire) :

- archive **portable** (mot de passe) : clé = PBKDF2-HMAC-SHA256 du mot de
  passe, sel aléatoire de 16 octets et itérations stockés dans l'en-tête ;
- archive **locale** (sans mot de passe) : clé du fichier ``secret.key`` de
  la machine, déjà utilisée par le coffre MDP et les profils multisite.

Le manifeste (``manifest.json``, racine du ZIP) décrit chaque fichier : section,
racine de restauration, empreinte SHA-256 et taille, plus une empreinte globale.
Un ZIP clair n'étant pas signé, l'intégrité qu'il garantit est « détecter une
altération » (corruption, édition) et non « prouver l'origine » ; une archive
chiffrée bénéficie en plus de l'authentification AES-GCM.
"""

from __future__ import annotations

import base64
import binascii
import hashlib
import io
import json
import os
import stat
import zipfile
import zlib
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

from edusync_ad.core.config import config_dir
from edusync_ad.core.crypto import decrypt_str, encrypt_str, get_or_create_key

#: Première ligne d'une enveloppe chiffrée.
MAGIC_ENVELOPPE = b"EDUSYNC-ARCHIVE"
#: Première ligne (octets) d'un ZIP.
MAGIC_ZIP = b"PK\x03\x04"

#: Identifiant du format d'archive (manifeste + enveloppe).
FORMAT_ARCHIVE = "edusync-backup"
#: Version du format. Toute autre valeur est refusée explicitement.
VERSION_FORMAT_ARCHIVE = 1

ALGORITHME_CHIFFREMENT = "AES-256-GCM"
#: Dérivation d'une clé à partir d'un mot de passe.
KDF_MOT_DE_PASSE = "pbkdf2-hmac-sha256"
#: Clé locale de la machine (``secret.key``).
KDF_CLE_LOCALE = "cle-locale"
KDF_ITERATIONS = 200_000
#: Garde-fou contre un en-tête hostile demandant une dérivation interminable.
KDF_ITERATIONS_MAXIMUM = 5_000_000
SEL_TAILLE_OCTETS = 16
CLE_TAILLE_OCTETS = 32

MANIFESTE_NOM = "manifest.json"
#: Préfixe de tous les fichiers de données dans le ZIP.
PREFIXE_MEMBRE = "payload"
#: Racines de restauration possibles d'une entrée du manifeste.
RACINES = ("config", "data")


class ArchiveError(Exception):
    """Erreur d'archive de sauvegarde (message déjà en français)."""


# -- Empreintes & clés -------------------------------------------------------


def empreinte(data: bytes) -> str:
    """Empreinte SHA-256 hexadécimale d'un buffer."""
    return hashlib.sha256(data).hexdigest()


def empreinte_fichier(path: Path) -> str:
    """Empreinte SHA-256 hexadécimale d'un fichier (lecture par blocs)."""
    digest = hashlib.sha256()
    with path.open("rb") as fichier:
        for bloc in iter(lambda: fichier.read(65536), b""):
            digest.update(bloc)
    return digest.hexdigest()


def calculer_empreinte_globale(entrees: list[dict[str, Any]]) -> str:
    """Empreinte globale d'une liste d'entrées de manifeste.

    SHA-256 de la concaténation triée ``chemin\\0empreinte`` de chaque
    fichier : modifiez un fichier *ou* son empreinte et l'empreinte globale
    change.
    """
    digest = hashlib.sha256()
    for entree in sorted(entrees, key=lambda e: str(e.get("chemin", ""))):
        digest.update(str(entree.get("chemin", "")).encode("utf-8"))
        digest.update(b"\0")
        digest.update(str(entree.get("sha256", "")).encode("ascii", "ignore"))
    return digest.hexdigest()


def chemin_cle_locale(key_path: Path | None = None) -> Path:
    """Emplacement de la clé locale (ne crée jamais le fichier)."""
    return key_path if key_path is not None else config_dir() / "secret.key"


def lire_cle_locale(key_path: Path | None = None) -> bytes | None:
    """Charge la clé locale si elle existe — **sans** la créer (voir
    ``crypto.get_or_create_key`` qui, lui, la crée)."""
    chemin = chemin_cle_locale(key_path)
    try:
        octets = chemin.read_bytes()
    except OSError:
        return None
    return octets if len(octets) == CLE_TAILLE_OCTETS else None


def deriver_cle_mot_de_passe(mot_de_passe: str, sel: bytes, iterations: int) -> bytes:
    """Dérive une clé AES-256 d'un mot de passe (PBKDF2-HMAC-SHA256).

    Même famille de dérivation que ``core/portal.py`` pour les codes à usage
    unique ; ici les itérations sont volontairement élevées (coût mesuré en
    dixièmes de seconde) car l'opération est rare (une sauvegarde).
    """
    if not 0 < iterations <= KDF_ITERATIONS_MAXIMUM:
        raise ArchiveError(f"Nombre d'iterations de dérivation invalide : {iterations}")
    return hashlib.pbkdf2_hmac(
        "sha256", mot_de_passe.encode("utf-8"), sel, iterations, dklen=CLE_TAILLE_OCTETS
    )


def _valider_empreinte(valeur: Any, contexte: str = "en-tête") -> str:
    if not isinstance(valeur, str) or len(valeur) != 64:
        raise ArchiveError(f"Empreinte SHA-256 invalide dans le {contexte}")
    try:
        bytes.fromhex(valeur)
    except ValueError as exc:
        raise ArchiveError(f"Empreinte SHA-256 invalide dans le {contexte}") from exc
    return valeur.lower()


# -- Écriture ----------------------------------------------------------------


def _ecrire_restreint(chemin: Path, contenu: bytes) -> None:
    """Écrit un fichier avec les permissions 0600 (l'archive contient des
    données sensibles, éventuellement chiffrées)."""
    chemin.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(chemin, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, stat.S_IRUSR | stat.S_IWUSR)
    try:
        os.write(fd, contenu)
    finally:
        os.close(fd)


def ecrire_archive(
    chemin: Path,
    octets_zip: bytes,
    *,
    chiffrer: bool,
    mot_de_passe: str | None = None,
    key_path: Path | None = None,
) -> dict[str, Any]:
    """Écrit l'archive sur disque et renvoie les informations de vérification.

    Args:
        chemin: Fichier à créer (parents créés si besoin, permissions 0600).
        octets_zip: Contenu du ZIP déjà construit.
        chiffrer: ``False`` → ZIP clair (aucun secret doit y figurer en clair).
        mot_de_passe: Clé portable dérivée du mot de passe (implique le
            chiffrement).
        key_path: Emplacement de la clé locale si pas de mot de passe.

    Returns:
        Dict ``chiffre`` / ``algorithme`` / ``kdf`` / ``empreinte_zip`` /
        ``cle`` (``"mot_de_passe"`` | ``"locale"`` | ``None``).

    Raises:
        ArchiveError: Écriture impossible ou paramètres incohérents.
    """
    if mot_de_passe is not None:
        chiffrer = True
    empreinte_zip = empreinte(octets_zip)

    try:
        if not chiffrer:
            _ecrire_restreint(chemin, octets_zip)
            return {
                "chiffre": False,
                "algorithme": "aucun",
                "kdf": None,
                "empreinte_zip": empreinte_zip,
                "cle": None,
            }

        if mot_de_passe is not None:
            sel = os.urandom(SEL_TAILLE_OCTETS)
            cle = deriver_cle_mot_de_passe(mot_de_passe, sel, KDF_ITERATIONS)
            kdf: dict[str, Any] = {
                "algorithme": KDF_MOT_DE_PASSE,
                "iterations": KDF_ITERATIONS,
                "sel": base64.b64encode(sel).decode("ascii"),
            }
            source_cle = "mot_de_passe"
        else:
            cle = charger_cle_locale(key_path)
            kdf = {"algorithme": KDF_CLE_LOCALE}
            source_cle = "locale"

        jeton = encrypt_str(cle, base64.b64encode(octets_zip).decode("ascii"))
        corps = jeton.encode("ascii")
        en_tete = {
            "format": FORMAT_ARCHIVE,
            "format_version": VERSION_FORMAT_ARCHIVE,
            "chiffre": True,
            "algorithme": ALGORITHME_CHIFFREMENT,
            "kdf": kdf,
            "empreinte_zip": empreinte_zip,
            "empreinte_contenu": empreinte(corps),
        }
        contenu = (
            MAGIC_ENVELOPPE
            + b"\n"
            + json.dumps(en_tete, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
            + b"\n"
            + corps
        )
        _ecrire_restreint(chemin, contenu)
    except ArchiveError:
        raise
    except OSError as exc:
        raise ArchiveError(f"Écriture de l'archive impossible ({chemin}) : {exc}") from exc

    return {
        "chiffre": True,
        "algorithme": ALGORITHME_CHIFFREMENT,
        "kdf": kdf,
        "empreinte_zip": empreinte_zip,
        "cle": source_cle,
    }


def charger_cle_locale(key_path: Path | None = None) -> bytes:
    """Clé locale pour chiffrer : chargée si présente, créée sinon (une
    sauvegarde locale doit pouvoir être la première à créer la clé)."""
    cle = lire_cle_locale(key_path)
    if cle is not None:
        return cle
    return get_or_create_key(key_path)


# -- Lecture -----------------------------------------------------------------


def est_enveloppe(contenu: bytes) -> bool:
    """Vrai si le contenu commence par les octets magiques d'une enveloppe."""
    return contenu.startswith(MAGIC_ENVELOPPE + b"\n")


@dataclass(frozen=True)
class Enveloppe:
    """En-tête d'une archive chiffrée + corps chiffré (sans déchiffrement)."""

    format_version: int
    chiffre: bool
    kdf: dict[str, Any]
    empreinte_zip: str
    empreinte_contenu: str
    corps: bytes

    def verifier_corps(self) -> None:
        """Vérifie que le contenu chiffré n'a pas été modifié (sans clé)."""
        if empreinte(self.corps) != self.empreinte_contenu:
            raise ArchiveError("Empreinte du contenu chiffré modifiée — archive altérée")


def lire_enveloppe(source: Path | bytes) -> Enveloppe:
    """Lit et valide la structure d'une enveloppe chiffrée (sans clé).

    Raises:
        ArchiveError: fichier absent, tronqué, format ou version inconnu.
    """
    if isinstance(source, Path):
        try:
            contenu = source.read_bytes()
        except OSError as exc:
            raise ArchiveError(f"Impossible de lire l'archive {source} : {exc}") from exc
    else:
        contenu = source

    if not est_enveloppe(contenu):
        if contenu.startswith(MAGIC_ZIP):
            raise ArchiveError("L'archive est un ZIP clair, pas une enveloppe chiffrée")
        raise ArchiveError("Format d'archive non reconnu (attendu : ZIP ou enveloppe EduSync)")

    parties = contenu.split(b"\n", 2)
    if len(parties) != 3 or not parties[1] or not parties[2]:
        raise ArchiveError("Enveloppe d'archive tronquée ou illisible")

    try:
        brut = json.loads(parties[1].decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ArchiveError(f"En-tête d'archive illisible : {exc}") from exc
    if not isinstance(brut, dict):
        raise ArchiveError("En-tête d'archive invalide (objet JSON attendu)")
    if brut.get("format") != FORMAT_ARCHIVE:
        raise ArchiveError(f"Format d'archive inconnu : {brut.get('format')!r}")
    version = brut.get("format_version")
    if version != VERSION_FORMAT_ARCHIVE:
        raise ArchiveError(f"Version de format d'archive non gérée : {version!r}")
    if brut.get("chiffre") is not True:
        raise ArchiveError("Enveloppe d'archive invalide : le champ « chiffre » doit être vrai")
    kdf = brut.get("kdf")
    if not isinstance(kdf, dict) or not isinstance(kdf.get("algorithme"), str):
        raise ArchiveError("En-tête d'archive invalide : paramètres de dérivation de clé absents")

    return Enveloppe(
        format_version=version,
        chiffre=True,
        kdf=kdf,
        empreinte_zip=_valider_empreinte(brut.get("empreinte_zip")),
        empreinte_contenu=_valider_empreinte(brut.get("empreinte_contenu")),
        corps=parties[2],
    )


def _cle_dechiffrement(
    enveloppe: Enveloppe, *, mot_de_passe: str | None, key_path: Path | None
) -> bytes:
    """Obtient la clé de déchiffrement — refuse proprement l'inconnu."""
    algorithme = enveloppe.kdf.get("algorithme")
    if algorithme == KDF_MOT_DE_PASSE:
        if not mot_de_passe:
            raise ArchiveError("Archive chiffrée avec un mot de passe — mot de passe requis")
        sel_b64 = enveloppe.kdf.get("sel")
        if not isinstance(sel_b64, str):
            raise ArchiveError("En-tête d'archive invalide : sel de dérivation absent")
        try:
            sel = base64.b64decode(sel_b64.encode("ascii"), validate=True)
        except (binascii.Error, ValueError) as exc:
            raise ArchiveError("En-tête d'archive invalide : sel de dérivation illisible") from exc
        if len(sel) != SEL_TAILLE_OCTETS:
            raise ArchiveError("En-tête d'archive invalide : taille de sel inattendue")
        iterations = enveloppe.kdf.get("iterations")
        if not isinstance(iterations, int):
            raise ArchiveError("En-tête d'archive invalide : itérations de dérivation absentes")
        return deriver_cle_mot_de_passe(mot_de_passe, sel, iterations)

    if algorithme == KDF_CLE_LOCALE:
        cle = lire_cle_locale(key_path)
        if cle is None:
            raise ArchiveError(
                "Clé de chiffrement locale introuvable ou invalide : "
                f"{chemin_cle_locale(key_path)} — archive locale indéchiffrable ici"
            )
        return cle

    raise ArchiveError(f"Algorithme de dérivation de clé inconnu : {algorithme!r}")


def ouvrir_archive(
    chemin: Path,
    *,
    mot_de_passe: str | None = None,
    key_path: Path | None = None,
) -> bytes:
    """Retourne les octets du ZIP contenu dans l'archive, après déchiffrement
    et vérification de toutes les empreintes disponibles.

    Raises:
        ArchiveError: fichier absent, format/versions inconnus, archive
            altérée, mot de passe ou clé incorrects.
    """
    try:
        contenu = chemin.read_bytes()
    except OSError as exc:
        raise ArchiveError(f"Impossible de lire l'archive {chemin} : {exc}") from exc

    if est_enveloppe(contenu):
        enveloppe = lire_enveloppe(contenu)
        enveloppe.verifier_corps()
        cle = _cle_dechiffrement(enveloppe, mot_de_passe=mot_de_passe, key_path=key_path)
        try:
            decore = decrypt_str(cle, enveloppe.corps.decode("ascii"))
            octets_zip = base64.b64decode(decore)
        except Exception as exc:  # clé fausse, tag GCM invalide, base64 cassé…
            if enveloppe.kdf.get("algorithme") == KDF_CLE_LOCALE:
                raise ArchiveError(
                    "Déchiffrement impossible : clé de chiffrement locale incorrecte "
                    "ou archive altérée"
                ) from exc
            raise ArchiveError(
                "Déchiffrement impossible : mot de passe incorrect ou archive altérée"
            ) from exc
        if empreinte(octets_zip) != enveloppe.empreinte_zip:
            raise ArchiveError("Empreinte du ZIP déchiffré modifiée — archive altérée")
        return octets_zip

    if contenu.startswith(MAGIC_ZIP):
        return contenu

    raise ArchiveError("Format d'archive non reconnu (attendu : ZIP ou enveloppe EduSync)")


# -- Manifeste ---------------------------------------------------------------


def _chemin_sur(chemin: str, *, prefixe: str | None = None) -> str:
    """Valide un chemin POSIX de manifeste (ni absolu, ni ``..``) et le
    renvoie normalisé. Lève :class:`ArchiveError`."""
    if not isinstance(chemin, str) or not chemin:
        raise ArchiveError("Chemin de fichier absent dans le manifeste")
    if "\\" in chemin:
        raise ArchiveError(f"Chemin de fichier invalide dans le manifeste : {chemin!r}")
    posix = PurePosixPath(chemin)
    if posix.is_absolute() or any(part == ".." for part in posix.parts):
        raise ArchiveError(f"Chemin de sortie hors archive refusé : {chemin!r}")
    if prefixe is not None and (len(posix.parts) == 0 or posix.parts[0] != prefixe):
        raise ArchiveError(f"Membre hors du préfixe {prefixe!r} refusé : {chemin!r}")
    return posix.as_posix()


def _lire_manifeste(zf: zipfile.ZipFile) -> dict[str, Any]:
    """Charge et valide le manifeste déjà ouvert (format + version)."""
    try:
        brut = zf.read(MANIFESTE_NOM)
    except KeyError as exc:
        raise ArchiveError(f"{MANIFESTE_NOM} absent de l'archive") from exc
    except (zipfile.BadZipFile, RuntimeError, OSError, zlib.error) as exc:
        # zlib.error : flux deflate corrompu — ni CRC ni BadZipFile à ce stade.
        raise ArchiveError(f"{MANIFESTE_NOM} illisible : {exc}") from exc
    try:
        manifeste = json.loads(brut.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ArchiveError(f"{MANIFESTE_NOM} illisible : {exc}") from exc
    if not isinstance(manifeste, dict):
        raise ArchiveError(f"{MANIFESTE_NOM} invalide (objet JSON attendu)")
    if manifeste.get("format") != FORMAT_ARCHIVE:
        raise ArchiveError(f"Format de manifeste inconnu : {manifeste.get('format')!r}")
    version = manifeste.get("format_version")
    if version != VERSION_FORMAT_ARCHIVE:
        raise ArchiveError(f"Version de format d'archive non gérée : {version!r}")
    return manifeste


def lire_manifeste(octets_zip: bytes) -> dict[str, Any]:
    """Lit le manifeste d'un ZIP. Lève :class:`ArchiveError`."""
    try:
        with zipfile.ZipFile(io.BytesIO(octets_zip)) as zf:
            return _lire_manifeste(zf)
    except zipfile.BadZipFile as exc:
        raise ArchiveError(f"Archive ZIP illisible : {exc}") from exc


def _valider_entree(entree: dict[str, Any], section: str) -> str | None:
    """Contrôle d'une entrée de manifeste. Renvoie le problème, ou ``None``."""
    try:
        membre = _chemin_sur(entree.get("chemin"), prefixe=PREFIXE_MEMBRE)
    except ArchiveError as exc:
        return str(exc)
    attendu = f"{PREFIXE_MEMBRE}/{section}/"
    if not membre.startswith(attendu):
        return f"Entrée de manifeste hors de sa section : {membre!r} (section {section!r})"
    if entree.get("racine") not in RACINES:
        return f"Racine de restauration inconnue : {entree.get('racine')!r}"
    try:
        _chemin_sur(entree.get("relatif"))
        _valider_empreinte(entree.get("sha256"), contexte="manifeste")
    except ArchiveError as exc:
        return str(exc)
    taille = entree.get("taille")
    if not isinstance(taille, int) or taille < 0:
        return f"Taille invalide dans le manifeste pour {membre}"
    return None


def verifier_manifeste(octets_zip: bytes) -> list[str]:
    """Contrôle d'intégrité complet d'un ZIP de sauvegarde.

    Vérifie : ouverture du ZIP (CRC), présence et validité du manifeste,
    chemins hostiles (``..``, absolu), présence de chaque membre, empreinte
    SHA-256 et taille de chaque fichier, empreinte globale.

    Returns:
        Liste des problèmes détectés — **vide = conforme**.
    """
    try:
        zf_context = zipfile.ZipFile(io.BytesIO(octets_zip))
    except zipfile.BadZipFile as exc:
        return [f"Archive ZIP illisible : {exc}"]

    erreurs: list[str] = []
    with zf_context as zf:
        try:
            manifeste = _lire_manifeste(zf)
        except ArchiveError as exc:
            return [str(exc)]

        sections = manifeste.get("sections")
        if not isinstance(sections, dict):
            return [f"{MANIFESTE_NOM} : champ « sections » absent ou invalide"]

        entrees_valides: list[dict[str, Any]] = []
        for section, liste in sections.items():
            if not isinstance(section, str) or not isinstance(liste, list):
                erreurs.append(f"{MANIFESTE_NOM} : section invalide ({section!r})")
                continue
            for entree in liste:
                if not isinstance(entree, dict):
                    erreurs.append(f"{MANIFESTE_NOM} : entrée invalide dans « {section} »")
                    continue
                probleme = _valider_entree(entree, section)
                if probleme is not None:
                    erreurs.append(probleme)
                    continue
                entrees_valides.append(entree)
                try:
                    donnees = zf.read(entree["chemin"])
                except KeyError:
                    erreurs.append(f"Fichier absent de l'archive : {entree['chemin']}")
                    continue
                except (zipfile.BadZipFile, RuntimeError, OSError, zlib.error) as exc:
                    erreurs.append(f"Lecture impossible de {entree['chemin']} : {exc}")
                    continue
                if len(donnees) != entree["taille"]:
                    erreurs.append(f"Taille incohérente : {entree['chemin']}")
                if empreinte(donnees) != entree["sha256"]:
                    erreurs.append(f"Empreinte SHA-256 invalide : {entree['chemin']}")

        attendu = manifeste.get("empreinte_globale")
        if not isinstance(attendu, str):
            erreurs.append(f"{MANIFESTE_NOM} : empreinte globale absente")
        elif calculer_empreinte_globale(entrees_valides) != attendu.lower():
            erreurs.append("Empreinte globale du manifeste invalide — contenu modifié")

    return erreurs
