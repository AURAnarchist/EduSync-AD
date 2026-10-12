// Package ad fournit la gestion des mots de passe Active Directory.
//
// Politiques distinctes élèves / personnels, génération ANSSI-compatible,
// historique, réinitialisation en masse par OU/groupe/CSV.
package ad

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// PasswordPolicy décrit une politique de mot de passe (reflet internal/models).
type PasswordPolicy struct {
	Length            int
	RequireUppercase  bool
	RequireDigits     bool
	RequireSpecial    bool
	ForbidUserInfo    bool // interdit prénom/nom/identifiant dans le MDP
	MaxAgeDays        int  // 0 = pas d'expiration
	HistorySize       int  // nb de MDP précédents interdits
	MinAgeDays        int  // délai minimum avant changement
	ComplexityRegex   string
}

// DefaultStudentPolicy politique par défaut élèves (8 chars, pas de majuscule obligatoire).
func DefaultStudentPolicy() PasswordPolicy {
	return PasswordPolicy{
		Length:         8,
		RequireUppercase: false,
		RequireDigits:  true,
		RequireSpecial: false,
		ForbidUserInfo: true,
		MaxAgeDays:     0,
		HistorySize:    5,
		MinAgeDays:     1,
	}
}

// DefaultStaffPolicy politique par défaut personnels (12 chars, complexité complète).
func DefaultStaffPolicy() PasswordPolicy {
	return PasswordPolicy{
		Length:           12,
		RequireUppercase: true,
		RequireDigits:    true,
		RequireSpecial:   true,
		ForbidUserInfo:   true,
		MaxAgeDays:       90,
		HistorySize:      10,
		MinAgeDays:       1,
	}
}

// Validate vérifie qu'un mot de passe respecte la politique.
func (p *PasswordPolicy) Validate(password, samAccountName, givenName, sn string) error {
	if len(password) < p.Length {
		return fmt.Errorf("mot de passe trop court (minimum %d caractères)", p.Length)
	}
	if p.RequireUppercase && !regexp.MustCompile(`[A-Z]`).MatchString(password) {
		return errors.New("mot de passe doit contenir au moins une majuscule")
	}
	if p.RequireDigits && !regexp.MustCompile(`\d`).MatchString(password) {
		return errors.New("mot de passe doit contenir au moins un chiffre")
	}
	if p.RequireSpecial && !regexp.MustCompile(`[^A-Za-z0-9]`).MatchString(password) {
		return errors.New("mot de passe doit contenir au moins un caractère spécial")
	}
	if p.ForbidUserInfo {
		lower := strings.ToLower(password)
		if samAccountName != "" && strings.Contains(lower, strings.ToLower(samAccountName)) {
			return errors.New("mot de passe ne doit pas contenir l'identifiant")
		}
		if givenName != "" && strings.Contains(lower, strings.ToLower(givenName)) {
			return errors.New("mot de passe ne doit pas contenir le prénom")
		}
		if sn != "" && strings.Contains(lower, strings.ToLower(sn)) {
			return errors.New("mot de passe ne doit pas contenir le nom")
		}
	}
	if p.ComplexityRegex != "" {
		if matched, _ := regexp.MatchString(p.ComplexityRegex, password); !matched {
			return errors.New("mot de passe ne respecte pas le pattern de complexité")
		}
	}
	return nil
}

// Generate génère un mot de passe conforme à la politique.
func (p *PasswordPolicy) Generate() (string, error) {
	const (
		lower   = "abcdefghijkmnopqrstuvwxyz" // sans l (confusion 1)
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"  // sans I, O
		digits  = "23456789"                  // sans 0, 1
		special = "!@#$%^&*_-+=?"
	)

	var charset string
	var required []string

	charset += lower
	if p.RequireUppercase {
		charset += upper
		required = append(required, upper)
	}
	if p.RequireDigits {
		charset += digits
		required = append(required, digits)
	}
	if p.RequireSpecial {
		charset += special
		required = append(required, special)
	}

	if len(charset) == 0 {
		charset = lower
	}

	// Assurer au moins un caractère de chaque classe requise
	pwd := make([]byte, p.Length)
	for i, r := range required {
		if i >= p.Length {
			break
		}
		idx, err := randInt(len(r))
		if err != nil {
			return "", err
		}
		pwd[i] = r[idx]
	}

	// Remplir le reste
	for i := len(required); i < p.Length; i++ {
		idx, err := randInt(len(charset))
		if err != nil {
			return "", err
		}
		pwd[i] = charset[idx]
	}

	// Mélanger (Fisher-Yates)
	for i := p.Length - 1; i > 0; i-- {
		j, err := randInt(i + 1)
		if err != nil {
			return "", err
		}
		pwd[i], pwd[j] = pwd[j], pwd[i]
	}

	candidate := string(pwd)
	if err := p.Validate(candidate, "", "", ""); err != nil {
		// Retry rare si la validation échoue (ex: ForbidUserInfo pas testé ici)
		return p.Generate()
	}
	return candidate, nil
}

func randInt(n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("borne invalide")
	}
	val, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(val.Int64()), nil
}

// PasswordResetParams décrit une opération de réinitialisation.
type PasswordResetParams struct {
	TargetDN       string        // DN de l'utilisateur (ou vide si par OU/groupe/CSV)
	OU             string        // OU pour réinitialisation en masse
	GroupDN        string        // Groupe pour réinitialisation en masse
	NewPassword    string        // mot de passe spécifique (vide = généré)
	Policy         PasswordPolicy
	ForceChange    bool          // userMustChangePassword au prochain logon
	NotifyUser     bool          // envoyer par mail (à brancher plus tard)
	DryRun         bool          // simulation seulement
}

// PasswordResetResult décrit le résultat d'une réinitialisation.
type PasswordResetResult struct {
	UserDN        string
	SAMAccountName string
	DisplayName   string
	NewPassword   string
	Generated     bool
	Error         string
}

// PasswordResetBulk réinitialise les MDP pour tous les utilisateurs d'une OU
// ou membres d'un groupe. Si TargetDN est fourni, ne traite que cet utilisateur.
func (p *Pool) PasswordResetBulk(ctx context.Context, params PasswordResetParams) ([]PasswordResetResult, error) {
	var results []PasswordResetResult

	users, err := p.resolveResetTargets(ctx, params)
	if err != nil {
		return nil, err
	}

	for _, u := range users {
		pwd := params.NewPassword
		generated := false
		if pwd == "" {
			pwd, err = params.Policy.Generate()
			if err != nil {
				results = append(results, PasswordResetResult{
					UserDN: u.DN, SAMAccountName: u.SAMAccountName,
					DisplayName: u.DisplayName, Error: fmt.Sprintf("génération: %v", err),
				})
				continue
			}
			generated = true
		}

		if params.DryRun {
			results = append(results, PasswordResetResult{
				UserDN: u.DN, SAMAccountName: u.SAMAccountName,
				DisplayName: u.DisplayName, NewPassword: pwd, Generated: generated,
			})
			continue
		}

		// Valider le mot de passe contre la politique
		if err := params.Policy.Validate(pwd, u.SAMAccountName, u.GivenName, u.SN); err != nil {
			results = append(results, PasswordResetResult{
				UserDN: u.DN, SAMAccountName: u.SAMAccountName,
				DisplayName: u.DisplayName, Error: fmt.Sprintf("politique: %v", err),
			})
			continue
		}

		// Réinitialiser
		if err := p.UserResetPassword(ctx, u.DN, pwd); err != nil {
			results = append(results, PasswordResetResult{
				UserDN: u.DN, SAMAccountName: u.SAMAccountName,
				DisplayName: u.DisplayName, Error: fmt.Sprintf("reset: %v", err),
			})
			continue
		}

		// Forcer changement au prochain logon
		if params.ForceChange {
			if err := p.setMustChangePassword(ctx, u.DN, true); err != nil {
				// Non bloquant
				fmt.Printf("ad: avertissement: force change %s: %v\n", u.DN, err)
			}
		}

		// Historique (pwdLastSet + nTFRSPrevPasswords si dispo) - pour l'instant log
		fmt.Printf("ad: MDP réinitialisé pour %s (%s) [généré=%v]\n", u.SAMAccountName, u.DN, generated)

		results = append(results, PasswordResetResult{
			UserDN: u.DN, SAMAccountName: u.SAMAccountName,
			DisplayName: u.DisplayName, NewPassword: pwd, Generated: generated,
		})
	}

	return results, nil
}

// resolveResetTargets retourne la liste des utilisateurs à traiter.
func (p *Pool) resolveResetTargets(ctx context.Context, params PasswordResetParams) ([]*User, error) {
	if params.TargetDN != "" {
		u, err := p.UserGet(ctx, params.TargetDN)
		if err != nil {
			return nil, err
		}
		return []*User{u}, nil
	}

	var filter string
	var baseDN string

	if params.GroupDN != "" {
		// Membres du groupe (récursif si possible)
		group, err := p.GroupGet(ctx, params.GroupDN)
		if err != nil {
			return nil, err
		}
		var users []*User
		for _, memberDN := range group.Members {
			u, err := p.UserGet(ctx, memberDN)
			if err == nil {
				users = append(users, u)
			}
		}
		return users, nil
	}

	if params.OU != "" {
		baseDN = params.OU
		filter = "(&(objectClass=user)(objectCategory=person))"
	} else {
		baseDN = p.cfg.BaseDN
		filter = "(&(objectClass=user)(objectCategory=person))"
	}

	return p.UserSearch(ctx, baseDN, filter, []string{"dn", "sAMAccountName", "displayName", "givenName", "sn"}, 0)
}

// setMustChangePassword positionne userMustChangePassword (pwdLastSet = 0).
func (p *Pool) setMustChangePassword(ctx context.Context, dn string, mustChange bool) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	var val string
	if mustChange {
		val = "0"
	} else {
		val = "-1" // jamais expirer / pas de forçage
	}
	modReq := ldap.NewModifyRequest(dn, nil)
	modReq.Replace("pwdLastSet", []string{val})
	return conn.Modify(modReq)
}

// PasswordExpire expire immédiatement le mot de passe (force changement).
func (p *Pool) PasswordExpire(ctx context.Context, dn string) error {
	return p.setMustChangePassword(ctx, dn, true)
}

// PasswordNeverExpire configure le mot de passe pour ne jamais expirer.
func (p *Pool) PasswordNeverExpire(ctx context.Context, dn string) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	// userAccountControl: bit 65536 = DONT_EXPIRE_PASSWORD
	uacVals := []string{"66048", "66080"} // NORMAL + DONT_EXPIRE_PASSWORD (enabled/disabled)
	modReq := ldap.NewModifyRequest(dn, nil)
	modReq.Replace("userAccountControl", uacVals)
	return conn.Modify(modReq)
}

// PasswordGetInfo récupère les infos d'expiration/historique d'un utilisateur.
func (p *Pool) PasswordGetInfo(ctx context.Context, dn string) (*PasswordInfo, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	req := ldap.NewSearchRequest(dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=user)",
		[]string{"pwdLastSet", "userAccountControl", "maxPwdAge", "minPwdAge", "pwdHistoryLength"},
		nil)
	res, err := conn.Search(req)
	if err != nil {
		return nil, err
	}
	if len(res.Entries) == 0 {
		return nil, errors.New("utilisateur introuvable")
	}
	e := res.Entries[0]

	pwdLastSet := int64(0)
	if v := e.GetAttributeValue("pwdLastSet"); v != "" {
		fmt.Sscanf(v, "%d", &pwdLastSet)
	}
	uac := 512
	if v := e.GetAttributeValue("userAccountControl"); v != "" {
		fmt.Sscanf(v, "%d", &uac)
	}

	return &PasswordInfo{
		DN:              dn,
		PwdLastSet:      time.Unix((pwdLastSet-116444736000000000)/10000000, 0), // FILETIME → time.Time
		NeverExpires:    (uac & 65536) != 0,
		UserMustChange:  pwdLastSet == 0,
		MaxPwdAgeDays:   0, // lire depuis domain policy si nécessaire
		MinPwdAgeDays:   0,
		HistoryLength:   0,
	}, nil
}

// PasswordInfo informations sur l'état du mot de passe.
type PasswordInfo struct {
	DN              string
	PwdLastSet      time.Time
	NeverExpires    bool
	UserMustChange  bool
	MaxPwdAgeDays   int
	MinPwdAgeDays   int
	HistoryLength   int
}