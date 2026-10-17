package ad

import (
	"errors"
	"testing"
)

// TestScriptTypeValide vérifie les types de script supportés.
func TestScriptTypeValide(t *testing.T) {
	cas := []struct {
		typ    ScriptType
		valide bool
	}{
		{ScriptTypeBat, true},
		{ScriptTypePowerShell, true},
		{ScriptTypeVBS, true},
		{ScriptType("inconnu"), false},
	}
	for _, c := range cas {
		if c.typ.Valide() != c.valide {
			t.Errorf("ScriptType %q.Valide() = %v, attendu %v", c.typ, !c.valide, c.valide)
		}
	}
}

// TestScriptParamsValidate vérifie la validation des paramètres.
func TestScriptParamsValidate(t *testing.T) {
	// DN manquant.
	p := &ScriptParams{Type: ScriptTypeBat, Contenu: "echo hello"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Type invalide.
	p = &ScriptParams{UserDN: "CN=Test,OU=Users,DC=X", Type: "invalide", Contenu: "echo hello"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrScriptTypeInvalide) {
		t.Errorf("Validate type invalide: err=%v, attendu ErrScriptTypeInvalide", err)
	}

	// Contenu vide.
	p = &ScriptParams{UserDN: "CN=Test,OU=Users,DC=X", Type: ScriptTypeBat, Contenu: ""}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrScriptContenuVide) {
		t.Errorf("Validate contenu vide: err=%v, attendu ErrScriptContenuVide", err)
	}

	// Valide complet.
	p = &ScriptParams{UserDN: "CN=Test,OU=Users,DC=X", Type: ScriptTypeBat, Contenu: "echo hello"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate complet: err=%v, attendu nil", err)
	}
}

// TestResoudreVariablesScript vérifie la substitution des variables.
func TestResoudreVariablesScript(t *testing.T) {
	cas := []struct {
		contenu string
		user    *User
		attendu string
	}{
		{
			"echo %USERNAME%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand"},
			"echo j.durand",
		},
		{
			"echo %FULLNAME%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand"},
			"echo Jean Durand",
		},
		{
			"echo %OU%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand", OU: "OU=Eleves,DC=X"},
			"echo Eleves",
		},
		{
			"echo %GROUP%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand", MemberOf: []string{"CN=Classe_6e_A,OU=Groupes,DC=X"}},
			"echo Classe_6e_A",
		},
		{
			"echo %EMAIL%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand", Email: "jean.durand@edusync.local"},
			"echo jean.durand@edusync.local",
		},
		{
			"echo %HOMEDIR%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand", OU: "OU=Eleves,DC=X"},
			"echo \\\\srv\\homes\\j.durand",
		},
		{
			"pas de variable",
			&User{SAMAccountName: "j.durand"},
			"pas de variable",
		},
	}
	for _, c := range cas {
		got := resoudreVariablesScript(c.contenu, c.user)
		if got != c.attendu {
			t.Errorf("resoudreVariablesScript(%q) = %q, attendu %q", c.contenu, got, c.attendu)
		}
	}
}

// TestScriptBulkParamsValidate vérifie la validation en masse.
func TestScriptBulkParamsValidate(t *testing.T) {
	// Aucun DN ni OU.
	p := &ScriptBulkParams{Type: ScriptTypeBat, Contenu: "echo hello"}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans DN ni OU devrait échouer")
	}

	// Avec OU seulement.
	p = &ScriptBulkParams{OU: "OU=Users,DC=X", Type: ScriptTypeBat, Contenu: "echo hello"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec OU: err=%v, attendu nil", err)
	}

	// Avec UserDNs seulement.
	p = &ScriptBulkParams{UserDNs: []string{"CN=Test,OU=Users,DC=X"}, Type: ScriptTypeBat, Contenu: "echo hello"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec UserDNs: err=%v, attendu nil", err)
	}
}

// TestExtensionScriptType vérifie l'extension de fichier par type.
func TestExtensionScriptType(t *testing.T) {
	cas := []struct {
		typ     ScriptType
		attendu string
	}{
		{ScriptTypeBat, ".bat"},
		{ScriptTypePowerShell, ".ps1"},
		{ScriptTypeVBS, ".vbs"},
	}
	for _, c := range cas {
		if got := extensionScript(c.typ); got != c.attendu {
			t.Errorf("extensionScript(%q) = %q, attendu %q", c.typ, got, c.attendu)
		}
	}
}
