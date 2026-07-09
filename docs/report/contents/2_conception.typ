#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Conception <chapter-conception>

  Le chapitre précédent a permis de définir le contexte général du projet, de revenir sur différents points abordés durant le projet de semestre@arcidiacono_systeme_2026 et de justifier le choix des technologies fondamentales telles que Kubernetes et Serf. 
  Ce deuxième chapitre détaille la conception du système et l'architecture globale retenue pour répondre au cahier des charges. L'objectif est de présenter les mécanismes décisionnels et l'organisation structurelle de la solution avant d'aborder, dans le chapitre suivant, son implémentation technique détaillée.

// TODO : supprimer car redondant avec le chapitre précédent ??
  La conception est basée autour d'un principe simple: l'utilisateur final ne doit pas avoir à connaître la topologie du cluster ni à intervenir sur les nœuds après leur mise sous tension. Le rôle du système consiste donc à automatiser la découverte, le bootstrap, la supervision et l'adaptation du cluster, tout en laissant à l'application finale une interface de contrôle minimale et lisible.

== Exigences et fonctionnalités <title-conception-requirements>

  Le besoin principal de cette solution réside dans la création d'un environnement à zéro-configuration. Concrètement, le client final doit uniquement brancher physiquement les machines au réseau local et à l'alimentation électrique. Dès cet instant, le système prend le relais de manière totalement autonome pour découvrir les autres membres présents sur le réseau et former un cluster de serveurs fonctionnels. La figure #ref(<fig_conception_use-case>) résume ce scénario, mettant en évidence l'absence d'intervention technique requise avant l'utilisation finale de l'application.

#hepia.sourced_figure(
  caption: [Diagramme de cas d'utilisation],
  source: [Réalisé par Jérémie Arcidiacono],
  label: <fig_conception_use-case>,
  image("../assets/diagrams/conception_use-case.svg"),
)

La tolérance aux pannes est aussi une contrainte forte. 
En raison des contraintes strictes liées au consensus de la base de données interne de K3s, le cluster doit garder un nombre impair de servers, entre trois et sept au maximum, afin de garder un quorum stable@etcd_etcd_nodate @k3s_high_2026. 
Si une machine tombe ou si une nouvelle machine arrive, antsd doit donc pouvoir ajuster le rôle des nœuds sans casser cet équilibre.
Un mécanisme de redimensionnement dynamique est prévu afin de promouvoir ou rétrograder des nœuds automatiquement.

== Architecture générale <title-conception-architecture>

  Afin de répondre aux différents besoins et contraintes énumérés précédemment, nous avons conçu une architecture cible pour notre solution. Pour commencer, partons d'une vue d'ensemble de ce système final. Dans la #ref(<fig_conception_layers>), nous avons représenté l'une de nos machines en quatre niveaux.

#hepia.sourced_figure(
  caption: [Architecture d'une machine dans le cluster],
  source: [Réalisé par Jérémie Arcidiacono],
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

La base du système est une image ARM64 prête à l'emploi. Pour le PoC, elle cible des Raspberry Pi 5, ce qui permet de tester la solution sur une plateforme simple et peu coûteuse, tout en restant proche des machines réelles de ANTS A.I. Systems. L'image contient K3s, antsd, les images de conteneurs nécessaires pour un fonctionnement hors ligne, et un service systemd pour lancer antsd au démarrage.

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
  source: [Réalisé par Jérémie Arcidiacono],
  label: <fig_conception_antsd-components>,
  image("../assets/diagrams/conception_antsd-components.svg"),
)

  Dans cette architecture, antsd joue le rôle de cerveau local. Un gestionnaire de cluster centralise la logique décisionnelle, un module de communication K3s pilote l'instance locale, un wrapper Serf gère les échanges de membership et de diffusion d'événements, et une persistance locale conserve les informations minimales nécessaires à la reprise après redémarrage. Cette séparation permet de limiter le couplage entre les responsabilités, tout en gardant un point d'entrée unique pour les décisions d'orchestration.

  Il est important de noter que le daemon interagit avec les autres machines exclusivement via l'agent Serf. Lorsqu'il communique avec une instance de K3s, il interagit toujours avec le processus K3s local, plutôt qu'avec les autres nœuds du cluster. Cela permet de réduire la complexité et d'éviter de dupliquer dans antsd des mécanismes déjà gérés par K3s.

  Le choix d'embarquer Serf sous forme de librairie plutôt que comme processus séparé suit la même logique. antsd conserve ainsi la maîtrise du cycle de vie du daemon, des événements et de la communication entre nœuds, sans ajouter une couche d'intégration supplémentaire entre deux programmes distincts.

  === Controle et Monitoring

  L'utilisateur final a besoin de pouvoir facilement contrôler et surveiller l'état du cluster. Encore une fois, pour suivre la contrainte de simplicité, ces fonctionnalités doivent être intégrées dans la partie "application web finale" de notre architecture. Bien que la réalisation de cette interface web sorte de notre périmètre, il faut néanmoins que nous fournissions les informations nécessaires à son bon fonctionnement. C'est antsd qui est responsable de fournir ces informations, ainsi que de recevoir les commandes de contrôle et de les exécuter sur le système et sur la ou les machines concernées.

  Pour l'instant, nous décidons que cela se fera via de simples requêtes HTTP. Certaines permettent de récupérer des informations sur l'état et la santé du cluster, tandis que d'autres permettent d'envoyer des commandes de contrôle, telles que le décommissionnement d'une machine du cluster.

  Cette interface reste volontairement minimale. Les besoins identifiés se limitent à quelques points d'accès utiles : un endpoint de statut pour exposer l'état du nœud et du cluster, des points de profilage pour le diagnostic, et une commande de décommissionnement explicite pour retirer proprement un nœud. Ce choix évite de concevoir une API complète alors qu'aucun autre service interne n'a vocation à la consommer.

  En pratique, ces points d'accès servent de socle à l'onglet de réglages de l'application web finale. L'utilisateur n'interagit donc pas directement avec antsd pour des opérations complexes : il déclenche une action simple, et antsd traduit ensuite cette demande en opérations sur K3s en s'appuyant de Serf. Pour accéder à cette interface, l'utilisateur saisit simplement l'adresse IP affichée sur un petit écran présent sur les machines ANTS. Cet affichage local évite de devoir chercher l'adresse du cluster par un autre moyen.

=== Bootstrapping <title-conception-bootstrap>

Le bootstrapping est la phase qui donne sa forme initiale au cluster. C'est à ce moment que antsd décide si la machine initialise un nouveau cluster ou si elle rejoint un cluster déjà présent sur le réseau local.

Lorsqu'une machine démarre pour la première fois, antsd lance Serf, attend que les autres nœuds deviennent visibles et observe si un cluster existe déjà. Si c'est le cas, la machine rejoint ce cluster et installe K3s avec le rôle attendu. Sinon, elle passe en mode bootstrap et participe à la création du premier cluster K3s.

La #ref(<fig_conception_bootstrap-discovery>) illustre cette première phase de décision. Elle montre comment la machine démarre, observe le réseau local, puis choisit entre rejoindre un cluster déjà formé ou participer au bootstrap initial.

#hepia.sourced_figure(
  caption: [Décision au premier démarrage d'une machine],
  source: [Réalisé par Jérémie Arcidiacono],
  label: <fig_conception_bootstrap-discovery>,
  image("../assets/diagrams/conception_bootstrap-discovery.svg"),
)

Pour éviter que plusieurs machines créent chacune leur propre cluster, les nœuds passent d'abord par un état d'attente commun. Chacun annonce sa présence, puis un timer local laisse le temps aux autres machines de démarrer. La première machine dont le timer expire devient le nœud initial, noté N0. Elle installe le premier K3s Server puis diffuse l'information aux autres nœuds.

Les autres machines se répartissent ensuite selon leur position dans le groupe découvert. Les nœuds N1 et N2 rejoignent le cluster en tant que K3s Servers afin d'atteindre le quorum minimal du cluster haute disponibilité. Les nœuds suivants rejoignent ensuite le cluster dans le rôle qui convient le mieux à l'état du système.

La #ref(<fig_conception_bootstrap-sequence>) détaille cette deuxième partie du bootstrap. On y voit le passage en mode d'attente, puis la désignation du premier nœud à installer K3s en mode initialisation, avant que les autres machines rejoignent le cluster en parallèle.

#hepia.sourced_figure(
  caption: [Séquence du mécanisme de bootstrapping],
  source: [Réalisé par Jérémie Arcidiacono],
  label: <fig_conception_bootstrap-sequence>,
  image("../assets/diagrams/conception_bootstrap-sequence.svg"),
)

Une fois cette phase terminée, antsd enregistre l'état local nécessaire pour retrouver la machine après un redémarrage. Le daemon peut alors reprendre son fonctionnement normal sans refaire tout le processus de départ.

=== Cycle de vie d'une machine

Le comportement de antsd tout au long du cycle de vie de la machine est représenté sous la forme d'une machine d'états. La #ref(<fig_conception_antsd-state-machine>) détaille les différents états possibles et les transitions.

#hepia.sourced_figure(
  caption: [Diagramme de cycle de vie d'une machine],
  source: [Réalisé par Jérémie Arcidiacono],
  label: <fig_conception_antsd-state-machine>,
  image("../assets/diagrams/conception_antsd-state-machine.svg"),
)

Le premier choix distingue un démarrage initial d'un redémarrage connu. Lors d'un premier démarrage, antsd doit déterminer si la machine crée un nouveau cluster ou rejoint un cluster déjà en place. Cette logique, dite de bootstrap, est détaillée dans la section #ref(<title-conception-bootstrap>).
Lors d'un redémarrage, la présence d'un état local persisté permet au daemon de retrouver rapidement sa place dans le système sans repartir de zéro.

Ensuite, on distingue deux familles d'états : les états stables et les états de transition. Les états stables correspondent aux machines déjà intégrées au cluster K3s et pleinement fonctionnelles. Les états de transition couvrent les opérations de bootstrap, le rescaling, le décommissionnement et la reprise après redémarrage.

Cette séparation évite de mélanger des cas qui ne demandent pas les mêmes actions. Une machine en bootstrap ne doit pas être traitée comme une machine déjà prête, et un nœud en cours de retrait ne doit plus recevoir de nouvelles décisions d'orchestration. 

Le rescaling ne se déclenche pas à la moindre variation. Si une panne est courte, K3s peut gérer seul la remise en route normale du nœud. antsd intervient surtout quand la panne dure ou quand l'équilibre du cluster n'est plus bon. Dans ce cas, il peut promouvoir ou rétrograder un nœud depuis l'extérieur du cluster, au lieu de laisser K3s changer le rôle des machines trop tôt. Cette séparation garde le cluster plus stable pendant les redémarrages simples.

Les événements Serf servent enfin à propager ces changements au reste du cluster.
La machine d'états s'appuie sur les événements Serf comme mécanisme de propagation.
Chaque changement d'état est diffusé vers le reste du cluster, ce qui permet aux autres
nœuds d'adapter leur propre comportement sans recourire à des requêtes explicites entre eux. 
antsd conserve ainsi une vision cohérente.
