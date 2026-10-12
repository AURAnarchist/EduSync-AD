"""Tests M32 — Migration des données vN → vN+1 (registre + table ordonnée)."""

import json
import sqlite3

import pytest

from edusync_ad.core.sauvegarde import (
    MIGRATIONS,
    VERSION_DONNEES_COURANTE,
    detecter_version_donnees,
    migrer_donnees_vn_vn1,
)
from edusync_ad.core.sauvegarde.migration import (
    FORMAT_REGISTRE,
    VERSION_INITIALE_DONNEES,
    MigrationError,
    lire_registre,
    registre_path,
)


def _machine_heritagee(tmp_path):
    """Poste hérité : config à champ unique, journal sans colonnes d'audit,
    coffre avec sam en majuscules, aucun registre de migrations."""
    repertoire_config = tmp_path / "config"
    repertoire_donnees = tmp_path / "data"
    repertoire_config.mkdir()
    repertoire_donnees.mkdir()

    (repertoire_config / "config.json").write_text(
        json.dumps({"identifiant_format": "prenom.nom", "theme": "sombre"}),
        encoding="utf-8",
    )
    with sqlite3.connect(repertoire_donnees / "journal.db") as connexion:
        connexion.execute(
            "CREATE TABLE actions (id INTEGER PRIMARY KEY, timestamp TEXT,"
            " action_type TEXT, compte TEXT, ou_source TEXT, ou_destination TEXT,"
            " resultat TEXT, session_id TEXT, simulation INTEGER, detail TEXT)"
        )
        connexion.execute(
            "INSERT INTO actions (timestamp, action_type, compte, resultat,"
            " session_id, simulation) VALUES ('2020-01-01', 'creation_compte',"
            " 'ancien.compte', 'succes', 'vieille', 0)"
        )
        connexion.execute(
            "CREATE TABLE pending_deletions (id INTEGER PRIMARY KEY, user_dn TEXT,"
            " sam_account_name TEXT, nom_complet TEXT, moved_at TEXT, session_id TEXT)"
        )
        connexion.commit()
    with sqlite3.connect(repertoire_donnees / "password_vault.sqlite3") as connexion:
        connexion.execute(
            "CREATE TABLE mots_de_passe (sam TEXT PRIMARY KEY, secret TEXT,"
            " horodatage TEXT)"
        )
        connexion.execute(
            "INSERT INTO mots_de_passe VALUES ('Thomas.Martin', 'jeton-1', '2020-01-01')"
        )
        connexion.commit()
    return repertoire_config, repertoire_donnees


def _migrer(repertoire_config, repertoire_donnees, **options):
    return migrer_donnees_vn_vn1(repertoire_config, repertoire_donnees, **options)


def _colonnes(base, table):
    with sqlite3.connect(base) as connexion:
        return {ligne[1] for ligne in connexion.execute(f"PRAGMA table_info({table})")}


# -- Application séquentielle ------------------------------------------------


def test_migration_applique_les_trois_etapes(tmp_path):
    config, data = _machine_heritagee(tmp_path)

    rapport = _migrer(config, data)

    assert rapport["reussite"] is True, rapport["erreurs"]
    assert rapport["version_depart"] == VERSION_INITIALE_DONNEES
    assert rapport["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert rapport["deja_migre"] is False
    assert rapport["migrations_appliquees"] == [m.identifiant for m in MIGRATIONS]
    assert rapport["migrations_en_attente"] == []
    assert rapport["erreurs"] == []
    # Rapport détaillé : une entrée par étape, statut et éléments renseignés
    assert [entree["statut"] for entree in rapport["rapport"]] == ["appliquée"] * 3
    assert all(entree["elements"] for entree in rapport["rapport"])

    # config.json : champ unique scindé, thème conservé
    donnees = json.loads((config / "config.json").read_text(encoding="utf-8"))
    assert donnees["identifiant_format_eleve"] == "prenom.nom"
    assert donnees["identifiant_format_personnel"] == "prenom.nom"
    assert "identifiant_format" not in donnees
    assert donnees["theme"] == "sombre"
    assert "config.identifiant_format_eleve" in rapport["elements_nouveaux"]

    # journal : colonnes ajoutées, ligne conservée
    colonnes = _colonnes(data / "journal.db", "actions")
    assert {"utilisateur", "domaine"} <= colonnes
    assert "delai_jours" in _colonnes(data / "journal.db", "pending_deletions")
    with sqlite3.connect(data / "journal.db") as connexion:
        assert connexion.execute("SELECT COUNT(*) FROM actions").fetchone()[0] == 1

    # coffre : sam normalisés, secret inchangé
    with sqlite3.connect(data / "password_vault.sqlite3") as connexion:
        sams = [ligne[0] for ligne in connexion.execute("SELECT sam FROM mots_de_passe")]
        assert sams == ["thomas.martin"]
        assert connexion.execute("SELECT secret FROM mots_de_passe").fetchone()[0] == "jeton-1"

    # registre : version courante + historique des trois étapes
    registre = lire_registre(registre_path(data))
    assert registre["format"] == FORMAT_REGISTRE
    assert registre["version"] == VERSION_DONNEES_COURANTE
    assert [entree["identifiant"] for entree in registre["historique"]] == [
        m.identifiant for m in MIGRATIONS
    ]
    assert all(entree["horodatage"] for entree in registre["historique"])


def test_second_appel_est_un_no_op(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    premiere = _migrer(config, data)
    config_avant = (config / "config.json").read_bytes()
    journal_avant = (data / "journal.db").read_bytes()

    seconde = _migrer(config, data)

    assert premiere["reussite"] is True
    assert seconde["reussite"] is True
    assert seconde["deja_migre"] is True
    assert seconde["migrations_appliquees"] == []
    assert seconde["migrations_en_attente"] == []
    assert seconde["version_depart"] == seconde["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert seconde["elements_migres"] == []
    # Aucune donnée réécrite
    assert (config / "config.json").read_bytes() == config_avant
    assert (data / "journal.db").read_bytes() == journal_avant
    registre = lire_registre(registre_path(data))
    assert len(registre["historique"]) == len(premiere["migrations_appliquees"])


def test_machine_sans_donnees_migre_sans_effet(tmp_path):
    config = tmp_path / "config"
    data = tmp_path / "data"
    config.mkdir()
    data.mkdir()

    rapport = _migrer(config, data)

    assert rapport["reussite"] is True, rapport["erreurs"]
    assert rapport["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert len(rapport["migrations_appliquees"]) == 3
    assert not (config / "config.json").exists()
    assert not (data / "journal.db").exists()
    # Le registre est tout de même initialisé
    assert detecter_version_donnees(registre_path(data)) == VERSION_DONNEES_COURANTE


def test_migration_reprend_apres_un_etap_echouee(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    (config / "config.json").write_text("{ json cassé", encoding="utf-8")

    echec = _migrer(config, data)

    assert echec["reussite"] is False
    assert echec["version_arrivee"] == VERSION_INITIALE_DONNEES
    assert echec["migrations_appliquees"] == []
    assert any("0001_config_format_identifiant" in erreur for erreur in echec["erreurs"])
    assert echec["rapport"][0]["statut"] == "échec"
    # Aucun registre écrit : la reprise se fera depuis le début
    assert not registre_path(data).exists()

    # Données réparées → la migration complète aboutit
    (config / "config.json").write_text(
        json.dumps({"identifiant_format": "prenom.nom"}), encoding="utf-8"
    )
    reprise = _migrer(config, data)

    assert reprise["reussite"] is True, reprise["erreurs"]
    assert reprise["version_arrivee"] == VERSION_DONNEES_COURANTE
    assert reprise["migrations_appliquees"] == [m.identifiant for m in MIGRATIONS]


def test_doublons_de_coffre_sont_fusionnes(tmp_path):
    config = tmp_path / "config"
    data = tmp_path / "data"
    config.mkdir()
    data.mkdir()
    with sqlite3.connect(data / "password_vault.sqlite3") as connexion:
        connexion.execute(
            "CREATE TABLE mots_de_passe (sam TEXT PRIMARY KEY, secret TEXT,"
            " horodatage TEXT)"
        )
        connexion.execute(
            "INSERT INTO mots_de_passe VALUES ('Thomas.Martin', 'ancien', '2020-01-01')"
        )
        connexion.execute(
            "INSERT INTO mots_de_passe VALUES ('thomas.martin', 'recent', '2024-01-01')"
        )
        connexion.commit()

    rapport = _migrer(config, data)

    assert rapport["reussite"] is True, rapport["erreurs"]
    with sqlite3.connect(data / "password_vault.sqlite3") as connexion:
        lignes = connexion.execute("SELECT sam, secret FROM mots_de_passe").fetchall()
    # Le doublon hérité fusionne sur la dernière écriture
    assert lignes == [("thomas.martin", "recent")]
    assert any("doublon" in element for element in rapport["elements_migres"])


# -- Refus explicites --------------------------------------------------------


def test_version_de_donnees_inconnue_est_refusee(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    registre_path(data).parent.mkdir(exist_ok=True)
    registre_path(data).write_text(
        json.dumps({"format": FORMAT_REGISTRE, "version": 99, "historique": []}),
        encoding="utf-8",
    )
    config_avant = (config / "config.json").read_bytes()

    rapport = _migrer(config, data)

    assert rapport["reussite"] is False
    assert any("Version de données inconnue : 99" in erreur for erreur in rapport["erreurs"])
    assert rapport["migrations_appliquees"] == []
    # Rien n'a été touché
    assert (config / "config.json").read_bytes() == config_avant
    assert json.loads(registre_path(data).read_text(encoding="utf-8"))["version"] == 99


def test_retrogradation_est_refusee(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    _migrer(config, data)

    rapport = _migrer(config, data, version_cible=VERSION_INITIALE_DONNEES)

    assert rapport["reussite"] is False
    assert any("Rétrogradation refusée" in erreur for erreur in rapport["erreurs"])
    assert detecter_version_donnees(registre_path(data)) == VERSION_DONNEES_COURANTE


def test_version_cible_inconnue_est_refusee(tmp_path):
    config, data = _machine_heritagee(tmp_path)

    rapport = _migrer(config, data, version_cible=99)

    assert rapport["reussite"] is False
    assert any("Version cible inconnue : 99" in erreur for erreur in rapport["erreurs"])
    assert not registre_path(data).exists()


def test_registre_corrompu_est_refuse(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    registre_path(data).write_text("ceci n'est pas du json", encoding="utf-8")

    rapport = _migrer(config, data)

    assert rapport["reussite"] is False
    assert any("Registre de migrations illisible" in erreur for erreur in rapport["erreurs"])
    assert rapport["migrations_appliquees"] == []
    with pytest.raises(MigrationError):
        detecter_version_donnees(registre_path(data))


def test_registre_de_format_inconnu_est_refuse(tmp_path):
    data = tmp_path / "data"
    data.mkdir()
    registre_path(data).write_text(
        json.dumps({"format": "autre-outil", "version": 2, "historique": []}),
        encoding="utf-8",
    )
    with pytest.raises(MigrationError):
        lire_registre(registre_path(data))


# -- Mode test & détection ---------------------------------------------------


def test_mode_test_detaille_sans_ecriture(tmp_path):
    config, data = _machine_heritagee(tmp_path)
    config_avant = (config / "config.json").read_bytes()

    rapport = _migrer(config, data, mode_test=True)

    assert rapport["reussite"] is True
    assert rapport["migrations_appliquees"] == []
    assert rapport["migrations_en_attente"] == [m.identifiant for m in MIGRATIONS]
    assert rapport["version_arrivee"] == VERSION_INITIALE_DONNEES
    assert [entree["statut"] for entree in rapport["rapport"]] == ["à appliquer"] * 3
    assert any("Mode test" in avis for avis in rapport["avertissements"])
    # Aucune écriture
    assert not registre_path(data).exists()
    assert (config / "config.json").read_bytes() == config_avant
    with sqlite3.connect(data / "journal.db") as connexion:
        colonnes = {ligne[1] for ligne in connexion.execute("PRAGMA table_info(actions)")}
    assert "utilisateur" not in colonnes


def test_detection_de_version_sans_registre(tmp_path):
    data = tmp_path / "data"
    data.mkdir()
    assert detecter_version_donnees(registre_path(data)) == VERSION_INITIALE_DONNEES

    _migrer(tmp_path / "config", data)
    assert detecter_version_donnees(registre_path(data)) == VERSION_DONNEES_COURANTE


def test_chemins_explicites_ne_touchent_pas_aux_donnees_reelles(tmp_path):
    # Des répertoires entièrement pilotés par l'appelant : rien n'est écrit
    # hors de tmp_path (les défauts pointent vers les dossiers natifs).
    config = tmp_path / "config"
    data = tmp_path / "data"
    registre = tmp_path / "registre-personnalise.json"
    config.mkdir()
    data.mkdir()

    rapport = migrer_donnees_vn_vn1(config, data, chemin_registre=registre)

    assert rapport["reussite"] is True
    assert registre.is_file()
    assert not registre_path(data).exists()


# -- Cohérence de la table ---------------------------------------------------


def test_table_de_migrations_contigue_et_unique():
    versions = [migration.version for migration in MIGRATIONS]
    assert versions == list(
        range(VERSION_INITIALE_DONNEES + 1, VERSION_INITIALE_DONNEES + len(MIGRATIONS) + 1)
    )
    assert versions == sorted(versions)
    identifiants = [migration.identifiant for migration in MIGRATIONS]
    assert len(set(identifiants)) == len(identifiants)
    assert versions[-1] == VERSION_DONNEES_COURANTE
    for migration in MIGRATIONS:
        assert migration.description
        assert migration.cibles
        assert callable(migration.appliquer)
