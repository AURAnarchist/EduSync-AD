"""Tests M32 — Génération de sauvegarde complète et vérification d'intégrité."""

import io
import json
import zipfile
from pathlib import Path

import pytest

from edusync_ad import __version__
from edusync_ad.core.audit import AuditLog, default_db_path
from edusync_ad.core.config import AppConfig, save_config
from edusync_ad.core.password_vault import PasswordVault, default_vault_path
from edusync_ad.core.sauvegarde import generer_backup_complet, verifier_integrite_backup
from edusync_ad.core.sauvegarde import format_archive as fmt
from edusync_ad.core.sauvegarde.backup import NOM_COFFRE, NOM_JOURNAL

CLE_API = "CLE-PRONOTE-SECRET-42"
MDP_COFFRE = "Sup3r$ecret!"


def _machine(tmp_path, *, journal: bool = True, coffre: bool = True):
    """Construit un poste source complet (config, journal, coffre, modèles)."""
    repertoire_config = tmp_path / "config"
    repertoire_donnees = tmp_path / "data"
    repertoire_config.mkdir()
    repertoire_donnees.mkdir()

    config = AppConfig(
        domaine_mail="lycee-victor-hugo.fr",
        ou_parente_classes="OU=eleves,DC=lycee,DC=local",
        ldaps_chemin_certificat_ca=str(repertoire_config / "lycee-ca.pem"),
    )
    save_config(config, repertoire_config / "config.json")
    (repertoire_config / "label_templates.json").write_text(
        json.dumps({"planches": [{"key": "carte"}]}), encoding="utf-8"
    )
    (repertoire_config / "group_templates.json").write_text(
        json.dumps({"templates": []}), encoding="utf-8"
    )
    (repertoire_config / "api_keys.json").write_text(
        json.dumps({"pronote": CLE_API}), encoding="utf-8"
    )
    (repertoire_config / "lycee-ca.pem").write_text(
        "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n",
        encoding="utf-8",
    )

    if journal:
        log = AuditLog(repertoire_donnees / NOM_JOURNAL)
        log.record("creation_compte", "thomas.martin", "succes", "session01")
        log.close()
    if coffre:
        vault = PasswordVault(
            repertoire_donnees / NOM_COFFRE, key_path=repertoire_config / "secret.key"
        )
        vault.store("thomas.martin", MDP_COFFRE)
        vault.close()
    return config, repertoire_config, repertoire_donnees


def _backup(config, repertoire_config, repertoire_donnees, repertoire_sortie, **options):
    """Sauvegarde d'un poste fabriqué par :func:`_machine` : sans mot de passe,
    la clé locale du poste est utilisée explicitement (jamais celle du poste
    réel sur lequel tournent les tests)."""
    if "password" not in options and "key_path" not in options:
        options["key_path"] = repertoire_config / "secret.key"
    return generer_backup_complet(
        config,
        repertoire_config=repertoire_config,
        repertoire_donnees=repertoire_donnees,
        repertoire_sortie=repertoire_sortie,
        **options,
    )


def _chemin(resultat) -> Path:
    return Path(resultat["archive_chemin"])


def _ouvrir(resultat, **kwargs) -> bytes:
    return fmt.ouvrir_archive(_chemin(resultat), **kwargs)


def _manifeste(resultat, **kwargs) -> dict:
    return fmt.lire_manifeste(_ouvrir(resultat, **kwargs))


def _membres(resultat, **kwargs) -> list[str]:
    with zipfile.ZipFile(io.BytesIO(_ouvrir(resultat, **kwargs))) as zf:
        return zf.namelist()


def _casser_deflate(chemin: Path, nom_membre: str) -> None:
    """Corrompt les données compressées d'un membre d'un ZIP clair.

    L'en-tête local et le répertoire central restent intacts : la décompression
    du membre échoue alors dans ``zlib`` **avant** tout contrôle de CRC.
    """
    brut = bytearray(chemin.read_bytes())
    with zipfile.ZipFile(chemin) as zf:
        info = zf.getinfo(nom_membre)
    assert info.compress_type == zipfile.ZIP_DEFLATED, "membre non compressé"
    offset = info.header_offset
    nom_len = int.from_bytes(brut[offset + 26 : offset + 28], "little")
    extra_len = int.from_bytes(brut[offset + 28 : offset + 30], "little")
    donnees = offset + 30 + nom_len + extra_len
    for indice in range(donnees, min(donnees + 60, len(brut))):
        brut[indice] ^= 0xFF
    chemin.write_bytes(bytes(brut))


# -- Génération --------------------------------------------------------------


def test_backup_defaut_est_chiffree_avec_la_cle_locale(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie")
    chemin = _chemin(resultat)

    assert chemin.is_file()
    # Enveloppe chiffrée, pas un ZIP ouvrable tel quel
    assert not zipfile.is_zipfile(chemin)
    assert chemin.read_bytes().startswith(fmt.MAGIC_ENVELOPPE + b"\n")
    assert resultat["verification"]["chiffre"] is True
    assert resultat["verification"]["algorithme"] == "AES-256-GCM"
    assert resultat["verification"]["source_cle"] == "locale"
    assert resultat["verification"]["kdf"]["algorithme"] == fmt.KDF_CLE_LOCALE
    assert resultat["taille_octets"] == chemin.stat().st_size
    assert resultat["taille_octets"] > 0
    assert resultat["nb_fichiers"] > 0
    assert "mise_en_garde" not in resultat["verification"]
    # Aucun secret en clair dans l'archive chiffrée
    brut = chemin.read_bytes()
    assert CLE_API.encode() not in brut
    assert b"lycee-victor-hugo.fr" not in brut
    assert MDP_COFFRE.encode() not in brut


def test_backup_avec_mot_de_passe_est_portable(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="M0t$d(e")
    chemin = _chemin(resultat)

    assert chemin.suffix == ".aes"
    assert resultat["verification"]["source_cle"] == "mot_de_passe"
    assert resultat["verification"]["kdf"]["algorithme"] == fmt.KDF_MOT_DE_PASSE
    assert resultat["verification"]["kdf"]["iterations"] >= 100_000
    assert CLE_API.encode() not in chemin.read_bytes()

    # Portable : la clé du coffre voyage DANS l'archive chiffrée.
    membres = _membres(resultat, mot_de_passe="M0t$d(e")
    assert "payload/coffre/secret.key" in membres


def test_backup_sans_chiffrement_exclut_la_cle_et_met_en_garde(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = _chemin(resultat)

    assert zipfile.is_zipfile(chemin)
    assert resultat["verification"]["chiffre"] is False
    assert resultat["verification"]["algorithme"] == "aucun"
    assert "non chiffrée" in resultat["verification"]["mise_en_garde"]
    assert "secret.key" in resultat["verification"]["mise_en_garde"]
    assert any("secret.key" in avertissement for avertissement in resultat["avertissements"])

    assert not any(nom.endswith("secret.key") for nom in _membres(resultat))
    with zipfile.ZipFile(chemin) as zf:
        manifeste = json.loads(zf.read("manifest.json"))
    assert manifeste["options"]["chiffre"] is False
    # Un ZIP clair reste vérifiable sans clé
    assert verifier_integrite_backup(str(chemin))["valide"] is True


def test_un_mot_de_passe_force_toujours_le_chiffrement(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(
        config, cfg, data, tmp_path / "sortie", chiffrer=False, password="p@ss"
    )
    assert resultat["verification"]["chiffre"] is True
    assert not zipfile.is_zipfile(_chemin(resultat))


def test_options_include_controlent_les_sections(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(
        config,
        cfg,
        data,
        tmp_path / "sortie",
        password="p@ss",
        include_journal=False,
        include_coffre=False,
        include_rgpd=False,
    )

    assert resultat["inclus"] == ["configuration", "modeles", "certificats"]
    assert resultat["config"]["inclure_journal"] is False
    assert resultat["config"]["inclure_coffre"] is False

    manifeste = _manifeste(resultat, mot_de_passe="p@ss")
    assert "journal" not in manifeste["sections"]
    assert "coffre" not in manifeste["sections"]
    assert not any(
        nom.endswith(NOM_JOURNAL) or nom.endswith(NOM_COFFRE)
        for nom in _membres(resultat, mot_de_passe="p@ss")
    )


def test_fichiers_absents_sont_signales_sans_faire_echouer(tmp_path):
    config = AppConfig()
    repertoire_config = tmp_path / "config"
    repertoire_donnees = tmp_path / "data"
    repertoire_config.mkdir()
    repertoire_donnees.mkdir()

    resultat = _backup(config, repertoire_config, repertoire_donnees, tmp_path / "sortie")

    # La clé de chiffrement voyage avec « coffre » même sans coffre : elle
    # protège aussi connection.local.json et domaines.json du dossier config.
    assert resultat["inclus"] == ["configuration", "coffre"]
    joined = " ".join(resultat["avertissements"])
    assert NOM_JOURNAL in joined
    assert NOM_COFFRE in joined
    assert "certificat" in joined
    assert "RGPD" in joined
    # L'archive reste valide malgré les absences
    controle = verifier_integrite_backup(
        resultat["archive_chemin"], key_path=repertoire_config / "secret.key"
    )
    assert controle["valide"] is True


def test_sauvegarde_totalement_vide_reste_verifiable(tmp_path):
    resultat = _backup(
        AppConfig(),
        tmp_path / "inexistant",
        tmp_path / "inexistant",
        tmp_path / "sortie",
        include_config=False,
        include_modeles=False,
        include_certificats=False,
        include_journal=False,
        include_coffre=False,
        include_rgpd=False,
    )
    assert resultat["nb_fichiers"] == 0
    assert resultat["inclus"] == []
    controle = verifier_integrite_backup(
        resultat["archive_chemin"], key_path=tmp_path / "inexistant" / "secret.key"
    )
    assert controle["valide"] is True
    assert controle["nb_fichiers"] == 0


def test_sortie_inaccessible_leve_archive_error(tmp_path):
    config, cfg, data = _machine(tmp_path)
    fichier = tmp_path / "ce_n_est_pas_un_dossier"
    fichier.write_text("blocage", encoding="utf-8")

    with pytest.raises(fmt.ArchiveError, match="Répertoire de sortie inaccessible"):
        _backup(config, cfg, data, fichier)

    # Variante : sous-dossier d'un fichier → échec de création du chemin.
    with pytest.raises(fmt.ArchiveError, match="Répertoire de sortie inaccessible"):
        _backup(config, cfg, data, fichier / "sous")


def test_fichier_collecte_illisible_leve_archive_error(tmp_path, monkeypatch):
    config, cfg, data = _machine(tmp_path)

    def _enraye(source, cible):
        raise OSError("disque retiré pendant la collecte")

    monkeypatch.setattr("edusync_ad.core.sauvegarde.backup._copier", _enraye)
    with pytest.raises(fmt.ArchiveError, match="Collecte impossible"):
        _backup(config, cfg, data, tmp_path / "sortie")


def test_cle_locale_inaccessible_leve_archive_error(tmp_path, monkeypatch):
    config, cfg, data = _machine(tmp_path)

    def _enraye(key_path=None):
        raise OSError("répertoire en lecture seule")

    monkeypatch.setattr("edusync_ad.core.sauvegarde.backup.fmt.charger_cle_locale", _enraye)
    with pytest.raises(fmt.ArchiveError, match="Clé locale inaccessible"):
        _backup(config, cfg, data, tmp_path / "sortie")


def test_rapport_rgpd_fourni_est_inclus(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(
        config,
        cfg,
        data,
        tmp_path / "sortie",
        password="p@ss",
        rapport_rgpd={"inventaire": {"total_comptes": 12}},
    )
    assert "rgpd" in resultat["inclus"]
    with zipfile.ZipFile(io.BytesIO(_ouvrir(resultat, mot_de_passe="p@ss"))) as zf:
        rapport = json.loads(zf.read("payload/rgpd/rapports/rapport_rgpd.json"))
    assert rapport == {"inventaire": {"total_comptes": 12}}


def test_manifeste_declare_l_application_et_la_version_de_donnees(tmp_path):
    config, cfg, data = _machine(tmp_path)
    (data / "migrations.json").write_text(
        json.dumps({"format": "edusync-migrations", "version": 3, "historique": []}),
        encoding="utf-8",
    )
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="p@ss")
    manifeste = _manifeste(resultat, mot_de_passe="p@ss")

    assert manifeste["format"] == fmt.FORMAT_ARCHIVE
    assert manifeste["format_version"] == fmt.VERSION_FORMAT_ARCHIVE
    assert manifeste["version_application"] == __version__
    assert manifeste["version_donnees"] == 3
    # Empreinte globale conforme + empreinte du ZIP exacte
    toutes = [entree for liste in manifeste["sections"].values() for entree in liste]
    assert manifeste["empreinte_globale"] == fmt.calculer_empreinte_globale(toutes)
    assert resultat["empreinte_zip"] == fmt.empreinte(
        _ouvrir(resultat, mot_de_passe="p@ss")
    )


def test_noms_de_bases_conformes_aux_helpers_applicatifs():
    # Garde-fou contre la dérive des littéraux partagés avec audit/password_vault.
    assert NOM_JOURNAL == default_db_path().name
    assert NOM_COFFRE == default_vault_path().name


# -- Vérification d'intégrité ------------------------------------------------


def test_verification_complete_avec_mot_de_passe(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="p@ss")

    controle = verifier_integrite_backup(resultat["archive_chemin"], mot_de_passe="p@ss")
    assert controle["valide"] is True
    assert controle["verification"] == "complète"
    assert controle["chiffre"] is True
    assert set(controle["contient"]) == set(resultat["inclus"])
    assert controle["nb_fichiers"] == resultat["nb_fichiers"]
    assert controle["version_application"] == __version__
    assert controle["empreinte_zip"] == resultat["empreinte_zip"]
    assert controle["erreur"] is None


def test_verification_sans_mot_de_passe_se_limite_a_l_enveloppe(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="p@ss")

    controle = verifier_integrite_backup(resultat["archive_chemin"])
    assert controle["valide"] is True
    assert controle["verification"] == "enveloppe"
    assert any("mot de passe" in avis for avis in controle["avertissements"])


def test_verification_avec_cle_locale_est_complete(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie")
    controle = verifier_integrite_backup(
        resultat["archive_chemin"], key_path=cfg / "secret.key"
    )
    assert controle["valide"] is True
    assert controle["verification"] == "complète"


def test_verification_sans_cle_signale_la_limite(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie")
    controle = verifier_integrite_backup(
        resultat["archive_chemin"], key_path=tmp_path / "cle_absente.key"
    )
    assert controle["valide"] is True
    assert controle["verification"] == "enveloppe"
    assert any("Clé locale introuvable" in avis for avis in controle["avertissements"])


def test_verification_fichier_introuvable(tmp_path):
    controle = verifier_integrite_backup(str(tmp_path / "absent.zip"))
    assert controle["valide"] is False
    assert "introuvable" in controle["erreur"]
    assert controle["verification"] == "aucune"


def test_verification_fichier_quelconque_refuse(tmp_path):
    gibberish = tmp_path / "pas_une_archive.bin"
    gibberish.write_bytes(b"bonjour, je ne suis ni un zip ni une enveloppe")
    controle = verifier_integrite_backup(str(gibberish))
    assert controle["valide"] is False
    assert "non reconnu" in controle["erreur"]


def test_verification_empreinte_d_un_fichier_modifiee_refusee(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = _chemin(resultat)

    # Réécriture du ZIP en modifiant config.json (manifeste inchangé).
    with zipfile.ZipFile(chemin) as zf_source:
        tampon = io.BytesIO()
        with zipfile.ZipFile(tampon, "w") as zf_cible:
            for info in zf_source.infolist():
                donnees = zf_source.read(info.filename)
                if info.filename.endswith("/config.json"):
                    donnees = donnees.replace(b"lycee-victor-hugo.fr", b"lycee-pirate.fr")
                zf_cible.writestr(info.filename, donnees)
    chemin.write_bytes(tampon.getvalue())

    controle = verifier_integrite_backup(str(chemin))
    assert controle["valide"] is False
    assert any("Empreinte SHA-256" in detail for detail in controle["details"])
    assert "config.json" in controle["erreur"]


def test_verification_contenu_chiffre_modifie_refuse_sans_cle(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="p@ss")
    chemin = _chemin(resultat)

    brut = bytearray(chemin.read_bytes())
    brut[-1] ^= 0xFF  # altération du jeton chiffré, en-tête intact
    chemin.write_bytes(bytes(brut))

    controle = verifier_integrite_backup(str(chemin))
    assert controle["valide"] is False
    assert "altérée" in controle["erreur"]


def test_verification_version_de_format_inconnue_refusee(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", password="p@ss")
    chemin = _chemin(resultat)

    brut = chemin.read_bytes()
    premiere, separateur, reste = brut.partition(b"\n")
    entete_json, _, corps = reste.partition(b"\n")
    entete = json.loads(entete_json.decode("utf-8"))
    entete["format_version"] = 99
    chemin.write_bytes(
        premiere + separateur + json.dumps(entete).encode("utf-8") + b"\n" + corps
    )

    controle = verifier_integrite_backup(str(chemin))
    assert controle["valide"] is False
    assert "Version de format d'archive non gérée : 99" in controle["erreur"]


def test_verification_manifeste_absent_refuse(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = _chemin(resultat)

    with zipfile.ZipFile(chemin) as zf_source:
        tampon = io.BytesIO()
        with zipfile.ZipFile(tampon, "w") as zf_cible:
            for info in zf_source.infolist():
                if info.filename == "manifest.json":
                    continue
                zf_cible.writestr(info.filename, zf_source.read(info.filename))
    chemin.write_bytes(tampon.getvalue())

    controle = verifier_integrite_backup(str(chemin))
    assert controle["valide"] is False
    assert "manifest.json absent" in controle["erreur"]


def test_verification_flux_deflate_corrompu_est_signale_sans_lever(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _backup(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = _chemin(resultat)
    _casser_deflate(chemin, "payload/configuration/config.json")

    # Un membre illisible doit être CONSIGNÉ, jamais levé : la fonction
    # renvoie un rapport, elle n'est pas censée échouer.
    controle = verifier_integrite_backup(str(chemin))

    assert controle["valide"] is False
    assert controle["erreur"]
    assert any("Lecture impossible" in detail for detail in controle["details"])


# -- Garde-fous de bas niveau ------------------------------------------------


def test_validation_empreinte_invalide_leve_erreur():
    with pytest.raises(fmt.ArchiveError):
        fmt._valider_empreinte("pas-une-empreinte", contexte="manifeste")


def test_lire_enveloppe_refuse_un_fichier_tronque(tmp_path):
    tronque = tmp_path / "tronquee.zip.aes"
    tronque.write_bytes(fmt.MAGIC_ENVELOPPE + b"\n")
    with pytest.raises(fmt.ArchiveError):
        fmt.lire_enveloppe(tronque)


def test_derivation_de_cle_refuse_un_nombre_d_iterations_absurde():
    with pytest.raises(fmt.ArchiveError):
        fmt.deriver_cle_mot_de_passe("secret", b"0" * 16, fmt.KDF_ITERATIONS_MAXIMUM + 1)


def test_verifier_manifeste_refuse_un_chemin_hors_archive():
    entree = {
        "chemin": "payload/configuration/evil.txt",
        "section": "configuration",
        "racine": "config",
        "relatif": "../evil.txt",
        "sha256": fmt.empreinte(b"evil"),
        "taille": 4,
    }
    donnees = _zip_fabrique([entree])
    erreurs = fmt.verifier_manifeste(donnees)
    assert any("hors archive" in erreur for erreur in erreurs)


def _zip_fabrique(entrees: list[dict], **extra) -> bytes:
    """Construit un ZIP de sauvegarde à la main (scénarios hostiles).

    Les empreintes sont recalculées sur le contenu réellement écrit : seuls
    les champs laissés tels quels (``relatif`` hostile, version…) déclenchent
    les rejets.
    """
    contenu = b"contenu"
    entrees = [
        dict(entree, sha256=fmt.empreinte(contenu), taille=len(contenu))
        for entree in entrees
    ]
    manifeste = {
        "format": fmt.FORMAT_ARCHIVE,
        "format_version": fmt.VERSION_FORMAT_ARCHIVE,
        "version_application": "0.0.0",
        "version_donnees": None,
        "sections": {"configuration": entrees},
        "empreinte_globale": fmt.calculer_empreinte_globale(entrees),
        "options": {},
        "avertissements": [],
        **extra,
    }
    tampon = io.BytesIO()
    with zipfile.ZipFile(tampon, "w") as zf:
        zf.writestr("manifest.json", json.dumps(manifeste, ensure_ascii=False))
        for entree in entrees:
            zf.writestr(entree["chemin"], contenu)
    return tampon.getvalue()
