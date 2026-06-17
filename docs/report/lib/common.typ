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


#let insert-abstract(
  title,
  illustration,
  author,
  teachers,
  clients,
  is_internship,
  is_confidential,
  body,
) = {
    set par(leading: 0.65em) //TODO

    heading(title, level: 1)

    body

    v(1fr)
    align(center, illustration)
    v(1fr)
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