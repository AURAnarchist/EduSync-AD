package ui

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
		"users.titre":        "Gestion des utilisateurs",
		"users.sous_titre":   "Importer, créer et gérer les comptes.",
		"users.import_titre": "Import CSV",
		"users.import_desc":  "Chargez un fichier CSV pour créer des comptes en masse.",
		"users.fichier_csv":  "Fichier CSV",
		"users.csv_format":   "Colonnes attendues : prenom;nom;ou;classe;email (séparateur ;)",
		"users.simuler":      "Simuler l'import",
		"users.liste_titre":  "Liste des utilisateurs",
		"users.liste_bientot": "Disponible prochainement.",

		// Password
		"pwd.titre":           "Réinitialisation mot de passe",
		"pwd.sous_titre":      "Réinitialiser les mots de passe par OU, groupe ou CSV.",
		"pwd.mode_ou":         "Par OU",
		"pwd.mode_groupe":     "Par groupe",
		"pwd.mode_csv":        "Par CSV",
		"pwd.politique":       "Politique",
		"pwd.forcer_changement": "Forcer changement au prochain logon",
		"pwd.simuler":         "Simuler",
		"pwd.executer":        "Exécuter",

		// Migration
		"migration.titre":       "Migration / Restructuration",
		"migration.sous_titre":  "Déplacer les comptes entre OU (fin d'année, changement de classe).",
		"migration.csv":         "Fichier CSV",
		"migration.plan_titre":  "Plan de migration",
		"migration.simuler":     "Simuler",
		"migration.executer":    "Exécuter",

		// Explorer
		"explorer.titre":      "Explorateur AD",
		"explorer.sous_titre": "Naviguer, rechercher, agir sur l'annuaire.",
		"explorer.recherche":  "Recherche",
		"explorer.arbre":      "Arborescence",
		"explorer.actions":    "Actions",

		// Export
		"export.titre":        "Export / Étiquettes",
		"export.sous_titre":   "Exporter CSV, PDF étiquettes (Avery L7160/L7163), trombinoscope.",
		"export.format":       "Format",
		"export.champs":       "Champs",
		"export.modele":       "Modèle étiquettes",
		"export.generer":      "Générer",

		// Audit
		"audit.titre":         "Journal d'actions",
		"audit.sous_titre":    "Historique complet des opérations.",
		"audit.filtres":       "Filtres",
		"audit.date_debut":    "Date début",
		"audit.date_fin":      "Date fin",
		"audit.type_action":   "Type d'action",
		"audit.resultat":      "Résultat",
		"audit.operateur":     "Opérateur",
		"audit.recherche":     "Recherche",

		// Password (détail — compléments)
		"pwd.mode_titre":       "Choisir le mode",
		"pwd.mode_ou_desc":     "Tous les utilisateurs d'une unité d'organisation",
		"pwd.mode_groupe_desc": "Tous les membres d'un groupe de sécurité",
		"pwd.mode_csv_desc":    "Liste d'identifiants depuis un fichier",
		"pwd.ou_titre":         "OU cible",
		"pwd.ou_label":         "DN de l'OU",
		"pwd.groupe_titre":     "Groupe cible",
		"pwd.groupe_label":     "DN du groupe",
		"pwd.csv_titre":        "Fichier CSV",
		"pwd.csv_label":        "Fichier CSV",
		"pwd.csv_format":       "Colonnes : identifiant (ou sAMAccountName)",
		"pwd.politique_eleve":  "Élèves (8 chars, 1 chiffre)",
		"pwd.politique_personnel": "Personnel (12 chars, complexe)",

		// Erreurs communes
		"erreur.connexion_ad": "Impossible de se connecter à l'AD",
		"erreur.fichier_trop_gros": "Fichier trop volumineux (max 10 Mo)",
		"erreur.csv_manquant": "Aucun fichier CSV fourni",
		"erreur.csv_invalide": "Fichier CSV illisible",
		"erreur.csv_vide": "Le CSV ne contient aucune donnée",
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
		"users.titre":        "User management",
		"users.sous_titre":   "Import, create and manage accounts.",
		"users.import_titre": "CSV Import",
		"users.import_desc":  "Upload a CSV file to create accounts in bulk.",
		"users.fichier_csv":  "CSV File",
		"users.csv_format":   "Expected columns: prenom;nom;ou;classe;email (separator ;)",
		"users.simuler":      "Simulate import",
		"users.liste_titre":  "User list",
		"users.liste_bientot": "Coming soon.",

		// Password
		"pwd.titre":           "Password reset",
		"pwd.sous_titre":      "Reset passwords by OU, group or CSV.",
		"pwd.mode_ou":         "By OU",
		"pwd.mode_groupe":     "By group",
		"pwd.mode_csv":        "By CSV",
		"pwd.politique":       "Policy",
		"pwd.forcer_changement": "Force change at next logon",
		"pwd.simuler":         "Simulate",
		"pwd.executer":        "Execute",

		// Migration
		"migration.titre":       "Migration / Restructuring",
		"migration.sous_titre":  "Move accounts between OUs (end of year, class change).",
		"migration.csv":         "CSV File",
		"migration.plan_titre":  "Migration plan",
		"migration.simuler":     "Simulate",
		"migration.executer":    "Execute",

		// Explorer
		"explorer.titre":      "AD Explorer",
		"explorer.sous_titre": "Browse, search, act on the directory.",
		"explorer.recherche":  "Search",
		"explorer.arbre":      "Tree",
		"explorer.actions":    "Actions",

		// Export
		"export.titre":        "Export / Labels",
		"export.sous_titre":   "Export CSV, PDF labels (Avery L7160/L7163), photo directory.",
		"export.format":       "Format",
		"export.champs":       "Fields",
		"export.modele":       "Label template",
		"export.generer":      "Generate",

		// Audit
		"audit.titre":         "Action log",
		"audit.sous_titre":    "Complete history of operations.",
		"audit.filtres":       "Filters",
		"audit.date_debut":    "Start date",
		"audit.date_fin":      "End date",
		"audit.type_action":   "Action type",
		"audit.resultat":      "Result",
		"audit.operateur":     "Operator",
		"audit.recherche":     "Search",

		// Common errors
		"erreur.connexion_ad": "Unable to connect to AD",
		"erreur.fichier_trop_gros": "File too large (max 10 MB)",
		"erreur.csv_manquant": "No CSV file provided",
		"erreur.csv_invalide": "Invalid CSV file",
		"erreur.csv_vide": "CSV contains no data",

		// Password (detail — compléments)
		"pwd.mode_titre":       "Choose mode",
		"pwd.mode_ou_desc":     "All users in an organizational unit",
		"pwd.mode_groupe_desc": "All members of a security group",
		"pwd.mode_csv_desc":    "List of identifiers from a file",
		"pwd.ou_titre":         "Target OU",
		"pwd.ou_label":         "OU DN",
		"pwd.groupe_titre":     "Target group",
		"pwd.groupe_label":     "Group DN",
		"pwd.csv_titre":        "CSV File",
		"pwd.csv_label":        "CSV File",
		"pwd.csv_format":       "Columns: identifier (or sAMAccountName)",
		"pwd.politique_eleve":  "Students (8 chars, 1 digit)",
		"pwd.politique_personnel": "Staff (12 chars, complex)",
	},
}

// Tr retourne la traduction d'une clé pour la langue demandée.
// Repli : langue demandée → français → clé.
func Tr(langue, cle string) string {
	if dico, ok := messages[langue]; ok {
		if texte, ok := dico[cle]; ok {
			return texte
		}
	}
	if dico, ok := messages["fr"]; ok {
		if texte, ok := dico[cle]; ok {
			return texte
		}
	}
	return cle
}
