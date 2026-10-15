package ui

import "fmt"

// Dictionnaire de l'interface (français par défaut, anglais en secours).
//
// Conventions :
//   - une clé est en « point.case » (namespace.élément) ;
//   - toute clé absente de la langue demandée retombe sur le français,
//     puis sur la clé elle-même (jamais de texte vide à l'écran).
//
// Ajouter une clé = l'ajouter aux deux langues (vérifié par le test
// TestDictionnaireComplet).
var messages = map[string]map[string]string{
	"fr": {
		"app.nom":          "EduSync AD",
		"app.baseline":     "Gestion du cycle de vie des comptes Active Directory",
		"nav.tableau_bord": "Tableau de bord",
		"nav.deconnexion":  "Déconnexion",
		"nav.langue":       "Langue",
		"nav.theme.clair":  "Thème clair",
		"nav.theme.sombre": "Thème sombre",

		"login.titre":       "Connexion au domaine",
		"login.sous_titre":  "Renseignez l'Active Directory à gérer.",
		"login.domaine":     "Domaine",
		"login.controleur":  "Contrôleur de domaine",
		"login.utilisateur": "Utilisateur",
		"login.mdp":         "Mot de passe",
		"login.action":      "Se connecter",
		"login.exemple":     "ex. EDUSYNC.LOCAL, dc1.edusync.local, Administrator",

		"erreur.formulaire":  "Formulaire illisible, vérifiez les champs saisis.",
		"erreur.domaine":     "Le domaine est obligatoire.",
		"erreur.controleur":  "Le contrôleur de domaine est obligatoire.",
		"erreur.utilisateur": "L'utilisateur est obligatoire.",

		"dash.titre":             "Tableau de bord",
		"dash.bienvenue":         "Bienvenue",
		"dash.carte.config":      "Configuration",
		"dash.carte.journal":     "Journal d'actions",
		"dash.carte.version":     "Version",
		"dash.carte.ad":          "Active Directory",
		"dash.carte.donnees":     "Données locales",
		"statut.ad":              "Connexion AD non branchée — étape en cours (Phase 0 : pool de connexions).",
		"dash.dernieres_actions": "Dernières actions",
		"dash.aucune_action":     "Aucune action journalisée pour l'instant.",
		"dash.nb_actions":        "entrées",

		"col.date":      "Horodatage",
		"col.action":    "Action",
		"col.compte":    "Compte",
		"col.resultat":  "Résultat",
		"col.operateur": "Opérateur",
		"col.detail":    "Détail",

		// Users
		"users.titre":         "Gestion des utilisateurs",
		"users.sous_titre":    "Importer, créer et gérer les comptes.",
		"users.import_titre":  "Import CSV",
		"users.import_desc":   "Chargez un fichier CSV pour créer des comptes en masse.",
		"users.fichier_csv":   "Fichier CSV",
		"users.csv_format":    "Colonnes attendues : prenom;nom;ou;classe;email (séparateur ; ou , détecté automatiquement, UTF-8 ou Windows-1252)",
		"users.simuler":       "Simuler l'import",
		"users.liste_titre":   "Liste des utilisateurs",
		"users.liste_bientot": "Disponible prochainement.",

		// Arrivées — prévisualisation et exécution
		"users.col_prenom":         "Prénom",
		"users.col_nom":            "Nom",
		"users.col_identifiant":    "Identifiant",
		"users.col_mdp":            "Mot de passe",
		"users.col_mail":           "Adresse mail",
		"users.col_ou":             "OU cible",
		"users.col_groupe":         "Groupe",
		"users.doublon":            "doublon résolu",
		"users.resume":             "%s lignes valides · %s erreurs · %s au total",
		"users.executer":           "Créer les comptes",
		"users.execute_hint":       "Seules les lignes cochées seront créées dans l'AD.",
		"users.execute_resume":     "%s comptes créés · %s échecs",
		"users.aucune_ligne":       "Aucune ligne cochée à créer",
		"users.aucune_simulation":  "Aucune simulation en cours.",
		"users.import_retour":      "← Retour à l'import",
		"users.simulate_titre":     "Simulation import",
		"users.simulate_desc":      "Vérifiez les comptes à créer. Modifiez si nécessaire, puis confirmez.",
		"users.execute_titre":      "Exécution import",
		"users.execute_desc":       "Résultat de la création des comptes dans l'Active Directory.",
		"users.simulation_preview": "Prévisualisation",
		"users.export_etiquettes":  "Exporter étiquettes",
		"users.opt_politique":      "Politique de mot de passe",
		"users.opt_domaine_mail":   "Domaine mail",
		"users.opt_ou_eleves":      "OU parente des élèves",
		"users.opt_forcer_mdp":     "Forcer le changement de MDP au premier logon",
		"users.opt_skip_ad":        "Ignorer la vérification des doublons dans l'AD (plus rapide)",
		"col.erreur":               "Erreur",
		"statut.succes":            "Succès",

		// Password
		"pwd.titre":             "Réinitialisation mot de passe",
		"pwd.sous_titre":        "Réinitialiser les mots de passe par OU, groupe ou CSV.",
		"pwd.mode_ou":           "Par OU",
		"pwd.mode_groupe":       "Par groupe",
		"pwd.mode_csv":          "Par CSV",
		"pwd.politique":         "Politique",
		"pwd.forcer_changement": "Forcer changement au prochain logon",
		"pwd.simuler":           "Simuler",
		"pwd.executer":          "Exécuter",

		// Explorer
		"explorer.titre":      "Explorateur AD",
		"explorer.sous_titre": "Naviguer, rechercher, agir sur l'annuaire.",
		"explorer.recherche":  "Recherche",
		"explorer.arbre":      "Arborescence",
		"explorer.actions":    "Actions",

		// Export
		"export.titre":      "Export / Étiquettes",
		"export.sous_titre": "Exporter CSV, PDF étiquettes (Avery L7160/L7163), trombinoscope.",
		"export.format":     "Format",
		"export.champs":     "Champs",
		"export.modele":     "Modèle étiquettes",
		"export.generer":    "Générer",

		// Audit
		"audit.titre":       "Journal d'actions",
		"audit.sous_titre":  "Historique complet des opérations.",
		"audit.filtres":     "Filtres",
		"audit.date_debut":  "Date début",
		"audit.date_fin":    "Date fin",
		"audit.type_action": "Type d'action",
		"audit.resultat":    "Résultat",
		"audit.operateur":   "Opérateur",
		"audit.recherche":   "Recherche",

		// Password (détail — compléments)
		"pwd.mode_titre":          "Choisir le mode",
		"pwd.mode_ou_desc":        "Tous les utilisateurs d'une unité d'organisation",
		"pwd.mode_groupe_desc":    "Tous les membres d'un groupe de sécurité",
		"pwd.mode_csv_desc":       "Liste d'identifiants depuis un fichier",
		"pwd.ou_titre":            "OU cible",
		"pwd.ou_label":            "DN de l'OU",
		"pwd.groupe_titre":        "Groupe cible",
		"pwd.groupe_label":        "DN du groupe",
		"pwd.csv_titre":           "Fichier CSV",
		"pwd.csv_label":           "Fichier CSV",
		"pwd.csv_format":          "Colonnes : identifiant (ou sAMAccountName)",
		"pwd.politique_eleve":     "Élèves (8 chars, 1 chiffre)",
		"pwd.politique_personnel": "Personnel (12 chars, complexe)",

		// Password — simulation & exécution
		"pwd.simulate_titre":     "Simulation réinitialisation MDP",
		"pwd.simulate_desc":      "Vérifiez les nouveaux mots de passe. Modifiez si nécessaire, puis confirmez.",
		"pwd.simulation_preview": "Prévisualisation",
		"pwd.resume":             "%s valides · %s erreurs · %s au total",
		"pwd.execute_titre":      "Exécution réinitialisation MDP",
		"pwd.execute_desc":       "Résultat de la réinitialisation des mots de passe dans l'Active Directory.",
		"pwd.execute_resume":     "%s réinitialisés · %s échecs",
		"pwd.import_retour":      "← Retour",
		"pwd.aucune_simulation":  "Aucune simulation en cours.",
		"pwd.col_prenom":         "Prénom",
		"pwd.col_nom":            "Nom",
		"pwd.col_identifiant":    "Identifiant",
		"pwd.col_mail":           "Adresse mail",
		"pwd.col_nouveau_mdp":    "Nouveau MDP",
		"pwd.col_ou":             "OU",
		"pwd.export_csv":         "Exporter CSV",
		"pwd.export_etiquettes":  "Exporter étiquettes",

		// Migration
		"migration.titre":            "Migration / Restructuration",
		"migration.sous_titre":       "Déplacer les comptes entre OU (fin d'année, changement de classe).",
		"migration.csv":              "Fichier CSV",
		"migration.csv_format":       "Colonnes : identifiant;ou_source;ou_cible (séparateur ;)",
		"migration.etape1":           "1. Charger le CSV",
		"migration.etape1_desc":      "Fichier avec identifiant, OU source et OU cible",
		"migration.charger":          "Charger le plan",
		"migration.plan_titre":       "Plan de migration",
		"migration.total":            "{{count}} comptes",
		"migration.col_identifiant":  "Identifiant",
		"migration.col_nom":          "Nom complet",
		"migration.col_ou_source":    "OU source",
		"migration.col_ou_cible":     "OU cible",
		"migration.col_action":       "Action",
		"migration.simuler":          "Simuler",
		"migration.executer":         "Exécuter",
		"migration.confirmer":        "Confirmer",
		"migration.annuler":          "Annuler",
		"migration.simulation_titre": "Simulation de la migration",
		"migration.simulation_desc":  "Vérifiez les déplacements avant de confirmer.",
		"migration.col_detail":       "Détail",

		// Erreurs communes
		"erreur.connexion_ad":               "Impossible de se connecter à l'AD",
		"erreur.connexion_ad_ldaps":         "Échec de la connexion LDAPS (port 636) : %s",
		"erreur.connexion_ad_ldap":          "Échec de la connexion LDAP (port 389) : %s",
		"erreur.connexion_ad_bind":          "Identifiants invalides ou compte verrouillé",
		"erreur.connexion_ad_reseau":        "Serveur inaccessible (vérifiez l'adresse et le pare-feu)",
		"erreur.connexion_ad_certificat":    "Certificat invalide : %s",
		"avertissement.ldap_fallback":       "⚠️ Connexion non chiffrée (LDAP 389 + StartTLS). Le mot de passe transite en clair sur le réseau.",
		"avertissement.ldap_fallback_titre": "Avertissement de sécurité",
		"erreur.fichier_trop_gros":          "Fichier trop volumineux (max 10 Mo)",
		"erreur.csv_manquant":               "Aucun fichier CSV fourni",
		"erreur.csv_invalide":               "Fichier CSV illisible",
		"erreur.csv_vide":                   "Le CSV ne contient aucune donnée",
		"erreur.pwd_ou_vide":                "L'OU cible est obligatoire",
		"erreur.pwd_groupe_vide":            "Le groupe cible est obligatoire",
		"erreur.pwd_mode_invalide":          "Mode de réinitialisation invalide",
	},
	"en": {
		"app.nom":          "EduSync AD",
		"app.baseline":     "Active Directory account lifecycle management",
		"nav.tableau_bord": "Dashboard",
		"nav.deconnexion":  "Sign out",
		"nav.langue":       "Language",
		"nav.theme.clair":  "Light theme",
		"nav.theme.sombre": "Dark theme",

		"login.titre":       "Domain sign-in",
		"login.sous_titre":  "Enter the Active Directory to manage.",
		"login.domaine":     "Domain",
		"login.controleur":  "Domain controller",
		"login.utilisateur": "User",
		"login.mdp":         "Password",
		"login.action":      "Sign in",
		"login.exemple":     "e.g. EDUSYNC.LOCAL, dc1.edusync.local, Administrator",

		"erreur.formulaire":  "Invalid form, please check the fields.",
		"erreur.domaine":     "The domain is required.",
		"erreur.controleur":  "The domain controller is required.",
		"erreur.utilisateur": "The user is required.",

		"dash.titre":             "Dashboard",
		"dash.bienvenue":         "Welcome",
		"dash.carte.config":      "Configuration",
		"dash.carte.journal":     "Action log",
		"dash.carte.version":     "Version",
		"dash.carte.ad":          "Active Directory",
		"dash.carte.donnees":     "Local data",
		"statut.ad":              "AD connection not wired yet — work in progress (Phase 0: connection pool).",
		"dash.dernieres_actions": "Recent actions",
		"dash.aucune_action":     "No logged action yet.",
		"dash.nb_actions":        "entries",

		"col.date":      "Timestamp",
		"col.action":    "Action",
		"col.compte":    "Account",
		"col.resultat":  "Result",
		"col.operateur": "Operator",
		"col.detail":    "Detail",

		// Users
		"users.titre":         "User management",
		"users.sous_titre":    "Import, create and manage accounts.",
		"users.import_titre":  "CSV Import",
		"users.import_desc":   "Upload a CSV file to create accounts in bulk.",
		"users.fichier_csv":   "CSV File",
		"users.csv_format":    "Expected columns: prenom;nom;ou;classe;email (separator ; or , auto-detected, UTF-8 or Windows-1252)",
		"users.simuler":       "Simulate import",
		"users.liste_titre":   "User list",
		"users.liste_bientot": "Coming soon.",

		// Arrivals — preview and execution
		"users.col_prenom":         "First name",
		"users.col_nom":            "Last name",
		"users.col_identifiant":    "Username",
		"users.col_mdp":            "Password",
		"users.col_mail":           "Email",
		"users.col_ou":             "Target OU",
		"users.col_groupe":         "Group",
		"users.doublon":            "duplicate resolved",
		"users.resume":             "%s valid rows · %s errors · %s total",
		"users.executer":           "Create accounts",
		"users.execute_hint":       "Only checked rows will be created in AD.",
		"users.execute_resume":     "%s accounts created · %s failures",
		"users.aucune_ligne":       "No checked row to create",
		"users.aucune_simulation":  "No simulation in progress.",
		"users.import_retour":      "← Back to import",
		"users.simulate_titre":     "Import simulation",
		"users.simulate_desc":      "Review accounts to create. Modify if needed, then confirm.",
		"users.execute_titre":      "Import execution",
		"users.execute_desc":       "Result of account creation in Active Directory.",
		"users.simulation_preview": "Preview",
		"users.export_etiquettes":  "Export labels",
		"users.opt_politique":      "Password policy",
		"users.opt_domaine_mail":   "Mail domain",
		"users.opt_ou_eleves":      "Students parent OU",
		"users.opt_forcer_mdp":     "Force password change at first logon",
		"users.opt_skip_ad":        "Skip AD duplicate check (faster)",
		"col.erreur":               "Error",
		"statut.succes":            "Success",

		// Password
		"pwd.titre":             "Password reset",
		"pwd.sous_titre":        "Reset passwords by OU, group or CSV.",
		"pwd.mode_ou":           "By OU",
		"pwd.mode_groupe":       "By group",
		"pwd.mode_csv":          "By CSV",
		"pwd.politique":         "Policy",
		"pwd.forcer_changement": "Force change at next logon",
		"pwd.simuler":           "Simulate",
		"pwd.executer":          "Execute",

		// Explorer
		"explorer.titre":      "AD Explorer",
		"explorer.sous_titre": "Browse, search, act on the directory.",
		"explorer.recherche":  "Search",
		"explorer.arbre":      "Tree",
		"explorer.actions":    "Actions",

		// Export
		"export.titre":      "Export / Labels",
		"export.sous_titre": "Export CSV, PDF labels (Avery L7160/L7163), photo directory.",
		"export.format":     "Format",
		"export.champs":     "Fields",
		"export.modele":     "Label template",
		"export.generer":    "Generate",

		// Audit
		"audit.titre":       "Action log",
		"audit.sous_titre":  "Complete history of operations.",
		"audit.filtres":     "Filters",
		"audit.date_debut":  "Start date",
		"audit.date_fin":    "End date",
		"audit.type_action": "Action type",
		"audit.resultat":    "Result",
		"audit.operateur":   "Operator",
		"audit.recherche":   "Search",

		// Migration
		"migration.titre":            "Migration / Restructuring",
		"migration.sous_titre":       "Move accounts between OUs (end of year, class change).",
		"migration.csv":              "CSV File",
		"migration.csv_format":       "Columns: identifier;source_ou;target_ou (separator ;)",
		"migration.etape1":           "1. Load CSV",
		"migration.etape1_desc":      "File with identifier, source OU and target OU",
		"migration.charger":          "Load plan",
		"migration.plan_titre":       "Migration plan",
		"migration.total":            "{{count}} accounts",
		"migration.col_identifiant":  "Identifier",
		"migration.col_nom":          "Full name",
		"migration.col_ou_source":    "Source OU",
		"migration.col_ou_cible":     "Target OU",
		"migration.col_action":       "Action",
		"migration.simuler":          "Simulate",
		"migration.executer":         "Execute",
		"migration.confirmer":        "Confirm",
		"migration.annuler":          "Cancel",
		"migration.simulation_titre": "Migration simulation",
		"migration.simulation_desc":  "Review moves before confirming.",
		"migration.col_detail":       "Detail",

		// Password (detail — compléments)
		"pwd.mode_titre":          "Choose mode",
		"pwd.mode_ou_desc":        "All users in an organizational unit",
		"pwd.mode_groupe_desc":    "All members of a security group",
		"pwd.mode_csv_desc":       "List of identifiers from a file",
		"pwd.ou_titre":            "Target OU",
		"pwd.ou_label":            "OU DN",
		"pwd.groupe_titre":        "Target group",
		"pwd.groupe_label":        "Group DN",
		"pwd.csv_titre":           "CSV File",
		"pwd.csv_label":           "CSV File",
		"pwd.csv_format":          "Columns: identifier (or sAMAccountName)",
		"pwd.politique_eleve":     "Students (8 chars, 1 digit)",
		"pwd.politique_personnel": "Staff (12 chars, complex)",

		// Password — simulation & execution
		"pwd.simulate_titre":     "Password reset simulation",
		"pwd.simulate_desc":      "Review new passwords. Modify if needed, then confirm.",
		"pwd.simulation_preview": "Preview",
		"pwd.resume":             "%s valid · %s errors · %s total",
		"pwd.execute_titre":      "Password reset execution",
		"pwd.execute_desc":       "Result of password reset in Active Directory.",
		"pwd.execute_resume":     "%s reset · %s failures",
		"pwd.import_retour":      "← Back",
		"pwd.aucune_simulation":  "No simulation in progress.",
		"pwd.col_prenom":         "First name",
		"pwd.col_nom":            "Last name",
		"pwd.col_identifiant":    "Username",
		"pwd.col_mail":           "Email",
		"pwd.col_nouveau_mdp":    "New password",
		"pwd.col_ou":             "OU",
		"pwd.export_csv":         "Export CSV",
		"pwd.export_etiquettes":  "Export labels",

		// Common errors
		"erreur.connexion_ad":               "Unable to connect to AD",
		"erreur.connexion_ad_ldaps":         "LDAPS connection failed (port 636): %s",
		"erreur.connexion_ad_ldap":          "LDAP connection failed (port 389): %s",
		"erreur.connexion_ad_bind":          "Invalid credentials or account locked",
		"erreur.connexion_ad_reseau":        "Server unreachable (check address and firewall)",
		"erreur.connexion_ad_certificat":    "Certificate invalid: %s",
		"avertissement.ldap_fallback":       "⚠️ Unencrypted connection (LDAP 389 + StartTLS). Password transits in clear text.",
		"avertissement.ldap_fallback_titre": "Security Warning",
		"erreur.fichier_trop_gros":          "File too large (max 10 MB)",
		"erreur.csv_manquant":               "No CSV file provided",
		"erreur.csv_invalide":               "Invalid CSV file",
		"erreur.csv_vide":                   "CSV contains no data",
		"erreur.pwd_ou_vide":                "Target OU is required",
		"erreur.pwd_groupe_vide":            "Target group is required",
		"erreur.pwd_mode_invalide":          "Invalid reset mode",
	},
}

// Tr retourne la traduction d'une clé pour la langue demandée.
// Repli : langue demandée → français → clé. Des arguments optionnels
// sont interpolés via fmt.Sprintf si la valeur de la clé contient %s/%d.
func Tr(langue, cle string, args ...any) string {
	var texte string
	if dico, ok := messages[langue]; ok {
		if t, ok := dico[cle]; ok {
			texte = t
		}
	}
	if texte == "" {
		if dico, ok := messages["fr"]; ok {
			if t, ok := dico[cle]; ok {
				texte = t
			}
		}
	}
	if texte == "" {
		return cle
	}
	if len(args) > 0 {
		return fmt.Sprintf(texte, args...)
	}
	return texte
}
