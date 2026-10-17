package ad

import (
	"context"
	"errors"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/rbac"
)

const (
	rbacOUA    = "OU=3emeA,OU=eleves,DC=lycee,DC=local"
	rbacOUB    = "OU=3emeB,OU=eleves,DC=lycee,DC=local"
	rbacGroupe = "CN=3emeA,OU=groupes,DC=lycee,DC=local"
)

// poolSansConnexion retourne un pool qui ne peut pas dialer : toute écriture
// autorisée échouera donc sur le réseau, jamais sur le RBAC.
func poolSansConnexion(t *testing.T) *Pool {
	t.Helper()
	pool, err := NewPoolNoValidate(Config{
		Domain:       "lycee.local",
		Controller:   "127.0.0.1:1", // port fermé : échec réseau rapide
		BaseDN:       "DC=lycee,DC=local",
		BindDN:       "CN=admin,DC=lycee,DC=local",
		BindPassword: "secret",
	})
	if err != nil {
		t.Fatalf("NewPoolNoValidate: %v", err)
	}
	return pool
}

func TestRBACSansPolitiqueAucunControle(t *testing.T) {
	pool := poolSansConnexion(t)
	// Sans politique attachée, le garde-fou ne refuse jamais : l'échec
	// éventuel vient du réseau, pas du RBAC.
	if err := pool.UserResetPassword(context.Background(), "CN=x,"+rbacOUA, "Motdepasse1!"); err != nil {
		if errors.Is(err, ErrInsufficientRights) {
			t.Fatalf("refus RBAC alors qu'aucune politique n'est attachée: %v", err)
		}
	}
}

func TestRBACLectureSeuleRefuseLesEcritures(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "bob", Role: rbac.LectureSeule})

	if err := pool.UserResetPassword(context.Background(), "CN=x,"+rbacOUA, "Motdepasse1!"); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("reset_password: %v", err)
	}
	if _, err := pool.OUCreate(context.Background(), OUCreateParams{Name: "refuse", ParentDN: "DC=lycee,DC=local"}); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("create_ou: %v", err)
	}
	if err := pool.UserDelete(context.Background(), "CN=x,"+rbacOUA); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("delete_user: %v", err)
	}
}

func TestRBACAdminSiteEcritPartout(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "alice", Role: rbac.AdminSite})

	if _, err := pool.OUCreate(context.Background(), OUCreateParams{Name: "libre", ParentDN: "DC=lycee,DC=local"}); errors.Is(err, ErrInsufficientRights) {
		t.Errorf("admin_site ne devrait pas être refusé: %v", err)
	}
}

func TestRBACRoleScopeConfineASesOU(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "alice", Role: rbac.AdminClasse, OUs: []string{rbacOUA}})

	// Hors portée : refus avant tout accès réseau.
	if _, err := pool.OUCreate(context.Background(), OUCreateParams{Name: "9emeX", ParentDN: rbacOUB}); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("hors portée devrait être refusé: %v", err)
	}
	// Dans la portée : autorisé (l'échec vient alors du réseau).
	if _, err := pool.OUCreate(context.Background(), OUCreateParams{Name: "3emeA-2", ParentDN: rbacOUA}); errors.Is(err, ErrInsufficientRights) {
		t.Errorf("dans la portée ne devrait pas être refusé: %v", err)
	}
}

func TestRBACRoleScopeSansPorteeNecritPas(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "alice", Role: rbac.Helpdesk})

	if err := pool.UserResetPassword(context.Background(), "CN=x,"+rbacOUA, "Motdepasse1!"); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("un rôle scopé sans portée ne devrait pas écrire: %v", err)
	}
}

func TestRBACGroupesCiblesSurLeGroupe(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "alice", Role: rbac.AdminClasse, Groupes: []string{"OU=groupes,DC=lycee,DC=local"}})

	// Le groupe délégué est autorisé (échec réseau, pas RBAC).
	if err := pool.GroupAddMember(context.Background(), rbacGroupe, "CN=x,"+rbacOUA); errors.Is(err, ErrInsufficientRights) {
		t.Errorf("groupe délégué ne devrait pas être refusé: %v", err)
	}
	// Un groupe hors portée est refusé.
	if err := pool.GroupAddMember(context.Background(), "CN=Autre,OU=autres,DC=lycee,DC=local", "CN=x,"+rbacOUA); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("groupe hors portée devrait être refusé: %v", err)
	}
}

func TestRBACMoveVerifieToutesLesCibles(t *testing.T) {
	pool := poolSansConnexion(t)
	pool.SetPolicy(&rbac.Policy{Operator: "alice", Role: rbac.AdminClasse, OUs: []string{rbacOUA}})

	// La destination est hors portée : refus.
	if err := pool.UserMove(context.Background(), "CN=x,"+rbacOUA, rbacOUB); !errors.Is(err, ErrInsufficientRights) {
		t.Errorf("destination hors portée devrait être refusée: %v", err)
	}
}
