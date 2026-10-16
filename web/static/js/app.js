/**
 * EduSync AD — état client minimal (Alpine.js).
 *
 * Règle du projet : le serveur fait tout le travail. Ce fichier ne gère que
 * deux préférences d'affichage (thème, langue) qu'il persiste aussitôt dans
 * config.json — les mêmes préférences que la version Python.
 */
function edusync() {
  return {
    theme: document.documentElement.classList.contains('sombre') ? 'sombre' : 'clair',
    langue: document.documentElement.lang || 'fr',

    /** Bascule clair/sombre et enregistre le choix côté serveur. */
    basculerTheme() {
      this.theme = this.theme === 'sombre' ? 'clair' : 'sombre';
      document.documentElement.classList.toggle('sombre', this.theme === 'sombre');
      enregistrer('/theme', { theme: this.theme });
    },
  };
}

/** Envoie une préférence au serveur sans recharger la page. */
function enregistrer(url, donnees) {
  var corps = new URLSearchParams(donnees).toString();
  return fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: corps,
    credentials: 'same-origin',
    keepalive: true,
  }).catch(function (erreur) {
    console.warn('EduSync : préférence non enregistrée', erreur);
  });
}

/* ── Explorateur AD ─────────────────────────────────────────────────── */

// État partagé de l'explorateur (sélection de nœud dans l'arbre).
document.addEventListener('alpine:init', function () {
  Alpine.store('explorer', { selection: '' });
});

// Chargement paresseux des enfants d'un nœud de l'arbre : déclenché par
// l'événement « arbre-noeud-developpe » émis au dépliage. La réponse est un
// fragment explorer_tree.html ; on n'en injecte que les <li> enfants.
document.addEventListener('arbre-noeud-developpe', function (evenement) {
  var dn = evenement.detail && evenement.detail.dn;
  if (!dn) return;

  var noeud = document.querySelector('li[data-dn="' + CSS.escape(dn) + '"]');
  if (!noeud) return;
  var lazy = noeud.querySelector(':scope > ul > li[data-lazy-dn]');
  // Inexistant (déjà chargé) ou requête en cours : on n'y va qu'une fois.
  if (!lazy || lazy.dataset.charge === 'encours' || lazy.dataset.charge === 'fait') return;

  lazy.dataset.charge = 'encours';
  fetch('/app/explorer/tree?parent_dn=' + encodeURIComponent(dn) + '&include_groups=1', {
    headers: { 'HX-Request': 'true' },
    credentials: 'same-origin',
  })
    .then(function (reponse) {
      if (!reponse.ok) throw new Error('HTTP ' + reponse.status);
      return reponse.text();
    })
    .then(function (html) {
      if (!lazy.isConnected) return;
      var gabarit = document.createElement('template');
      gabarit.innerHTML = html;
      var ul = gabarit.content.querySelector('ul.explorer-tree');
      var enfants = ul
        ? Array.prototype.filter.call(ul.children, function (li) {
            return li.hasAttribute('data-dn');
          })
        : [];
      if (enfants.length === 0) {
        // Aucun enfant : on remplace le spinner par le message traduit.
        var vide = lazy.querySelector('[data-lazy-vide]');
        lazy.textContent = '';
        if (vide) {
          vide.classList.remove('hidden');
          lazy.appendChild(vide);
        }
        lazy.dataset.charge = 'fait';
        return;
      }
      enfants.forEach(function (li) {
        lazy.parentNode.insertBefore(li, lazy);
      });
      lazy.remove();
    })
    .catch(function () {
      // Repli silencieux : le spinner part au repos, le prochain dépliage
      // relancera la requête.
      lazy.dataset.charge = '';
    });
});
