#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Conception <chapter-conception>

  Le chapitre précédent a permis de définir le contexte général du projet, de revenir sur différents points abordés durant le projet de semestre@arcidiacono_systeme_2026 et de justifier le choix des technologies fondamentales telles que Kubernetes et Serf. 
  Ce deuxième chapitre détaille la conception du système et l'architecture globale retenue pour répondre au cahier des charges. L'objectif est de présenter les mécanismes décisionnels et l'organisation structurelle de la solution avant d'aborder, dans le chapitre suivant, son implémentation technique détaillée.

== Architecture générale <title-conception-architecture>

  Afin de répondre aux différents besoins et contraintes énumérés précédemment, nous avons conçu une architecture cible pour notre solution. Pour commencer, partons d'une vue d'ensemble de ce système final. Dans la #ref(<fig_conception_layers>), nous avons représenté l'une de nos machines en quatre niveaux.

#hepia.sourced_figure(
  caption: [Architecture d'une machine dans le cluster],
  label: <fig_conception_layers>,
  image("../assets/diagrams/conception_layers.svg"),
)

  - *Hardware et OS* : la couche la plus basse, elle représente les composants matériels et le système d'exploitation de nos machines physiques. Voir #ref(<title-conception-ants-os>).
  - *Couche basse* : composée de ants daemon, qui permet de gérer les machines physiques, de les découvrir, de les provisionner, et de maintenir un état sain au sein du cluster. Voir #ref(<title-conception-antsd>).
  - *Couche haute* : composée de K3s, la distribution Kubernetes choisie (voir #ref(<title-context-kubernetes>)).
  - *Application finale* : la couche la plus haute. Elle représente l'application métier exploitée par ANTS A.I. Systems. Elle consomme les services exposés par K3s sans se préoccuper du matériel ni de la logique interne de découverte et de maintenance du cluster.

  Cette séparation permet d'isoler clairement les responsabilités. Le matériel et le système d'exploitation fournissent une base stable. antsd assure l'orchestration locale et la cohérence du cluster. K3s fournit l'environnement d'exécution des services conteneurisés. Enfin, l'application finale peut se concentrer sur ses propres fonctionnalités métier.
  Il est important de rappeler que l'application finale n'est pas développée dans le cadre de ce projet.

== ants-os <title-conception-ants-os>

La base du système est une image ARM64 prête à l'emploi. 
Pour le PoC, elle cible des Raspberry Pi 5, ce qui permet de tester la solution sur une plateforme simple et peu coûteuse, tout en restant proche des machines réelles de ANTS A.I. Systems. 
L'image contient K3s, antsd, les images de conteneurs nécessaires pour un fonctionnement hors ligne, et un service systemd pour lancer antsd au démarrage. 
// Pendant le développement, antsd est en revanche déposé sur les machines par un autre moyen, car c'est la partie du système qui change le plus souvent.
// Ce point sera détaillé dans le #ref(<chapter-tests>) dédié aux tests.

Cette image est construite à l'avance avec HashiCorp Packer@hashicorp_hashicorppacker_2026. Ce choix évite une installation manuelle sur chaque machine, réduit les différences logicielles entre nœuds et enlève la dépendance au réseau lors du premier démarrage. 
L'image contient le binaire K3s complet, les images de conteneurs requises pour fonctionner hors ligne, le binaire antsd avec sa
configuration, ainsi qu'un service qui lance automatiquement le daemon au démarrage.
Les outils de base restent aussi présents pour simplifier le diagnostic lors de la phase de développement.

Le choix de Packer plutôt que d'autres outils tel que `rpi-image-gen` est motivé par sa capacité à créer des images pour différentes plateformes et architectures.

En pratique, ants-os ne fait pas la logique du cluster. Il prépare simplement une machine propre, stable et identique aux autres, pour que antsd et K3s puissent démarrer de manière fiable.

== ants daemon <title-conception-antsd>

  Ants-daemon, aussi appelé `antsd`, est un daemon Go qui s'exécute sur chaque machine physique du cluster. Il est responsable de la gestion des machines, de leur découverte, de leur provisionnement et de la maintenance d'un état sain au sein du cluster. Il embarque un agent Serf, auquel il délègue la découverte des machines et la communication entre elles.
  C'est lui qui remplace le rôle humain dans un cluster traditionnel, en automatisant les tâches complexes et manuelles.

  La #ref(<fig_conception_antsd-components>) illustre les composants internes du programme et leurs interactions. On peut par exemple y voir l'agent Serf, auquel une boucle d'événements est attachée pour réagir aux changements de topologie et autres événements du cluster.

#hepia.sourced_figure(
  caption: [Diagramme de composants du daemon ants],
  label: <fig_conception_antsd-components>,
  image("../assets/diagrams/conception_antsd-components.svg"),
)

  Dans cette architecture, antsd joue le rôle de cerveau local. Un gestionnaire de cluster centralise la logique décisionnelle, un module de communication K3s pilote l'instance locale, un wrapper Serf gère les échanges de membership et de diffusion d'événements, et une persistance locale conserve les informations minimales nécessaires à la reprise après redémarrage. Cette séparation permet de limiter le couplage entre les responsabilités, tout en gardant un point d'entrée unique pour les décisions d'orchestration.

  Il est important de noter que le daemon interagit avec les autres machines exclusivement via l'agent Serf. Lorsqu'il communique avec une instance de K3s, il interagit toujours avec le processus K3s local, plutôt qu'avec les autres nœuds du cluster. Cela permet de réduire la complexité et d'éviter de dupliquer dans antsd des mécanismes déjà gérés par K3s.

  Le choix d'embarquer Serf sous forme de librairie plutôt que comme processus séparé suit la même logique. antsd conserve ainsi la maîtrise du cycle de vie du daemon, des événements et de la communication entre nœuds, sans ajouter une couche d'intégration supplémentaire entre deux programmes distincts.

  === Contrôle et supervision

  L'utilisateur final a besoin de pouvoir facilement contrôler et surveiller l'état du cluster. Encore une fois, pour suivre la contrainte de simplicité, ces fonctionnalités doivent être intégrées dans la partie "application web finale" de notre architecture. Bien que la réalisation de cette interface web sorte de notre périmètre, il faut néanmoins que nous fournissions les informations nécessaires à son bon fonctionnement. C'est antsd qui est responsable de fournir ces informations, ainsi que de recevoir les commandes de contrôle et de les exécuter sur le système et sur la ou les machines concernées.

  Pour l'instant, nous décidons que cela se fera via de simples requêtes HTTP. Certaines permettent de récupérer des informations sur l'état et la santé du cluster, tandis que d'autres permettent d'envoyer des commandes de contrôle.

  Cette interface reste volontairement minimale. Les besoins identifiés se limitent à quelques points d'accès utiles : un point de statut qui expose l'état du nœud et du cluster, les commandes de création d'un cluster décrites plus bas, et une commande de décommissionnement explicite pour retirer proprement un nœud. Ce choix évite de concevoir une API complète alors qu'aucun autre service interne n'a vocation à la consommer. Le décommissionnement n'est pas encore réalisé à ce stade du projet, contrairement aux deux autres.

  En pratique, ces points d'accès servent de socle à l'onglet de réglages de l'application web finale. L'utilisateur n'interagit donc pas directement avec antsd pour des opérations complexes : il déclenche une action simple, et antsd traduit ensuite cette demande en opérations sur K3s en s'appuyant sur Serf. Pour accéder à cette interface, l'utilisateur saisit simplement l'adresse IP affichée sur un petit écran présent sur les machines ANTS. Cet affichage local évite de devoir chercher l'adresse du cluster par un autre moyen.

=== Bootstrapping <title-conception-bootstrap>

Le bootstrapping est la phase qui donne sa forme initiale au cluster. C'est à ce moment que antsd décide si la machine initialise un nouveau cluster ou si elle rejoint un cluster déjà présent sur le réseau local.

Lorsqu'une machine démarre pour la première fois, antsd lance Serf, attend que les autres nœuds deviennent visibles et observe si un cluster existe déjà. Si c'est le cas, la machine rejoint ce cluster et s'y installe en agent, sans jamais toucher aux machines déjà en place : décider de la taille du plan de contrôle revient aux machines qui voient le cluster en entier, et non à celle qui arrive. Sinon, la machine se tient prête à participer à la création d'un premier cluster.

La #ref(<fig_conception_bootstrap-discovery>) illustre cette première phase de décision. Elle montre comment la machine démarre, observe le réseau local, puis choisit entre rejoindre un cluster déjà formé ou participer au bootstrap initial.

#hepia.sourced_figure(
  caption: [Décision au premier démarrage d'une machine],
  label: <fig_conception_bootstrap-discovery>,
  image("../assets/diagrams/conception_bootstrap-discovery.svg"),
)

La création d'un cluster est le seul moment où le système demande quelque chose à l'utilisateur.
Une machine qui ne trouve aucun cluster ne prend pas l'initiative d'en fabriquer un toute seule, car elle n'a aucun moyen de distinguer un réseau réellement vide d'un réseau dont les autres machines n'ont pas encore fini de démarrer.
Elle attend donc une demande explicite, suivie d'une confirmation, que l'utilisateur donne depuis l'écran de la machine.
Ce geste unique ajoute une sécurité conséquente : sans lui, brancher une machine à côté d'une installation existante mais momentanément arrêtée créerait un second cluster.

Une fois la confirmation reçue, toutes les machines en premier démarrage passent par un état d'attente commun. Chacune arme un minuteur local, ce qui laisse le temps à la liste des membres de se stabiliser avant que quoi que ce soit ne soit décidé. La première machine dont le minuteur expire diffuse simplement le signal de départ au groupe.

À la réception de ce signal, chaque machine trie la liste des membres visibles par nom et en déduit sa propre position, donc son rôle. La machine qui arrive en tête, notée N0, initialise le cluster et installe le premier K3s Server. Comme toutes les machines partent de la même liste et appliquent le même calcul, elles aboutissent au même résultat sans avoir à négocier.

Le nombre de servers à former découle d'une contrainte de la base de données interne de K3s.
Celle-ci fonctionne par consensus, ce qui suppose qu'une majorité de servers reste joignable pour que le cluster soit pilotable @k3s_high_2026.
Ce nombre doit donc rester impair, et ne pas dépasser sept @etcd_etcd_nodate.
Trois servers constituent le plancher de la haute disponibilité, c'est à dire le plus petit nombre qui permette au cluster de survivre à la perte d'une machine.
Un cluster peut compter moins de machines que cela, et le système doit alors fonctionner quand même, avec un seul server et sans haute disponibilité.
Les machines dont la position tombe dans ce quota deviennent servers, les autres deviennent agents.

Les machines suivantes rejoignent alors le cluster selon leur position.
Celles qui doivent devenir des servers le font une par une, dans l'ordre, car la base de données interne de K3s n'accepte qu'un ajout de membre à la fois. Celles qui deviennent des agents attendent que les servers attendus soient en place, puis rejoignent le cluster toutes ensemble.

La #ref(<fig_conception_bootstrap-sequence>) détaille cette deuxième partie du bootstrap. On y voit le passage en mode d'attente, puis l'installation du premier nœud en mode initialisation, l'arrivée en série des autres servers et enfin celle des agents.

#hepia.sourced_figure(
  caption: [Séquence du mécanisme de bootstrapping],
  label: <fig_conception_bootstrap-sequence>,
  image("../assets/diagrams/conception_bootstrap-sequence.svg"),
)

Une fois cette phase terminée, antsd enregistre l'état local nécessaire pour retrouver la machine après un redémarrage. Le daemon peut alors reprendre son fonctionnement normal sans refaire tout le processus de départ.

=== Cycle de vie d'une machine

Le comportement de antsd tout au long du cycle de vie de la machine est représenté sous la forme d'une machine d'états. La #ref(<fig_conception_antsd-state-machine>) détaille les différents états possibles et les transitions.

#highlight("TODO : trouver comment afficher ce diagramme sans que ca soit illisible")

#hepia.sourced_figure(
  caption: [Diagramme de cycle de vie d'une machine],
  label: <fig_conception_antsd-state-machine>,
  image("../assets/diagrams/conception_antsd-state-machine.svg"),
)

Le premier choix distingue un démarrage initial d'un redémarrage connu. Lors d'un premier démarrage, antsd doit déterminer si la machine crée un nouveau cluster ou rejoint un cluster déjà en place. Cette logique, dite de bootstrap, est détaillée dans la section #ref(<title-conception-bootstrap>).
Lors d'un redémarrage, la présence d'un état local persisté permet au daemon de retrouver rapidement sa place dans le système sans repartir de zéro.
Si cet état est illisible, ou incohérent avec l'installation K3s trouvée sur la machine, antsd s'arrête dans un état d'échec plutôt que de retomber sur un premier démarrage : celui-ci réinstallerait K3s par-dessus des données existantes.

Ensuite, on distingue deux familles d'états : les états stables et les états de transition. Les états stables correspondent aux machines déjà intégrées au cluster K3s et pleinement fonctionnelles. Les états de transition couvrent la création d'un cluster, l'arrivée d'une machine dans un cluster déjà en service, la reprise après redémarrage, le rescaling et le décommissionnement.

Cette séparation évite de mélanger des cas qui ne demandent pas les mêmes actions. Une machine en bootstrap ne doit pas être traitée comme une machine déjà prête, et un nœud en cours de retrait ne doit plus recevoir de nouvelles décisions d'orchestration.

Le rescaling existe parce que la règle sur le nombre de servers vaut pour toute la vie du cluster, et pas seulement à sa création. Chaque machine qui arrive ou qui disparaît change la population, donc le nombre de servers souhaitable. antsd doit pouvoir promouvoir et rétrograder des nœuds pour suivre cette valeur, sans jamais la laisser devenir paire.

Ce mécanisme ne se déclenche cependant pas à la moindre variation. Si une panne est courte, K3s peut gérer seul la remise en route normale du nœud. antsd intervient surtout quand la panne dure ou quand l'équilibre du cluster n'est plus bon. Il commence alors par retirer du cluster les machines durablement perdues, car une machine morte reste comptée dans la majorité requise et rapproche donc le cluster de la perte de son quorum. Il peut ensuite promouvoir ou rétrograder un nœud depuis l'extérieur du cluster, au lieu de laisser K3s changer le rôle des machines trop tôt. Cette séparation garde le cluster plus stable pendant les redémarrages simples.

Les événements Serf servent enfin à propager ces changements au reste du cluster.
La machine d'états s'appuie sur les événements Serf comme mécanisme de propagation.
Chaque changement d'état est diffusé vers le reste du cluster, ce qui permet aux autres
nœuds d'adapter leur propre comportement sans recourir à des requêtes explicites entre eux.
antsd conserve ainsi une vision cohérente.
