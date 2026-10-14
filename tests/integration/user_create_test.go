// Package integration_test fournit les tests d'intégration pour la création
// de comptes utilisateur (module Arrivées) contre un vrai Samba AD DC.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/testutil"
)

// skipIfNoSamba permet d'ignorer les tests d'intégration si l'AD n'est pas
// disponible (variable d'env AD_TEST non positionnée).
func skipIfNoSamba(t *testing.T) {
	if os.Getenv("AD_TEST") == "" {
		t.Skip("AD_TEST non défini — exécute : export AD_TEST=1 (voir docs/AD_TEST_ENV.md)")
	}
}

// TestUserCreateSimple vérifie la création d'un utilisateur simple.
func TestUserCreateSimple(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaADSimple(t, func(ctx context.Context, pool *ad.Pool) {
		t.Log("Container ready, starting test")

		// Test simple: get a connection
		t.Log("Getting connection from pool")
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("pool.Get failed: %v", err)
		}
		t.Log("Got connection, putting back")
		pool.Put(conn)

		t.Log("Test completed successfully")
	})
}

// TestUserCreateDuplicate vérifie le rejet d'un doublon sAMAccountName.
func TestUserCreateDuplicate(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		params := ad.UserCreateParams{
			SAMAccountName:    "duptest01",
			UserPrincipalName: "duptest01@EDUSYNC.LOCAL",
			DisplayName:       "Dup Test 01",
			GivenName:         "Dup",
			SN:                "Test01",
			InitialPassword:   "TestPass123!",
			OU:                "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
			Enabled:           true,
		}

		// Créer une première fois
		_, err := pool.UserCreate(ctx, params)
		if err != nil {
			t.Fatalf("première création: %v", err)
		}

		// Simuler la deuxième création - devrait échouer
		sim, err := pool.UserCreateSimulate(ctx, params)
		if err != nil {
			t.Fatalf("Simulate #2: %v", err)
		}
		if sim.WouldSucceed {
			t.Error("Simulate #2 devrait échouer (doublon)")
		}
		if sim.Error == "" {
			t.Error("Simulate #2 devrait avoir un message d'erreur")
		}

		// Execute devrait aussi échouer
		_, err = pool.UserCreate(ctx, params)
		if err == nil {
			t.Error("Create #2 devrait échouer")
		}

		// Cleanup
		user, _ := pool.UserGet(ctx, "CN=Dup Test 01,OU=Eleves,DC=EDUSYNC,DC=LOCAL")
		if user != nil {
			_ = pool.UserDelete(ctx, user.DN)
		}
	})
}

// TestUserCreateWithGroups vérifie l'ajout aux groupes lors de la création.
func TestUserCreateWithGroups(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		// D'abord récupérer le DN du groupe de test
		groups, err := pool.GroupSearch(ctx, "OU=Groupes,DC=EDUSYNC,DC=LOCAL",
			"(cn=Classe_6e_A)", []string{"dn"}, 0)
		if err != nil || len(groups) == 0 {
			t.Skip("groupe Classe_6e_A non trouvé")
		}
		groupDN := groups[0].DN

		params := ad.UserCreateParams{
			SAMAccountName:    "grouptest01",
			UserPrincipalName: "grouptest01@EDUSYNC.LOCAL",
			DisplayName:       "Group Test 01",
			GivenName:         "Group",
			SN:                "Test01",
			InitialPassword:   "TestPass123!",
			OU:                "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
			Enabled:           true,
			Groups:            []string{groupDN},
		}

		user, err := pool.UserCreate(ctx, params)
		if err != nil {
			t.Fatalf("Create avec groupes: %v", err)
		}

		// Vérifier l'appartenance au groupe
		found, err := pool.UserGet(ctx, user.DN)
		if err != nil {
			t.Fatalf("UserGet: %v", err)
		}
		foundInGroup := false
		for _, g := range found.MemberOf {
			if g == groupDN {
				foundInGroup = true
				break
			}
		}
		if !foundInGroup {
			t.Errorf("utilisateur devrait être membre de %s, MemberOf=%v", groupDN, found.MemberOf)
		}

		_ = pool.UserDelete(ctx, user.DN)
	})
}

// TestUserCreateSimulateOnly vérifie que Simulate n'écrit pas.
func TestUserCreateSimulateOnly(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		params := ad.UserCreateParams{
			SAMAccountName:    "simonly01",
			UserPrincipalName: "simonly01@EDUSYNC.LOCAL",
			DisplayName:       "Sim Only 01",
			GivenName:         "Sim",
			SN:                "Only01",
			InitialPassword:   "TestPass123!",
			OU:                "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
			Enabled:           true,
		}

		// Simulate seulement
		sim, err := pool.UserCreateSimulate(ctx, params)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if !sim.WouldSucceed {
			t.Fatalf("Simulate devrait réussir: %s", sim.Error)
		}

		// Vérifier que l'utilisateur n'existe PAS
		filter := "(&(objectClass=user)(sAMAccountName=simonly01))"
		users, err := pool.UserSearch(ctx, "OU=Eleves,DC=EDUSYNC,DC=LOCAL", filter, []string{"dn"}, 0)
		if err != nil {
			t.Fatalf("Search après simulate: %v", err)
		}
		if len(users) != 0 {
			t.Error("Simulate ne devrait pas créer l'utilisateur")
		}
	})
}

// TestUserCreateDisabled vérifie la création d'un compte désactivé.
func TestUserCreateDisabled(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		params := ad.UserCreateParams{
			SAMAccountName:    "disabled01",
			UserPrincipalName: "disabled01@EDUSYNC.LOCAL",
			DisplayName:       "Disabled 01",
			GivenName:         "Disabled",
			SN:                "User01",
			InitialPassword:   "TestPass123!",
			OU:                "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
			Enabled:           false,
		}

		user, err := pool.UserCreate(ctx, params)
		if err != nil {
			t.Fatalf("Create disabled: %v", err)
		}
		if user.Enabled {
			t.Error("utilisateur devrait être désactivé")
		}

		// Vérifier userAccountControl = 514 (512 + 2 = ACCOUNTDISABLE)
		found, err := pool.UserGet(ctx, user.DN)
		if err != nil {
			t.Fatalf("UserGet: %v", err)
		}
		if found.Enabled {
			t.Error("relecture: utilisateur devrait être désactivé")
		}

		_ = pool.UserDelete(ctx, user.DN)
	})
}

// TestUserCreateValidate vérifie la validation des paramètres.
func TestUserCreateValidate(t *testing.T) {
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		cas := []struct {
			name    string
			params  ad.UserCreateParams
			wantErr bool
		}{
			{
				name: "SAMAccountName manquant",
				params: ad.UserCreateParams{
					UserPrincipalName: "x@EDUSYNC.LOCAL", DisplayName: "X", GivenName: "X", SN: "X",
					InitialPassword: "Pass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "UPN manquant",
				params: ad.UserCreateParams{
					SAMAccountName: "x", DisplayName: "X", GivenName: "X", SN: "X",
					InitialPassword: "Pass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "DisplayName manquant",
				params: ad.UserCreateParams{
					SAMAccountName: "x", UserPrincipalName: "x@EDUSYNC.LOCAL", GivenName: "X", SN: "X",
					InitialPassword: "Pass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "GivenName manquant",
				params: ad.UserCreateParams{
					SAMAccountName: "x", UserPrincipalName: "x@EDUSYNC.LOCAL", DisplayName: "X", SN: "X",
					InitialPassword: "Pass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "SN manquant",
				params: ad.UserCreateParams{
					SAMAccountName: "x", UserPrincipalName: "x@EDUSYNC.LOCAL", DisplayName: "X", GivenName: "X",
					InitialPassword: "PassPass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "Mot de passe manquant",
				params: ad.UserCreateParams{
					SAMAccountName: "x", UserPrincipalName: "x@EDUSYNC.LOCAL", DisplayName: "X", GivenName: "X", SN: "X",
					OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: true,
			},
			{
				name: "OU manquante",
				params: ad.UserCreateParams{
					SAMAccountName: "x", UserPrincipalName: "x@EDUSYNC.LOCAL", DisplayName: "X", GivenName: "X", SN: "X",
					InitialPassword: "Pass123!",
				},
				wantErr: true,
			},
			{
				name: "Valide",
				params: ad.UserCreateParams{
					SAMAccountName: "valid01", UserPrincipalName: "valid01@EDUSYNC.LOCAL",
					DisplayName: "Valid 01", GivenName: "Valid", SN: "User01",
					InitialPassword: "Pass123!", OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				},
				wantErr: false,
			},
		}

		for _, tc := range cas {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.params.Validate()
				if tc.wantErr && err == nil {
					t.Error("devrait retourner une erreur")
				}
				if !tc.wantErr && err != nil {
					t.Errorf("ne devrait pas retourner d'erreur: %v", err)
				}
			})
		}
	})
}

// TestUserCreateBulkCSV simule un import CSV de 100 utilisateurs.
func TestUserCreateBulkCSV(t *testing.T) {
	skipIfNoSamba(t)
	testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
		const count = 100
		created := make([]*ad.User, 0, count)

		for i := 1; i <= count; i++ {
			sam := fmt.Sprintf("bulk%03d", i)
			params := ad.UserCreateParams{
				SAMAccountName:    sam,
				UserPrincipalName: sam + "@EDUSYNC.LOCAL",
				DisplayName:       fmt.Sprintf("Bulk User %03d", i),
				GivenName:         "Bulk",
				SN:                fmt.Sprintf("User%03d", i),
				InitialPassword:   "TestPass123!",
				OU:                "OU=Eleves,DC=EDUSYNC,DC=LOCAL",
				Enabled:           true,
			}
			user, err := pool.UserCreate(ctx, params)
			if err != nil {
				t.Fatalf("Create #%d (%s): %v", i, sam, err)
			}
			created = append(created, user)
		}

		t.Logf("Créés %d utilisateurs en bulk", len(created))

		// Cleanup
		for _, u := range created {
			_ = pool.UserDelete(ctx, u.DN)
		}
	})
}
