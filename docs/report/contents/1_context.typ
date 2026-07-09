#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Contexte <chapter-context>

Ce premier chapitre présente le contexte dans lequel s'inscrit ce projet, et pose toutes les bases nécessaires à la compréhension de la problématique et de la solution proposée.
Il revient sur de nombreuses notions déjà présentées dans le mémoire du projet de semestre@arcidiacono_systeme_2026, qui sont essentielles pour comprendre ce Travail de Bachelor qui en est la continuité.

Il est divisé en plusieurs parties : tout d'abord, une présentation des outils Kubernetes et Serf, qui font partie intégrante de la solution proposée. 
Ensuite, nous reviendrons rapidement sur différents choix effectués lors du projet de semestre@arcidiacono_systeme_2026.
Enfin, nous présenterons les différents besoins et contraintes identifiés.

== Présentation de Kubernetes <title-context-kubernetes>

Kubernetes@kubernetes_documentation_2026, aussi connu sous le nom de "K8s", intervient en tant qu'orchestrateur de conteneurs open-source.
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
Elle est conçue pour fonctionner sur des machines aux ressources matérielles limitées, notamment dans des contextes
d'edge computing ou d'architectures ARM, tout en restant conforme à l'API Kubernetes standard.

C'est la distribution retenue pour ce projet, en raison de sa faible consommation de ressources, de sa simplicité
d'installation et de ses optimisations pour ARM, ce qui correspond aux besoins d'ANTS A.I. Systems.

Dans K3s, la terminologie est légèrement différente de celle de Kubernetes :

- Un *Agent* désigne un nœud Worker.
- Un *Server* représente un nœud Control Plane, mais il intègre également tous les composants d'un Agent et peut donc exécuter des Pods.

Il est possible de former un cluster composé exclusivement de nœuds Server, sans obligation d'avoir des nœuds Agent dédiés.
Cela s'explique par le fait qu'un nœud Server est en fait un Agent avec des responsabilités supplémentaires. Il peut donc exécuter des charges de travail tout en assurant la gestion du cluster.

Dans la suite de ce document, nous utiliserons la terminologie de K3s plutôt que celle de Kubernetes.

== Présentation de Serf <title-context-serf>

Serf est l'outil retenu pour assurer la découverte des nœuds et la communication entre eux. Dans le mémoire du projet de semestre@arcidiacono_systeme_2026, ce sujet est détaillé plus largement dans la section consacrée à Serf. Ici, nous en gardons seulement les éléments utiles à la compréhension de la solution.

Au cœur de Serf, nous retrouvons la bibliothèque Memberlist, qui gère le maintien de l'état du cluster via le protocole SWIM. Serf ajoute la couche d'orchestration manquante à Memberlist en complétant le système de gestion de l'appartenance avec la propagation d'événements arbitraires et l'exécution de requêtes/réponses (queries).

Contrairement à Consul, Serf ne s'intéresse pas à l'abstraction de services, mais bien à la gestion des nœuds. Ce point est important pour notre système, dont le rôle est avant tout de gérer l'infrastructure matérielle afin de préparer un environnement stable pour la couche haute, c'est-à-dire K3s.

Serf permet d'abord de découvrir automatiquement les autres machines du réseau local. Pour cela, chaque nœud doit avoir un nom unique, qui peut par exemple être construit à partir de son adresse MAC. Une fois lancé, Serf émet des requêtes multicast sur le réseau local afin de trouver les autres membres du cluster. Cette approche permet une découverte automatique sans configuration d'adresse préalable, ce qui correspond bien à notre objectif zéro-configuration.

Serf propose aussi plusieurs mécanismes de communication entre nœuds. Les tags servent à associer des métadonnées clé/valeur à chaque machine, les événements permettent de diffuser des informations à tout le cluster, et les queries offrent un système synchrone de requête/réponse pour interroger tout ou partie du cluster. Ces mécanismes sont suffisants pour transmettre des informations simples, déclencher des actions et suivre l'état des nœuds.

Enfin, Serf peut conserver l'état du cluster sous forme de snapshots. Cela permet à un nœud qui redémarre de retrouver plus rapidement sa place dans le cluster et d'éviter de repartir de zéro. Pour notre projet, cette capacité est utile, car elle rend la reprise après redémarrage plus rapide et plus robuste.

== Présentation des besoins et contraintes <title-context-needs>

#highlight("TODO")
