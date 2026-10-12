// Package web contient l'interface graphique d'EduSync-AD, embarquée dans
// le binaire via //go:embed (zéro fichier externe à distribuer).
//
// Choix technologiques (ROADMAP_GO_MIGRATION.md §2) :
//   - HTMX 2 pour les échanges serveur (l'IA génère du HTML, pas de composants JS) ;
//   - Alpine.js pour l'état client minimal (thème, langue) ;
//   - Tailwind CSS compilé une fois puis versionné (aucun build obligatoire
//     à l'installation, voir Taskfile « web:css »).
package web

import "embed"

// FS regroupe les templates et les assets statiques.
//
//go:embed templates static
var FS embed.FS
