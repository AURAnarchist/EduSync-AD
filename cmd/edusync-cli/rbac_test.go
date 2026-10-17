package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/rbac"
)

const (
	rbacOUA = "OU=3emeA,OU=eleves,DC=lycee,DC=local"
	rbacOUB = "OU=3emeB,OU=eleves,DC=lycee,DC=local"
)

func TestRBACRolesEtPortee(t *testing.T) {
	out, err := execution(t, "rbac", "roles", "--json")
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	var roles []struct {
		Role   string `json:"role"`
		Scoped bool   `json:"scoped"`
	}
	if err := json.Unmarshal([]byte(out), &roles); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(roles) != 5 || roles[0].Role != "super_admin" {
		t.Errorf("roles = %+v", roles)
	}
	scoped := map[string]bool{}
	for _, role := range roles {
		scoped[role.Role] = role.Scoped
	}
	if !scoped["admin_classe"] || !scoped["helpdesk"] || scoped["admin_site"] {
		t.Errorf("scoped = %v", scoped)
	}
}

func TestRBACAddListRemovePolicy(t *testing.T) {
	fichier := filepath.Join(t.TempDir(), "delegations.json")

	out, err := execution(t, "rbac", "add", "--operateur", "alice", "--role", "admin_classe",
		"--ou", rbacOUA, "--file", fichier, "--json")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var grant rbac.Grant
	if err := json.Unmarshal([]byte(out), &grant); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if grant.ID == "" || grant.Role != rbac.AdminClasse || len(grant.OUs) != 1 {
		t.Fatalf("grant = %+v", grant)
	}

	// La politique résolue reflète le délégué.
	outPolitique, err := execution(t, "rbac", "policy", "--operateur", "alice",
		"--domain", "lycee.local", "--file", fichier, "--json")
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	policy, err := decoderRBACPolicy([]byte(outPolitique))
	if err != nil {
		t.Fatalf("JSON: %v\n%s", err, outPolitique)
	}
	if policy["role"] != "admin_classe" || policy["scoped"] != true || policy["readonly"] != false {
		t.Errorf("policy = %+v", policy)
	}

	// Un opérateur non référencé est en lecture seule.
	outBob, err := execution(t, "rbac", "policy", "--operateur", "bob", "--file", fichier, "--json")
	if err != nil {
		t.Fatal(err)
	}
	policyBob, err := decoderRBACPolicy([]byte(outBob))
	if err != nil {
		t.Fatal(err)
	}
	if policyBob["role"] != "lecture_seule" || policyBob["readonly"] != true {
		t.Errorf("policy bob = %+v", policyBob)
	}

	outList, err := execution(t, "rbac", "list", "--file", fichier, "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var grants []rbac.Grant
	if err := json.Unmarshal([]byte(outList), &grants); err != nil {
		t.Fatalf("JSON: %v\n%s", err, outList)
	}
	if len(grants) != 1 || grants[0].Operateur != "alice" {
		t.Errorf("grants = %+v", grants)
	}

	if _, err := execution(t, "rbac", "remove", "--id", grant.ID, "--file", fichier); err != nil {
		t.Fatalf("remove: %v", err)
	}
	outVide, err := execution(t, "rbac", "list", "--file", fichier, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(outVide) != "[]" && strings.TrimSpace(outVide) != "null" {
		t.Errorf("liste après suppression = %s", outVide)
	}
}

func TestRBACAddValideLesChamps(t *testing.T) {
	fichier := filepath.Join(t.TempDir(), "delegations.json")
	if _, err := execution(t, "rbac", "add", "--role", "admin_site", "--file", fichier); err == nil {
		t.Error("opérateur manquant accepté")
	}
	// Rôle scopé sans portée.
	if _, err := execution(t, "rbac", "add", "--operateur", "alice", "--role", "helpdesk", "--file", fichier); err == nil {
		t.Error("rôle scopé sans portée accepté")
	}
	// Portée sans « = ».
	if _, err := execution(t, "rbac", "add", "--operateur", "alice", "--role", "admin_classe",
		"--ou", "pas-un-dn", "--file", fichier); err == nil {
		t.Error("portée invalide acceptée")
	}
	if _, err := execution(t, "rbac", "remove", "--id", "inconnu", "--file", fichier); err == nil {
		t.Error("suppression d'un délégué inconnu acceptée")
	}
}

func TestRBACPolicySansDelegueAccesComplet(t *testing.T) {
	fichier := filepath.Join(t.TempDir(), "delegations.json")
	out, err := execution(t, "rbac", "policy", "--operateur", "admin", "--file", fichier, "--json")
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	policy, err := decoderRBACPolicy([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if policy["role"] != "super_admin" || policy["can_manage_delegation"] != true {
		t.Errorf("policy = %+v", policy)
	}
}
