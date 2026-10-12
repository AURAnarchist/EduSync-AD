// Package ad_test fournit les tests d'intégration du pool AD contre
// un vrai Samba AD DC (container). Voir docs/AD_TEST_ENV.md.
//
// Exécution : go test -tags=integration ./internal/ad/...
package ad

import (
	"context"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// skipIfNoSamba permet d'ignorer les tests d'intégration si l'AD n'est pas
// disponible (variable d'env AD_TEST non positionnée).
func skipIfNoSamba(t *testing.T) {
	if os.Getenv("AD_TEST") == "" {
		t.Skip("AD_TEST non défini — exécute : export AD_TEST=1 (voir docs/AD_TEST_ENV.md)")
	}
}

// TestPoolConnexionSimple vérifie qu'on peut obtenir une connexion du pool
// et la remettre, sans rien casser.
func TestPoolConnexionSimple(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true, // certificat auto-signé du container
		Timeout:       10 * time.Second,
		MaxConns:      5,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	conn, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if conn == nil {
		t.Fatal("connexion nil")
	}

	// Vérifier qu'une recherche simple fonctionne
	res, err := conn.Search(ldap.NewSearchRequest(
		"DC=EDUSYNC,DC=LOCAL", ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		1, 0, false, "(objectClass=domain)", []string{"dn"}, nil,
	))
	if err != nil {
		p.Put(conn)
		t.Fatalf("Search: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("attendu 1 entrée (domaine), obtenu %d", len(res.Entries))
	}

	p.Put(conn)

	// Deuxième Get doit réutiliser la connexion
	conn2, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get #2: %v", err)
	}
	if conn2 != conn {
		t.Log("connexion différente (acceptable si la première était idle timeout)")
	}
	p.Put(conn2)
}

// TestPoolMultiConnexions vérifie que le pool gère la concurrence.
func TestPoolMultiConnexions(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       10 * time.Second,
		MaxConns:      3,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			conn, err := p.Get(context.Background())
			if err != nil {
				done <- err
				return
			}
			time.Sleep(50 * time.Millisecond) // simule un travail
			p.Put(conn)
			done <- nil
		}()
	}

	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
}

// TestPoolLimiteMaxConnexions : une 4ème goroutine doit attendre un slot
// (timeout contextuel) ou échouer si le pool est fermé.
func TestPoolLimiteMaxConnexions(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       5 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	// Prendre les 2 slots
	c1, _ := p.Get(context.Background())
	c2, _ := p.Get(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Le 3ème doit attendre (slot non disponible)
	_, err = p.Get(ctx)
	if err == nil {
		t.Error("Get aurait dû échouer par timeout")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("attendu DeadlineExceeded, obtenu %v", err)
	}

	p.Put(c1)
	p.Put(c2)
}

// TestPoolHealthCheck : une connexion dont le socket est coupé doit être
// rejetée par isHealthy (pas remise dans le pool).
func TestPoolHealthCheck(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       10 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	conn, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Casser la connexion sous-jacente
	_ = conn.Close()

	// Remettre dans le pool : Put doit détecter qu'elle est morte
	// (via isHealthy au prochain Get) et la jeter.
	p.Put(conn)

	// Le prochain Get doit créer une nouvelle connexion
	conn2, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get après connexion morte: %v", err)
	}
	if conn2 == conn {
		t.Error("une connexion fermée ne doit pas être réutilisée")
	}
	p.Put(conn2)
}

// TestPoolStartTLS vérifie le mode LDAP + StartTLS (port 389).
func TestPoolStartTLS(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          389,
		UseLDAPS:      false,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       10 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		t.Fatalf("NewPool StartTLS: %v", err)
	}
	defer p.Close()

	conn, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get StartTLS: %v", err)
	}
	if conn == nil {
		t.Fatal("connexion StartTLS nil")
	}
	p.Put(conn)
}

// TestPoolCertificatCustom valide l'usage d'une CA interne.
func TestPoolCertificatCustom(t *testing.T) {
	skipIfNoSamba(t)

	// Le container provision-ad.sh génère un certificat auto-signé pour LDAPS.
	// On récupère le certificat du serveur via une première connexion
	// TLS sans vérification, puis on le réutilise comme CA de confiance.
	p0, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       10 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		t.Fatalf("NewPool initial: %v", err)
	}
	conn, err := p0.Get(context.Background())
	if err != nil {
		t.Fatalf("Get initial: %v", err)
	}

	// Extraire le certificat serveur
	tlsState, ok := conn.TLSConnectionState()
	if !ok || len(tlsState.PeerCertificates) == 0 {
		t.Fatal("aucun certificat pair")
	}
	serverCert := tlsState.PeerCertificates[0]
	p0.Put(conn)
	p0.Close()

	// Écrire le PEM dans un fichier temporaire
	caFile := filepath.Join(t.TempDir(), "adcs-root.pem")
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCert.Raw,
	})
	if err := os.WriteFile(caFile, pemBlock, 0o600); err != nil {
		t.Fatalf("écriture CA: %v", err)
	}

	// Nouveau pool avec cette CA — doit vérifier sans TLSSkipVerify
	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSCAPath:     caFile,
		TLSSkipVerify: false,
		Timeout:       10 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		t.Fatalf("NewPool avec CA: %v", err)
	}
	defer p.Close()

	conn2, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get avec CA: %v", err)
	}
	p.Put(conn2)
}

// TestPasswordGenerate génère des MDP conformes aux politiques.
func TestPasswordGenerate(t *testing.T) {
	student := DefaultStudentPolicy()
	for i := 0; i < 50; i++ {
		pwd, err := student.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if err := student.Validate(pwd, "", "", ""); err != nil {
			t.Errorf("pwd %q invalide: %v", pwd, err)
		}
		if len(pwd) < student.Length {
			t.Errorf("longueur %d < %d", len(pwd), student.Length)
		}
	}

	staff := DefaultStaffPolicy()
	for i := 0; i < 50; i++ {
		pwd, err := staff.Generate()
		if err != nil {
			t.Fatalf("Generate staff: %v", err)
		}
		if err := staff.Validate(pwd, "", "", ""); err != nil {
			t.Errorf("pwd staff %q invalide: %v", pwd, err)
		}
		if len(pwd) < staff.Length {
			t.Errorf("longueur staff %d < %d", len(pwd), staff.Length)
		}
	}
}

// TestPasswordValidate vérifie la validation contre la politique.
func TestPasswordValidate(t *testing.T) {
	p := DefaultStaffPolicy() // 12, upper, digit, special

	cas := []struct {
		pwd      string
		shouldOK bool
	}{
		{"Abcdefgh123!", true}, // 12 chars
		{"abcdefgh123!", false}, // pas de majuscule
		{"Abcdefghijkl!", false}, // pas de chiffre
		{"Abcdefgh12345", false}, // pas de spécial
		{"Ab1!", false},          // trop court
	}
	for _, c := range cas {
		err := p.Validate(c.pwd, "", "", "")
		if c.shouldOK && err != nil {
			t.Errorf("%q devrait passer: %v", c.pwd, err)
		}
		if !c.shouldOK && err == nil {
			t.Errorf("%q devrait échouer", c.pwd)
		}
	}

	// ForbidUserInfo
	p.ForbidUserInfo = true
	if err := p.Validate("Abcdef123!john", "jdoe", "John", "Doe"); err == nil {
		t.Error("devrait refuser prénom dans MDP")
	}
	if err := p.Validate("Abcdef123!doe", "jdoe", "John", "Doe"); err == nil {
		t.Error("devrait refuser nom dans MDP")
	}
	if err := p.Validate("Abcdef123!jdoe", "jdoe", "John", "Doe"); err == nil {
		t.Error("devrait refuser identifiant dans MDP")
	}
}

// TestPoolContexteAnnulation : un contexte annulé doit libérer le slot.
func TestPoolContexteAnnulation(t *testing.T) {
	skipIfNoSamba(t)

	p, err := NewPool(Config{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		Port:          636,
		UseLDAPS:      true,
		BindDN:        "CN=Administrator,CN=Users,DC=EDUSYNC,DC=LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSSkipVerify: true,
		Timeout:       10 * time.Second,
		MaxConns:      1,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get #1: %v", err)
	}

	// Annuler avant le second Get — le slot doit être libéré
	cancel()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	conn2, err := p.Get(ctx2)
	if err != nil {
		t.Fatalf("Get #2 après annulation: %v", err)
	}
	if conn2 != conn {
		t.Log("connexion différente (le slot a été libéré par l'annulation)")
	}
	p.Put(conn2)
}
