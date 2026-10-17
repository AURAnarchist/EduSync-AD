package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NomFichierOpenAPI est le document OpenAPI écrit sur disque (racine du
// dossier de travail, comme `api-spec.json` côté Python).
const NomFichierOpenAPI = "api-spec.json"

// TitreDocument est le titre du document OpenAPI.
const TitreDocument = "EduSync AD REST API"

// DescriptionDocument décrit l'API dans le document OpenAPI.
const DescriptionDocument = "API REST pour la gestion du cycle de vie des comptes " +
	"Active Directory — établissement scolaire, PME, collectivités. " +
	"Architecture standard (http.server côté Python, net/http côté Go) : " +
	"aucune dépendance externe."

// openapi construit le document OpenAPI 3.0.3 (route `GET /openapi.json`).
func (d Dependances) openapi(_ *Requete) (int, any) {
	return ReponseOK(DocumentOpenAPI(d.Version, d.Routeur))
}

// DocumentOpenAPI génère le document OpenAPI 3.0.3 à partir des routes
// enregistrées.
//
// Les chemins sont décrits sans leur préfixe `/api/v1` (le serveur OpenAPI
// standard), et les paramètres de chemin sont déduits des motifs.
func DocumentOpenAPI(version string, routeur *Routeur) map[string]any {
	if version == "" {
		version = VersionAPI
	}
	chemins := map[string]any{}
	if routeur != nil {
		for _, route := range routeur.Routes() {
			if route.Publique {
				continue // la documentation ne se documente pas elle-même
			}
			chemin := strings.TrimPrefix(route.Motif, PrefixeAPI)
			if chemin == "" || !strings.HasPrefix(chemin, "/") {
				continue
			}
			operations, ok := chemins[chemin].(map[string]any)
			if !ok {
				operations = map[string]any{}
				chemins[chemin] = operations
			}
			operations[strings.ToLower(route.Methode)] = operationOpenAPI(route)
		}
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       TitreDocument,
			"description": DescriptionDocument,
			"version":     version,
			"license":     map[string]any{"name": "MIT"},
		},
		"servers": []map[string]any{
			{"url": PrefixeAPI, "description": "API locale"},
		},
		"tags": []map[string]any{
			{"name": "users", "description": "Gestion des comptes"},
			{"name": "groups", "description": "Gestion des groupes"},
			{"name": "ous", "description": "Unités d'organisation"},
			{"name": "portal", "description": "Portail auto-service"},
			{"name": "webhooks", "description": "Notifications événementielles"},
			{"name": "audit", "description": "Journal d'actions"},
			{"name": "meta", "description": "Santé et informations"},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"ApiKeyHeader": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-API-Key",
				},
				"BearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "api-key",
				},
			},
			"schemas": schemasOpenAPI(),
		},
		"security": []map[string]any{
			{"ApiKeyHeader": []string{}},
			{"BearerAuth": []string{}},
		},
		"paths": chemins,
	}
}

// operationOpenAPI décrit une opération à partir d'une route.
func operationOpenAPI(route Route) map[string]any {
	operation := map[string]any{
		"operationId": operationID(route),
		"summary":     resumeRoute(route),
		"tags":        []string{etiquetteRoute(route.Motif)},
		"responses": map[string]any{
			"200": map[string]any{"description": "Succès"},
			"401": map[string]any{"description": "Clé d'API absente ou invalide"},
			"403": map[string]any{"description": "Permissions insuffisantes"},
		},
	}
	if parametres := parametresRoute(route.Motif); len(parametres) > 0 {
		operation["parameters"] = parametres
	}
	switch route.Methode {
	case http.MethodPost, http.MethodPatch:
		operation["requestBody"] = map[string]any{
			"required": false,
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"type": "object"}},
			},
		}
		operation["responses"].(map[string]any)["422"] =
			map[string]any{"description": "Données invalides"}
	}
	return operation
}

// operationID construit un identifiant lisible et stable pour l'opération.
func operationID(route Route) string {
	chemin := strings.TrimPrefix(route.Motif, PrefixeAPI)
	remplacement := strings.NewReplacer("/", "_", "{", "", "}", "")
	return strings.ToLower(route.Methode) + remplacement.Replace(chemin)
}

// resumeRoute retourne un libellé court de la route.
func resumeRoute(route Route) string {
	chemin := strings.TrimPrefix(route.Motif, PrefixeAPI)
	libelles := map[string]string{
		"GET /users":                 "Lister les utilisateurs",
		"POST /users":                "Créer un utilisateur",
		"GET /users/{sam}":           "Détail d'un utilisateur",
		"PATCH /users/{sam}":         "Modifier un utilisateur",
		"DELETE /users/{sam}":        "Supprimer un utilisateur",
		"POST /users/{sam}/password": "Réinitialiser un mot de passe",
		"GET /groups":                "Lister les groupes",
		"POST /groups":               "Créer un groupe",
		"GET /groups/{cn}":           "Détail d'un groupe",
		"GET /ous":                   "Lister les unités d'organisation",
		"POST /ous":                  "Créer une unité d'organisation",
		"GET /ous/{dn}":              "Détail d'une unité d'organisation",
		"GET /portal/requests":       "Lister les demandes de pré-inscription",
		"POST /portal/requests/{demande_id}/approve": "Valider une demande",
		"POST /portal/requests/{demande_id}/refuse":  "Refuser une demande",
		"GET /webhooks":                    "Lister les webhooks",
		"POST /webhooks":                   "Créer un webhook",
		"GET /webhooks/{webhook_id}":       "Détail d'un webhook",
		"PATCH /webhooks/{webhook_id}":     "Modifier un webhook",
		"DELETE /webhooks/{webhook_id}":    "Supprimer un webhook",
		"POST /webhooks/{webhook_id}/test": "Envoyer un événement de test",
		"GET /webhooks/events":             "Lister les événements disponibles",
		"GET /audit":                       "Consulter le journal d'actions",
		"GET /health":                      "État du service",
		"GET /info":                        "Informations et points d'entrée",
	}
	if libelle, ok := libelles[route.Methode+" "+chemin]; ok {
		return libelle
	}
	return route.Methode + " " + chemin
}

// etiquetteRoute associe un chemin à son étiquette OpenAPI.
func etiquetteRoute(motif string) string {
	chemin := strings.TrimPrefix(motif, PrefixeAPI)
	premier := strings.SplitN(strings.TrimPrefix(chemin, "/"), "/", 2)[0]
	switch premier {
	case "users", "groups", "ous", "portal", "webhooks", "audit":
		return premier
	default:
		return "meta"
	}
}

// parametresRoute déduit les paramètres de chemin d'un motif (`{sam}`).
func parametresRoute(motif string) []map[string]any {
	var parametres []map[string]any
	for _, trouve := range motifParam.FindAllStringSubmatch(motif, -1) {
		parametres = append(parametres, map[string]any{
			"name":     trouve[1],
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		})
	}
	return parametres
}

// schemasOpenAPI décrit les objets manipulés.
func schemasOpenAPI() map[string]any {
	return map[string]any{
		"Utilisateur": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sam":   map[string]any{"type": "string", "example": "l.martin"},
				"cn":    map[string]any{"type": "string", "example": "Léa Martin"},
				"mail":  map[string]any{"type": "string", "format": "email"},
				"dn":    map[string]any{"type": "string"},
				"upn":   map[string]any{"type": "string"},
				"ou":    map[string]any{"type": "string"},
				"actif": map[string]any{"type": "boolean"},
			},
		},
		"Groupe": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cn":          map[string]any{"type": "string"},
				"sam":         map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"dn":          map[string]any{"type": "string"},
				"members": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
		},
		"Unite": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dn":          map[string]any{"type": "string"},
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
		},
		"Demande": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":         map[string]any{"type": "string"},
				"profil":     map[string]any{"type": "string"},
				"prenom":     map[string]any{"type": "string"},
				"nom":        map[string]any{"type": "string"},
				"mail":       map[string]any{"type": "string", "format": "email"},
				"classe":     map[string]any{"type": "string"},
				"motif":      map[string]any{"type": "string"},
				"statut":     map[string]any{"type": "string"},
				"created_at": map[string]any{"type": "string", "format": "date-time"},
			},
		},
		"Webhook": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string"},
				"name":   map[string]any{"type": "string"},
				"url":    map[string]any{"type": "string", "format": "uri"},
				"events": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"active": map[string]any{"type": "boolean"},
				"retry_policy": map[string]any{
					"type": "string",
					"enum": []string{"exponential", "fixed", "none"},
				},
				"max_retries":     map[string]any{"type": "integer", "minimum": 0, "maximum": 20},
				"timeout_seconds": map[string]any{"type": "number", "minimum": 1, "maximum": 60},
			},
		},
		"Reponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"success": map[string]any{"type": "boolean"},
				"data":    map[string]any{},
				"meta":    map[string]any{"type": "object"},
			},
		},
		"Erreur": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"success": map[string]any{"type": "boolean", "example": false},
				"error": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code":    map[string]any{"type": "string"},
						"message": map[string]any{"type": "string"},
						"details": map[string]any{"type": "object"},
					},
				},
			},
		},
	}
}

// EcrireDocumentOpenAPI écrit le document sur disque (`api-spec.json`).
func EcrireDocumentOpenAPI(chemin, version string, routeur *Routeur) (string, error) {
	if chemin == "" {
		chemin = NomFichierOpenAPI
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return "", err
	}
	donnees, err := json.MarshalIndent(DocumentOpenAPI(version, routeur), "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return "", err
	}
	return chemin, nil
}
