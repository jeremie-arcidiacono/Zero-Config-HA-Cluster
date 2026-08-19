#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Résultats et discussion <chapter-results>

Les chapitres précédents ont posé la conception du système, détaillé son implémentation, mesuré son comportement sur le banc d'essai et exploré sa sécurisation.
Ce dernier chapitre referme le travail : il évalue dans quelle mesure les objectifs du cahier des charges sont atteints, revient sur les problèmes qui ont le plus marqué le développement, compare la planification prévue à son déroulement effectif, puis ouvre sur les améliorations qui restent à apporter.

== Réalisation des objectifs <section-results-objectives>

L'énoncé de ce travail fixe six exigences : l'autoconfiguration réseau des nœuds sans serveur externe, la construction du cluster initial par Serf et K3s avec une installation autonome de K3s à chaque nouvel arrivant, une image système complète et air-gapée pour l'architecture ARM64, une preuve de concept fonctionnelle sur du matériel physique, des tests de résilience accompagnés d'une logique de récupération, et une investigation sur la sécurisation du cluster, à mettre en œuvre si le temps le permet.
Nous reprenons chacune de ces exigences à la lumière du travail réalisé.

Les deux premières se vérifient ensemble, puisqu'elles forment une même chaîne : une machine se découvre par #acr("mDNS"), s'associe au groupe Serf sans intervention, puis installe K3s d'elle-même selon le rôle que le protocole lui attribue (#ref(<chapter-conception>), #ref(<chapter-implementation>)).
Aucune de ces étapes ne s'appuie sur un service externe propre au projet.
Seule une nuance mérite d'être rappelée : le réseau du client peut désormais compter sur un serveur #acr("DHCP") déjà présent, une contrainte revue en cours de route (#ref(<chapter-context>)), ce qui reste cohérent avec l'exigence puisque celle-ci vise l'absence d'infrastructure propre à antsd, pas l'absence de tout service sur le réseau du client.
La campagne de tests valide ce fonctionnement aussi bien à la création du cluster qu'au rattachement d'une machine, seule ou en lot (#ref(<part-tests-bootstrap>), #ref(<part-tests-joining>)).

La troisième et la quatrième exigence tiennent également ensemble.
L'image ants-os embarque K3s et antsd, ne demande aucun téléchargement pour s'installer, et a été reconstruite puis reflashée sur les machines du banc pour la campagne finale (#ref(<section-implementation-ants-os>)).
La preuve de concept qui en résulte tourne sur six Raspberry Pi 5 (#ref(<section-tests-testbed>)), et le déploiement d'une application de test prouve que le fonctionnement interne du cluster est réellement opérationnel, pas seulement que antsd déclare K3s installé (#ref(<part-tests-deployment>)).

La cinquième exigence est plus large que les précédentes, car aucune liste fermée de pannes à couvrir n'a jamais été fixée.
Rapportée à l'ensemble des scénarios envisageables, la campagne montre qu'une bonne partie d'entre eux sont soit encaissés automatiquement, comme un redémarrage, une coupure générale ou un retrait définitif tant qu'un quorum de serveurs reste vivant, soit ramenés à une procédure de récupération simple qui ne coûte que la machine concernée, jamais le cluster entier (#ref(<section-tests-limits>)).
Seuls deux cas, tous deux rares, exigent encore de repartir de zéro sur l'ensemble du banc.

La sixième exigence, enfin, s'arrête là où l'énoncé l'autorisait à s'arrêter.
L'investigation aboutit à des mesures priorisées et argumentées (#ref(<chapter-security>)), mais aucune n'a été implémentée dans le code, faute de temps disponible après la résilience.

Sur les six exigences du cahier des charges, quatre sont donc pleinement remplies, la cinquième l'est avec les limites qui viennent d'être rappelées, et la sixième se limite, comme l'énoncé le permettait, à une investigation.

== Problèmes rencontrés <section-results-problems>

Les difficultés propres à un mécanisme de antsd sont commentées au chapitre où ce mécanisme apparaît, ce qui est le bon endroit pour les comprendre.
Trois difficultés plus générales méritent en revanche d'être rassemblées ici, parce qu'elles tiennent moins à notre propre code qu'aux outils choisis et au matériel visé par ce travail.

La première touche à la complexité de K3s et d'etcd.
Ces outils étant complexes et très riches en fonctionnalités, leur compréhension demande un temps d'apprentissage conséquent.
N'étant familiarisé ni avec l'un ni avec l'autre, il a fallu un certain temps pour comprendre leur fonctionnement et leurs interactions, et pour identifier les détails pertinents dans notre situation.

La deuxième tient à l'écart entre le matériel visé par ce travail et celui pour lequel ces outils sont pensés@k3s_high_2026.
etcd, qui porte la base de données interne de K3s, est conçu pour un stockage rapide.
La documentation le rappelle, mais seule la mesure sur le banc en donne la portée concrète sur une carte SD de Raspberry Pi.
La création d'un cluster à quatre machines en a donné une illustration : le serveur initial s'est déclaré disponible avant d'avoir digéré son propre démarrage, et l'arrivée du serveur suivant a suffi à faire grimper la latence d'une écriture locale à près de deux secondes, ce qui lui a fait perdre son élection de leader et redémarrer.
Le système s'est rétabli seul en une quarantaine de secondes sans aucune intervention, mais l'épisode illustre bien les limitations qui ne peuvent pas être prévues lors de la phase initiale de conception.

La troisième difficulté est liée aux itérations qui ont été nécessaires pour obtenir une première image ants-os opérationnelle.
Certaines particularités propres à l'OS Raspbian on mit du temps à être trouvées et comprises, et la construction d'une image air-gapée a demandé plusieurs essais avant d'aboutir à un résultat satisfaisant.

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

== Améliorations futures <section-results-improvements>

Les pistes suivantes prolongent ce travail.
Certaines corrigent une limite déjà observée, d'autres répondent à une question restée ouverte en cours de projet.

- *Corriger les cas non supportés restants* : le #ref(<table_tests_limits>) détaille déjà, cas par cas, la correction envisagée.
- *Réaliser le décommissionnement* : volontairement écarté de ce travail pour prioriser le cœur des fonctionnalités (#ref(<section-implementation-decommission>)), il reste une commande de confort plutôt qu'une capacité manquante, et sa conception est déjà posée.
- *Mettre en œuvre les mesures de sécurité retenues* : l'investigation aboutit à des recommandations priorisées, dont la fermeture de l'entrée du cluster comme mesure centrale (#ref(<section-security-priorities>)).
- *Faciliter l'accès au cluster sans connaître son adresse IP* : afficher cette adresse sur l'écran d'une machine ants n'est pas un problème technique, mais demander à un client sans compétence de la saisir dans un navigateur va à l'encontre du produit visé. La question a été soulevée en cours de projet sans être tranchée. Deux pistes, hors du périmètre de ce travail, restent envisageables : diffuser un nom résolvable (par exemple `ants.local`), ou afficher un QR code sur l'écran de la machine, qui ouvrirait directement le tableau de bord depuis un téléphone.
