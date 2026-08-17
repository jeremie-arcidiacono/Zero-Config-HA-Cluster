#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Contexte <chapter-context>

Ce premier chapitre présente le contexte dans lequel s'inscrit ce projet, et pose toutes les bases nécessaires à la compréhension de la problématique et de la solution proposée.
Il revient sur de nombreuses notions déjà présentées dans le mémoire du projet de semestre@arcidiacono_systeme_2026, qui sont essentielles pour comprendre ce Travail de Bachelor qui en est la continuité.

Il est divisé en trois parties. Nous présentons d'abord Kubernetes et sa distribution K3s, puis Serf, les deux outils sur lesquels repose la solution.
Ces deux présentations reviennent au passage sur les choix effectués lors du projet de semestre@arcidiacono_systeme_2026 et sur ce qui les a motivés.
Nous exposons enfin les besoins et les contraintes du projet, ainsi que la manière dont certains d'entre eux ont évolué en cours de route.

== Présentation de Kubernetes <section-context-kubernetes>

Kubernetes@kubernetes_documentation_2026, aussi connu sous le nom de "K8s", est un orchestrateur de conteneurs open source.
Son rôle est d'automatiser le déploiement, la mise à l'échelle et la gestion d'applications conteneurisées sur un
ensemble de machines appelé "cluster".
Kubernetes est aujourd'hui devenu le standard largement adopté par l'industrie.

Outre ses capacités de déploiement, Kubernetes se distingue par ses mécanismes natifs de résilience.
Cela justifie son choix pour notre projet qui, par sa nature distribuée, doit être capable de faire face à des pannes matérielles ou des interruptions de réseau.

Le premier pilier de cette robustesse est la gestion de la haute disponibilité. Dans un cluster Kubernetes, la charge de travail n'est pas restreinte à une unique machine. L'orchestrateur permet de répliquer facilement les instances d'une application pour les répartir sur de multiples nœuds. Cette architecture garantit qu'en cas de défaillance matérielle isolée, les requêtes sont automatiquement prises en charge par les répliques fonctionnelles. Le service global de la plateforme ne subit ainsi aucune interruption.

Afin de maintenir cette stabilité sur la durée, le système s'appuie également sur une capacité d'auto-réparation ("self-healing"). 
Ce mécanisme repose sur la surveillance continue des charges de travail déployées. 
Lorsqu'un processus s'arrête de manière inattendue, se bloque ou échoue à un bilan de santé, l'orchestrateur intervient de façon autonome. Il se charge d'éliminer le conteneur défaillant et de démarrer immédiatement une nouvelle instance saine, garantissant ainsi que l'état de l'environnement converge en permanence vers l'état désiré.

Enfin, cette vigilance s'applique tout autant à l'infrastructure physique sous-jacente grâce à un mécanisme de surveillance par heartbeats. Chaque machine du cluster signale continuellement son état de santé à l'orchestrateur. Si une machine vient à subir une panne ou une déconnexion et cesse d'émettre ces signaux, le système identifie le nœud comme étant hors service. En réaction, les charges de travail qui y étaient associées sont immédiatement replanifiées et redémarrées sur les nœuds encore sains. L'ensemble de ces comportements autonomes fait de Kubernetes une solution très pertinente pour gérer une infrastructure distribuée à haute disponibilité.

=== Architecture d'un cluster

Après avoir présenté pourquoi Kubernetes est un choix pertinent, il est nécessaire de présenter brièvement son architecture.
Un cluster Kubernetes est composé de deux types de nœuds :

- *Control Plane* (nœud maître) : responsable de la gestion de l'état et de la configuration du cluster.
- *Worker* : responsable de l'exécution des applications.

Dans la #ref(<fig_context_kubernetes-architecture>), nous pouvons voir un nœud Control Plane qui gère deux nœuds Worker
(appelés "Node" sur la figure) et les différents éléments qui les constituent.
Nous ignorons volontairement le "Cloud provider API" et "Cloud Controller Manager" qui sont des composants optionnels
utilisés dans les environnements cloud, ce qui n'est pas notre cas.

#hepia.sourced_figure(
caption: [Architecture d'un cluster Kubernetes],
source: [tiré de #hepia.source_url(urls, 1)],
label: <fig_context_kubernetes-architecture>,
image("../assets/images/context_components-of-kubernetes.svg"))

Le Control Plane est composé de quatre composants principaux :

- *etcd* : base de données clé/valeur distribuée qui stocke l'état complet du cluster.
- *API Server* : point d'entrée unique pour administrer le cluster ; toutes les communications internes et externes
  transitent par lui.
- *Scheduler* : détermine sur quel nœud Worker chaque charge de travail doit être déployée.
- *Controller Manager* : surveille en permanence l'état du cluster et effectue les ajustements nécessaires pour
  maintenir l'état souhaité.

Un nœud Worker est quant à lui composé des éléments suivants :

- *Kubelet* : agent qui reçoit les instructions du Control Plane et gère les charges de travail localement.
- *Kube-Proxy* (optionnel) : assure la gestion du trafic réseau à destination et en provenance des conteneurs.
- *Container Runtime* : moteur d'exécution des conteneurs (par exemple, containerd).

L'unité de déploiement de base dans Kubernetes est le Pod, qui peut contenir un ou plusieurs conteneurs partageant le
même espace réseau et de stockage.

=== K3s

K3s@k3s_k3s_2026 est une distribution légère de Kubernetes développée par Rancher (Suse) sous la licence Apache 2.0.
Ce point explique la possibilité d'exploitation commerciale de la solution par ANTS A.I. Systems.
Cette licence permissive autorise l'utilisation, la modification et la redistribution du logiciel, y compris au sein d'un produit commercial et sans obligation de publier les modifications apportées.
Les seules obligations sont de conserver les mentions de licence et de droits d'auteur, et de signaler les fichiers modifiés.
Contrairement à une licence copyleft telle que la GPL, elle n'impose donc pas que le produit final soit lui-même distribué en source ouverte.

K3s est conçu pour fonctionner sur des machines aux ressources matérielles limitées, notamment dans des contextes
d'edge computing ou d'architectures ARM, tout en restant conforme à l'API Kubernetes standard.

C'est la distribution retenue pour ce projet, en raison de sa faible consommation de ressources, de sa simplicité
d'installation et de ses optimisations pour ARM, ce qui correspond aux besoins d'ANTS A.I. Systems.

Dans K3s, la terminologie est légèrement différente de celle de Kubernetes@k3s_architecture_2026 :

- Un *Agent* désigne un nœud Worker.
- Un *Server* représente un nœud Control Plane, mais il intègre également tous les composants d'un Agent et peut donc exécuter des Pods.

Il est possible de former un cluster composé exclusivement de nœuds Server, sans obligation d'avoir des nœuds Agent dédiés.
Cela s'explique par le fait qu'un nœud Server est en fait un Agent avec des responsabilités supplémentaires. Il peut donc exécuter des charges de travail tout en assurant la gestion du cluster.

Il faut cependant souligner qu'il y a quelques inconvénients à cette approche.
Premièrement, la base de données interne (etcd) d'un Server est très sensible à la latence d'écriture, si bien qu'une charge de travail qui sature le disque ou le processeur retarde ses écritures.
Un Server qui répond trop lentement est considéré comme défaillant par ses pairs, ce qui peut dégrader le quorum du cluster.
K3s signale d'ailleurs explicitement ce risque sur des machines dont le stockage est lent, comme un Raspberry Pi équipé d'une carte SD@k3s_high_2026.
Le deuxième inconvénient concerne la sécurité, puisqu'un Server détient les identifiants du plan de contrôle et la totalité de la base de données du cluster.
Une charge applicative qui s'échappe de son conteneur peut donc théoriquement accéder à ces informations sensibles, bien que le risque reste faible. Nous revenons sur cet aspect au #ref(<chapter-security>).

Nous acceptons malgré tout ces inconvénients, car l'alternative est trop coûteuse dans notre contexte.
Le produit vendu par ANTS A.I. Systems est destiné à des clients qui n'acquièrent que quelques machines.
En réserver au moins trois à la seule gestion du cluster reviendrait à en soustraire une part considérable.
Kubernetes fournit par ailleurs l'option nécessaire si ce choix devait être revu plus tard, sous la forme d'un paramètre qui empêchent l'ordonnanceur de placer des charges ordinaires sur les nœuds Server@kubernetes_taints_2026.

Dans la suite de ce document, nous utiliserons la terminologie de K3s plutôt que celle de Kubernetes.

== Présentation de Serf <section-context-serf>

Serf@hashicorp_hashicorpserf_2026 est l'outil retenu pour assurer la découverte des nœuds et la communication entre eux. Dans le mémoire du projet de semestre@arcidiacono_systeme_2026, ce sujet est détaillé plus largement dans la section consacrée à Serf. Ici, nous en gardons seulement les éléments utiles à la compréhension de la solution.

Au cœur de Serf, nous retrouvons la bibliothèque Memberlist@hashicorp_hashicorpmemberlist_2026, qui gère l'appartenance au cluster et la détection de pannes en se basant sur une version modifiée du protocole #acr("SWIM")@wikipedia_swim_2026. 
Ce dernier repose sur deux mécanismes distincts : la détection de pannes, assurée par un sondage périodique aléatoire (chaque nœud teste un membre choisi au hasard et, en cas de non-réponse, délègue ce test à quelques autres membres), et la diffusion des changements d'appartenance.
Cette diffusion se fait de proche en proche : un nœud qui apprend une information la transmet à quelques membres tirés au hasard, qui la relaient à leur tour jusqu'à ce que tout le groupe soit informé.
Ce mode de propagation est désigné par le terme #emph[gossip], que nous utilisons dans la suite de ce document.
Ces deux mécanismes étant entièrement décentralisés, le protocole reste efficace lorsque le nombre de nœuds augmente, sans introduire de point de défaillance unique.
Serf ajoute la couche d'orchestration manquante à Memberlist en complétant le système de gestion de l'appartenance avec la propagation d'événements arbitraires et l'exécution de requêtes/réponses (queries).

Contrairement à Consul, Serf ne s'intéresse pas à l'abstraction de services, mais bien à la gestion des nœuds. Ce point est important pour notre système, dont le rôle est avant tout de gérer l'infrastructure matérielle afin de préparer un environnement stable pour la couche haute, c'est-à-dire K3s.

Serf permet d'abord de découvrir automatiquement les autres machines du réseau local. Pour cela, chaque nœud doit avoir un nom unique, qui peut par exemple être construit à partir de son adresse MAC. Une fois lancé, Serf émet des requêtes multicast sur le réseau local afin de trouver les autres membres du cluster. Cette approche permet une découverte automatique sans configuration d'adresse préalable, ce qui correspond bien à notre objectif zéro-configuration.

Serf propose aussi plusieurs mécanismes de communication entre nœuds. Les tags servent à associer des métadonnées clé/valeur à chaque machine, les événements permettent de diffuser des informations à tout le cluster, et les queries offrent un système synchrone de requête/réponse pour interroger tout ou partie du cluster. Ces mécanismes sont suffisants pour transmettre des informations simples, déclencher des actions et suivre l'état des nœuds.

Enfin, Serf peut conserver l'état du cluster sous forme de snapshots. Cela permet à un nœud qui redémarre de retrouver plus rapidement sa place dans le cluster et d'éviter de repartir de zéro. Pour notre projet, cette capacité est utile, car elle rend la reprise après redémarrage plus rapide et plus robuste.

== Présentation des besoins et contraintes <section-context-needs>

Les besoins de ce projet ont trois origines. Ils viennent du produit que ANTS A.I. Systems commercialise, de l'énoncé du travail de Bachelor, et du projet de semestre qui l'a précédé@arcidiacono_systeme_2026, où une première liste de contraintes avait été posée pour cadrer la recherche de solutions existantes.

Les contraintes du projet de semestre ont été écrites avant toute expérimentation sur du matériel réel, et deux d'entre elles ont évolué. Nous les présentons ici telles qu'elles étaient, puis nous expliquons ce qui a motivé leur évolution. 
// Cette section décrit donc le problème à résoudre, et non la solution : la manière dont l'architecture y répond fait l'objet du #ref(<chapter-conception>).

=== Besoins

Le point de départ est un lot de machines identiques, sorties de leur emballage, que le client branche à l'alimentation et au réseau local. À partir de cet instant, tout doit se dérouler sans lui.

Les machines doivent d'abord se trouver mutuellement sur le réseau, sans que personne n'ait à saisir la moindre adresse. Elles doivent ensuite installer K3s, puis s'accorder sur la configuration du cluster à former et sur le rôle de chacune. Une machine ajoutée plus tard, alors que le cluster est déjà en service, doit rejoindre celui-ci d'elle-même, sans que le cluster existant ait à être arrêté ou reconfiguré.

Le système doit enfin survivre aux pannes. La perte d'une machine ne doit pas interrompre le service, et le cluster doit se réorganiser pour retrouver un état sain, ce qui suppose aussi bien d'écarter la machine défectueuse que de redistribuer les responsabilités qu'elle portait. De cette manière le système est capable de tenir dans la durée.

L'ensemble doit rester clé en main. Le client type de ANTS A.I. Systems est une petite entreprise ou un indépendant, qui ne possède pas d'équipe technique ni les ressources nécessaires pour gérer une infrastructure complexe. Les machines sont donc vendues comme un produit autonome.

Le client doit malgré tout pouvoir garder un œil sur son cluster, pour en consulter l'état et déclencher les rares actions qui lui reviennent. Cet accès prend la forme d'une interface web, dont la réalisation sort du périmètre de ce travail.
Cependant, les informations et les commandes qu'elle expose doivent être fournies par le système.
De plus, les machines vendues par ANTS A.I. Systems possèderont probablement, en plus de l'interface web, un écran et quelques boutons physiques (ou un écran tactile), qui permettront là aussi d'afficher l'état du cluster et de déclencher quelques actions.
Pour les besoins de démonstration et de validation, une interface web minimale a été développée.
Cette interface est une fusion de l'interface web et de l'écran physique.


La #ref(<fig_context_use-case>) résume ces attentes. Elle sépare les actions du client, à savoir brancher les machines, consulter le cluster et exploiter ses applications, de ce que le système prend en charge seul, à savoir former le cluster et le rétablir après une panne.

#hepia.sourced_figure(
  caption: [Diagramme de cas d'utilisation],
  label: <fig_context_use-case>,
  image("../assets/diagrams/context_use-case.svg"),
)

=== Contraintes

Toutes les machines sont identiques au départ, autant par leur matériel que par leur système d'exploitation et leur configuration. Aucune ne porte de rôle particulier avant d'être branchée, et aucune ne peut donc être désignée à l'avance comme la machine principale. 
Cette homogénéité est une propriété du produit, puisque le client reçoit un lot de machines interchangeables. Elle a une conséquence directe sur la conception, car la répartition des rôles doit alors être décidée par le système lui-même, au démarrage.

L'intervention humaine doit rester minimale. 
L'énoncé du travail emploie précisément ce mot, là où le projet de semestre parlait d'absence totale d'intervention. 
Le système s'autorise donc à solliciter l'utilisateur, mais seulement à des moments clés et pour des gestes courts, du type lire ce qu'affiche l'écran de la machine et appuyer sur un ou deux boutons. 
C'est le cas à la création du tout premier cluster, qui demande une confirmation dont les raisons sont exposées au #ref(<chapter-conception>). Ce peut aussi être le cas lorsqu'une machine se retrouve dans une situation dont elle ne sait pas se sortir seule, ce qui doit rester exceptionnel. 
En dehors de ces moments, le système fonctionne sans intervention externe.

L'important est qu'aucune de ces sollicitations ne soit de nature technique. 
Le client n'a ni configuration à écrire, ni commande à saisir, ni topologie à connaître.

La solution doit être sous licence libre. 
ANTS A.I. Systems ne souhaite pas fonder son produit sur des logiciels propriétaires, ce qui écarte non seulement les solutions payantes, mais aussi celles dont la licence contraindrait la commercialisation.

=== Contraintes révisées en cours de projet

Le projet de semestre imposait que le système ne devait s'appuyer sur aucune infrastructure présente sur le réseau, et citait explicitement le serveur #acr("DHCP") comme dépendance à éviter. 
Le raisonnement s'appuyait sur l'auto-attribution d'adresses que permet IPv6, laquelle supprime effectivement ce besoin, et sur l'idée qu'un service extérieur constitue un problème d'autonomie.

Cette position a été réevaluée durant le projet (en juin 2026). 
Un réseau d'entreprise, même modeste, dispose presque toujours d'un serveur #acr("DHCP"), et un client qui n'en possède pas exploite un réseau suffisamment particulier pour ne pas correspondre à la cible du produit. Se priver d'une option universellement disponible pour couvrir un cas de figure rare complique la solution sans rien apporter au client type. 
Nous considérons donc désormais qu'un serveur #acr("DHCP") est présent. La contrainte de fond n'est pas abandonnée pour autant, puisque le système continue de fonctionner sans aucun serveur ou infrastructure qui lui soit propre : seul la configuration réseau est déléguée à l'existant.

La seconde contrainte revue concerne l'accès à Internet, et il faut ici distinguer deux exigences.

La première porte sur le système d'exploitation, qui doit contenir tout le nécessaire pour installer et démarrer anstd et K3s sans rien télécharger. Cette exigence figure dans l'énoncé du travail et elle est satisfaite. 
Elle protège le client contre une installation qui échouerait à cause d'un réseau lent, temporairement indisponible, mal configuré ou d'un registre d'images indisponible, situations bien plus fréquentes qu'une absence complète de connectivité.

La seconde portait sur le réseau lui-même, que le projet de semestre supposait totalement coupé d'Internet. 
Grâce à des tests en conditions réels sur des machines physiques (le banc d'essai), nous avons constaté que cette hypothèse compliquait inutilement la solution.

Le banc d'essai ayant été volontairement placé sur un réseau isolé afin de ressembler le plus possible aux conditions de départ, nous avons constaté que les machines n'avaient plus la bonne date et heure. 
Leur date était fausse de plusieurs mois, et surtout elle différait d'une machine à l'autre. 
L'explication est simple : les Raspberry Pi utilisés ne conservent pas l'heure lorsqu'ils sont hors tension, faute d'horloge sauvegardée par pile, et le service de synchronisation (#acr("NTP")) n'a aucun serveur de temps à interroger sur un réseau isolé. 
Le problème est plus marqué sur des Raspberry Pi que sur les machines commercialisées par ANTS A.I. Systems, mais il peut tout de même survenir, en particulier sur de longues périodes.

Une heure fausse peut causer de nombreux problèmes, et fournir un mécanisme de synchronisation du temps est trop complexe et n'apporte pas de valeur au client. 

Nous avons donc accepté, en juillet 2026, de lever cette contrainte. 
Le cluster peut disposer d'un accès à Internet, ce qui est de toute façon le cas d'un réseau d'entreprise ordinaire, et il en tire la synchronisation de l'heure. 
Cet accès n'est pas une dépendance forte : rien dans la formation du cluster ni dans sa réparation ne nécéssite Internet, et une coupure de la liaison extérieure ne modifie en rien le comportement du système.

L'ensemble des besoins et des contraintes étant posé, nous pouvons maintenant présenter l'architecture conçue pour y répondre.
