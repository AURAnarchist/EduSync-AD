package ad

import "strings"

// EchapperRDN échappe une valeur destinée à composer un RDN (nom relatif de
// DN), en reproduisant exactement `ldap3.utils.dn.escape_rdn` utilisé par la
// version Python : les caractères « , + " \ ; < > = » et l'octet nul sont
// précédés d'un antislash, « # » ne l'est qu'en tête, l'espace qu'en tête ou
// en fin de valeur.
//
// Cette fonction est volontairement distincte de `ldap.EscapeDN` (go-ldap) :
// celle-ci suit la RFC 4514 et n'échappe pas « = », ce qui produirait un DN
// différent de celui écrit par Python pour un nom contenant ce caractère.
func EchapperRDN(valeur string) string {
	if valeur == "" {
		return valeur
	}
	runes := []rune(valeur)
	var b strings.Builder
	for index, r := range runes {
		besoin := false
		switch r {
		case ',', '+', '"', '\\', ';', '<', '>', '=', 0x00:
			besoin = true
		case '#':
			besoin = index == 0
		case ' ':
			besoin = index == 0 || index == len(runes)-1
		}
		if besoin {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
