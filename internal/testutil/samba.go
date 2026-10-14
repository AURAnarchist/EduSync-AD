// Package testutil fournit des helpers pour les tests d'intégration contre
// un vrai Active Directory Samba (container Docker).
package testutil

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// ADConfig contient la configuration pour se connecter à l'AD de test.
type ADConfig struct {
	Domain        string
	Controller    string
	PortLDAP      int
	PortLDAPS     int
	UseLDAPS      bool
	BindDN        string
	BindPassword  string
	BaseDN        string
	TLSCAPath     string // fichier PEM temporaire (CA du container)
	TLSSkipVerify bool
	Timeout       time.Duration
	MaxConns      int
}

// NewSambaADContainer démarre un container Samba AD DC provisionné pour les tests.
// Retourne le container (à nettoyer via t.Cleanup) et une config AD prête à l'emploi.
func NewSambaADContainer(ctx context.Context, t TB) (testcontainers.Container, *ADConfig) {
	t.Helper()

	// S'assurer que l'image est construite
	// Le contexte de build est la racine du projet (où se trouve Dockerfile.samba-ad)
	// On utilise le répertoire de travail courant pour trouver la racine du projet
	workDir, _ := os.Getwd()
	projectRoot := workDir
	// Si on est dans tests/integration, remonter de 2 niveaux
	if strings.HasSuffix(workDir, "tests/integration") {
		projectRoot = filepath.Join(workDir, "..", "..")
	} else if strings.HasSuffix(workDir, "internal/testutil") {
		projectRoot = filepath.Join(workDir, "..", "..")
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    projectRoot,
			Dockerfile: "Dockerfile.samba-ad",
			Repo:       "edusync-ad-samba",
			Tag:        "test",
		},
		Hostname:     "edusync-dc",
		CapAdd:       []string{"SYS_ADMIN"},
		ExposedPorts: []string{"389/tcp", "636/tcp", "88/udp", "53/udp", "445/tcp"},
		WaitingFor: wait.ForLog("Samba AD DC ready").
			WithStartupTimeout(3 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("testutil: démarrage container Samba AD: %v", err)
	}

	// Récupérer les ports publiés
	ldapPort, err := container.MappedPort(ctx, "389/tcp")
	if err != nil {
		t.Fatalf("testutil: port LDAP non publié: %v", err)
	}
	ldapsPort, err := container.MappedPort(ctx, "636/tcp")
	if err != nil {
		t.Fatalf("testutil: port LDAPS non publié: %v", err)
	}

	// Récupérer le CA auto-signé du container
	caData, err := copyCAFromContainer(ctx, container)
	if err != nil {
		t.Fatalf("testutil: copie CA: %v", err)
	}

	// Écrire le CA dans un fichier temporaire
	caFile := filepath.Join(t.TempDir(), "edusync-ad-ca.pem")
	if err := os.WriteFile(caFile, caData, 0o600); err != nil {
		t.Fatalf("testutil: écriture CA: %v", err)
	}

	cfg := &ADConfig{
		Domain:        "EDUSYNC.LOCAL",
		Controller:    "localhost",
		PortLDAP:      int(ldapPort.Num()),
		PortLDAPS:     int(ldapsPort.Num()),
		UseLDAPS:      false, // Try StartTLS on port 389
		BindDN:        "Administrator@EDUSYNC.LOCAL",
		BindPassword:  "TestPass123",
		BaseDN:        "DC=EDUSYNC,DC=LOCAL",
		TLSCAPath:     caFile,
		TLSSkipVerify: false,
		Timeout:       10 * time.Second,
		MaxConns:      5,
	}

	// Nettoyage automatique à la fin du test
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: arrêt container: %v", err)
		}
	})

	return container, cfg
}

// NewSambaADContainerWithLDAP démarre un container et retourne une config pour LDAP+StartTLS (port 389).
func NewSambaADContainerWithLDAP(ctx context.Context, t TB) (testcontainers.Container, *ADConfig) {
	t.Helper()

	container, cfg := NewSambaADContainer(ctx, t)
	cfg.UseLDAPS = false
	cfg.PortLDAPS = 0
	cfg.TLSSkipVerify = true // StartTLS sans CA pour tests simples
	return container, cfg
}

// NewPoolFromConfig crée un pool AD à partir d'une config de test.
func NewPoolFromConfig(cfg *ADConfig) (*ad.Pool, error) {
	adCfg := ad.Config{
		Domain:        cfg.Domain,
		Controller:    cfg.Controller,
		Port:          cfg.PortLDAPS,
		UseLDAPS:      cfg.UseLDAPS,
		BindDN:        cfg.BindDN,
		BindPassword:  cfg.BindPassword,
		BaseDN:        cfg.BaseDN,
		TLSCAPath:     cfg.TLSCAPath,
		TLSSkipVerify: cfg.TLSSkipVerify,
		Timeout:       cfg.Timeout,
		MaxConns:      cfg.MaxConns,
	}
	if !cfg.UseLDAPS {
		adCfg.Port = cfg.PortLDAP
	}
	return ad.NewPool(adCfg)
}

// NewPoolFromConfigLDAP crée un pool pour LDAP+StartTLS.
func NewPoolFromConfigLDAP(cfg *ADConfig) (*ad.Pool, error) {
	adCfg := ad.Config{
		Domain:        cfg.Domain,
		Controller:    cfg.Controller,
		Port:          cfg.PortLDAP,
		UseLDAPS:      false,
		BindDN:        cfg.BindDN,
		BindPassword:  cfg.BindPassword,
		BaseDN:        cfg.BaseDN,
		TLSCAPath:     cfg.TLSCAPath,
		TLSSkipVerify: cfg.TLSSkipVerify,
		Timeout:       cfg.Timeout,
		MaxConns:      cfg.MaxConns,
	}
	return ad.NewPool(adCfg)
}

// copyCAFromContainer récupère le certificat CA du container Samba.
func copyCAFromContainer(ctx context.Context, container testcontainers.Container) ([]byte, error) {
	// Le CA est dans /var/lib/samba/private/tls/ca.pem
	reader, err := container.CopyFileFromContainer(ctx, "/var/lib/samba/private/tls/ca.pem")
	if err != nil {
		return nil, fmt.Errorf("copie CA: %w", err)
	}
	defer reader.Close()

	// Lire le contenu
	caData := make([]byte, 0, 4096)
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			caData = append(caData, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	// Valider que c'est bien un PEM
	block, _ := pem.Decode(caData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("CA récupéré invalide (pas de certificat PEM)")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("certificat CA invalide: %w", err)
	}

	return caData, nil
}

// WithSambaAD est un helper pour les tests d'intégration qui évite le boilerplate.
// Usage :
//
//	func TestSomething(t *testing.T) {
//	    testutil.WithSambaAD(t, func(ctx context.Context, pool *ad.Pool) {
//	        // test avec pool connecté
//	    })
//	}
func WithSambaAD(t TB, f func(ctx context.Context, pool *ad.Pool)) {
	ctx := context.Background()
	// Configurer la lecture de fichiers pour les tests (CA, keytab, etc.)
	ad.SetReadFile(os.ReadFile)

	t.Log("Starting Samba AD container...")
	_, cfg := NewSambaADContainer(ctx, t)
	t.Log("Container started, creating pool...")
	pool, err := NewPoolFromConfig(cfg)
	if err != nil {
		t.Fatalf("testutil: création pool: %v", err)
	}
	t.Log("Pool created, running test...")
	t.Cleanup(func() { pool.Close() })
	f(ctx, pool)
	t.Log("Test function completed")
}

// WithSambaADSimple version qui crée le pool sans pré-validation (pour debug).
func WithSambaADSimple(t TB, f func(ctx context.Context, pool *ad.Pool)) {
	ctx := context.Background()
	ad.SetReadFile(os.ReadFile)

	t.Log("Starting Samba AD container...")
	_, cfg := NewSambaADContainer(ctx, t)
	t.Log("Container started, creating pool without pre-validation...")

	// Créer le pool sans pré-validation
	adCfg := ad.Config{
		Domain:        cfg.Domain,
		Controller:    cfg.Controller,
		Port:          cfg.PortLDAPS,
		UseLDAPS:      cfg.UseLDAPS,
		BindDN:        cfg.BindDN,
		BindPassword:  cfg.BindPassword,
		BaseDN:        cfg.BaseDN,
		TLSCAPath:     cfg.TLSCAPath,
		TLSSkipVerify: cfg.TLSSkipVerify,
		Timeout:       cfg.Timeout,
		MaxConns:      cfg.MaxConns,
	}
	pool, err := ad.NewPoolNoValidate(adCfg)
	if err != nil {
		t.Fatalf("testutil: création pool: %v", err)
	}
	t.Log("Pool created (no pre-validate), running test...")
	t.Cleanup(func() { pool.Close() })
	f(ctx, pool)
	t.Log("Test function completed")
}

// WithSambaADLDAP version LDAP+StartTLS.
func WithSambaADLDAP(t TB, f func(ctx context.Context, pool *ad.Pool)) {
	ctx := context.Background()
	// Configurer la lecture de fichiers pour les tests (CA, keytab, etc.)
	ad.SetReadFile(os.ReadFile)

	_, cfg := NewSambaADContainerWithLDAP(ctx, t)
	pool, err := NewPoolFromConfigLDAP(cfg)
	if err != nil {
		t.Fatalf("testutil: création pool LDAP: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	f(ctx, pool)
}

// TB est l'interface testing.TB (testing.T, testing.B) pour les helpers.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Log(args ...any)
	Cleanup(func())
	TempDir() string
}

// GetTestUsers retourne une liste de sAMAccountName de test prévisibles.
func GetTestUsers() map[string]string {
	return map[string]string{
		"eleve01": "CN=eleve01,OU=Eleves,DC=EDUSYNC,DC=LOCAL",
		"eleve25": "CN=eleve25,OU=Eleves,DC=EDUSYNC,DC=LOCAL",
		"perso01": "CN=perso01,OU=Personnels,DC=EDUSYNC,DC=LOCAL",
		"perso25": "CN=perso25,OU=Personnels,DC=EDUSYNC,DC=LOCAL",
	}
}

// GetTestGroups retourne les noms de groupes de test prévisibles.
func GetTestGroups() []string {
	return []string{
		"Classe_6e_A", "Classe_6e_B", "Classe_5e_A", "Classe_4e_A", "Classe_3e_A",
		"Enseignants", "Administration", "IT",
		"RBAC_Direction_Tous", "RBAC_Scolarite_Eleves", "RBAC_RH_Personnels", "RBAC_IT_Services", "RBAC_Lecture_Archive",
	}
}

// GetTestOUs retourne les OUs de test.
func GetTestOUs() []string {
	return []string{
		"OU=Eleves,DC=EDUSYNC,DC=LOCAL",
		"OU=Personnels,DC=EDUSYNC,DC=LOCAL",
		"OU=Groupes,DC=EDUSYNC,DC=LOCAL",
		"OU=Services,DC=EDUSYNC,DC=LOCAL",
		"OU=Archive,DC=EDUSYNC,DC=LOCAL",
	}
}
