// Package ad fournit la gestion des heures de connexion (logonHours) selon
// M16 de la roadmap.
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
//
// L'attribut LDAP logonHours est un champ de 21 octets (168 bits) : un bit
// par heure de la semaine (7 jours × 24 heures). Bit 1 = accès autorisé,
// bit 0 = accès interdit. L'index du bit = jour*24 + heure avec dimanche=0.
package ad

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

// LogonHours représente une grille d'autorisation 7 jours × 24 heures.
// Jours[0] = dimanche, Jours[6] = samedi. Chaque jour comporte 24 heures
// (index 0 = minuit-1h, …, index 23 = 23h-minuit).
type LogonHours struct {
	Jours [7][24]bool
}

// EstAutorise indique si l'accès est autorisé à l'heure donnée.
func (h *LogonHours) EstAutorise(jour, heure int) bool {
	if jour < 0 || jour > 6 || heure < 0 || heure > 23 {
		return false
	}
	return h.Jours[jour][heure]
}

// Definir positionne l'autorisation à l'heure donnée.
func (h *LogonHours) Definir(jour, heure int, autorise bool) {
	if jour < 0 || jour > 6 || heure < 0 || heure > 23 {
		return
	}
	h.Jours[jour][heure] = autorise
}

// logonHoursFromBytes convertit 21 octets LDAP en LogonHours.
func logonHoursFromBytes(b []byte) LogonHours {
	h := LogonHours{}
	if len(b) != 21 {
		return h
	}
	for jour := 0; jour < 7; jour++ {
		for heure := 0; heure < 24; heure++ {
			idx := indexBit(jour, heure)
			octet := idx / 8
			bit := idx % 8
			h.Jours[jour][heure] = (b[octet] & (1 << bit)) != 0
		}
	}
	return h
}

// ToBytes convertit LogonHours en 21 octets LDAP.
func (h LogonHours) ToBytes() []byte {
	out := make([]byte, 21)
	for jour := 0; jour < 7; jour++ {
		for heure := 0; heure < 24; heure++ {
			if h.Jours[jour][heure] {
				idx := indexBit(jour, heure)
				out[idx/8] |= 1 << (idx % 8)
			}
		}
	}
	return out
}

// indexBit retourne l'index de bit (0-167) pour un jour et une heure.
func indexBit(jour, heure int) int {
	return jour*24 + heure
}

// HeuresCours retourne le préréglage "Heures cours" : lundi-vendredi 8h-17h.
func HeuresCours() LogonHours {
	h := LogonHours{}
	for jour := 1; jour <= 5; jour++ { // lundi à vendredi
		for heure := 8; heure < 17; heure++ {
			h.Jours[jour][heure] = true
		}
	}
	return h
}

// HeuresAdmin retourne le préréglage "Heures admin" : toujours autorisé.
func HeuresAdmin() LogonHours {
	h := LogonHours{}
	for jour := 0; jour < 7; jour++ {
		for heure := 0; heure < 24; heure++ {
			h.Jours[jour][heure] = true
		}
	}
	return h
}

// HeuresPersonnalise retourne une grille vide (tout interdit).
func HeuresPersonnalise() LogonHours {
	return LogonHours{}
}

// Sentinelles d'erreur pour les heures de connexion.
var (
	// ErrHeuresInvalides est retournée quand les heures sont invalides.
	ErrHeuresInvalides = errors.New("ad: heures de connexion invalides")
)

// LogonHoursParams décrit les paramètres de définition des heures de connexion.
type LogonHoursParams struct {
	UserDN string      // DN de l'utilisateur cible
	Heures *LogonHours // heures à appliquer (nil = pas de modification)
}

// Validate vérifie la cohérence des paramètres.
func (p *LogonHoursParams) Validate() error {
	if p.UserDN == "" {
		return ErrUserDNManquant
	}
	return nil
}

// LogonHoursResult décrit le résultat d'une opération d'heures de connexion.
type LogonHoursResult struct {
	UserDN   string      // DN de l'utilisateur
	Heures   *LogonHours // heures appliquées
	Statut   string      // "succes" | "echec" | "skip"
	Detaille string      // détail / message d'erreur
}

// LogonHoursSimulate simule l'application des heures de connexion (vérifie
// l'existence de l'utilisateur, ne modifie pas l'AD).
func (p *Pool) LogonHoursSimulate(ctx context.Context, params LogonHoursParams) (*LogonHoursResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &LogonHoursResult{
			UserDN:   params.UserDN,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	return &LogonHoursResult{
		UserDN:   u.DN,
		Heures:   params.Heures,
		Statut:   "succes",
		Detaille: fmt.Sprintf("heures de connexion à appliquer pour %s", u.SAMAccountName),
	}, nil
}

// LogonHoursExecute applique réellement les heures de connexion (écrit
// l'attribut logonHours sur l'utilisateur).
func (p *Pool) LogonHoursExecute(ctx context.Context, params LogonHoursParams) (*LogonHoursResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.LogonHoursSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation échouée: %s", sim.Detaille)
	}

	// Si Heures est nil, pas de modification à appliquer.
	if params.Heures == nil {
		return &LogonHoursResult{
			UserDN:   sim.UserDN,
			Statut:   "skip",
			Detaille: "aucune heure à appliquer (inchangé)",
		}, nil
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	donnees := params.Heures.ToBytes()
	modReq := ldap.NewModifyRequest(params.UserDN, nil)
	modReq.Replace("logonHours", []string{string(donnees)})
	if err := conn.Modify(modReq); err != nil {
		return &LogonHoursResult{
			UserDN:   sim.UserDN,
			Heures:   params.Heures,
			Statut:   "echec",
			Detaille: fmt.Sprintf("écriture logonHours: %v", err),
		}, nil
	}

	sim.Statut = "succes"
	sim.Detaille = fmt.Sprintf("heures de connexion appliquées pour %s", sim.UserDN)
	return sim, nil
}

// LogonHoursBulkParams décrit une opération d'heures de connexion en masse.
type LogonHoursBulkParams struct {
	UserDNs []string    // DN des utilisateurs (vide = tous de l'OU)
	OU      string      // OU pour sélectionner tous les utilisateurs
	Heures  *LogonHours // heures à appliquer
}

// Validate vérifie les paramètres en masse.
func (p *LogonHoursBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	return nil
}

// LogonHoursBulkExecute applique les heures de connexion à plusieurs utilisateurs.
func (p *Pool) LogonHoursBulkExecute(ctx context.Context, params LogonHoursBulkParams) ([]LogonHoursResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	var users []*User
	var err error

	if len(params.UserDNs) > 0 {
		for _, dn := range params.UserDNs {
			u, e := p.UserGet(ctx, dn)
			if e == nil {
				users = append(users, u)
			}
		}
	} else {
		users, err = p.UserSearch(ctx, params.OU,
			"(&(objectClass=user)(objectCategory=person))",
			[]string{"dn", "sAMAccountName", "displayName"}, 0)
		if err != nil {
			return nil, fmt.Errorf("recherche utilisateurs dans %s: %w", params.OU, err)
		}
	}

	results := make([]LogonHoursResult, 0, len(users))
	for _, u := range users {
		simParams := LogonHoursParams{
			UserDN: u.DN,
			Heures: params.Heures,
		}
		result, err := p.LogonHoursExecute(ctx, simParams)
		if err != nil {
			result = &LogonHoursResult{
				UserDN:   u.DN,
				Statut:   "echec",
				Detaille: err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

var _ = ldap.EscapeFilter // conservé pour cohérence avec les autres fichiers du paquet
