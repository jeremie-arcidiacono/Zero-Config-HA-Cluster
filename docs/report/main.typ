#import "/lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "globals.typ": urls

#show: hepia.semester.with(
  title: [Zero config high-availability cluster],
  short-title: title => [Zero config high-availability cluster], // Vous pouvez utiliser une fonction dans ce format pour créer votre titre court (en-tête de page), sinon le titre est utilisé
  author: (
    firstname: [Jérémie],
    lastname: [Arcidiacono]
  ),
  date: datetime(day: 19, month: 08, year: 2026), // Saisir la date du dépôt
  teachers: (
    [Florent Glück],
  ),
  clients: (
    [ANTS A.I. Systems],
  ),
  illustration: (
    illustration: image("/assets/images/main-project-illustration.jpg"),
    legend-source: [
      Ensemble de serveurs interconnectés représentant un système distribué.
      Source : #hepia.source_url(urls, 0)
    ],
  ),
  abstract: [
    #highlight("TODO")
  ],
  abstract-illustration: image(
    "/assets/images/main-project-illustration.jpg",
    alt: "Ensemble de serveurs interconnectés représentant un système distribué",
    height: 25%
    ),
  internship: false,
  confidential: false,
  orientation: [Informatique logicielle],
  dedication: none,
  acknowledgement: [
    Je souhaite remercier M. Florent Glück pour son encadrement et ses conseils tout au long de ce projet.
    Je remercie également M. Guillaume Chanel pour la création de la feuille de style Typst utilisée pour l'écriture de ce rapport.
    ],
  acronyms: (
    "HEPIA": ("Haute école du paysage, d'ingénierie et d'architecture"),
  ),
  figures_urls: urls,
  introduction: include("contents/introduction.typ"),
  conclusion: include("contents/conclusion.typ"),
  appendixes: (),
  bibliography-bytes: read("bibliography.bib", encoding: none)
)
