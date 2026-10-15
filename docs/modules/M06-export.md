# Module M6 — Export (CSV / Étiquettes PDF)

> **Objectif** : Sélection OU ± sous-OU → champs cochables (incl. MDP mémorisés) → PDF Avery L7160/L7163 + QR codes + couleurs pastel → CSV

---

## Fonctionnalités Python (référence v1.17.1)

| Fonctionnalité | Fichier Python | Description |
|----------------|----------------|-------------|
| Sélection OU arborescence | `src/edusync_ad/plugins/export/` | Checkbox OU + « Inclure sous-OU » |
| Choix champs | `src/edusync_ad/plugins/export/` | Checklist : identifiant, nom, prénom, mail, MDP (si dans coffre), téléphone, OU, groupes |
| Export CSV | `src/edusync_ad/core/export.py` | UTF-8, séparateur `;`, BOM optionnel |
| Étiquettes PDF | `src/edusync_ad/core/labels.py` | Avery L7160 (3x7) / L7163 (2x7), QR code vCard, couleurs pastel par OU/groupe |
| Trombinoscope | `src/edusync_ad/core/labels.py` | Grille photos + noms, A4/A3, mise en page auto |
| MDP mémorisés | `src/edusync_ad/core/coffre.py` | Lecture coffre chiffré si autorisation (Super-admin) |

---

## Fichiers Go à créer / modifier

### Backend (`internal/`)
```
internal/label/
├── csv.go                 # NOUVEAU : ExportCSV (streaming, champs sélectionnés)
├── pdf.go                 # NOUVEAU : ExportLabelsPDF (Avery, QR, couleurs)
├── template.go            # EXISTE : LabelTemplate, GridLayout

internal/ad/
├── export.go              # MODIFIER : Handlers réels (remplacer stubs)

internal/models/
├── export.go              # NOUVEAU : ExportOptions, LabelOptions, FieldSelection
```

### UI (`internal/ui/`)
```
internal/ui/
├── export.go              # NOUVEAU : Handlers exportCSV, exportLabels, exportPreview
├── server.go              # MODIFIER : Routes /export/*
└── i18n.go                # MODIFIER : Clés fr/en export
```

### Templates (`web/templates/`)
```
web/templates/
├── export.html              # NOUVEAU : Page export (sélection OU + champs + format)
├── export_preview.html      # NOUVEAU : Aperçu CSV / PDF (HTMX)
└── partials/
    ├── export_ou_selector.html      # NOUVEAU (arborescence checkbox)
    ├── export_field_selector.html   # NOUVEAU (champs cochables)
    ├── export_format_options.html   # NOUVEAU (CSV / L7160 / L7163 / Trombinoscope)
    └── export_qr_options.html       # NOUVEAU (vCard QR, taille, correction)
```

### CLI (`cmd/edusync-cli/`)
```
cmd/edusync-cli/
├── export.go                # NOUVEAU : csv, labels (JSON + fichiers sortie)
```

---

## Avancement actuel (14/10/2026)

| Composant | État |
|-----------|------|
| Routes `/export/*` | ✅ Existent + i18n |
| Backend `label/` | ✅ Package existant (templates, grid) |
| Handlers export | 🔨 **Stubs** (CSV/PDF factices) |
| Gabarit `export.html` | ⬜ Absent |
| Gabarit preview | ⬜ Absent |
| Sélecteur OU arborescence | ⬜ Absent |
| Sélecteur champs | ⬜ Absent |
| PDF Avery réel | ⬜ À implémenter (gopdf) |
| Commande CLI | ⬜ Absente |
| i18n | ⚠️ Partiel |
| Tests | ⬜ À écrire d'abord |

---

## Étapes restantes (ordre)

1. **Test d'abord** : `internal/label/csv_test.go`, `internal/label/pdf_test.go`, `internal/ui/export_test.go`
2. **Backend CSV** : `ExportCSV(ctx, options)` → streaming writer, champs sélectionnés, lecture MDP coffre si autorisé
3. **Backend PDF** : `ExportLabelsPDF(ctx, options)` → `github.com/signintech/gopdf`
   - Grille Avery L7160 (21 étiquettes/page) / L7163 (14 étiquettes/page)
   - QR code vCard (`github.com/skip2/go-qrcode`)
   - Couleurs pastel par OU/groupe (haché depuis DN)
   - Police, taille, marges configurables
4. **Template principal** : `export.html` — 3 sections : OU arborescence (gauche), Champs (centre), Format/Options (droite)
5. **Sélecteur OU** : `export_ou_selector.html` — arbre checkbox (réutiliser `explorer_tree` partial), « Inclure sous-OU »
6. **Sélecteur champs** : `export_field_selector.html` — checkboxes groupées (Identité, Contact, Sécurité, AD)
7. **Options format** : Radio CSV / L7160 / L7163 / Trombinoscope, options QR
8. **Aperçu** : `export_preview.html` — HTMX : CSV → 5 premières lignes, PDF → 1 page preview (base64 embed)
9. **Routes** : `GET /export`, `POST /export/preview`, `POST /export/csv`, `POST /export/pdf`
10. **i18n** : Clés `export.title`, `export.ou_selector`, `export.fields.identity`, `export.fields.password`, `export.format.csv`, `export.format.l7160`, `export.format.l7163`, `export.format.trombinoscope`, `export.preview` (fr+en)
11. **CLI** : `edusync-cli export csv --ou "OU=Eleves" --fields "sam,givenName,sn,mail" --output file.csv`, `export labels --format L7160 --qr --output file.pdf`
12. **Validation** : `task lint && task test && task test:python`

---

## Critères de validation

- [ ] Export CSV 5000 users → streaming (pas OOM), champs sélectionnés, MDP si autorisé
- [ ] PDF L7160 : 21 étiquettes/page, alignement correct, QR code scannable
- [ ] PDF L7163 : 14 étiquettes/page
- [ ] Trombinoscope : grille photos + noms, multi-pages A4, mise en page responsive
- [ ] Couleurs pastel : cohérentes par OU/groupe (hash DN → palette)
- [ ] Aperçu fonctionne : CSV 5 lignes, PDF 1 page
- [ ] CLI génère fichiers corrects (vérif `file` command, ouverture PDF)
- [ ] i18n fr/en complet
- [ ] Tests verts : `go test ./internal/label/... ./internal/ui/... -run Export`

---

## Commandes de test

```bash
# Tests unitaires
go test ./internal/label/... -v
go test ./internal/ui/... -run Export -v

# Test manuel
task ad:start
task dev
# UI : Export → Sélection OU Élèves → Champs (identité + MDP) → Format L7160 → Aperçu → Générer
# Vérifier : PDF valide, QR scannable, couleurs, champs corrects
```

---

## Fichiers Python de référence

- `src/edusync_ad/core/export.py` : Logique export CSV
- `src/edusync_ad/core/labels.py` : PDF Avery, QR codes, trombinoscope
- `src/edusync_ad/plugins/export/` : Plugin UI PyQt6
- `tests/core/test_export.py` : Tests Python