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
