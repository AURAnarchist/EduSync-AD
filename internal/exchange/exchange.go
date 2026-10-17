// Package exchange produit les plans de boîtes aux lettres et les scripts
// PowerShell du module Microsoft Exchange (M20).
//
// Deux modes, cohérents avec Exchange :
//
//   - « onprem » — Remote PowerShell vers le serveur Exchange
//     (Enable-Mailbox / Set-Mailbox / Set-DistributionGroup) ;
//   - « online » — module ExchangeOnlineManagement (Connect-ExchangeOnline en
//     app-only : TenantId + ClientId + empreinte de certificat).
//
// Le paquet produit des plans (SMTP principal + alias + quota + archive +
// rétention) et les scripts correspondants. Les adresses (mail /
// proxyAddresses) peuvent aussi être écrites directement dans l'AD par LDAP :
// c'est le volet exécutable depuis n'importe quelle plateforme.
//
// La logique et le format du fichier exchange.json reprennent
// src/edusync_ad/core/exchange.py pour rester compatibles avec Python.
package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/config"
)

// FichierConfig est le nom du fichier de configuration Exchange.
const FichierConfig = "exchange.json"

// Modes est le libellé affichable de chaque mode Exchange.
var Modes = map[string]string{
	"onprem": "Exchange sur site (Remote PowerShell)",
	"online": "Exchange Online (EXO)",
}

// MotifsAlias décrit les motifs de construction d'alias.
var MotifsAlias = map[string]string{
	"{identifiant}":  "identifiant seul (jdurand)",
	"{prenom}.{nom}": "prenom.nom (recommandé)",
	"{nom}.{prenom}": "nom.prenom",
	"{prenom}{nom}":  "prenomnom",
	"{prenom}_{nom}": "prenom_nom",
}

// jetonsEAP associe un motif d'alias au gabarit de politique d'adresses
// Exchange (%g = prénom, %s = nom).
var jetonsEAP = map[string]string{
	"{prenom}.{nom}": "%g.%s",
	"{nom}.{prenom}": "%s.%g",
	"{prenom}{nom}":  "%g%s",
	"{prenom}_{nom}": "%g_%s",
	"{prenom}-{nom}": "%g-%s",
}

// Config décrit les paramètres du module Exchange.
type Config struct {
	Mode                  string `json:"mode"`
	SMTPDomain            string `json:"smtp_domain"`
	AliasPattern          string `json:"alias_pattern"`
	MailboxQuota          string `json:"mailbox_quota"`
	ArchiveEnabled        bool   `json:"archive_enabled"`
	RetentionPolicy       string `json:"retention_policy"`
	ExchangeServer        string `json:"exchange_server"`
	Database              string `json:"database"`
	TenantID              string `json:"tenant_id"`
	ClientID              string `json:"client_id"`
	CertificateThumbprint string `json:"certificate_thumbprint"`
	Organization          string `json:"organization"`
}

// ConfigParDefaut retourne une configuration Exchange avec les valeurs par défaut.
func ConfigParDefaut() Config {
	return Config{
		Mode:         "onprem",
		AliasPattern: "{prenom}.{nom}",
		MailboxQuota: "10GB",
	}
}

var quotaRe = regexp.MustCompile(`^(\d+)\s?(B|KB|MB|GB|TB)$`)

// Validate vérifie la cohérence de la configuration.
func (c Config) Validate() []string {
	var erreurs []string
	if _, ok := Modes[c.Mode]; !ok {
		erreurs = append(erreurs, "Mode Exchange inconnu (onprem / online).")
	}
	if strings.TrimSpace(c.SMTPDomain) == "" {
		erreurs = append(erreurs, "Le domaine SMTP est vide.")
	} else if strings.Contains(c.SMTPDomain, " ") || strings.Contains(c.SMTPDomain, "@") {
		erreurs = append(erreurs, "Le domaine SMTP doit être un simple domaine (ex : lycee.fr).")
	}
	if _, ok := MotifsAlias[c.AliasPattern]; !ok {
		erreurs = append(erreurs, "Motif d'alias inconnu.")
	}
	if !quotaRe.MatchString(strings.TrimSpace(c.MailboxQuota)) {
		erreurs = append(erreurs, "Quota invalide (ex : 10GB, 500MB).")
	}
	if c.Mode == "onprem" {
		if strings.TrimSpace(c.ExchangeServer) == "" {
			erreurs = append(erreurs, "Le serveur Exchange est vide (mode sur site).")
		}
	} else {
		for _, champ := range []struct{ label, valeur string }{
			{"tenant ID", c.TenantID},
			{"client ID", c.ClientID},
			{"empreinte de certificat", c.CertificateThumbprint},
			{"organisation", c.Organization},
		} {
			if strings.TrimSpace(champ.valeur) == "" {
				erreurs = append(erreurs, champ.label+" vide (mode Exchange Online).")
			}
		}
	}
	return erreurs
}

// DossierConfig retourne le dossier de configuration partagé avec Python.
func DossierConfig() (string, error) {
	return config.ConfigDir()
}

// ChargerConfig lit la configuration Exchange (défaut si absente ou illisible).
func ChargerConfig(chemin string) Config {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return ConfigParDefaut()
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	cfg := ConfigParDefaut()
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return cfg
	}
	// Décodage tolérant : les clés absentes gardent la valeur par défaut.
	if err := json.Unmarshal(donnees, &cfg); err != nil {
		return ConfigParDefaut()
	}
	return cfg
}

// SauvegarderConfig écrit la configuration Exchange (JSON indenté UTF-8).
func SauvegarderConfig(chemin string, cfg Config) error {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return err
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return fmt.Errorf("exchange: création du dossier de configuration: %w", err)
	}
	return ecrireJSON(chemin, cfg)
}

// ecrireJSON sérialise en JSON indenté (2 espaces, sans échappement HTML).
func ecrireJSON(chemin string, valeur any) error {
	f, err := os.Create(chemin)
	if err != nil {
		return fmt.Errorf("exchange: écriture %s: %w", chemin, err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(valeur); err != nil {
		return fmt.Errorf("exchange: encodage JSON: %w", err)
	}
	return nil
}

// ─── Adresses ─────────────────────────────────────────────────────────────

var (
	localInvalideRe = regexp.MustCompile(`[^a-z0-9._-]`)
	pointsMultiRe   = regexp.MustCompile(`\.{2,}`)
)

// NormaliserPartieLocale construit une partie locale : accents retirés,
// minuscules, espaces → points, seuls [a-z0-9._-] conservés.
func NormaliserPartieLocale(texte string) string {
	out := strings.ReplaceAll(strings.TrimSpace(retirerAccents(strings.ToLower(texte))), " ", ".")
	out = localInvalideRe.ReplaceAllString(out, "")
	out = pointsMultiRe.ReplaceAllString(out, ".")
	out = strings.Trim(out, ".")
	if out == "" {
		return "adresse"
	}
	return out
}

// retirerAccents supprime les accents et les caractères non ASCII (comme le
// normalize NFKD + encode ascii/ignore de Python).
func retirerAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		switch r {
		case 'à', 'â', 'ä', 'á', 'ã', 'å':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'î', 'ï', 'í':
			b.WriteRune('i')
		case 'ô', 'ö', 'ó', 'õ':
			b.WriteRune('o')
		case 'û', 'ü', 'ú', 'ù':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		case 'ñ':
			b.WriteRune('n')
		default:
			if r > unicode.MaxASCII {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DecouperNomAffichage découpe un « cn » « Prénom Nom » (suffixe de doublon
// « (sam) » retiré).
func DecouperNomAffichage(cn, sam string) (string, string) {
	texte := strings.TrimSpace(cn)
	if sam != "" && strings.HasSuffix(texte, "("+sam+")") {
		texte = strings.TrimSpace(texte[:len(texte)-len(sam)-2])
	}
	if texte == "" {
		return "", ""
	}
	idx := -1
	for i, r := range texte {
		if unicode.IsSpace(r) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return texte, ""
	}
	return texte[:idx], strings.TrimSpace(texte[idx:])
}

// adresseMotif applique un motif d'alias puis normalise la partie locale.
func adresseMotif(motif, sam, prenom, nom string) string {
	s := motif
	s = strings.ReplaceAll(s, "{identifiant}", sam)
	s = strings.ReplaceAll(s, "{prenom}", prenom)
	s = strings.ReplaceAll(s, "{nom}", nom)
	return NormaliserPartieLocale(s)
}

// ─── Plans ────────────────────────────────────────────────────────────────

// PlanBal décrit une boîte aux lettres (ou un groupe de messagerie) à traiter.
type PlanBal struct {
	Kind     string   `json:"kind"`
	DN       string   `json:"dn"`
	SAM      string   `json:"sam"`
	UPN      string   `json:"upn"`
	CN       string   `json:"cn"`
	Prenom   string   `json:"prenom"`
	Nom      string   `json:"nom"`
	Primaire string   `json:"primary"`
	Alias    []string `json:"aliases"`
}

// AdressesSMTP retourne la liste préfixée SMTP:/smtp: attendue par Exchange.
func (p PlanBal) AdressesSMTP() []string {
	adresses := []string{"SMTP:" + p.Primaire}
	for _, a := range p.Alias {
		adresses = append(adresses, "smtp:"+a)
	}
	return adresses
}

// Label retourne un libellé lisible du plan.
func (p PlanBal) Label() string {
	qui := p.SAM
	if qui == "" {
		qui = p.CN
	}
	extra := ""
	if len(p.Alias) > 0 {
		extra = fmt.Sprintf(" +%d alias(es)", len(p.Alias))
	}
	return qui + " → " + p.Primaire + extra
}

// UtilisateurBal décrit un compte AD minimal pour construire un plan.
type UtilisateurBal struct {
	SAM string `json:"sam"`
	CN  string `json:"cn"`
	DN  string `json:"dn"`
}

// GroupeBal décrit un groupe AD minimal pour construire un plan.
type GroupeBal struct {
	DN  string `json:"dn"`
	Nom string `json:"nom"`
}

// ConstruirePlansBals construit les plans de boîtes depuis des comptes AD.
// « domain » sert à construire l'identité Exchange (UPN).
func ConstruirePlansBals(utilisateurs []UtilisateurBal, cfg Config, domain string) []PlanBal {
	plans := []PlanBal{}
	for _, u := range utilisateurs {
		sam := strings.TrimSpace(u.SAM)
		if sam == "" {
			continue
		}
		cn := u.CN
		if cn == "" {
			cn = sam
		}
		prenom, nom := DecouperNomAffichage(cn, sam)
		primaire := NormaliserPartieLocale(sam) + "@" + cfg.SMTPDomain
		alias := []string{}
		if cfg.AliasPattern != "{identifiant}" {
			candidat := adresseMotif(cfg.AliasPattern, sam, prenom, nom) + "@" + cfg.SMTPDomain
			if !strings.EqualFold(candidat, primaire) {
				alias = append(alias, candidat)
			}
		}
		upn := ""
		if domain != "" {
			upn = sam + "@" + domain
		}
		plans = append(plans, PlanBal{
			Kind:     "user",
			DN:       u.DN,
			SAM:      sam,
			UPN:      upn,
			CN:       cn,
			Prenom:   prenom,
			Nom:      nom,
			Primaire: primaire,
			Alias:    alias,
		})
	}
	return plans
}

// ConstruirePlansGroupes construit les plans de groupes de messagerie.
func ConstruirePlansGroupes(groupes []GroupeBal, cfg Config) []PlanBal {
	plans := []PlanBal{}
	for _, g := range groupes {
		local := NormaliserPartieLocale(strings.ReplaceAll(g.Nom, " ", ""))
		primaire := local + "@" + cfg.SMTPDomain
		dotted := NormaliserPartieLocale(g.Nom)
		alias := []string{}
		if dotted != "" && !strings.EqualFold(dotted+"@"+cfg.SMTPDomain, primaire) {
			alias = append(alias, dotted+"@"+cfg.SMTPDomain)
		}
		plans = append(plans, PlanBal{
			Kind: "group", DN: g.DN, SAM: g.Nom, CN: g.Nom, Primaire: primaire, Alias: alias,
		})
	}
	return plans
}

// QuotaAlerte retourne le quota d'alerte = 90 % du quota de prohibition
// (inchangé si non calculable).
func QuotaAlerte(quota string) string {
	m := quotaRe.FindStringSubmatch(strings.TrimSpace(quota))
	if m == nil {
		return quota
	}
	var valeur int
	if _, err := fmt.Sscanf(m[1], "%d", &valeur); err != nil {
		return quota
	}
	unite := strings.ToUpper(m[2])
	alerte := int(math.Max(1, math.RoundToEven(float64(valeur)*0.9)))
	return fmt.Sprintf("%d%s", alerte, unite)
}

// ─── Résultats ────────────────────────────────────────────────────────────

// ResultatAlias décrit l'issue d'une écriture d'adresses dans l'AD.
type ResultatAlias struct {
	Key    string `json:"key"`
	Action string `json:"action"` // "updated" | "error"
	Detail string `json:"detail"`
}

// OK indique si l'écriture a réussi.
func (r ResultatAlias) OK() bool { return r.Action != "error" }

// ─── Application LDAP ─────────────────────────────────────────────────────

// AppliquerAdressesAD écrit « mail » + « proxyAddresses » (SMTP:/smtp:) sur
// chaque objet via LDAP. Exchange lit ces attributs pour le routage (scénario
// hybride / mise en place des adresses avant les boîtes).
func AppliquerAdressesAD(ctx context.Context, pool *ad.Pool, plans []PlanBal) []ResultatAlias {
	resultats := []ResultatAlias{}
	for _, plan := range plans {
		key := plan.SAM
		if key == "" {
			key = plan.CN
		}
		if key == "" {
			key = plan.DN
		}

		if pool == nil {
			resultats = append(resultats, ResultatAlias{key, "error", "aucune connexion AD"})
			continue
		}
		if plan.DN == "" {
			resultats = append(resultats, ResultatAlias{key, "error", "DN inconnu"})
			continue
		}
		majs := []ad.AttributeUpdate{
			{Nom: "mail", Valeurs: []string{plan.Primaire}},
			{Nom: "proxyAddresses", Valeurs: plan.AdressesSMTP()},
		}
		if err := pool.UserSetAttributes(ctx, plan.DN, majs); err != nil {
			resultats = append(resultats, ResultatAlias{key, "error", err.Error()})
			continue
		}
		resultats = append(resultats, ResultatAlias{
			key, "updated",
			fmt.Sprintf("mail=%s (+%d alias(es))", plan.Primaire, len(plan.Alias)),
		})
	}
	return resultats
}

// ─── Scripts PowerShell ───────────────────────────────────────────────────

// psQuote échappe une valeur pour une chaîne PowerShell entre apostrophes.
func psQuote(valeur string) string {
	return strings.ReplaceAll(valeur, "'", "''")
}

// addressesLitteral construit la liste PowerShell des adresses SMTP.
func addressesLitteral(plan PlanBal) string {
	items := make([]string, 0)
	for _, a := range plan.AdressesSMTP() {
		items = append(items, "'"+psQuote(a)+"'")
	}
	return strings.Join(items, ", ")
}

// lignesCommunesBAL construit les lignes quotas + archive + rétention.
func lignesCommunesBAL(cfg Config, identiteVar string) []string {
	quota := cfg.MailboxQuota
	lignes := []string{
		fmt.Sprintf("Set-Mailbox -Identity $%s -ProhibitSendReceiveQuota '%s' -ProhibitSendQuota '%s' -IssueWarningQuota '%s'",
			identiteVar, quota, quota, QuotaAlerte(quota)),
	}
	if cfg.ArchiveEnabled {
		lignes = append(lignes, fmt.Sprintf(
			"if (-not (Get-Mailbox -Identity $%s -Archive -ErrorAction SilentlyContinue)) { Enable-Mailbox -Identity $%s -Archive }",
			identiteVar, identiteVar))
	}
	if strings.TrimSpace(cfg.RetentionPolicy) != "" {
		lignes = append(lignes, fmt.Sprintf("Set-Mailbox -Identity $%s -RetentionPolicy '%s'",
			identiteVar, psQuote(cfg.RetentionPolicy)))
	}
	return lignes
}

// ScriptEnLigne génère le script Exchange Online (module ExchangeOnlineManagement).
func ScriptEnLigne(plans []PlanBal, cfg Config) string {
	var utilisateurs, groupes []PlanBal
	for _, p := range plans {
		if p.Kind == "user" {
			utilisateurs = append(utilisateurs, p)
		} else {
			groupes = append(groupes, p)
		}
	}

	lignes := []string{
		"#Requires -Modules ExchangeOnlineManagement",
		"<#",
		" Script généré par EduSync-AD — Exchange Online (M20)",
		fmt.Sprintf(" Boîtes : %d — groupes de messagerie : %d", len(utilisateurs), len(groupes)),
		" Quota : " + cfg.MailboxQuota + " — archive : " + ouiNon(cfg.ArchiveEnabled) + " — rétention : " + valeurOu(cfg.RetentionPolicy, "aucune"),
		" Prérequis : module ExchangeOnlineManagement, certificat d'application",
		" enregistré côté Entra ID avec le rôle « Exchange Administrator ».",
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"",
		"# 1. Connexion app-only",
		fmt.Sprintf("Connect-ExchangeOnline -Organization '%s' -TenantId '%s' -ClientId '%s' -CertificateThumbprint '%s' -ShowBanner:$false",
			psQuote(cfg.Organization), psQuote(cfg.TenantID), psQuote(cfg.ClientID), psQuote(cfg.CertificateThumbprint)),
		"",
	}

	for _, plan := range utilisateurs {
		identite := plan.UPN
		if identite == "" {
			identite = plan.Primaire
		}
		nom := plan.SAM
		if nom == "" {
			nom = plan.CN
		}
		lignes = append(lignes,
			"# --- "+nom+" ---",
			"$identity = '"+psQuote(identite)+"'",
			"if (-not (Get-Mailbox -Identity $identity -ErrorAction SilentlyContinue)) {",
			"    Write-Warning \"Boîte absente (compte ou licence manquante) : $identity\"",
			"} else {",
			"    $addresses = @("+addressesLitteral(plan)+")",
			"    Set-Mailbox -Identity $identity -EmailAddresses @{Replace = $addresses}",
		)
		for _, extra := range lignesCommunesBAL(cfg, "identity") {
			lignes = append(lignes, "    "+extra)
		}
		lignes = append(lignes, "}", "")
	}

	for _, plan := range groupes {
		identite := plan.DN
		if identite == "" {
			identite = plan.Primaire
		}
		lignes = append(lignes,
			"# --- groupe : "+plan.CN+" ---",
			"$group = '"+psQuote(identite)+"'",
			"if (-not (Get-DistributionGroup -Identity $group -ErrorAction SilentlyContinue)) {",
			"    Write-Warning \"Groupe de messagerie absent : $group\"",
			"} else {",
			"    $addresses = @("+addressesLitteral(plan)+")",
			"    Set-DistributionGroup -Identity $group -EmailAddresses @{Replace = $addresses}",
			"}",
			"",
		)
	}

	lignes = append(lignes,
		"Disconnect-ExchangeOnline -Confirm:$false",
		"Write-Host 'Adresses Exchange Online appliquées.'")
	return strings.Join(lignes, "\n") + "\n"
}

// ScriptSurSite génère le script Exchange sur site (Remote PowerShell + création
// si absence).
func ScriptSurSite(plans []PlanBal, cfg Config) string {
	var utilisateurs, groupes []PlanBal
	for _, p := range plans {
		if p.Kind == "user" {
			utilisateurs = append(utilisateurs, p)
		} else {
			groupes = append(groupes, p)
		}
	}

	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Exchange sur site (M20)",
		fmt.Sprintf(" Boîtes : %d — groupes de messagerie : %d", len(utilisateurs), len(groupes)),
		fmt.Sprintf(" Serveur : %s — base : %s", cfg.ExchangeServer, valeurOu(cfg.Database, "(défaut)")),
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"",
		"# 1. Session Exchange",
		"$session = New-PSSession -ConfigurationName Microsoft.Exchange -ComputerName '" + psQuote(cfg.ExchangeServer) + "'",
		"Import-PSSession $session -DisableNameChecking",
		"",
	}

	for _, plan := range utilisateurs {
		identite := plan.UPN
		if identite == "" {
			identite = plan.Primaire
		}
		nom := plan.SAM
		if nom == "" {
			nom = plan.CN
		}
		alias := plan.SAM
		if alias == "" {
			alias = NormaliserPartieLocale(plan.CN)
		}
		bdd := ""
		if cfg.Database != "" {
			bdd = " -Database '" + psQuote(cfg.Database) + "'"
		}
		lignes = append(lignes,
			"# --- "+nom+" ---",
			"$identity = '"+psQuote(identite)+"'",
			"if (-not (Get-Mailbox -Identity $identity -ErrorAction SilentlyContinue)) {",
			"    Enable-Mailbox -Identity $identity -Alias '"+psQuote(alias)+"'"+bdd,
			"}",
			"$addresses = @("+addressesLitteral(plan)+")",
			"Set-Mailbox -Identity $identity -EmailAddresses @{Replace = $addresses}",
		)
		for _, extra := range lignesCommunesBAL(cfg, "identity") {
			lignes = append(lignes, extra)
		}
		lignes = append(lignes, "")
	}

	for _, plan := range groupes {
		identite := plan.DN
		if identite == "" {
			identite = plan.Primaire
		}
		aliasGroupe := ""
		if plan.SAM != "" {
			aliasGroupe = " -Alias '" + psQuote(NormaliserPartieLocale(plan.CN)) + "'"
		}
		lignes = append(lignes,
			"# --- groupe : "+plan.CN+" ---",
			"$group = '"+psQuote(identite)+"'",
			"if (-not (Get-DistributionGroup -Identity $group -ErrorAction SilentlyContinue)) {",
			"    Enable-DistributionGroup -Identity $group"+aliasGroupe,
			"}",
			"$addresses = @("+addressesLitteral(plan)+")",
			"Set-DistributionGroup -Identity $group -EmailAddresses @{Replace = $addresses}",
			"",
		)
	}

	lignes = append(lignes,
		"Remove-PSSession $session",
		"Write-Host 'Adresses Exchange appliquées.'")
	return strings.Join(lignes, "\n") + "\n"
}

// ScriptPolitiqueAdresses génère la politique d'adresses (Exchange on-prem).
func ScriptPolitiqueAdresses(cfg Config) string {
	motif, ok := jetonsEAP[cfg.AliasPattern]
	if !ok {
		motif = "%g.%s"
	}
	note := ""
	if !ok {
		note = fmt.Sprintf("# Motif %s non exprimable en jeton EAP : gabarit proposé %%g.%%s (prénom.nom) à adapter manuellement.", cfg.AliasPattern)
	}
	lignes := []string{
		"#Requires -RunAsAdministrator",
		"<#",
		" Script généré par EduSync-AD — Politique d'adresses (M20)",
		" Domaine SMTP : " + cfg.SMTPDomain,
		" Exchange Online ne prend plus en charge les EAP : dans ce mode,",
		" les adresses sont posées par les scripts de boîtes ci-dessus.",
		"#>",
		"$ErrorActionPreference = 'Stop'",
		"$session = New-PSSession -ConfigurationName Microsoft.Exchange -ComputerName '" + psQuote(cfg.ExchangeServer) + "'",
		"Import-PSSession $session -DisableNameChecking",
		"",
		"$policyName = 'EduSync-AD — politique principale'",
		"if (-not (Get-EmailAddressPolicy -Identity $policyName -ErrorAction SilentlyContinue)) {",
		"    New-EmailAddressPolicy -Name $policyName `",
		"        -IncludedDomains '" + psQuote(cfg.SMTPDomain) + "' `",
		"        -Priority 1 `",
		"        -LdapRecipientFilter \"(&(objectCategory=person)(objectClass=user)(mailNickname=*))\" `",
		"        -EnabledEmailAddressTemplates @{ SMTP:'" + motif + "@" + psQuote(cfg.SMTPDomain) + "' }",
		"}",
		"Update-EmailAddressPolicy -Identity $policyName",
		"",
		"Remove-PSSession $session",
		"Write-Host \"Politique d'adresses appliquée : $policyName\"",
	}
	if note != "" {
		// Inséré après « Domaine SMTP » (index 4), comme en Python.
		lignes = append(lignes[:4], append([]string{note}, lignes[4:]...)...)
	}
	return strings.Join(lignes, "\n") + "\n"
}

// ouiNon retourne « oui » ou « non ».
func ouiNon(valeur bool) string {
	if valeur {
		return "oui"
	}
	return "non"
}

// valeurOu retourne la valeur, ou défaut si vide.
func valeurOu(valeur, defaut string) string {
	if valeur == "" {
		return defaut
	}
	return valeur
}

// SauvegarderScript écrit un script PowerShell (UTF-8 avec BOM — requis par
// Windows PowerShell 5.1).
func SauvegarderScript(dest, contenu string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("exchange: création du dossier du script: %w", err)
	}
	donnees := append([]byte{0xEF, 0xBB, 0xBF}, []byte(contenu)...)
	if err := os.WriteFile(dest, donnees, 0o644); err != nil {
		return fmt.Errorf("exchange: écriture du script %s: %w", dest, err)
	}
	return nil
}

// RapportCSV écrit le rapport des adresses (CSV ';' UTF-8 BOM).
func RapportCSV(plans []PlanBal, dest string) error {
	lignes := []string{"Identifiant;Type;SMTP principal;Aliases"}
	for _, plan := range plans {
		identifiant := plan.SAM
		if identifiant == "" {
			identifiant = plan.CN
		}
		lignes = append(lignes, fmt.Sprintf("%s;%s;%s;%s",
			identifiant, plan.Kind, plan.Primaire, strings.Join(plan.Alias, " | ")))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("exchange: création du dossier du rapport: %w", err)
	}
	contenu := "\xEF\xBB\xBF" + strings.Join(lignes, "\n") + "\n"
	if err := os.WriteFile(dest, []byte(contenu), 0o644); err != nil {
		return fmt.Errorf("exchange: écriture du rapport %s: %w", dest, err)
	}
	return nil
}
