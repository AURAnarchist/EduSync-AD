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
