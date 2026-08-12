#import "common.typ": *

#let pager(
  title: none,
  orientation: none,
  author: none,
  teachers: (),
  clients: (),
  illustration: none,
  date: datetime.today(),
  internship: true,
  confidential: true,
  doc,
) = {

  show: common-headings 
  show: unnumbered-headings
  
  set page(
    paper: "a4",
    margin: (
      top: 3cm,
      rest: 2cm,
    ),
    header: grid(columns: (1fr, 1fr), align: (bottom, right+horizon), inset: 5pt,
      image("../assets/logos/logo-hepia.svg"),
      [
        Printemps #date.year() \
        Session de bachelor
      ]
    )
  )

  set par(first-line-indent:  (amount: 1cm, all: true), leading: 1em, spacing: 1.75em, justify: true)

  insert-pager(
    title,
    get_orientation(orientation),
    illustration,
    author,
    teachers,
    clients,
    internship,
    confidential,
    doc,        
  )

}