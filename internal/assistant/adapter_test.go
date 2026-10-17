package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

func TestNouveauCollecteurADNil(t *testing.T) {
	collecteur := NouveauCollecteurAD(nil)
	if _, err := collecteur.Collecter(context.Background(), "DC=lycee,DC=local"); err == nil {
		t.Fatal("une erreur est attendue sans pool AD")
	}
}

func TestCollecterSansBaseDN(t *testing.T) {
	// Un pool sans base DN ne permet aucune collecte : l'assistant doit
	// remonter une erreur explicite plutôt que d'interroger une racine vide.
	collecteur := NouveauCollecteurAD(&ad.Pool{})
	_, err := collecteur.Collecter(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "base DN") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestCollecterAnnuaireInjoignable(t *testing.T) {
	pool, err := ad.NewPoolNoValidate(ad.Config{
		Domain:       "lycee.local",
		Controller:   "127.0.0.1",
		Port:         1, // rien n'écoute : le dial doit échouer vite
		BaseDN:       "DC=lycee,DC=local",
		BindDN:       "CN=admin,DC=lycee,DC=local",
		BindPassword: "secret",
		Timeout:      500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPoolNoValidate: %v", err)
	}

	collecteur := NouveauCollecteurAD(pool)
	ctx, annuler := context.WithTimeout(context.Background(), 5*time.Second)
	defer annuler()

	_, err = collecteur.Collecter(ctx, "")
	if err == nil {
		t.Fatal("une erreur est attendue quand l'annuaire est injoignable")
	}
	if !strings.Contains(err.Error(), "collecte des comptes") {
		t.Fatalf("erreur = %v", err)
	}
}
