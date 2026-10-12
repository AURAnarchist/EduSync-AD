#!/usr/bin/env bash
# =============================================================================
#  provision-ad.sh — Environnement de test Active Directory (Samba AD DC)
# -----------------------------------------------------------------------------
#  Entrypoint du conteneur construit à partir de Dockerfile.samba-ad.
#
#  Au premier démarrage du conteneur :
#    1. provisionne le domaine EDUSYNC.LOCAL (realm identique) ;
#    2. crée l'arborescence OU : Eleves, Personnels, Groupes, Services, Archive ;
#    3. crée 50 comptes de test déterministes (25 élèves + 25 personnels,
#       prénoms/noms français avec accents, sAMAccountName eleveNN / persoNN) ;
#    4. crée les groupes (5 classes, 3 services, 5 délégations RBAC) et
#       calcule les adhésions (élèves -> classe, personnels -> service) ;
#    5. génère un certificat auto-signé et active LDAPS sur le port 636 ;
#    6. démarre Samba AD DC puis crée les GPO de test et les politiques de
#       mot de passe granulaires (fine-grained password policies / PSO) ;
#    7. termine par le message exact : « Samba AD DC ready ».
#
#  Le script est idempotent : il est relu à chaque démarrage, repère ce qui
#  existe déjà et ne recrée que ce qui manque (re-lancer ne doit pas échouer).
#
#  Variables d'environnement lues :
#    AD_REALM            domaine/realm à provisionner (défaut : EDUSYNC.LOCAL)
#    AD_ADMIN_PASSWORD   mot de passe du compte Administrator (défaut : TestPass123)
#    AD_DOMAIN           nom NetBIOS (défaut : 1er composant du realm)
#    PROVISION_ANNEXE=1  provisionne le 2e domaine de test ANNEXE.LOCAL :
#                        à lancer dans un 2e conteneur (tests multisite,
#                        cf. docs/AD_TEST_ENV.md)
#    PROVISION_FORCE=1   recrée les objets de test même s'ils existent déjà
#    DNS_FORWARDER       force « dns forwarder » dans smb.conf (défaut : auto)
#
#  ATTENTION : la provision AD de Samba écrit des xattr « security.* » (ACL du
#  partage SYSVOL), ce qui exige CAP_SYS_ADMIN. Lancer le conteneur avec
#  `docker run --cap-add SYS_ADMIN ...` sinon la provision échoue avec
#  « set_nt_acl ... NT_STATUS_ACCESS_DENIED ».
# =============================================================================

set -euo pipefail

# -----------------------------------------------------------------------------
# Paramétrage (surchargeable par variables d'environnement)
# -----------------------------------------------------------------------------
REALM="${AD_REALM:-EDUSYNC.LOCAL}"
# Le second domaine de test se provisionne dans son propre conteneur.
if [ "${PROVISION_ANNEXE:-0}" = "1" ]; then
    REALM="ANNEXE.LOCAL"
fi
DOMAINE="${AD_DOMAIN:-${REALM%%.*}}"          # EDUSYNC.LOCAL -> EDUSYNC
MDP_ADMIN="${AD_ADMIN_PASSWORD:-TestPass123}"
REALM_MIN="${REALM,,}"                        # edusync.local (pour les mails)
BASE_DN="DC=${REALM//./,DC=}"                 # DC=EDUSYNC,DC=LOCAL

SAMB_ADLIB="/var/lib/samba"                   # répertoire des données Samba
SAMDB="$SAMB_ADLIB/private/sam.ldb"
TLS_DIR="$SAMB_ADLIB/private/tls"
SMB_CONF="/etc/samba/smb.conf"
KRBTMP="/etc/krb5.conf"
RESOLV_CONF="/etc/resolv.conf"
CA_SYSTEME="/usr/local/share/ca-certificates/edusync-ad.crt"
# Marqueurs d'état : écrits uniquement quand l'étape a réellement réussi.
MARQUEUR_PROVISION="$SAMB_ADLIB/.edusync_provision_ok"
MARQUEUR_SEMIS="$SAMB_ADLIB/.edusync_semis_ok"

PORT_LDAP=389
PORT_LDAPS=636
HEURE_DEBUT=$(date +%s)

# -----------------------------------------------------------------------------
# Journalisation
# -----------------------------------------------------------------------------
log()  { printf '[provision-ad] %s\n' "$*"; }
avert() { printf '[provision-ad] [AVERTISSEMENT] %s\n' "$*" >&2; }
die()  { printf '[provision-ad] [ERREUR] %s\n' "$*" >&2; exit 1; }

# -----------------------------------------------------------------------------
# Jeu de données de test (déterministe : jamais de valeur aléatoire)
# -----------------------------------------------------------------------------

# Arborescence OU créée sous DC=...  ->  « nom OU|description »
OUS=(
    "Eleves|Comptes des eleves (lycee/college)"
    "Personnels|Comptes du personnel (enseignants, administration, IT)"
    "Groupes|Groupes de securite : classes, services, delegations RBAC"
    "Services|Comptes de service et de machine"
    "Archive|Comptes archives / departs (purge, gestion des départs)"
)

# Les 5 groupes de classes (5 eleves par classe -> 25 eleves)
CLASSES=(Classe_6e_A Classe_6e_B Classe_5e_A Classe_4e_A Classe_3e_A)

# Eleves : sAMAccountName|Prenom|Nom  (25 : eleve01 .. eleve25)
ELEVES=(
    "eleve01|Léa|Martin"
    "eleve02|Çagla|Durand"
    "eleve03|François|Lefebvre"
    "eleve04|Maëlys|Girard"
    "eleve05|Théo|Moreau"
    "eleve06|Camille|Bernard"
    "eleve07|Enzo|Roux"
    "eleve08|Jade|Fontaine"
    "eleve09|Nathan|Mercier"
    "eleve10|Chloé|Petit"
    "eleve11|Lucas|Garnier"
    "eleve12|Manon|Faure"
    "eleve13|Hugo|Blanc"
    "eleve14|Inès|Chevalier"
    "eleve15|Jules|Robin"
    "eleve16|Zoé|Marchand"
    "eleve17|Sacha|Dupont"
    "eleve18|Adèle|Meyer"
    "eleve19|Antoine|Guérin"
    "eleve20|Anaïs|Perrin"
    "eleve21|Gabriel|Aubert"
    "eleve22|Louna|Berger"
    "eleve23|Rayan|Guillaume"
    "eleve24|Éloïse|Barthélémy"
    "eleve25|Mathis|Caron"
)

# Personnels : sAMAccountName|Prenom|Nom|Service|Classes encadrees (CSV, vide si n/a)
#   perso01-12 -> Enseignants | perso13-18,23,24 -> Administration | perso19-22,25 -> IT
PERSONNELS=(
    "perso01|Marie|Courtois|Enseignants|Classe_6e_A,Classe_6e_B"
    "perso02|Philippe|Renard|Enseignants|Classe_5e_A,Classe_4e_A"
    "perso03|Isabelle|Lemoine|Enseignants|Classe_3e_A,Classe_6e_A"
    "perso04|Julien|Perrot|Enseignants|Classe_6e_B,Classe_5e_A"
    "perso05|Sandrine|Aubry|Enseignants|Classe_4e_A,Classe_3e_A"
    "perso06|Olivier|Meunier|Enseignants|Classe_6e_A,Classe_4e_A"
    "perso07|Céline|Rolland|Enseignants|Classe_6e_B,Classe_3e_A"
    "perso08|Karim|Haddad|Enseignants|Classe_5e_A,Classe_6e_B"
    "perso09|Aurélie|Bonnet|Enseignants|Classe_4e_A,Classe_6e_A"
    "perso10|Vincent|Marchal|Enseignants|Classe_3e_A,Classe_5e_A"
    "perso11|Élodie|Prévost|Enseignants|Classe_6e_A,Classe_5e_A"
    "perso12|Damien|Leclercq|Enseignants|Classe_6e_B,Classe_4e_A"
    "perso13|Nathalie|Benali|Administration|"
    "perso14|Bruno|Vasseur|Administration|"
    "perso15|Sophie|Delaunay|Administration|"
    "perso16|Karine|Nguyen|Administration|"
    "perso17|Yann|Delacroix|Administration|"
    "perso18|Mireille|Fabre|Administration|"
    "perso19|Éric|Chaput|IT|"
    "perso20|Romain|Giraud|IT|"
    "perso21|Fatou|Diallo|IT|"
    "perso22|Ludovic|Sanchez|IT|"
    "perso23|Hélène|Robert|Administration|"
    "perso24|Patrick|Chevallier|Administration|"
    "perso25|Nicolas|Bonhomme|IT|"
)

# Groupes de service : nom|description
SERVICES=(
    "Enseignants|Tous les enseignants (matieres et classes)"
    "Administration|Secretariat, scolarite et direction"
    "IT|Service informatique et administration technique"
)

# Groupes de delgation RBAC (scopes de droits de l'application)
RBAC=(
    "RBAC_Direction_Tous|Delegation globale : toutes les OU (direction)"
    "RBAC_Scolarite_Eleves|Delegation scolarite : OU Eleves et classes"
    "RBAC_RH_Personnels|Delegation RH : OU Personnels"
    "RBAC_IT_Services|Delegation technique : OU Services et Groupes"
    "RBAC_Lecture_Archive|Delegation lecture seule : OU Archive"
)

# Adhésions des groupes RBAC : groupe|membres (sAMAccountName ou groupe)
RBAC_MEMBRES=(
    "RBAC_Direction_Tous|perso23,perso24,RBAC_Scolarite_Eleves,RBAC_RH_Personnels"
    "RBAC_Scolarite_Eleves|perso13,perso14,perso15"
    "RBAC_RH_Personnels|perso16,perso17"
    "RBAC_IT_Services|perso19,perso20,perso21,perso22,perso25"
    "RBAC_Lecture_Archive|perso18"
)

# GPO de test et politiques de mots de passe granulaires
GPO_NOM="EduSync - Baseline test"
PSO_ELEVES="PSO_Eleves_Standard"
PSO_PERSONNELS="PSO_Personnels_Standard"

# -----------------------------------------------------------------------------
# Utilitaires
# -----------------------------------------------------------------------------

# Renvoie 0 si le port TCP passé en argument accepte une connexion sur localhost.
port_ouvert() {
    (exec 3<>/dev/tcp/127.0.0.1/"$1") 2>/dev/null
}

# Attendre qu'un port TCP s'ouvre (délai en secondes, défaut 120 s).
attendre_port() {
    local port="$1" delai="${2:-120}" i
    for ((i = 1; i <= delai; i++)); do
        if port_ouvert "$port"; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# S'assure que le nom d'hôte du conteneur est résoluble (Samba en a besoin).
verifier_hostname() {
    local h ip
    h="$(hostname)"
    if ! getent hosts "$h" >/dev/null 2>&1; then
        ip="$(hostname -i 2>/dev/null | awk '{print $1}' || true)"
        if [ -n "$ip" ]; then
            log "Ajout de '$h' ($ip) à /etc/hosts (résolution requise par Samba)"
            printf '%s %s\n' "$ip" "$h" >> /etc/hosts
        fi
    fi
}

# Écrit /etc/krb5.conf pour le domaine courant (idempotent).
ecrire_krb5_conf() {
    log "Écriture de $KRBTMP (realm $REALM)"
    cat > "$KRBTMP" <<EOF
# Généré par provision-ad.sh — domaine de test $REALM
[libdefaults]
    default_realm = $REALM
    dns_lookup_realm = false
    dns_lookup_kdc = true
    ticket_lifetime = 24h
    renew_lifetime = 7d
    forwardable = true
    rdns = false
    default_ccache_name = FILE:/tmp/krb5cc_%{uid}

[domain_realm]
    .$REALM_MIN = $REALM
    $REALM_MIN = $REALM
EOF
}

# Insère des lignes juste après « [global] » dans smb.conf.
# Utilisé uniquement si le paramètre n'existe pas déjà (idempotent).
inserer_apres_global() {
    local bloc="$SMB_CONF.bloc.$$" tmp="$SMB_CONF.tmp.$$"
    printf '%s\n' "$@" > "$bloc"
    awk -v bloc="$bloc" '
        /^\[global\]$/ && !insere { while ((getline l < bloc) > 0) print l; insere = 1 }
        { print }
    ' "$SMB_CONF" > "$tmp"
    mv "$tmp" "$SMB_CONF"
    rm -f "$bloc"
}

# Provisionne le domaine AD (uniquement au premier démarrage).
provisionner_domaine() {
    # Le smb.conf fourni par le paquet Debian (« standalone server ») empêche
    # samba-tool de provisionner : on le retire pour que Samba le régénère.
    rm -f "$SMB_CONF"
    log "Provisionnement du domaine $REALM (domaine NetBIOS $DOMAINE, serveur $(hostname)) ..."
    samba-tool domain provision \
        --realm="$REALM" \
        --domain="$DOMAINE" \
        --server-role=dc \
        --function-level=2008_R2 \
        --dns-backend=SAMBA_INTERNAL \
        --adminpass="$MDP_ADMIN" \
        --use-rfc2307 >/dev/null
    touch "$MARQUEUR_PROVISION"
    log "Domaine $REALM provisionné (Administrator / $MDP_ADMIN)"
}

# Nettoie un provisionnement resté inachevé (crash au démarrage précédent).
nettoier_provision_incomplet() {
    avert "Provisionnement détecté incomplet : nettoyage de l'état partiel"
    rm -rf "$SAMB_ADLIB/private" "$SAMB_ADLIB/sysvol" "$SAMB_ADLIB/netlogon" \
           "$SAMB_ADLIB/lock" "$SAMB_ADLIB/cache"
    rm -f "$SMB_CONF" "$MARQUEUR_PROVISION" "$MARQUEUR_SEMIS"
}

# --- Étape 2 : arborescence OU ----------------------------------------------
creer_ous() {
    local ligne nom desc
    log "Création de l'arborescence OU sous $BASE_DN"
    for ligne in "${OUS[@]}"; do
        IFS='|' read -r nom desc <<< "$ligne"
        if samba-tool ou list 2>/dev/null | grep -qx "OU=$nom"; then
            log "  OU=[$nom] déjà présente"
        else
            samba-tool ou create "OU=$nom" --description="$desc" >/dev/null
            log "  OU=[$nom] créée"
        fi
    done
}

# --- Étape 3 : groupes (classes, services, délégations RBAC) ----------------
creer_groupes() {
    local classe svc nom desc
    log "Création des groupes de test dans OU=Groupes"
    for classe in "${CLASSES[@]}"; do
        _creer_groupe "$classe" "Classe de test ($classe)"
    done
    for ligne in "${SERVICES[@]}"; do
        IFS='|' read -r nom desc <<< "$ligne"
        _creer_groupe "$nom" "$desc"
    done
    for ligne in "${RBAC[@]}"; do
        IFS='|' read -r nom desc <<< "$ligne"
        _creer_groupe "$nom" "$desc"
    done
}

_creer_groupe() {
    local nom="$1" desc="$2"
    if samba-tool group list 2>/dev/null | grep -qx "$nom"; then
        log "  Groupe [$nom] déjà présent"
    else
        samba-tool group add "$nom" --groupou="OU=Groupes" \
            --group-scope=Global --group-type=Security \
            --description="$desc" >/dev/null
        log "  Groupe [$nom] créé"
    fi
}

# --- Étape 4 : comptes de test ----------------------------------------------
creer_comptes() {
    local existe i ligne id prenom nom ou classe classe_txt service
    # Un seul appel : la liste des comptes existants sert d'index de déduplication.
    existe="$(samba-tool user list 2>/dev/null || true)"
    log "Création des 25 comptes élèves (eleve01..eleve25)"

    i=0
    for ligne in "${ELEVES[@]}"; do
        IFS='|' read -r id prenom nom <<< "$ligne"
        classe="${CLASSES[$((i % ${#CLASSES[@]}))]}"
        if grep -qx "$id" <<< "$existe"; then
            log "  Compte [$id] déjà présent"
        else
            samba-tool user create "$id" "$MDP_ADMIN" \
                --use-username-as-cn \
                --userou="OU=Eleves" \
                --given-name="$prenom" --surname="$nom" \
                --mail-address="$id@$REALM_MIN" \
                --description="Eleve de test - $classe" >/dev/null
            log "  Compte [CN=$id,OU=Eleves,$BASE_DN] créé ($prenom $nom)"
        fi
        i=$((i + 1))
    done

    log "Création des 25 comptes personnels (perso01..perso25)"
    for ligne in "${PERSONNELS[@]}"; do
        IFS='|' read -r id prenom nom service classe_txt <<< "$ligne"
        if grep -qx "$id" <<< "$existe"; then
            log "  Compte [$id] déjà présent"
        else
            samba-tool user create "$id" "$MDP_ADMIN" \
                --use-username-as-cn \
                --userou="OU=Personnels" \
                --given-name="$prenom" --surname="$nom" \
                --mail-address="$id@$REALM_MIN" \
                --department="$service" \
                --description="Personnel de test - service $service" >/dev/null
            log "  Compte [CN=$id,OU=Personnels,$BASE_DN] créé ($prenom $nom)"
        fi
    done
}

# --- Étape 5 : adhésions (élèves -> classes, personnels -> services) --------
gerer_adhesions() {
    local -A voulus=()                      # groupe -> liste CSV de membres
    local ligne id service classe_txt g membre manquants existants i

    # Élèves : chaque élève rejoint sa classe (déterministe, par index).
    i=0
    for ligne in "${ELEVES[@]}"; do
        id="${ligne%%|*}"
        classe_txt="${CLASSES[$((i % ${#CLASSES[@]}))]}"
        voulus["$classe_txt"]="${voulus[$classe_txt]:+${voulus[$classe_txt]},}$id"
        i=$((i + 1))
    done

    # Personnels : service + classes encadrées.
    for ligne in "${PERSONNELS[@]}"; do
        IFS='|' read -r id _ _ service classe_txt <<< "$ligne"
        voulus["$service"]="${voulus[$service]:+${voulus[$service]},}$id"
        if [ -n "$classe_txt" ]; then
            IFS=',' read -ra liste_classes <<< "$classe_txt"
            for classe_txt in "${liste_classes[@]}"; do
                voulus["$classe_txt"]="${voulus[$classe_txt]:+${voulus[$classe_txt]},}$id"
            done
        fi
    done

    # Délégations RBAC (membres fixes, groupes imbriqués compris).
    for ligne in "${RBAC_MEMBRES[@]}"; do
        g="${ligne%%|*}"
        voulus["$g"]="${ligne#*|}"
    done

    log "Calcul des adhésions aux groupes"
    for g in "${!voulus[@]}"; do
        existants="$(samba-tool group listmembers "$g" 2>/dev/null || true)"
        manquants=""
        IFS=',' read -ra liste <<< "${voulus[$g]}"
        for membre in "${liste[@]}"; do
            if [ -z "$membre" ]; then
                continue
            fi
            if ! grep -qx "$membre" <<< "$existants"; then
                manquants="${manquants:+$manquants,}$membre"
            fi
        done
        if [ -n "$manquants" ]; then
            samba-tool group addmembers "$g" "$manquants" >/dev/null
            log "  [$g] += $manquants"
        fi
    done
}

# --- Étape 5bis : politique de mot de passe au niveau domaine ----------------
# On conserve les valeurs « Windows Server » par défaut (complexité, historique,
# âge minimum, lockout désactivé) mais on désactive l'expiration : un
# conteneur de test reste en cache plusieurs jours/semaines et un mot de passe
# expiré ferait échouer les tests. Les PSO, eux, imposent des durées de vie
# réelles (90 j pour les élèves, 180 j pour les personnels).
ajuster_politique_domaine() {
    local age_max
    age_max="$(samba-tool domain passwordsettings show 2>/dev/null \
               | awk -F': ' '/Maximum password age/{print $2}')"
    if [ "$age_max" = "0" ]; then
        log "Politique domaine : expiration des mots de passe déjà désactivée"
        return 0
    fi
    if samba-tool domain passwordsettings set --max-pwd-age=0 >/dev/null 2>&1; then
        log "Politique domaine : expiration désactivée (${age_max:-?} j -> 0 j, PSO inchangés)"
    else
        avert "Réglage de la politique de mots de passe impossible (non bloquant)"
    fi
}

# --- Étape 6 : LDAPS (certificat auto-signé + smb.conf) ----------------------
configurer_ldaps() {
    local ip courant
    ip="$(hostname -i 2>/dev/null | awk '{print $1}' || true)"

    if [ ! -s "$TLS_DIR/cert.pem" ] || [ ! -s "$TLS_DIR/key.pem" ]; then
        log "Génération du certificat auto-signé LDAPS (10 ans, RSA 2048)"
        mkdir -p "$TLS_DIR"
        openssl req -newkey rsa:2048 -nodes \
            -keyout "$TLS_DIR/key.pem" \
            -x509 -days 3650 -out "$TLS_DIR/cert.pem" \
            -subj "/C=FR/O=EduSync-AD/OU=AD de test/CN=$(hostname)" \
            -addext "subjectAltName=DNS:$(hostname),DNS:localhost,DNS:edusync-dc,DNS:$REALM,DNS:$REALM_MIN,IP:127.0.0.1,IP:$ip" \
            >/dev/null 2>&1
        # Le CA auto-signé est le certificat lui-même (les tests lui font confiance).
        cp "$TLS_DIR/cert.pem" "$TLS_DIR/ca.pem"
        chmod 600 "$TLS_DIR/key.pem"
        log "  Écrit : $TLS_DIR/{cert.pem,key.pem,ca.pem}"
    else
        log "Certificat TLS déjà présent : $TLS_DIR/cert.pem"
    fi

    # On enregistre le CA dans le magasin système du conteneur : les outils
    # (ldapsearch -H ldaps://..., openssl) font alors confiance au certificat
    # sans avoir à désactiver la vérification. Les clients extérieurs doivent
    # importer ce même fichier (cf. docs/AD_TEST_ENV.md).
    if [ ! -f "$CA_SYSTEME" ]; then
        cp "$TLS_DIR/ca.pem" "$CA_SYSTEME"
        update-ca-certificates >/dev/null 2>&1 || true
        log "CA auto-signé ajouté au magasin système du conteneur ($CA_SYSTEME)"
    fi

    # smb.conf généré par la provision ne contient aucun paramètre TLS.
    if grep -qE '^[[:space:]]*tls enabled[[:space:]]*=' "$SMB_CONF"; then
        log "Paramètres TLS déjà présents dans smb.conf"
    else
        inserer_apres_global \
            "	tls enabled = yes" \
            "	tls keyfile = $TLS_DIR/key.pem" \
            "	tls certfile = $TLS_DIR/cert.pem" \
            "	tls cafile = $TLS_DIR/ca.pem"
        log "LDAPS activé : port 636 (tls enabled = yes)"
    fi

    # Par défaut, Samba exige une liaison LDAP « forte » (TLS ou SASL) : toute
    # simple bind en clair sur 389 est rejetée (erreur 8). Windows Server
    # accepte lui la liaison simple en clair (« LDAP server signing = None »),
    # et les tests doivent pouvoir vérifier le repli LDAP 389 non chiffré.
    if grep -qE '^[[:space:]]*ldap server require strong auth[[:space:]]*=' "$SMB_CONF"; then
        sed -i "s|^[[:space:]]*ldap server require strong auth[[:space:]]*=.*|\tldap server require strong auth = no|" "$SMB_CONF"
        log "Liaison simple LDAP autorisée en clair (ldap server require strong auth = no)"
    else
        inserer_apres_global "	ldap server require strong auth = no"
        log "Liaison simple LDAP autorisée en clair (ldap server require strong auth = no)"
    fi

    # Forwarder DNS : conservé tel qu'écrit par la provision s'il n'est pas forcé.
    if [ -n "${DNS_FORWARDER:-}" ]; then
        if grep -qE '^[[:space:]]*dns forwarder[[:space:]]*=' "$SMB_CONF"; then
            courant="$(grep -E '^[[:space:]]*dns forwarder[[:space:]]*=' "$SMB_CONF" | head -1)"
            sed -i "s|^[[:space:]]*dns forwarder[[:space:]]*=.*|\tdns forwarder = ${DNS_FORWARDER}|" "$SMB_CONF"
            log "dns forwarder : ${courant#*= } -> $DNS_FORWARDER"
        else
            inserer_apres_global "	dns forwarder = $DNS_FORWARDER"
            log "dns forwarder = $DNS_FORWARDER"
        fi
    fi
}

# --- Étape 7 : démarrage de Samba AD DC --------------------------------------
SAMBA_PID=""

arreter_samba() {
    if [ -n "$SAMBA_PID" ]; then
        log "Arrêt de Samba (signal reçu)"
        kill -TERM "$SAMBA_PID" 2>/dev/null || true
    fi
}
trap 'arreter_samba' TERM INT

demarrer_samba() {
    if port_ouvert "$PORT_LDAP"; then
        log "Samba AD DC déjà en cours d'exécution (port $PORT_LDAP ouvert)"
        return 0
    fi
    log "Démarrage de Samba AD DC (journal Samba -> sortie standard)"
    # -F : pas de démonisation ; --debug-stdout : logs visibles via docker logs.
    samba -F --debug-stdout --no-process-group &
    SAMBA_PID=$!
    log "PID Samba : $SAMBA_PID"

    # Attente du port LDAP : on échoue vite si Samba meurt au démarrage
    # (port 53 déjà pris, config invalide, droits insuffisants...).
    local i
    for ((i = 1; i <= 120; i++)); do
        if ! kill -0 "$SAMBA_PID" 2>/dev/null; then
            die "Samba s'est arrêté au démarrage (voir journal ci-dessus)"
        fi
        if port_ouvert "$PORT_LDAP"; then
            break
        fi
        if [ "$i" -eq 120 ]; then
            die "Samba n'a pas ouvert le port LDAP $PORT_LDAP (timeout 120 s)"
        fi
        sleep 1
    done
    log "LDAP répond sur le port $PORT_LDAP"

    if ! attendre_port "$PORT_LDAPS" 60; then
        die "LDAPS indisponible : le port $PORT_LDAPS ne répond pas"
    fi
    log "LDAPS répond sur le port $PORT_LDAPS"
}

# --- Étape 8 : résolution DNS locale -----------------------------------------
# Les commandes Samba (découverte du DC via SRV _ldap._tcp) et Kerberos ont
# besoin de résoudre la zone du domaine : elle n'est servie que par Samba.
# On pointe donc /etc/resolv.conf vers le DNS intégré du DC (comme sur un
# Windows Server qui se référence lui-même), le forwarder externe étant déjà
# renseigné dans smb.conf lors de la provision.
configurer_resolution_dns() {
    local ancien
    if ! port_ouvert 53; then
        avert "DNS du domaine non à l'écoute (port 53) : /etc/resolv.conf inchangé"
        return 0
    fi
    if grep -qE '^[[:space:]]*nameserver[[:space:]]+127\.0\.0\.1$' "$RESOLV_CONF"; then
        log "Résolution DNS déjà locale (127.0.0.1 = DNS du domaine)"
        return 0
    fi
    ancien="$(grep -E '^[[:space:]]*nameserver' "$RESOLV_CONF" | head -1 | awk '{print $2}')"
    cp -a "$RESOLV_CONF" "$RESOLV_CONF.edusync-bak" 2>/dev/null || true
    printf 'nameserver 127.0.0.1\nsearch %s\n' "$REALM_MIN" > "$RESOLV_CONF"
    log "Résolution DNS : ${ancien:-inconnu} -> 127.0.0.1 (DNS Samba, zone $REALM)"
}

# --- Étape 9 : GPO et politiques de mots de passe granulaires ----------------
# Les commandes « gpo » interrogent le DC via CLDAP : elles exigent donc la
# résolution DNS locale (ci-dessus) ET des identifiants (la liaison anonyme
# ne donne pas les droits d'écriture sur CN=Policies).
GPO_ETAT="absente"

creer_gpo_de_test() {
    local sortie guid
    if samba-tool gpo listall -U "Administrator%$MDP_ADMIN" 2>/dev/null | grep -qF "$GPO_NOM"; then
        GPO_ETAT="présente"
        log "GPO [$GPO_NOM] déjà présente"
        return 0
    fi
    if ! sortie="$(samba-tool gpo create "$GPO_NOM" -U "Administrator%$MDP_ADMIN" 2>&1)"; then
        avert "Création de la GPO de test impossible (non bloquant)"
        avert "  $(printf '%s' "$sortie" | grep -m1 -E '^(ERROR|LDAP)' || printf '%s' "$sortie" | tail -1)"
        GPO_ETAT="échec"
        return 0
    fi
    guid="$(printf '%s' "$sortie" | grep -oE '\{[0-9A-Fa-f-]{36}\}' | head -1)"
    GPO_ETAT="présente"
    log "GPO créée : [$GPO_NOM] $guid"
    # Liaison à l'OU Eleves (best-effort) : samba-tool gpo setlink attend le GUID.
    if [ -n "$guid" ] && samba-tool gpo setlink "OU=Eleves,$BASE_DN" "$guid" \
            -U "Administrator%$MDP_ADMIN" >/dev/null 2>&1; then
        log "  liée à OU=Eleves"
    else
        avert "Liaison GPO -> OU=Eleves non appliquée (non bloquant)"
    fi
}

creer_pso_de_test() {
    # Politique granulaire 1 : élèves (appliquée aux 5 groupes de classes).
    if samba-tool domain passwordsettings pso list 2>/dev/null | grep -q "$PSO_ELEVES"; then
        log "PSO [$PSO_ELEVES] déjà présent"
    else
        samba-tool domain passwordsettings pso create "$PSO_ELEVES" 10 \
            --min-pwd-length=8 --complexity=on --history-length=5 \
            --min-pwd-age=0 --max-pwd-age=90 \
            --account-lockout-threshold=5 --account-lockout-duration=10 \
            --reset-account-lockout-after=10 >/dev/null 2>&1 \
            || { avert "Création PSO élèves impossible (non bloquant)"; return 0; }
        log "PSO créé : [$PSO_ELEVES] (priorité 10, 8 car. min, lockout 5)"
        local classe
        for classe in "${CLASSES[@]}"; do
            samba-tool domain passwordsettings pso apply "$PSO_ELEVES" "$classe" \
                >/dev/null 2>&1 || true
        done
        log "  appliqué aux groupes : ${CLASSES[*]}"
    fi

    # Politique granulaire 2 : personnels (appliquée aux 3 groupes de service).
    if samba-tool domain passwordsettings pso list 2>/dev/null | grep -q "$PSO_PERSONNELS"; then
        log "PSO [$PSO_PERSONNELS] déjà présent"
    else
        samba-tool domain passwordsettings pso create "$PSO_PERSONNELS" 20 \
            --min-pwd-length=12 --complexity=on --history-length=12 \
            --min-pwd-age=1 --max-pwd-age=180 \
            --account-lockout-threshold=10 --account-lockout-duration=15 \
            --reset-account-lockout-after=30 >/dev/null 2>&1 \
            || { avert "Création PSO personnels impossible (non bloquant)"; return 0; }
        log "PSO créé : [$PSO_PERSONNELS] (priorité 20, 12 car. min)"
        local svc
        for svc in Enseignants Administration IT; do
            samba-tool domain passwordsettings pso apply "$PSO_PERSONNELS" "$svc" \
                >/dev/null 2>&1 || true
        done
        log "  appliqué aux groupes : Enseignants Administration IT"
    fi
}

# --- Étape 10 : récapitulatif ------------------------------------------------
afficher_resume() {
    local nb_eleves nb_persos nb_ous duree
    nb_eleves="$(samba-tool user list 2>/dev/null | grep -cE '^eleve[0-9]+$' || true)"
    nb_persos="$(samba-tool user list 2>/dev/null | grep -cE '^perso[0-9]+$' || true)"
    nb_ous="$(samba-tool ou list 2>/dev/null | grep -cE '^OU=(Eleves|Personnels|Groupes|Services|Archive)$' || true)"
    duree=$(( $(date +%s) - HEURE_DEBUT ))

    log "-----------------------------------------------------------"
    log "Domaine ............ $REALM (NetBIOS $DOMAINE)"
    log "Base DN ............ $BASE_DN"
    log "Identifiants ....... Administrator@$REALM / $MDP_ADMIN"
    log "LDAP / LDAPS ....... ports $PORT_LDAP / $PORT_LDAPS (certificat auto-signé)"
    log "SMB / DNS / Kerberos  445 / 53 / 88"
    log "OUs de test ........ $nb_ous/5 (Eleves, Personnels, Groupes, Services, Archive)"
    log "Comptes élèves ..... $nb_eleves/25 (CN=eleveNN,OU=Eleves,$BASE_DN)"
    log "Comptes personnels . $nb_persos/25 (CN=persoNN,OU=Personnels,$BASE_DN)"
    log "Groupes ............ ${#CLASSES[@]} classes + ${#SERVICES[@]} services + ${#RBAC[@]} délégations RBAC"
    log "GPO / PSO .......... GPO [$GPO_NOM] : $GPO_ETAT | PSO [$PSO_ELEVES] + [$PSO_PERSONNELS]"
    log "Provisionnement terminé en ${duree}s"
    log "-----------------------------------------------------------"
}

# =============================================================================
# Déroulement principal
# =============================================================================
verifier_hostname
ecrire_krb5_conf

# 1. Domaine AD ---------------------------------------------------------------
if [ -f "$MARQUEUR_PROVISION" ]; then
    log "Domaine déjà provisionné : $REALM (skip)"
elif [ -f "$SAMDB" ]; then
    # sam.ldb présent mais sans marqueur = provisionnement interrompu.
    nettoier_provision_incomplet
    provisionner_domaine
else
    provisionner_domaine
fi

# 2. Objets de test (OU, groupes, comptes, adhésions) -------------------------
if [ -f "$MARQUEUR_SEMIS" ] && [ "${PROVISION_FORCE:-0}" != "1" ]; then
    log "Objets de test déjà créés : passage en idempotent (skip)"
else
    creer_ous
    creer_groupes
    creer_comptes
    gerer_adhesions
    ajuster_politique_domaine
    touch "$MARQUEUR_SEMIS"
    log "Objets de test créés (OU, groupes, 50 comptes, adhésions)"
fi

# 3. LDAPS --------------------------------------------------------------------
configurer_ldaps

# 4. Samba AD DC --------------------------------------------------------------
demarrer_samba

# 5. Résolution locale de la zone du domaine (découverte du DC, Kerberos) -----
configurer_resolution_dns

# 6. GPO + politiques granulaires (après démarrage : accès LDAP garanti) -------
creer_gpo_de_test
creer_pso_de_test

# 7. Récapitulatif puis message d'appel (attendu par testcontainers) ----------
afficher_resume
printf 'Samba AD DC ready\n'

# Le conteneur reste actif tant que Samba tourne (docker stop → SIGTERM → exit).
code_statut=0
if [ -n "$SAMBA_PID" ]; then
    set +e
    wait "$SAMBA_PID"
    code_statut=$?
    set -e
else
    # Samba est déjà supervisé (script relancé à l'intérieur du conteneur) :
    # on garde simplement le conteneur vivant.
    log "Samba supervisé par un autre processus : le conteneur reste actif"
    while :; do
        sleep 5
    done
fi
log "Samba AD DC s'est arrêté (code $code_statut)"
exit "$code_statut"
