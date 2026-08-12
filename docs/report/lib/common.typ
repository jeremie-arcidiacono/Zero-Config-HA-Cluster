// TODO: right now it is not possible to use placement and
//scope parameters of figures because that will be done
//relatively to the parent container. A solution would be
// to put the source in the caption and filter out the
// source part of the caption in the outline. This could
// be done by adding metatdata to the source and filtering it
// in the outline
#let sourced_figure(content, source: none, label: none, ..args) = {
  align(center, block(breakable: false, {
    set par(spacing: 1em)
    [
      #figure(
        content,
        ..args
      )
      #label
    ]
    if source != none [
      Source: #source
    ]
  }))
}

#let source_url(urls, i) = {
  let url = urls.at(i).trim(regex("(http|https)://"), at: start)
  let url = url.trim(regex("/.*"), at: end)
  [#link(url), ref. URL#{i+1}]
}

// Allow a body containing blocks (list, figure, ...)
#let titled_paragraph(title, body) = {
  set par(first-line-indent: 0cm)
  [*#title*: #body]
}

#let get_orientation(id) = {
  if id == none {
    return none
  } else if lower(id) == "sécurité" {
    return [Sécurité informatique]
  } else if lower(id) == "logiciel" {
    return [Informatique logicielle]
  } else if lower(id) == "embarqué" {
    return [Systèmes informatiques embarqués]
  } else {
    return text(red)[Entrer "sécurité" ou "logiciel" ou "embarqué"]
  }
}

#let common-headings(body) = {
  show smallcaps: set text(font: "Roboto") //TODO: could not work with Liberation / Arial find other font ?
  show heading: smallcaps //TODO: this makes the letter numbering also small caps...
  show heading: pad.with(top: 1em, bottom: 1em)
  show heading.where(level: 1): body => {
    pagebreak(weak: true) //TODO: to: "odd" ? If yes then not for starting tables (illustrations+sommaire etc.)
    align(center, body)
  }
  show heading.where(level: 3): pad.with(left: 0.5cm)
  show heading.where(level: 4): pad.with(left: 1cm)

  body
}


#let unnumbered-headings(body) = {
  show heading: set heading(numbering: none)

  body
}

#let month_to_content(m) = {
  return (
    "1": [janvier], "2": [février],  "3": [mars],  "4": [avril],
    "5": [mai], "6": [juin],  "7": [juillet],  "8": [août],
    "9": [septembre], "10": [octobre], "11": [novembre], "12": [décembre],
  ).at(str(m))
}

#let insert-pager(
  title,
  orientation,
  illustration,
  author,
  teachers,
  clients,
  is_internship,
  is_confidential,
  body,
) = {
    set par(leading: 0.65em) //TODO
    //show heading: pad.with(top: 0em, bottom: 0em)
    heading(
      {
        title
        if orientation != none {
          text(0.9em)[\ Orientation: #orientation]
        }
      },
      level: 1
    )

    v(0.3fr)

    body

    v(1fr)

    if illustration != none {
      align(center, illustration)
      v(1fr)
    }

    grid(columns: (0.8fr, 1fr),
      [
        #set par(first-line-indent: 0cm)
        Candidat-e:

        #strong(author)

        #text(0.8em)[Filière d'études: ISC]
      ],
      [
        #set par(first-line-indent: 0cm)
        Professeur-e(s) responsable(s):
        #for t in teachers {
          linebreak()
          strong(t)
        }
        #set text(0.8em)

        #strong[En collaboration avec:]
        #for c in clients {
                  linebreak()
          c
        }

        #let bool_to_fr(bool) = {if bool [oui] else [non]}
        Travail de stage soumis à une convention en entreprise: #bool_to_fr(is_internship)

        Travail soumis à un contrat de confidentialité: #bool_to_fr(is_confidential)
      ]
    )
    v(0.3fr)
}