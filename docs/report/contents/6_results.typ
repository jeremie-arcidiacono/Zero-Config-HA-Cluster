#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Résultats et discussion <chapter-results>

#highlight("TODO")

== Planification prévue et effective <section-results-planning>

Le travail s'est déroulé sur douze semaines, selon un découpage établi durant la phase préparatoire.
La #ref(<fig_results_gantt_planned>) reprend le planning prévisionnel.

#hepia.sourced_figure(
  caption: [Planning prévisionnel du projet],
  label: <fig_results_gantt_planned>,
  image("../assets/diagrams/results_gantt_planned.svg", width: 100%),
)

La #ref(<fig_results_gantt_effective>) donne le déroulement effectif.
Les deux figures restent volontairement approximatives : elles regroupent ou omettent certaines tâches afin de rester lisibles, et ne montrent pas les allers-retours entre elles.

#hepia.sourced_figure(
  caption: [Déroulement effectif du projet],
  label: <fig_results_gantt_effective>,
  image("../assets/diagrams/results_gantt_effective.svg", width: 100%),
)

Le premier écart concerne le système d'exploitation.
L'apprentissage et la complexité des outils utilisés demande un travail que la planification a sous-estimé.

Le deuxième écart touche la rédaction.
Le mémoire devait démarrer au début du mois de juin, il n'a réellement commencé qu'à la mi-juin.
Le temps réservé à l'écriture avant l'échéance du rendu intermédiaire était donc plus court, ce qui a décalé le début du développement de antsd.

Le troisième écart n'est pas un retard mais un découpage inadapté.
Le planning séparait le développement de antsd d'une phase de résilience qui aurait suivi.
En pratique, la tolérance aux pannes est présente dans presque chaque décision du protocole, et une bonne partie des tâches rangées sous antsd la traite déjà.

À l'inverse, plusieurs tâches du planning effectif ne pouvaient pas être anticipées, car elles sont nées des tests sur le banc d'essai.
C'est le cas par exemple du protocole d'oubli et du coffre d'assets air-gap.
La classification des incidents, prévue à la fin, s'est quant à elle fondue dans le reste des tâches de résilience.

L'investigation sur la sécurité, enfin, avait été placée en fin de projet et pensée comme malléable, de manière à s'étendre ou à se réduire en fonction du temps disponible.
Elle a finalement demandé moins de temps que ce qui lui était réservé.
