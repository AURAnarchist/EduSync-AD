"""Tests M32 — Restauration « 1-clic » d'une sauvegarde."""

import io
import json
import sqlite3
import zipfile
from pathlib import Path

from edusync_ad.core.audit import AuditLog
from edusync_ad.core.config import AppConfig, load_config, save_config
from edusync_ad.core.password_vault import PasswordVault
from edusync_ad.core.sauvegarde import (
    generer_backup_complet,
    migrer_donnees_vn_vn1,
    restorer_dans_nouvelle_machine,
)
from edusync_ad.core.sauvegarde import format_archive as fmt
from edusync_ad.core.sauvegarde.migration import VERSION_DONNEES_COURANTE

MDP_COFFRE = "Sup3r$ecret!"


def _machine(tmp_path):
    """Poste source complet : config, journal (1 action), coffre (1 MDP)."""
    repertoire_config = tmp_path / "config"
    repertoire_donnees = tmp_path / "data"
    repertoire_config.mkdir()
    repertoire_donnees.mkdir()

    config = AppConfig(
        domaine_mail="lycee-victor-hugo.fr",
        ou_parente_classes="OU=eleves,DC=lycee,DC=local",
        ldaps_chemin_certificat_ca="/etc/edusync/ad-ca.pem",
    )
    save_config(config, repertoire_config / "config.json")
    (repertoire_config / "label_templates.json").write_text(
        json.dumps({"planches": [{"key": "carte"}]}), encoding="utf-8"
    )
    (repertoire_config / "api_keys.json").write_text(
        json.dumps({"pronote": "CLE-PRONOTE-SECRET-42"}), encoding="utf-8"
    )

    log = AuditLog(repertoire_donnees / "journal.db")
    log.record("creation_compte", "thomas.martin", "succes", "session01")
    log.close()
    vault = PasswordVault(
        repertoire_donnees / "password_vault.sqlite3",
        key_path=repertoire_config / "secret.key",
    )
    vault.store("thomas.martin", MDP_COFFRE)
    vault.close()
    return config, repertoire_config, repertoire_donnees


def _sauvegarder(config, cfg, data, sortie, **options):
    if "password" not in options and "key_path" not in options:
        options["key_path"] = cfg / "secret.key"
    return generer_backup_complet(
        config,
        repertoire_config=cfg,
        repertoire_donnees=data,
        repertoire_sortie=sortie,
        **options,
    )


def _zip_fabrique(entrees: list[dict], **extra) -> bytes:
    """ZIP de sauvegarde fabriqué à la main (scénarios hostiles)."""
    contenu = b"contenu"
    entrees = [dict(e, sha256=fmt.empreinte(contenu), taille=len(contenu)) for e in entrees]
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


def _colonne(table: str, nom: str, base: Path) -> bool:
    with sqlite3.connect(base) as connexion:
        return any(
            ligne[1] == nom
            for ligne in connexion.execute(f"PRAGMA table_info({table})").fetchall()
        )


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


# -- Round-trip --------------------------------------------------------------


def test_round_trip_complet_avec_mot_de_passe(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="M0t$d(e")
    destination = tmp_path / "nouvelle_machine"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "M0t$d(e", str(destination)
    )

    assert restauration["reussite"] is True, restauration["erreurs"]
    assert restauration["erreurs"] == []
    assert restauration["integrite"]["valide"] is True
    assert restauration["integrite"]["verification"] == "complète"
    assert restauration["nb_fichiers"] > 0
    assert "config/config.json" in restauration["elements_restores"]
    assert "data/journal.db" in restauration["elements_restores"]
    assert restauration["destination"] == str(destination)

    # Configuration restaurée à l'identique
    assert load_config(destination / "config" / "config.json") == config
    # Modèles et clés API
    assert json.loads(
        (destination / "config" / "label_templates.json").read_text(encoding="utf-8")
    )["planches"][0]["key"] == "carte"
    assert "pronote" in (destination / "config" / "api_keys.json").read_text(
        encoding="utf-8"
    )
    # Journal restauré (contenu, pas seulement le fichier)
    journal = AuditLog(destination / "data" / "journal.db")
    entrees = journal.query()
    journal.close()
    assert len(entrees) == 1
    assert entrees[0].compte == "thomas.martin"
    assert entrees[0].resultat == "succes"
    # Coffre restauré ET lisible (la clé voyage dans l'archive chiffrée)
    coffre = PasswordVault(
        destination / "data" / "password_vault.sqlite3",
        key_path=destination / "config" / "secret.key",
    )
    assert coffre.get("thomas.martin") == MDP_COFFRE
    coffre.close()
    # Registre de migrations initialisé par la migration post-restauration
    registre = json.loads((destination / "data" / "migrations.json").read_text("utf-8"))
    assert registre["version"] == VERSION_DONNEES_COURANTE


def test_round_trip_avec_cle_locale(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie")
    destination = tmp_path / "restaure"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], None, str(destination), key_path=cfg / "secret.key"
    )

    assert restauration["reussite"] is True, restauration["erreurs"]
    coffre = PasswordVault(
        destination / "data" / "password_vault.sqlite3",
        key_path=destination / "config" / "secret.key",
    )
    assert coffre.get("thomas.martin") == MDP_COFFRE
    coffre.close()


def test_archive_sans_cle_signale_un_coffre_illisible(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    destination = tmp_path / "restaure"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], None, str(destination)
    )

    assert restauration["reussite"] is True, restauration["erreurs"]
    assert any(
        "Coffre MDP restauré sans sa clé" in avis for avis in restauration["avertissements"]
    )
    # Sans secret.key, le coffre restauré est illisible (comportement attendu)
    coffre = PasswordVault(
        destination / "data" / "password_vault.sqlite3",
        key_path=destination / "config" / "secret.key",
    )
    assert coffre.get("thomas.martin") is None
    coffre.close()


def test_certificat_ca_absent_de_la_nouvelle_machine_est_signale(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(tmp_path / "dest")
    )
    assert restauration["reussite"] is True
    assert any(
        "/etc/edusync/ad-ca.pem" in avis for avis in restauration["avertissements"]
    )


def test_restaurer_deux_fois_est_idempotent(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    destination = tmp_path / "restaure"

    premiere = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination)
    )
    avant = (destination / "config" / "config.json").read_bytes()
    seconde = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination)
    )

    assert premiere["reussite"] is True, premiere["erreurs"]
    assert seconde["reussite"] is True, seconde["erreurs"]
    assert (destination / "config" / "config.json").read_bytes() == avant
    # La migration enregistrée n'est pas rejouée : second appel sans effet.
    assert seconde["migrations"]["deja_migre"] is True
    assert seconde["migrations"]["migrations_appliquees"] == []
    registre = json.loads((destination / "data" / "migrations.json").read_text("utf-8"))
    assert registre["version"] == VERSION_DONNEES_COURANTE
    assert len(registre["historique"]) == len(premiere["migrations"]["migrations_appliquees"])


def test_restaurer_sans_migration_laisse_le_registre_chez_l_hote(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination), appliquer_migrations=False
    )

    assert restauration["reussite"] is True, restauration["erreurs"]
    assert restauration["migrations"] is None
    assert not (destination / "data" / "migrations.json").exists()
    assert (destination / "data" / "journal.db").is_file()


# -- Refus -------------------------------------------------------------------


def test_archive_inexistante_refusee(tmp_path):
    restauration = restorer_dans_nouvelle_machine(
        str(tmp_path / "absent.zip.aes"), "p@ss", str(tmp_path / "dest")
    )
    assert restauration["reussite"] is False
    assert any("Archive introuvable" in erreur for erreur in restauration["erreurs"])
    assert restauration["elements_restores"] == []
    assert not (tmp_path / "dest").exists()


def test_mot_de_passe_faux_refuse_sans_ecrire_un_fichier(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "faux", str(destination)
    )

    assert restauration["reussite"] is False
    assert any("mot de passe" in erreur for erreur in restauration["erreurs"])
    assert restauration["elements_restores"] == []
    assert not destination.exists()


def test_archive_chiffree_corrompue_refusee(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    chemin = Path(resultat["archive_chemin"])
    brut = bytearray(chemin.read_bytes())
    brut[-3] ^= 0xFF
    chemin.write_bytes(bytes(brut))
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(
        str(chemin), "p@ss", str(destination)
    )

    assert restauration["reussite"] is False
    assert any("altérée" in erreur for erreur in restauration["erreurs"])
    assert not destination.exists()


def test_archive_non_chiffree_tamponnee_refusee(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = Path(resultat["archive_chemin"])

    # Altération d'un fichier sans mettre le manifeste à jour
    with zipfile.ZipFile(chemin) as source:
        tampon = io.BytesIO()
        with zipfile.ZipFile(tampon, "w") as cible:
            for info in source.infolist():
                donnees = source.read(info.filename)
                if info.filename.endswith("/config.json"):
                    donnees = donnees.replace(b"lycee-victor-hugo.fr", b"lycee-pirate.fr")
                cible.writestr(info.filename, donnees)
    chemin.write_bytes(tampon.getvalue())
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(str(chemin), None, str(destination))

    assert restauration["reussite"] is False
    assert any("intégrité" in erreur for erreur in restauration["erreurs"])
    assert any("Empreinte SHA-256" in detail for detail in restauration["integrite"]["details"])
    assert not destination.exists()


def test_archive_flux_deflate_corrompu_refuse_sans_lever(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", chiffrer=False)
    chemin = Path(resultat["archive_chemin"])
    _casser_deflate(chemin, "payload/configuration/config.json")
    destination = tmp_path / "dest"

    # Membre décompressible : l'erreur zlib doit être reportée dans « erreurs »
    # (contrat « aucune exception »), pas propagée à l'appelant.
    restauration = restorer_dans_nouvelle_machine(str(chemin), None, str(destination))

    assert restauration["reussite"] is False
    assert any("intégrité" in erreur for erreur in restauration["erreurs"])
    assert not destination.exists()


def test_archive_avec_sortie_hors_destination_refusee(tmp_path):
    # Manœuvre « zip-slip » : chemin relatif contenant ..
    entree = {
        "chemin": "payload/configuration/evil.txt",
        "section": "configuration",
        "racine": "config",
        "relatif": "../evil.txt",
    }
    destination = tmp_path / "dest"
    hote = tmp_path / "hostile.zip"
    hote.write_bytes(_zip_fabrique([entree]))

    restauration = restorer_dans_nouvelle_machine(str(hote), None, str(destination))

    assert restauration["reussite"] is False
    assert any("hors archive" in erreur for erreur in restauration["erreurs"])
    assert restauration["elements_restores"] == []
    assert not (tmp_path / "evil.txt").exists()
    assert not destination.exists()


def test_archive_d_une_version_de_donnees_inconnue_refusee(tmp_path):
    hote = tmp_path / "futur.zip"
    hote.write_bytes(_zip_fabrique([], version_donnees=99))
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(str(hote), None, str(destination))

    assert restauration["reussite"] is False
    assert any("version de données 99" in erreur for erreur in restauration["erreurs"])
    assert not destination.exists()


def test_destination_enregistre_d_un_echec_refusee(tmp_path):
    # Registre de la destination illisible : refus avant toute écriture.
    destination = tmp_path / "dest"
    (destination / "data").mkdir(parents=True)
    (destination / "data" / "migrations.json").write_text(
        "{ pas du json", encoding="utf-8"
    )
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination)
    )

    assert restauration["reussite"] is False
    assert any(
        "Registre de migrations illisible" in erreur for erreur in restauration["erreurs"]
    )
    assert not (destination / "config").exists()


# -- Migration intégrée ------------------------------------------------------


def test_restaurer_migre_les_donnees_heritees(tmp_path):
    """Un poste hérité (config à champ unique, journal sans colonnes d'audit,
    coffre en majuscules) est sauvegardé puis restauré : la destination est
    migrée vers la version de schéma courante."""
    repertoire_config = tmp_path / "config"
    repertoire_donnees = tmp_path / "data"
    repertoire_config.mkdir()
    repertoire_donnees.mkdir()

    # config.json hérité
    (repertoire_config / "config.json").write_text(
        json.dumps({"identifiant_format": "prenom.nom", "theme": "sombre"}),
        encoding="utf-8",
    )
    # journal hérité : colonnes utilisateur / domaine absentes
    with sqlite3.connect(repertoire_donnees / "journal.db") as connexion:
        connexion.execute(
            "CREATE TABLE actions (id INTEGER PRIMARY KEY, timestamp TEXT, action_type TEXT,"
            " compte TEXT, ou_source TEXT, ou_destination TEXT, resultat TEXT,"
            " session_id TEXT, simulation INTEGER, detail TEXT)"
        )
        connexion.execute(
            "INSERT INTO actions (timestamp, action_type, compte, resultat, session_id,"
            " simulation) VALUES ('2020-01-01', 'creation_compte', 'ancien.compte',"
            " 'succes', 'vieille', 0)"
        )
        connexion.execute(
            "CREATE TABLE pending_deletions (id INTEGER PRIMARY KEY, user_dn TEXT,"
            " sam_account_name TEXT, nom_complet TEXT, moved_at TEXT, session_id TEXT)"
        )
        connexion.commit()
    # coffre hérité : sam en majuscules
    with sqlite3.connect(repertoire_donnees / "password_vault.sqlite3") as connexion:
        connexion.execute(
            "CREATE TABLE mots_de_passe (sam TEXT PRIMARY KEY, secret TEXT,"
            " horodatage TEXT)"
        )
        connexion.execute(
            "INSERT INTO mots_de_passe VALUES ('Thomas.Martin', 'jeton-chiffre', '2020-01-01')"
        )
        connexion.commit()

    config = AppConfig()
    resultat = generer_backup_complet(
        config,
        repertoire_config=repertoire_config,
        repertoire_donnees=repertoire_donnees,
        repertoire_sortie=tmp_path / "sortie",
        password="p@ss",
    )
    destination = tmp_path / "dest"
    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination)
    )

    assert restauration["reussite"] is True, restauration["erreurs"]
    migrations = restauration["migrations"]
    assert migrations["version_depart"] == 1
    assert migrations["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert len(migrations["migrations_appliquees"]) == 3

    # config.json migré (champ unique scindé, thème conservé)
    donnees = json.loads((destination / "config" / "config.json").read_text("utf-8"))
    assert donnees["identifiant_format_eleve"] == "prenom.nom"
    assert donnees["identifiant_format_personnel"] == "prenom.nom"
    assert "identifiant_format" not in donnees
    assert donnees["theme"] == "sombre"
    # journal : colonnes ajoutées, contenu conservé
    assert _colonne("actions", "utilisateur", destination / "data" / "journal.db")
    assert _colonne("actions", "domaine", destination / "data" / "journal.db")
    assert _colonne("pending_deletions", "delai_jours", destination / "data" / "journal.db")
    # coffre : sam normalisés, secret inchangé
    with sqlite3.connect(destination / "data" / "password_vault.sqlite3") as connexion:
        sams = [ligne[0] for ligne in connexion.execute("SELECT sam FROM mots_de_passe")]
        secret = connexion.execute("SELECT secret FROM mots_de_passe").fetchone()[0]
    assert sams == ["thomas.martin"]
    assert secret == "jeton-chiffre"
    # Le poste source n'a pas été modifié par la restauration
    assert json.loads(
        (repertoire_config / "config.json").read_text(encoding="utf-8")
    )["identifiant_format"] == "prenom.nom"


def test_migration_peut_etre_evitee_puis_relancee(tmp_path):
    config, cfg, data = _machine(tmp_path)
    resultat = _sauvegarder(config, cfg, data, tmp_path / "sortie", password="p@ss")
    destination = tmp_path / "dest"

    restauration = restorer_dans_nouvelle_machine(
        resultat["archive_chemin"], "p@ss", str(destination), appliquer_migrations=False
    )
    assert restauration["reussite"] is True
    assert not (destination / "data" / "migrations.json").exists()

    rattrapage = migrer_donnees_vn_vn1(destination / "config", destination / "data")
    assert rattrapage["reussite"] is True
    assert rattrapage["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert (destination / "data" / "migrations.json").is_file()
