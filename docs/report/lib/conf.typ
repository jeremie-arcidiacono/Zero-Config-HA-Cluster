#import "common.typ": *
#import "@preview/acrostiche:0.7.0": *

#let in-outline = state("in-outline", false)

#let month_to_content(m) = {
  return (
    "1": [Janvier],
    "2": [Février],
    "3": [Mars],
    "4": [Avril],
    "5": [Mai],
    "6": [Juin],
    "7": [Juillet],
    "8": [Août],
    "9": [Setptembre],
    "10": [Octobre],
    "11": [Novembre],
    "12": [Decembre],
  ).at(str(m))
}


#let chapter_supplement = [Chapitre]
#let numbered-headings(body) = {
  // Number sections for main document content (except final chapters)
  show heading.where(level: 1): set heading(
    numbering: num => chapter_supplement + [~] + numbering("1", num) + [~:],
    supplement: chapter_supplement,
  )
  show heading.where(level: 2): set heading(numbering: "1.1.")
  show heading.where(level: 3): set heading(numbering: (..numbers, last) => numbering("a)", last))

  body
}

#let appendix_supplement = [Annexe]
#let appendix-headings(body) = {
  // Number sections for main document content (except final chapters)
  show heading: set heading(outlined: false)
  show heading.where(level: 2): set heading(outlined: true)
  show heading.where(level: 2): set align(center)

  counter(heading).update(0)

  let format_numbering(..nums) = appendix_supplement + [~] + numbering("1", nums.pos().last()) + [~:]
  show heading.where(level: 2): set heading(
    numbering: format_numbering,
    supplement: appendix_supplement,
  )

  body
}

#let format_ref_headings(body) = {
  // For section level 1 the reference only contain the supplement + numbering
  show ref.where(form: "normal"): it => {
    let el = it.element
    if el == none or el.func() != heading { return it }

    if el.level == 1 and el.supplement == chapter_supplement {
      // Handling chapters references
      return link(
        el.location(),
        el.supplement
          + [~]
          + numbering(
            "1",
            ..counter(heading).at(el.location()),
          ),
      )
    } else if el.level == 2 and el.supplement == appendix_supplement {
      // Handling appendix references
      return link(
        el.location(),
        el.supplement
          + [~]
          + numbering(
            "1",
            counter(heading).at(el.location()).last(),
          ),
      )
    } else { return it }
  }

  body
}

#let title_pages(
  title,
  author,
  illustration,
  orientation,
  date,
  teachers,
  clients,
) = {
  set text(font: "Liberation Sans", size: 14pt)

  // Logos
  grid(
    columns: (1fr, 1fr),
    align: (left, right),
    image("../assets/logos/logo-hepia.svg", width: 66%), image("../assets/logos/logo-hes-so-ge.svg", width: 66%),
  )

  align(center + horizon, [

    #v(2fr)

    // Title
    #text(size: 16pt, strong(upper(title)))

    #v(2fr)

    // Illustration
    #block(
      breakable: false,
      height: 30%,
      illustration.illustration,
    )

    #v(3fr)

    // Author
    Thèse de bachelor présentée par
    #v(1fr)
    #strong[#author.firstname #upper(author.lastname)]

    #v(1fr)

    // ISC + Orientation
    #v(1fr)
    #strong[Informatique et systèmes de communication avec orientation en \ #orientation]

    #v(2fr)

    // Date
    // TODO: why is it Septembre and not the month of the date as in the header ? yes same as date
    #strong[#month_to_content(date.month()) #datetime.year(date)]

    #v(3fr)

    #set text(size: 12pt)

    // Teachers and Clients
    #grid(
      columns: (1fr, 1fr),
      align: center + top,
      [
        Professeur-e(s) HES responsable(s)
        #for t in teachers {
          par(strong(t))
        }
      ],
      [

        Mandant(s) (si existant(s))
        #for c in clients {
          par(strong(c))
        }
      ],
    )
  ])

  pagebreak()

  text(size: 12pt, font: "Liberation Serif")[
    #v(1fr)
    Légende et source de l'illustration de couverture:


    #illustration.legend-source
    #v(0.2fr)
  ]
}

#let semester(
  title: none,
  short-title: title => { title },
  author: (
    firstname: none,
    lastname: none,
  ),
  orientation: none,
  teachers: (),
  clients: (),
  illustration: (
    illustration: none,
    legend-source: none,
  ),
  abstract: none,
  abstract-illustration: [Illustration obligatoire],
  internship: true,
  confidential: true,
  date: datetime.today(),
  dedication: none,
  acknowledgement: none,
  acronyms: none,
  figures_urls: none,
  introduction: [],
  conclusion: [],
  appendixes: (),
  bibliography-bytes: none,
  bibliography-style: "iso690-numeric-fr-no-abstract.csl",
  doc,
) = {
  //Configure page and text content
  set page(
    paper: "a4",
    margin: (
      top: 1.5cm,
      bottom: 1.5cm,
      inside: 2.5cm,
      outside: 2cm,
    ),
  )

  show outline: it => {
    in-outline.update(true)
    it
    in-outline.update(false)
  }

  show: common-headings
  show: unnumbered-headings

  set text(font: "Liberation Serif", size: 12pt, lang: "fr") // TODO: verify all fonts
  set par(first-line-indent: (amount: 1cm, all: true), leading: 1em, spacing: 1.75em, justify: true)

  // Two first pages
  title_pages(
    title,
    author,
    illustration,
    orientation,
    date,
    teachers,
    clients,
  )

  // Same header for the rest of the doc
  set page(header: text(size: 8pt, fill: gray)[
    #author.lastname, #author.firstname #sym.dash #short-title(title) #sym.dash Thèse BA #sym.dash #month_to_content(date.month()), #date.year()
  ])

  // Footer numbering for introduction pages
  set page(numbering: (..n) => context {
    if in-outline.get() {
      numbering("i", n.at(0))
    } else {
      numbering(sym.dash + " i " + sym.dash, n.at(0))
    }
  })


  // Outline
  {
    set text(font: "Liberation Serif")
    show outline.entry.where(level: 1): strong
    outline(title: [Table des matières], depth: 3, indent: 2em)
  }

  // Dedication
  if dedication != none {
    page[
      #v(20%)
      #align(right, text(font: "Liberation Serif", emph(dedication)))
    ]
  }

  // Acknowledgements
  if acknowledgement != none [
    = Remerciements
    #acknowledgement
  ]

  insert-abstract(
    [Résumé],
    abstract-illustration,
    [#author.firstname #author.lastname],
    teachers,
    clients,
    internship,
    confidential,
    abstract,
  )

  // Acronyms
  if acronyms != none {
    init-acronyms(acronyms)
    print-index(title: "Liste des acronymes", outlined: true, delimiter: none, row-gutter: 1em, sorted: "down")
  }

  // List of figures
  heading(level: 1)[Liste des figures et des tableaux]
  outline(title: none, target: figure)
  if figures_urls != none {
    set par(first-line-indent: 0cm)
    v(3em)
    smallcaps(strong[
      Références des url
    ])
    grid(
      columns: (0.2fr, 1fr), gutter: 1.3em,
      ..for (i, url) in figures_urls.enumerate() {
        ([URL#(i+1)], link(url))
      }
    )
  }

  //TODO: ajouter liste des annexes uniquement si nb_annexes > 3, sinon les annexes apparaissent dans la table des matière
  // si pas possible opter pour la solution de table des annexes séparée

  // Footer numbering for the rest of the document
  set page(numbering: (..n) => context {
    if in-outline.get() {
      numbering("1", n.at(0))
    } else {
      numbering(sym.dash + " 1 " + sym.dash, n.at(0))
    }
  })


  counter(page).update(1)

  // Introduction
  [
    = Introduction

    #introduction
  ]


  show: numbered-headings
  show: format_ref_headings

  doc

  show: unnumbered-headings

  // Conclusion
  [
    = Conclusion

    #conclusion
  ]

  // Add appendixes
  if appendixes.len() > 0 {
    heading(level: 1)[Annexes]
    show: appendix-headings
    for content in appendixes {
      pagebreak()
      content
    }
  }

  if bibliography-bytes != none {
    bibliography(bibliography-bytes, style: "../assets/styles/" + bibliography-style, title: [Références documentaires])
  }
}
