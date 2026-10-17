package exchange

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// ─── Fixtures ─────────────────────────────────────────────────────────────

func configSurSite() Config {
	cfg := ConfigParDefaut()
	cfg.Mode = "onprem"
	cfg.SMTPDomain = "lycee.fr"
	cfg.ExchangeServer = "exch01.lycee.local"
	cfg.MailboxQuota = "10GB"
	return cfg
}

func configEnLigne() Config {
	cfg := ConfigParDefaut()
	cfg.Mode = "online"
	cfg.SMTPDomain = "lycee.fr"
	cfg.TenantID = "11111111-2222-3333-4444-555555555555"
	cfg.ClientID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	cfg.CertificateThumbprint = "ABC123DEF456"
	cfg.Organization = "lycee.onmicrosoft.com"
	return cfg
}

func utilisateursExemple() []UtilisateurBal {
	return []UtilisateurBal{
		{SAM: "jdurand", CN: "Jean Durand", DN: "CN=Jean Durand,OU=Profs,DC=lycee,DC=local"},
		{SAM: "amartin", CN: "Alice Martin (amartin)", DN: "CN=Alice Martin,OU=6A,DC=lycee,DC=local"},
		{SAM: "", CN: "Sans identifiant"},
	}
}

// ─── Configuration ────────────────────────────────────────────────────────

func TestConfigSurSiteValide(t *testing.T) {
	if errs := configSurSite().Validate(); len(errs) != 0 {
		t.Errorf("config sur site rejetée : %v", errs)
	}
	if errs := configEnLigne().Validate(); len(errs) != 0 {
		t.Errorf("config en ligne rejetée : %v", errs)
	}
}

func TestConfigValidate(t *testing.T) {
	cas := []struct {
		nom     string
		cfg     Config
		attendu string
	}{
		{"domaine vide", func() Config { c := configSurSite(); c.SMTPDomain = ""; return c }(), "domaine SMTP"},
		{"domaine avec @", func() Config { c := configSurSite(); c.SMTPDomain = "x@lycee.fr"; return c }(), "simple domaine"},
		{"quota invalide", func() Config { c := configSurSite(); c.MailboxQuota = "beaucoup"; return c }(), "Quota invalide"},
		{"serveur vide", func() Config { c := configSurSite(); c.ExchangeServer = ""; return c }(), "serveur Exchange"},
		{"client id vide", func() Config { c := configEnLigne(); c.ClientID = ""; return c }(), "client ID"},
		{"empreinte vide", func() Config { c := configEnLigne(); c.CertificateThumbprint = ""; return c }(), "empreinte"},
		{"mode inconnu", Config{Mode: "lotus"}, "Mode"},
	}
	for _, c := range cas {
		errs := c.cfg.Validate()
		if !contientContient(errs, c.attendu) {
			t.Errorf("%s : %q absent de %v", c.nom, c.attendu, errs)
		}
	}
}

func TestConfigPersistence(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, FichierConfig)
	cfg := configSurSite()
	cfg.ArchiveEnabled = true
	if err := SauvegarderConfig(chemin, cfg); err != nil {
		t.Fatalf("SauvegarderConfig: %v", err)
	}
	if relu := ChargerConfig(chemin); !reflect.DeepEqual(relu, cfg) {
		t.Errorf("round-trip : %+v ≠ %+v", relu, cfg)
	}
}

func TestConfigAbsentEtCorrompuEtPartiel(t *testing.T) {
	dir := t.TempDir()
	if cfg := ChargerConfig(filepath.Join(dir, "absent.json")); !reflect.DeepEqual(cfg, ConfigParDefaut()) {
		t.Errorf("absent : %+v", cfg)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := ChargerConfig(bad); !reflect.DeepEqual(cfg, ConfigParDefaut()) {
		t.Errorf("corrompu : %+v", cfg)
	}
	partiel := filepath.Join(dir, "p.json")
	if err := os.WriteFile(partiel, []byte(`{"smtp_domain":"lycee.fr"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ChargerConfig(partiel)
	if cfg.Mode != "onprem" || cfg.MailboxQuota != "10GB" || cfg.SMTPDomain != "lycee.fr" {
		t.Errorf("partiel : %+v", cfg)
	}
}

func TestMotifsAlias(t *testing.T) {
	for _, motif := range []string{"{identifiant}", "{prenom}.{nom}"} {
		if _, ok := MotifsAlias[motif]; !ok {
			t.Errorf("motif %q manquant", motif)
		}
	}
}

// ─── Adresses ─────────────────────────────────────────────────────────────

func TestNormaliserPartieLocale(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"Jean Dupont", "jean.dupont"},
		{"Élève De La Croix", "eleve.de.la.croix"},
		{"  #Déjà  vu!  ", "deja.vu"},
		{"???", "adresse"},
	}
	for _, c := range cas {
		if got := NormaliserPartieLocale(c.entree); got != c.attendu {
			t.Errorf("NormaliserPartieLocale(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
}

func TestDecouperNomAffichage(t *testing.T) {
	prenom, nom := DecouperNomAffichage("Alice Martin (amartin)", "amartin")
	if prenom != "Alice" || nom != "Martin" {
		t.Errorf("découpage = %q / %q", prenom, nom)
	}
	if p, n := DecouperNomAffichage("", ""); p != "" || n != "" {
		t.Errorf("vide = %q / %q", p, n)
	}
	if p, n := DecouperNomAffichage("Solo", ""); p != "Solo" || n != "" {
		t.Errorf("mono = %q / %q", p, n)
	}
}

func TestQuotaAlerte(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"10GB", "9GB"},
		{"500MB", "450MB"},
		{"1B", "1B"},
		{"inconnu", "inconnu"},
	}
	for _, c := range cas {
		if got := QuotaAlerte(c.entree); got != c.attendu {
			t.Errorf("QuotaAlerte(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
}

func TestConstructionPlans(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "lycee.local")
	if len(plans) != 2 {
		t.Fatalf("plans = %d (le compte sans sam doit être exclu)", len(plans))
	}
	jd := plans[0]
	if jd.Primaire != "jdurand@lycee.fr" {
		t.Errorf("primaire = %q", jd.Primaire)
	}
	if len(jd.Alias) != 1 || jd.Alias[0] != "jean.durand@lycee.fr" {
		t.Errorf("alias = %v", jd.Alias)
	}
	if jd.UPN != "jdurand@lycee.local" {
		t.Errorf("upn = %q", jd.UPN)
	}
	adresses := jd.AdressesSMTP()
	if adresses[0] != "SMTP:jdurand@lycee.fr" || adresses[1] != "smtp:jean.durand@lycee.fr" {
		t.Errorf("adresses = %v", adresses)
	}
	if plans[1].UPN != "amartin@lycee.local" {
		t.Errorf("upn amartin = %q", plans[1].UPN)
	}
}

func TestPlansMotifsEtDoublons(t *testing.T) {
	// sam identique au motif → pas d'alias doublon
	users := []UtilisateurBal{{SAM: "jean.dupont", CN: "Jean Dupont", DN: "CN=x,DC=l,DC=local"}}
	plans := ConstruirePlansBals(users, configSurSite(), "lycee.local")
	if plans[0].Primaire != "jean.dupont@lycee.fr" || len(plans[0].Alias) != 0 {
		t.Errorf("doublon non supprimé : %+v", plans[0])
	}

	cfgIdent := configSurSite()
	cfgIdent.AliasPattern = "{identifiant}"
	if plans := ConstruirePlansBals(utilisateursExemple(), cfgIdent, "lycee.local"); len(plans[0].Alias) != 0 {
		t.Errorf("motif identifiant produit un alias : %v", plans[0].Alias)
	}

	cfgNom := configSurSite()
	cfgNom.AliasPattern = "{nom}.{prenom}"
	if plans := ConstruirePlansBals(utilisateursExemple(), cfgNom, "lycee.local"); len(plans[0].Alias) != 1 || plans[0].Alias[0] != "durand.jean@lycee.fr" {
		t.Errorf("motif nom.prenom = %v", plans[0].Alias)
	}

	if plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), ""); plans[0].UPN != "" {
		t.Errorf("upn sans domaine = %q", plans[0].UPN)
	}
}

func TestLabelPlan(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "lycee.local")
	label := plans[0].Label()
	if !strings.Contains(label, "jdurand → jdurand@lycee.fr") || !strings.Contains(label, "+1 alias") {
		t.Errorf("label = %q", label)
	}
}

func TestPlansGroupes(t *testing.T) {
	plans := ConstruirePlansGroupes([]GroupeBal{
		{DN: "CN=Conseil de classe,OU=Grp,DC=lycee,DC=local", Nom: "Conseil de classe"},
	}, configSurSite())
	plan := plans[0]
	if plan.Kind != "group" || plan.Primaire != "conseildeclasse@lycee.fr" {
		t.Errorf("plan groupe = %+v", plan)
	}
	if len(plan.Alias) != 1 || plan.Alias[0] != "conseil.de.classe@lycee.fr" {
		t.Errorf("alias groupe = %v", plan.Alias)
	}
}

// ─── Scripts ──────────────────────────────────────────────────────────────

func TestScriptSurSite(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "lycee.local")
	script := ScriptSurSite(plans, configSurSite())
	for _, a := range []string{
		"#Requires -RunAsAdministrator",
		"New-PSSession -ConfigurationName Microsoft.Exchange",
		"-ComputerName 'exch01.lycee.local'",
		"Import-PSSession",
		"Remove-PSSession",
		"if (-not (Get-Mailbox -Identity $identity",
		"Set-Mailbox -Identity $identity -EmailAddresses @{Replace = $addresses}",
		"-ProhibitSendReceiveQuota '10GB'",
		"-IssueWarningQuota '9GB'",
	} {
		if !strings.Contains(script, a) {
			t.Errorf("script sur site sans %q", a)
		}
	}
	if strings.Contains(script, "Enable-Mailbox -Identity $identity -Archive") || strings.Contains(script, "RetentionPolicy") {
		t.Error("archive/rétention présents alors que désactivés")
	}

	avecBase := ScriptSurSite(plans, func() Config { c := configSurSite(); c.Database = "MBX01"; return c }())
	if !strings.Contains(avecBase, "-Alias 'jdurand' -Database 'MBX01'") {
		t.Errorf("base de données absente :\n%s", avecBase)
	}

	archive := ScriptSurSite(plans, func() Config {
		c := configSurSite()
		c.ArchiveEnabled = true
		c.RetentionPolicy = "EduSync - 3 ans"
		return c
	}())
	if !strings.Contains(archive, "-Archive") || !strings.Contains(archive, "RetentionPolicy 'EduSync - 3 ans'") {
		t.Errorf("archive/rétention absents :\n%s", archive)
	}

	sansDomaine := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "")
	if got := ScriptSurSite(sansDomaine, configSurSite()); !strings.Contains(got, "$identity = 'jdurand@lycee.fr'") {
		t.Errorf("identité de repli absente")
	}
}

func TestScriptSurSiteGroupes(t *testing.T) {
	groupes := ConstruirePlansGroupes([]GroupeBal{{DN: "CN=Profs,OU=Grp,DC=lycee,DC=local", Nom: "Profs"}}, configSurSite())
	script := ScriptSurSite(groupes, configSurSite())
	if !strings.Contains(script, "Enable-DistributionGroup") || !strings.Contains(script, "Set-DistributionGroup -Identity $group -EmailAddresses") {
		t.Errorf("cmdlets de groupe absentes :\n%s", script)
	}
	if strings.Contains(script, "Enable-Mailbox") {
		t.Error("Enable-Mailbox présent pour un groupe seul")
	}
}

func TestScriptEnLigne(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configEnLigne(), "lycee.local")
	script := ScriptEnLigne(plans, configEnLigne())
	for _, a := range []string{
		"#Requires -Modules ExchangeOnlineManagement",
		"Connect-ExchangeOnline -Organization 'lycee.onmicrosoft.com'",
		"-TenantId '11111111-2222-3333-4444-555555555555'",
		"-CertificateThumbprint 'ABC123DEF456'",
		"Disconnect-ExchangeOnline",
		"Boîte absente (compte ou licence manquante)",
		"Set-Mailbox -Identity $identity -EmailAddresses @{Replace = $addresses}",
		"-ProhibitSendReceiveQuota '5GB'",
	} {
		if a == "-ProhibitSendReceiveQuota '5GB'" {
			continue // quota 5GB testé séparément
		}
		if !strings.Contains(script, a) {
			t.Errorf("script en ligne sans %q", a)
		}
	}
	avecQuota := ScriptEnLigne(plans, func() Config { c := configEnLigne(); c.MailboxQuota = "5GB"; return c }())
	if !strings.Contains(avecQuota, "-ProhibitSendReceiveQuota '5GB'") {
		t.Error("quota 5GB absent")
	}

	groupes := ConstruirePlansGroupes([]GroupeBal{{DN: "CN=Eleves,OU=Grp,DC=lycee,DC=local", Nom: "Eleves"}}, configEnLigne())
	if got := ScriptEnLigne(groupes, configEnLigne()); !strings.Contains(got, "Set-DistributionGroup") {
		t.Errorf("groupe absent du script en ligne")
	}
}

func TestScriptPolitiqueAdresses(t *testing.T) {
	script := ScriptPolitiqueAdresses(configSurSite())
	for _, a := range []string{
		"New-EmailAddressPolicy", "Update-EmailAddressPolicy", "-IncludedDomains 'lycee.fr'",
		"SMTP:'%g.%s@lycee.fr'",
	} {
		if !strings.Contains(script, a) {
			t.Errorf("politique sans %q", a)
		}
	}

	surMesure := ScriptPolitiqueAdresses(func() Config { c := configSurSite(); c.AliasPattern = "{nom}.{prenom}"; return c }())
	if !strings.Contains(surMesure, "SMTP:'%s.%g@lycee.fr'") {
		t.Errorf("jeton EAP nom.prenom absent :\n%s", surMesure)
	}

	repli := ScriptPolitiqueAdresses(func() Config { c := configSurSite(); c.AliasPattern = "{identifiant}"; return c }())
	if !strings.Contains(repli, "SMTP:'%g.%s@lycee.fr'") || !strings.Contains(repli, "à adapter manuellement") {
		t.Errorf("repli EAP absent :\n%s", repli)
	}

	enLigne := ScriptPolitiqueAdresses(func() Config { c := configEnLigne(); c.ExchangeServer = "exch01"; return c }())
	if !strings.Contains(enLigne, "Exchange Online ne prend plus en charge les EAP") {
		t.Error("limite Exchange Online non mentionnée")
	}
}

// ─── Sorties ──────────────────────────────────────────────────────────────

func TestSauvegarderScriptBOM(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.ps1")
	if err := SauvegarderScript(dest, "# script\r\nWrite-Host 'ok'"); err != nil {
		t.Fatalf("SauvegarderScript: %v", err)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(donnees) < 3 || donnees[0] != 0xEF || donnees[1] != 0xBB || donnees[2] != 0xBF {
		t.Error("BOM UTF-8 absent")
	}
}

func TestRapportCSV(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "lycee.local")
	dest := filepath.Join(t.TempDir(), "adresses.csv")
	if err := RapportCSV(plans, dest); err != nil {
		t.Fatalf("RapportCSV: %v", err)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	contenu := strings.TrimPrefix(string(donnees), "\xEF\xBB\xBF")
	lignes := strings.Split(strings.TrimRight(contenu, "\n"), "\n")
	if lignes[0] != "Identifiant;Type;SMTP principal;Aliases" {
		t.Errorf("en-tête = %q", lignes[0])
	}
	if len(lignes) != 3 {
		t.Fatalf("lignes = %d", len(lignes))
	}
	if lignes[1] != "jdurand;user;jdurand@lycee.fr;jean.durand@lycee.fr" {
		t.Errorf("ligne 1 = %q", lignes[1])
	}
}

// ─── Application AD (volet LDAP) ──────────────────────────────────────────

func TestAppliquerAdressesSansConnexion(t *testing.T) {
	plans := ConstruirePlansBals(utilisateursExemple(), configSurSite(), "lycee.local")
	resultats := AppliquerAdressesAD(context.Background(), nil, plans)
	if len(resultats) != 2 {
		t.Fatalf("résultats = %d", len(resultats))
	}
	for _, r := range resultats {
		if r.OK() || !strings.Contains(r.Detail, "aucune connexion AD") {
			t.Errorf("résultat sans connexion = %+v", r)
		}
	}
}

func TestAppliquerAdressesSansDN(t *testing.T) {
	pool, err := ad.NewPoolNoValidate(ad.Config{
		Domain:       "lycee.local",
		Controller:   "dc.lycee.local",
		BaseDN:       "DC=lycee,DC=local",
		BindDN:       "CN=admin,DC=lycee,DC=local",
		BindPassword: "secret",
	})
	if err != nil {
		t.Fatalf("NewPoolNoValidate: %v", err)
	}
	plans := []PlanBal{{Kind: "user", SAM: "x", Primaire: "x@lycee.fr"}}
	resultats := AppliquerAdressesAD(context.Background(), pool, plans)
	if len(resultats) != 1 || resultats[0].OK() || !strings.Contains(resultats[0].Detail, "DN inconnu") {
		t.Errorf("DN manquant non signalé : %+v", resultats)
	}
}

func TestResultatAliasOK(t *testing.T) {
	if !(ResultatAlias{Action: "updated"}).OK() {
		t.Error("updated devrait être OK")
	}
	if (ResultatAlias{Action: "error"}).OK() {
		t.Error("error ne devrait pas être OK")
	}
}

// contientContient indique si une chaîne attendue apparaît dans un slice.
func contientContient(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if strings.Contains(v, cible) {
			return true
		}
	}
	return false
}
