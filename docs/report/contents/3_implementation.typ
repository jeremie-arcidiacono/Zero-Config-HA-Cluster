#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls, src, src_dir, pkg

= Implémentation <chapter-implementation>

Le chapitre précédent a décrit l'architecture retenue pour notre solution, les responsabilités de chacune de ses couches et les mécanismes qui permettent à un groupe de machines de former un cluster sans intervention humaine.
Ces éléments expliquent ce que le système doit accomplir, mais pas encore comment il s'y prend.
C'est l'objet de ce chapitre : nous y présentons la manière dont ces décisions se traduisent en code, les problèmes concrets rencontrés lors de la réalisation, ainsi que les compromis retenus pour les résoudre.

Nous commençons par l'organisation générale du code de antsd, puis nous suivons le trajet d'une information à travers le programme.
Nous regardons d'abord la couche qui produit les événements, c'est-à-dire l'encapsulation de Serf et la découverte des machines, ensuite le composant qui les traite, et enfin les actions que ce dernier déclenche sur K3s et sur le disque.
Nous terminons par ce qui entoure le daemon : son interface de contrôle et la construction de l'image ants-os.
La manière dont tout ceci est vérifié, ainsi que les essais menés sur les machines physiques, font l'objet du #ref(<chapter-tests>).

== Organisation du code de antsd <section-implementation-structure>

Avant de détailler le fonctionnement interne du daemon, il convient de justifier le choix du langage de programmation, puis de présenter la manière dont le code est découpé.

Le langage Go@go_go_2026 s'impose assez naturellement pour ce projet, et ce pour plusieurs raisons.
La première tient à la nature du livrable : la compilation produit un binaire unique, lié statiquement, qui ne dépend d'aucune bibliothèque présente sur la machine cible.
Ce binaire se dépose tel quel dans l'image système, sans gestionnaire de paquets ni environnement d'exécution à installer, ce qui correspond exactement à notre contrainte de fonctionnement hors ligne.
La deuxième raison est plus structurelle.
antsd est un programme entièrement piloté par des événements, qui doit réagir à des messages réseau tout en surveillant des minuteurs et des installations longues. Les goroutines et les canaux de Go sont conçus pour ce type de problème, et nous verrons dans la #ref(<section-implementation-manager>, supplement: [section]) que le cœur du daemon en tire directement parti.
Enfin, Serf et Memberlist sont eux-mêmes écrits en Go.
Les embarquer sous forme de bibliothèque, comme décidé lors de la conception, ne demande donc aucune couche d'adaptation entre deux langages ou entre deux processus.

Comparons ce choix à d'autres langages possibles.
Go est un langage compilé et typé statiquement : il offre de meilleures performances qu'un langage interprété et déplace une partie des erreurs vers la compilation, ce qui compte pour un programme qui s'exécute sans surveillance sur des machines installées chez un client, et qui devra être maintenu sur la durée.
Le C partage ces deux propriétés, mais il ne fournit ni gestion automatique de la mémoire ni primitives de concurrence légères, alors que ce sont exactement les besoins de ce daemon.
Ses avantages habituels, à savoir le contrôle fin de la mémoire et l'absence de garbage collector, ne se manifesteraient nulle part dans un programme qui passe l'essentiel de son temps à attendre des événements.
Rust apporte de son côté des garanties de sûreté supérieures à celles de Go, au prix d'une courbe d'apprentissage et d'un temps de développement nettement plus élevés, ce qui se conçoit mal dans un projet de douze semaines.

Le code suit la disposition habituelle des projets Go.
Le répertoire #src_dir("antsd/cmd/antsd", body: [`cmd/antsd`]) contient le point d'entrée du programme, qui reste volontairement simple : il charge la configuration, construit la journalisation, met en place l'interception des signaux d'arrêt du système, puis délègue tout le reste au gestionnaire de cluster.
Tout le code métier se trouve sous `internal/`.

Le découpage en paquets suit les responsabilités identifiées lors de la conception, illustrées par le diagramme de composants de la #ref(<fig_conception_antsd-components>).
Le paquet #pkg("cluster") contient le gestionnaire, c'est-à-dire l'orchestrateur central qui possède l'état du cycle de vie et enchaîne les étapes des différents scénarios.
Le paquet #pkg("serfnode") encapsule l'instance Serf embarquée, et s'appuie sur le paquet #pkg("discovery") qui réalise la découverte des machines par #acr("mDNS").
Le paquet #pkg("k3s") regroupe tout ce qui concerne l'installation, la surveillance et le contrôle de l'instance locale de K3s.
Le paquet #pkg("admin") expose l'interface HTTP de supervision et de contrôle.
Le paquet #pkg("config") rassemble la lecture et la validation des paramètres de démarrage.
Un paquet supplémentaire, #pkg("logbridge"), joue un rôle plus discret : les bibliothèques de HashiCorp écrivent leurs messages dans un format qui leur est propre (avec l'ancien paquet `log` de la bibliothèque standard de Go@go_log_2026), et ce paquet les traduit vers le nôtre afin que tout le programme produise un journal homogène.
antsd utilise le paquet `log/slog`, le standard moderne de journalisation de Go, introduit dans la version 1.21 du langage@amsterdam_structured_2023.

Un dernier paquet, nommé #pkg("node"), mérite une attention particulière, car il occupe une place à part dans cette organisation.
Il ne contient aucune logique technique, mais plutôt divers types et fonctions utiles : les états du cycle de vie, le rôle K3s d'une machine, la détermination du rôle et la structure de l'état persisté.
Surtout, il ne dépend d'aucun autre paquet du projet.
Cette contrainte est utile : puisque tous les autres paquets ont besoin de manipuler des états et des rôles, confier ces types à un paquet sans dépendance évite les cycles d'importation.
Les dépendances du programme sont ainsi toutes dirigées vers ce noyau commun, et jamais l'inverse.

La #ref(<fig_implementation_packages>) rend cette organisation visible.
Il faut la distinguer du diagramme de composants présenté lors de la conception : ce dernier montre quels éléments dialoguent entre eux pendant l'exécution, alors que celui-ci montre quel paquet en importe un autre à la compilation.
Les trois paquets en vert n'ont aucune flèche sortante, ce qui traduit exactement la règle énoncée ci-dessus.

#hepia.sourced_figure(
  caption: [Dépendances entre les paquets de antsd],
  label: <fig_implementation_packages>,
  image("../assets/diagrams/implementation_packages.svg", width: 90%),
)

La configuration suit la même volonté de simplicité.
Tous les paramètres de fonctionnement, comme le port de Serf, le port du serveur HTTP ou le chemin du fichier d'état, sont lus au démarrage, à partir des arguments de la ligne de commande ou, à défaut, des variables d'environnement.
La configuration est validée au début, afin de détecter immédiatement toute incohérence et éviter que le programme démarre dans un état incorrect.
Le nom de la machine n'est pas choisi par l'utilisateur mais dérivé du matériel, pour les raisons exposées dans la #ref(<part-implementation-node-naming>, supplement: [partie]).

== Encapsulation de Serf et découverte des nœuds <section-implementation-serf>

La conception a établi que antsd embarque Serf sous forme de bibliothèque plutôt que de piloter un processus séparé (voir #ref(<section-conception-antsd>, supplement: [section])).
Voyons maintenant ce que cette décision implique concrètement.

Le paquet `serfnode` construit lui-même la configuration de l'agent, l'instancie dans le processus de antsd, et récupère les événements directement sur un canal Go.
Il n'y a donc ni port #acr("RPC"), ni sérialisation, ni supervision d'un processus tiers : le cycle de vie de l'agent est celui du daemon.
Ce paquet joue également le rôle de frontière entre la bibliothèque de HashiCorp et le reste de notre code.
Plutôt que de laisser circuler les types de Serf dans tout le programme, il les traduit vers un type d'événement qui nous est propre, et n'expose qu'une poignée d'opérations : rejoindre des pairs, quitter le cluster, publier l'état local via les tags, diffuser un événement, et fournir une vue des membres connus.

Cette frontière offre un grand avantage : si l'API de Serf évolue, ou si nous devions un jour changer de mécanisme de communication, un seul paquet serait à reprendre.

`serfnode` traduit également les événements.
Serf livre les changements d'appartenance sous forme de lots, qui peuvent concerner plusieurs machines à la fois.
Nous les dépilons pour émettre un événement par machine concernée, ce qui simplifie le traitement en aval.

Le mécanisme le plus important de ce paquet est la publication de l'état local.
À chaque changement d'étape du cycle de vie, antsd met à jour un tag Serf nommé `state`, qui contient simplement le nom de l'état courant, par exemple `fb_bootstrap_waiting` ou `stable_server`.
Serf se charge ensuite de propager cette modification à tout le cluster par son mécanisme de diffusion.
La conséquence est essentielle pour la suite du chapitre : chaque machine connaît en permanence l'état de toutes les autres sans jamais avoir à les interroger.

La diffusion d'événements demande elle aussi une attention particulière.
Serf sait regrouper plusieurs événements portant le même nom lorsqu'ils surviennent dans un intervalle rapproché, afin de réduire le trafic.
Ce comportement est désactivé pour les événements de notre protocole de démarrage, car leur fusion pourrait supprimer une information nécessaire à la progression du cluster.

Reste la question de l'amorçage de la découverte.
Serf sait entretenir un groupe de machines, mais il faut lui fournir l'adresse d'au moins un pair pour qu'il puisse le rejoindre, ce qui est incompatible avec notre objectif de zéro-configuration.
Le projet de semestre@arcidiacono_systeme_2026 avait choisi de s'appuyer sur la fonctionnalité native de découverte #acr("mDNS") de Serf qui répond précisément à ce besoin.
Des tests avaient montré que cette fonctionnalité fonctionnait correctement, mais ils n'ont porté que sur la version standalone de Serf, et non sur la version embarquée dans un programme.
Or, la fonctionnalité #acr("mDNS") de Serf n'existe en réalité pas dans le cœur de la bibliothèque, mais dans le paquet annexe qui est propre à l'outil en ligne de commande.
Cet imprévu nous oblige donc à réimplémenter la découverte #acr("mDNS") dans notre code.

C'est le rôle du paquet #pkg("discovery") (#src("antsd/internal/discovery/mdns.go")), qui s'appuie sur la bibliothèque mDNS de HashiCorp@hashicorp_hashicorpmdns_2026.
Puisque Serf utilise lui-même cette bibliothèque dans sa version standalone en ligne de commande, nous nous inspirons de son code pour construire notre propre implémentation.
Chaque machine se tient prête à annoncer sa présence sur le réseau local (elle écoute les requêtes entrantes) et interroge périodiquement ce dernier (elle envoie des requêtes), toutes les cinq secondes, à la recherche d'autres machines qui écoutent.
Lorsqu'une nouvelle adresse apparaît, elle est transmise à `serfnode`, qui demande alors à Serf de rejoindre ce pair.
Les adresses déjà vues sont mémorisées afin de ne pas répéter inutilement l'opération à chaque interrogation.

Le nom du service annoncé joue un rôle de cloisonnement.
Il est construit à partir du nom du cluster, sous la forme `_antsd-<cluster>._tcp`.
Deux ensembles de machines ANTS branchés sur le même réseau local ne se découvrent donc pas mutuellement s'ils portent des noms de cluster différents, et ne risquent pas de fusionner par accident.
Le mécanisme est en place, mais il faut préciser que ce nom est pour l'instant figé : il n'est pas prévu d'exploiter plusieurs clusters simultanément sur un même réseau local, et toutes les machines utilisent donc le même nom.

Ce cloisonnement a par ailleurs révélé qu'il ne fonctionnait que dans un seul sens.
Donner un nom précis à nos requêtes garantit que les autres n'y répondent pas, mais ne garantit rien sur ce que nous recevons : la bibliothèque mDNS analyse tous les enregistrements qui circulent sur le groupe de diffusion, et pas seulement les réponses à notre propre requête.
De ce fait, si d'autres machines ou d'autres services annoncent leur présence via #acr("mDNS") ou d'autres protocoles de découverte similaires, nous les recevons également.
Cela s'est notamment produit à cause d'un service interne à K3s, et antsd tentait donc de rejoindre des adresses qui n'avaient rien à voir avec notre système.
Le filtrage doit donc être appliqué correctement pour éviter ce problème.

== Gestionnaire de cluster <section-implementation-manager>

Le gestionnaire de cluster est le composant le plus délicat du daemon, car c'est lui qui possède l'état du cycle de vie décrit par la machine d'états de la #ref(<fig_conception_antsd-state-machine>).

Le problème posé par ce composant tient au nombre de sources qui cherchent à le solliciter simultanément.
Les événements de Serf arrivent à tout moment, au rythme des changements survenant sur le réseau.
L'utilisateur peut déclencher une action depuis l'interface HTTP, laquelle est servie par des goroutines distinctes.
Les minuteurs du protocole de démarrage expirent indépendamment du reste.
Enfin, les installations de K3s, qui durent plusieurs minutes, doivent signaler leur issue une fois terminées.
Toutes ces sources aboutissent au même endroit : l'état courant de la machine.

Or chacune d'elles s'exécute dans sa propre goroutine, alors que cet état est une donnée unique, partagée entre toutes.
Deux problèmes en découlent, et il faut les distinguer pour comprendre la suite.
Le premier est technique : deux goroutines qui accèdent à la même variable sans précaution forment un accès concurrent, dont le résultat est indéterminé.
Le second est plus gênant, car il subsiste même si chaque lecture et chaque écriture est correcte. Une transition ne se résume pas à une écriture : elle lit l'état, décide d'une action, publie un tag Serf et démarre parfois une installation de K3s. Si une autre source intervient au milieu de cette séquence, la machine peut prendre deux décisions contradictoires, par exemple lancer deux installations à la fois ou installer un rôle qui ne correspond plus à l'état qu'elle vient de publier.
C'est donc la séquence entière, et pas seulement la variable, qu'il faut protéger.

La réponse la plus immédiate consisterait à poser un verrou sur cet état.
Nous l'avons écartée.
Un verrou est simple tant qu'il ne protège qu'une lecture ou une écriture isolée, mais il faudrait ici le conserver pendant toute la séquence décrite ci-dessus, ce qui multiplie les risques d'interblocage et rend le raisonnement local difficile dès que les transitions s'enchaînent.

Nous avons retenu l'approche inverse : plutôt que de partager l'état entre plusieurs goroutines, une seule goroutine le possède, et les autres lui envoient des messages.
Le gestionnaire exécute donc une unique boucle qui attend, en parallèle, l'arrêt du programme, l'arrivée d'un événement Serf, ou une commande interne.
Le #ref(<code_implementation_runloop>) présente cette boucle, qui tient en quelques lignes.

#hepia.sourced_figure(
  caption: [Boucle principale du gestionnaire de cluster (#src("antsd/internal/cluster/manager.go"))],
  label: <code_implementation_runloop>,
  supplement: [Code],
  ```go
  for {
      select {
      case <-ctx.Done():
          m.bootstrap.stopTimer()
          m.joining.stopTimer()
          m.rescale.stopTimer()
          // no serf.Leave() here : it's reserved for the decommission
          // workflow
          m.logger.Info("manager shutting down")
          return nil
      case e := <-events:
          m.handleSerfEvent(e)
      case c := <-m.commands:
          m.handleCommand(c)
      }
  }
  ```
)

Le commentaire de la branche d'arrêt mérite une explication, car l'absence d'une ligne y est plus importante que les lignes présentes.
Serf offre une opération qui annonce le départ d'une machine au reste du groupe, et l'appeler ici serait le réflexe naturel.
Nous ne le faisons jamais, car ce départ annoncé porte un sens précis dans notre système : il signifie que la machine est retirée définitivement (décommissionnement).
Un redémarrage, un plantage et une coupure de courant doivent au contraire se ressembler, puisque dans les trois cas la machine peut revenir.
En restant silencieux à l'arrêt, le daemon garantit que ces trois situations produisent exactement le même signal chez les autres, ce qui permet ensuite à un seul chemin de reprise de les couvrir toutes.
L'opération reste disponible dans notre encapsulation de Serf, mais elle est réservée au décommissionnement.

La conséquence de cette organisation est que l'état du cycle de vie n'a besoin d'aucun verrou : il n'est lu et modifié que par la goroutine de la boucle, et toutes les modifications sont donc naturellement réalisées en série.
Une seule exception subsiste, pour le serveur HTTP qui doit pouvoir afficher l'état courant à n'importe quel instant sans perturber la boucle.
Une copie de cet état est donc entretenue dans une variable atomique, mise à jour à chaque transition, que les lecteurs concurrents consultent librement.
Cette copie est en lecture seule : elle sert à l'affichage uniquement.

Les actions de l'utilisateur posent un problème supplémentaire, car elles attendent une réponse.
Lorsque l'utilisateur demande la création d'un cluster, l'interface HTTP doit savoir si l'action a été acceptée afin de répondre correctement.
Chaque action est donc transformée en une commande accompagnée d'un canal de réponse : la goroutine HTTP dépose la commande puis attend le verdict de la boucle, qui lui renvoie soit une réussite, soit une erreur. Une erreur typique est que l'action est impossible dans l'état courant (il n'est par exemple pas possible de demander la création d'un cluster s'il existe déjà).
Cette erreur particulière est ensuite traduite par le serveur en un code HTTP 409, comme nous le verrons dans la #ref(<section-implementation-admin>, supplement: [section]).

Les installations de K3s posent elles aussi un problème particulier.
Les exécuter directement dans la boucle la bloquerait pendant plusieurs minutes, durant lesquelles le daemon cesserait de réagir aux événements du cluster et de répondre aux requêtes de supervision.
Chaque installation est donc lancée dans une goroutine dédiée, qui ne touche jamais à l'état. Lorsqu'elle se termine, elle envoie simplement une commande de réussite ou d'échec dans la boucle.
C'est cette dernière qui décide alors de la suite, en fonction de l'état dans lequel elle se trouve.
Le daemon reste ainsi pleinement réactif pendant toute la durée de l'installation, ce qui est indispensable puisque les autres machines continuent, elles, de progresser dans le protocole.

Un dernier point mérite d'être signalé, car il a des conséquences bien au-delà de ce composant.
Le gestionnaire ne s'adresse pas directement à notre encapsulation de Serf, mais à une interface qui n'en décrit qu'une petite partie, reproduite dans le #ref(<code_implementation_serfapi>).
Elle ne contient rien de plus que ce dont la boucle a besoin : démarrer l'agent, publier l'état local, diffuser un événement, retirer une machine du groupe et obtenir une photo des membres.

#hepia.sourced_figure(
  caption: [Interface par laquelle le gestionnaire accède à Serf (#src("antsd/internal/cluster/manager.go"))],
  label: <code_implementation_serfapi>,
  supplement: [Code],
  ```go
  // serfAPI is the subset of serfnode.Node used by the Manager.
  // Allows tests to inject a fake Serf implementation.
  type serfAPI interface {
      Start(ctx context.Context) (<-chan serfnode.Event, error)
      Leave() error
      RemoveFailedNode(name string) error
      SetState(state node.State) error
      SendUserEvent(name string, payload []byte) error
      LocalIP() string
      Snapshot() admin.Snapshot
  }
  ```
)

Cette interface a une première utilité de conception, puisqu'elle énonce en un seul endroit tout ce que la logique du cycle de vie est capable de demander au réseau.
Sa conséquence la plus utile est cependant ailleurs : nous pouvons créer une implémentation qui la satisfait mais qui ne communique pas réellement sur le réseau.
Associée à l'interface d'installation de K3s présentée plus loin, elle permet d'exécuter le gestionnaire entier sans Serf, sans K3s et sans matériel, ce qui est très utile pour réaliser des tests.
Nous y revenons dans le #ref(<chapter-tests>).

== Bootstrapping <section-implementation-bootstrap>

Le déroulement du premier démarrage a été décrit lors de la conception, et illustré par le diagramme de séquence de la #ref(<fig_conception_bootstrap-sequence>).
Son implémentation se trouve dans le fichier #src("antsd/internal/cluster/bootstrap.go"), et nous nous concentrons ici sur les trois difficultés qu'elle pose.

=== Détermination du rôle

La première difficulté consiste à attribuer un rôle à chaque machine.
Une approche courante dans les systèmes distribués consisterait à mettre en place une élection, c'est-à-dire un échange de messages au terme duquel les participants s'accordent sur un vainqueur.
Nous avons choisi une solution plus simple, rendue possible par une propriété du système : grâce aux tags Serf, toutes les machines observent la même liste de membres.

Chaque machine trie donc la liste des membres actifs par ordre alphabétique de leur nom, et en déduit son propre rang.
Le rang détermine ensuite le rôle.
Puisque l'entrée du calcul est identique partout et que le calcul lui-même est déterministe, toutes les machines aboutissent au même résultat sans échanger le moindre message à ce sujet.
Le #ref(<code_implementation_role>) montre ces deux fonctions, dont la simplicité illustre bien l'intérêt de l'approche.

#hepia.sourced_figure(
  caption: [Détermination du rang et du rôle d'une machine (#src("antsd/internal/node/role.go"))],
  label: <code_implementation_role>,
  supplement: [Code],
  ```go
  // Rank returns the position of self among names, ordered lexicographically.
  // Every node computes the same ranking from the same
  // member list, which is what makes the role election leaderless.
  // rank 0 is N0, the node that initializes the cluster.
  func Rank(names []string, self string) (int, error) {
      sorted := slices.Clone(names)
      slices.Sort(sorted)

      rank := slices.Index(sorted, self)
      if rank < 0 {
          return 0, fmt.Errorf(
              "node %q not found in member list %v", self, sorted)
      }
      return rank, nil
  }

  // RoleForRank returns the K3s role of the node with the given rank in a
  // cluster of total nodes.
  func RoleForRank(rank, total int) Role {
      if rank < DesiredServerCount(total) {
          return RoleServer
      }
      return RoleAgent
  }
  ```
)

Cette économie a une contrepartie qu'il faut assumer : le calcul n'est juste que si toutes les machines observent la même liste au même moment.
C'est précisément la raison d'être de la période d'attente introduite lors de la conception.
Elle ne sert pas à négocier, mais à laisser à la vue des membres le temps de converger avant que la décision ne soit prise.

Le nombre de serveurs visé est calculé par la fonction `DesiredServerCount`, qui renvoie le plus grand nombre impair inférieur ou égal à la population, plafonné à sept.
Une population d'une ou deux machines appelle donc un seul serveur, trois ou quatre en appellent trois, cinq ou six en appellent cinq, et au-delà de sept la cible ne bouge plus.
Trois est le plancher de la haute disponibilité, c'est-à-dire le plus petit nombre de membres permettant à la base de données interne de K3s de conserver un quorum malgré la perte d'une machine. Sept en est le plafond recommandé, au-delà duquel le coût de la performance (à cause de la réplication des données) dépasse le gain de résilience@etcd_etcd_2024.
La justification de la parité, ainsi que le mécanisme qui maintient cette cible au cours de la vie du cluster, sont présentés dans la #ref(<section-implementation-rescaling>, supplement: [section]).

=== Tolérance aux doublons et au désordre

La deuxième difficulté est de s'assurer que le protocole tolère que l'ordre d'arrivée des messages soit différent de l'ordre dans lequel ils ont été émis, et que certains messages soient reçus plusieurs fois.

Le protocole s'appuie sur deux événements applicatifs seulement, résumés dans le #ref(<table_implementation_events>).
Leur nom réel est préfixé par `antsd:`, afin qu'ils ne puissent pas être confondus.

#hepia.sourced_figure(
  caption: [Les deux événements Serf du protocole de premier démarrage],
  label: <table_implementation_events>,
  table(
    columns: (auto, auto, 1fr),
    align: left,
    [*Événement*], [*Contenu*], [*Effet à la réception*],
    [`bootstrap-requested`],
    [aucun],
    [Toute machine en premier démarrage passe en attente et arme son minuteur.],
    [`bootstrap-start`],
    [aucun],
    [Chaque machine calcule son rang et son rôle, puis agit en conséquence.],
  ),
)

Aucun des deux ne transporte de donnée, et le protocole en comptait un troisième au départ.
Ce dernier annonçait l'adresse du premier serveur dès que celui-ci devenait disponible.
Il a été supprimé après qu'un test a révélé le défaut de conception : une machine qui échouait pendant son installation, puis redémarrait, attendait ensuite cette adresse indéfiniment, car l'annonce avait été diffusée pendant son absence.

Le problème n'était pas l'événement lui-même, mais ce qu'on lui faisait porter.
Un événement Serf signale un changement à l'instant où il se produit, et n'est jamais rejoué : une machine qui n'écoutait pas à ce moment précis reste aveugle pour toujours.
Un tag, à l'inverse, décrit une situation qui dure, et Serf le retransmet à toute machine qui arrive ou revient, y compris lors de la synchronisation complète entre deux membres.
Or l'adresse à laquelle rejoindre le cluster est typiquement une information durable, pas un instant.
Elle est donc lue aujourd'hui dans la liste des membres, en cherchant la machine vivante dont le tag annonce l'état `stable_server`, et les événements ne servent plus qu'à donner le tempo.
La règle que nous en retenons dépasse ce cas particulier : un état durable se publie dans un tag. 
Une information instantanée se publie dans un événement.

Notre réponse au problème du désordre tient en un principe appliqué systématiquement : chaque fonction de traitement commence par vérifier que l'état courant autorise l'action demandée, et ignore l'événement dans le cas contraire.
Ce principe rend les traitements idempotents, c'est-à-dire sans effet supplémentaire lorsqu'ils sont répétés.

Deux situations concrètes illustrent son utilité.
La première est celle des doublons.
À la fin de la période d'attente, chaque machine dont le minuteur expire diffuse le signal de départ. Comme les minuteurs sont armés presque simultanément, plusieurs machines peuvent parfaitement le diffuser en même temps.
Les récepteurs traitent le premier signal, changent d'état, puis ignorent les suivants qui ne correspondent plus à leur état : aucune coordination supplémentaire n'est nécessaire pour éviter le double traitement.

La seconde situation est celle de l'inversion.
Une machine peut voir un serveur devenir disponible dans les tags avant même d'avoir reçu le signal de départ, tout simplement parce que les deux informations se propagent de proche en proche par le gossip de Serf, et n'empruntent donc pas nécessairement le même chemin.
C'est très peu probable compte tenu de la durée d'installation, mais cela reste possible.
Pour absorber ce cas, la décision de lancer l'installation n'est attachée à aucun événement particulier.
Elle est prise par deux fonctions, une pour les serveurs et une pour les agents, qui sont appelées aussi bien à la réception du signal de départ qu'à chaque changement observé dans la liste des membres.
Chacune relit cette liste et vérifie ses propres conditions : le rôle attribué, la présence d'un serveur joignable et, pour un agent, le fait d'observer le nombre de serveurs que sa cohorte attend.
Une machine qui doit devenir serveur attend en plus son tour, car les serveurs rejoignent le cluster en série et non en parallèle : la base de données interne de K3s (etcd) n'accepte qu'un ajout de membre à la fois.
L'installation démarre dès que ces conditions sont réunies, quel que soit l'ordre dans lequel les informations sont arrivées.

=== Traitement des échecs

La troisième difficulté concerne les échecs, en particulier ceux de l'installation de K3s.
Le choix retenu est de placer la machine dans un état terminal, à partir duquel elle ne progresse plus, plutôt que de relancer automatiquement l'opération.

Ce choix est motivé par des besoins de sûreté.
Une machine qui réessaie indéfiniment produit un cluster partiellement formé dont l'état oscille, ce qui est bien plus difficile à diagnostiquer qu'une machine clairement arrêtée sur une erreur.
Comme l'état est publié dans les tags Serf, la panne devient visible depuis n'importe quelle autre machine du cluster, et depuis l'interface de supervision.
Nous privilégions donc un échec net et observable à une tentative de récupération automatique dont la logique resterait à concevoir.
La reprise consiste alors à remettre la machine à zéro, ce qui n'est pas sans conséquence pour le cluster : nous y revenons dans la #ref(<part-implementation-forget-me>, supplement: [partie]).

== Rejoindre un cluster existant et redimensionnement <section-implementation-rescaling>

Le protocole de la section précédente construit un cluster.
Il reste à le maintenir : une machine ajoutée doit s'y intégrer, et une machine définitivement perdue doit en sortir.

Ces deux besoins sont traités par deux mécanismes distincts, implémentés dans #src("antsd/internal/cluster/joining.go") et #src("antsd/internal/cluster/rescale.go"), et la frontière entre les deux doit être explicite.
Le chemin de #emph[joining] concerne une machine vierge qui démarre à côté d'un cluster déjà en service : elle s'installe en agent, sans jamais toucher aux machines existantes.
Le #emph[redimensionnement] concerne à l'inverse les machines déjà installées, dont il change le rôle.

=== Protection contre la création d'un second cluster

Le risque principal du premier démarrage n'est pas qu'une machine échoue, mais qu'un lot de machines vierges branchées à côté d'un cluster sain se mette à en construire un deuxième.
Les deux clusters coexisteraient alors sur le même réseau, chacun ignorant l'autre, ce qui créerait une incohérence.

Le critère de ce choix est la présence, dans les tags Serf, d'une machine vivante annonçant l'état `stable_server`.
C'est le seul signal qui prouve qu'un cluster est non seulement présent, mais joignable, alors qu'un cluster dont tous les serveurs redémarrent ne l'est pas.
Le refus de créer un cluster à côté d'un cluster en cours de reprise est traité séparément, par une sécurité propre au bouton de création.

Une machine qui a déjà commencé un bootstrap avec ses semblables est elle aussi détournée vers le joining dès qu'elle aperçoit un serveur extérieur à sa cohorte.
Sans ce détournement, la machine de rang zéro refuserait correctement de continuer grâce à sa sécurité dédiée, mais toutes les autres atteindraient leur étape d'installation en voyant le serveur étranger dans les tags. Cependant, elles le rejoindraient avec un rôle calculé sur leur propre cohorte au lieu de celui qu'elles auraient dû obtenir si elles avaient pu observer l'ensemble du cluster.
Cinq machines vierges branchées à côté d'un cluster sain lui auraient ainsi ajouté cinq serveurs, peu importe le besoin de la population.

Ce détournement n'est pas la seule protection contre la création d'un second cluster, et les trois qui répondent à ce risque posent des questions différentes qu'il ne faut pas confondre.
Celle qui vient d'être décrite demande s'il existe un serveur joignable, c'est-à-dire un cluster capable d'accueillir la machine tout de suite.
La deuxième s'oppose à l'utilisateur qui demande la création d'un cluster alors qu'une machine du réseau appartient déjà à un cluster, y compris lorsque celle-ci est en panne ou en cours de redémarrage.
Un cluster qui se relève n'est en effet pas joignable, mais il existe bel et bien, et créer un second cluster à côté de lui serait une erreur.
La troisième est purement locale et se déclenche juste avant l'installation : une machine dépourvue de fichier d'état mais sur laquelle K3s est déjà installé refuse d'installer par-dessus.
Ce cas se produit après un premier démarrage qui a échoué tardivement, une fois K3s en place mais avant que l'état ne soit écrit.
Ces trois refus se recouvrent en partie, mais aucun ne remplace les deux autres.

Reste à décider du rôle que prend la machine qui rejoint.
Dans la première version du protocole, la machine déterminait ce rôle elle-même : elle comparait le nombre de serveurs déjà engagés à la cible correspondant à la population observée, et s'installait en serveur tant qu'un emplacement restait libre.
Un essai sur les machines physiques a montré que c'est précisément la décision qu'une machine qui arrive ne peut pas prendre.
Une machine démarrée #emph[après] une panne n'apprend jamais l'existence du membre qui ne répond pas.
C'est un choix d'implémentation de Serf : lorsque la bibliothèque de gossip voit un nœud annoncé comme mort, elle l'ignore si elle n'a jamais entendu parler de lui.
La machine comptait donc un serveur de moins qu'il n'y en a, revendiquait l'emplacement qu'elle croyait libre, et se heurtait au refus d'etcd, qui n'accepte aucun ajout de membre tant que l'un des siens est injoignable.
Cela pouvait créer un deadlock.

La conclusion retenue est que dimensionner le plan de contrôle appartient aux machines qui voient le cluster en entier, jamais à celle qui arrive : le joining installe désormais toujours un agent, que le redimensionnement promeut ensuite si la population le demande.

=== Nom d'une machine <part-implementation-node-naming>

Tous les mécanismes qui suivent désignent des machines par leur nom.
Il faut donc d'abord dire ce qu'est ce nom.

antsd dérive automatiquement son nom à partir des trois derniers octets de l'adresse MAC de la carte réseau de la machine, ce qui donne par exemple `ants-669eae` (#src("antsd/internal/config/nodename.go")).
Le nom peut toutefois être remplacé par l'utilisateur, mais il est conseillé de ne pas le modifier manuellement pour éviter les collisions.
Ce nom est transmis à K3s lors de l'installation, afin que le nœud Kubernetes porte exactement le même.

Kubernetes impose une forme précise, celle des labels définis par la RFC 1123@kubernetes_object_2026.
antsd s'assure que le nom est conforme à cette forme.

Autre point d'attention : l'adresse matérielle est lue sur une interface physique uniquement, reconnue à la présence d'un périphérique associé dans le système de fichiers du kernel (`/sys/class/net/`).
C'est obligatoire, car une fois K3s démarré, la machine possède aussi des interfaces relatives aux conteneurs et au fonctionnement de Kubernetes.
La machine pourrait donc alors changer d'identité à chaque redémarrage, ce qui causerait des incohérences.

=== Protocole de gestion des machines réinitialisées <part-implementation-forget-me>

Une machine dont le premier démarrage a échoué doit être remise à zéro avant d'être rebranchée, procédure sur laquelle nous revenons plus bas.

Dans le cas où la machine n'est pas remise à zéro, elle est simplement évincée par le coordinateur (présenté plus bas).
Cependant, après une réinitialisation et une remise en route, l'éviction ne se déclenche pas, puisqu'elle ne retire que les machines que Serf voit en panne, et que celle-ci est revenue et est bien vivante.

Pour les raisons exposées dans la #ref(<part-conception-bootstrap>, supplement: [partie]), une machine réinitialisée ne peut pas rejoindre le cluster comme si de rien n'était.

La correction consiste à confier ce nettoyage à la machine elle-même, au seul moment où c'est possible, c'est-à-dire à son retour et avant qu'elle n'installe quoi que ce soit (#src("antsd/internal/cluster/forget.go")).
La machine vérifie d'abord localement qu'aucun K3s n'est installé chez elle.
Elle diffuse ensuite une demande d'oubli portant son nom.
Le coordinateur cherche un nœud K3s avec ce nom et le supprime s'il existe, puis diffuse une confirmation.
La machine installe enfin son agent.

La #ref(<fig_implementation_forget-me>) résume cet échange.

#hepia.sourced_figure(
  caption: [Protocole d'oubli d'une machine réinitialisée],
  label: <fig_implementation_forget-me>,
  image("../assets/diagrams/implementation_forget-me.svg"),
)

Deux décisions de ce protocole sont intéressantes à commenter.

La première est que l'attente de la confirmation n'est bornée par aucune échéance, et la machine réémet sa demande toutes les 15 secondes (les diffusions de Serf sont best-effort, et le coordinateur peut être occupé par une autre opération).
Installer sans confirmation serait inutile : la machine échouerait à rejoindre le cluster, et demanderait à nouveau un reset, ce qui créerait un cycle sans fin.

La deuxième concerne une vérification chez le coordinateur.
Il n'honore une demande que si Serf voit la machine nommée vivante et en cours de premier démarrage.
Cela évite qu'une requête égarée supprime un nœud K3s fonctionnel.

=== Nombre de serveurs dynamique

Le redimensionnement repose entièrement sur la cible calculée par `DesiredServerCount`, définie plus haut.
Le maintien de la parité impaire y est indispensable.
Un groupe etcd de quatre membres exige un quorum de trois, exactement comme un groupe de cinq : il ne tolère donc pas plus de pannes, tout en doublant la surface sur laquelle une panne peut survenir.
Le cas d'une population de deux machines est le plus contre-intuitif : la cible y descend à un seul serveur, parce que deux machines sont mieux servies par un serveur que par une base à deux membres qui ne survit à aucune perte.

=== Rôle du coordinateur

Toutes les machines évaluent le cluster à chaque événement Serf, mais une seule agit : le `stable_server` vivant portant le plus petit nom.
Le procédé est le même que pour l'élection de rôle du bootstrap, et il en tire le même avantage : chaque machine dérive le même coordinateur de la même liste de membres, sans échange préalable ni élection.
Si ce coordinateur disparaît en cours de réparation, la machine suivante reprend le tour et refait le travail depuis une observation fraîche, ce qui suppose que chacune de ses étapes soit reproductible sans effet supplémentaire (idempotent).

Cette élection a une faiblesse qu'il faut traiter explicitement, car elle est calculée sur la vue Serf locale : une machine coupée du réseau se voit seule serveur vivant, s'élit donc elle-même, et lit toutes les autres comme durablement perdues.
Chaque tour de coordination commence pour cette raison par une lecture de la vue Kubernetes, qui sépare les deux côtés d'une coupure, puisque la base de données interne n'accepte aucune lecture sans quorum.
Une machine restée minoritaire n'obtient donc pas de réponse, abandonne son tour sans avoir rien tenté, et le retente plus tard.

Il n'existe volontairement aucun verrou propre au redimensionnement.
La sérialisation est assurée par le mécanisme qui existe déjà pour les ajouts de serveurs : chaque machine publie son état dans son tag Serf, et le coordinateur s'abstient tant qu'une autre annonce une opération modifiant la composition de la base de données interne de K3s.
Le redimensionnement, le bootstrap et la reprise après redémarrage se sérialisent donc les uns par rapport aux autres sans mécanisme supplémentaire, ce qui est exactement la propriété recherchée : cette base n'admet qu'un changement de membres à la fois.

Cette protection n'offre en revanche aucune garantie de vivacité, et c'est la contrainte qu'elle impose au reste du système : tout état qui la déclenche doit avoir une durée bornée.
Une machine qui s'y immobilise gèle en effet toutes les réparations du cluster, y compris celle qui la débloquerait.
Les installations et les conversions sont donc soumises à une échéance, qui couvre le script d'installation autant que l'attente de disponibilité qui le suit, pour la raison détaillée dans la #ref(<section-implementation-k3s>, supplement: [section]).

=== Protection best-effort <part-implementation-advisory>

Expliquer pourquoi cette sérialisation est une protection best-effort est important, car elle ressemble à un verrou sans en être un.
Lire les tags des autres machines puis modifier le sien ne constitue pas une opération atomique : le nouveau tag met du temps à parvenir aux autres, si bien que deux machines peuvent décider d'agir après s'être mutuellement lues inactives.

Ce qui rend un changement concurrent sûr n'est donc pas cette garde, mais la base de données etcd elle-même, dont les reconfigurations sont déjà protégées contre les opérations dangereuses@etcd_runtime_2025.
Trois propriétés se combinent ici, et ce sont elles qui portent réellement la sûreté du système.
La première est que le protocole de consensus n'applique qu'une reconfiguration à la fois, si bien que deux changements simultanés ne peuvent pas s'entremêler.
La deuxième est que K3s ajoute tout nouveau serveur comme membre non votant (#emph[learner]) avant de le promouvoir, si bien qu'un ajout ne dégrade jamais le quorum pendant la synchronisation du nouveau venu.
La troisième est un contrôle de reconfiguration stricte, qui refuse tout ajout ou tout retrait qui casserait le quorum actif.
Une opération impossible reçoit donc une erreur plutôt que de corrompre le cluster.
Le coordinateur abandonne, et retente plus tard, avec une observation plus récente.

La conséquence est que le pire cas d'une garde qui échoue reste borné.
Une opération refusée ou mise en attente par etcd finit par atteindre son échéance, ce qui place la machine concernée dans un état terminal, dont la reprise est une réinitialisation.
C'est un coût réel pour cette machine, mais le cluster, lui, n'est jamais corrompu.

La protection conserve dans ce cadre une utilité : elle évite le travail redondant et les opérations superflues.
Elle épargne par exemple une conversion inutile, et elle empêche le cluster d'empiler des demandes qu'etcd refuserait de toute façon.

=== Éviction des machines mortes

Le coordinateur commence par évincer toute machine que Serf signale en panne depuis plus longtemps qu'une période de grâce, réglable au démarrage du daemon par le paramètre `-rescale-eviction-grace`.
Cette durée doit être longue : l'éviction est irréversible pour la machine concernée, et un simple redémarrage ne doit jamais en déclencher une.
Elle est en revanche volontairement raccourcie pendant les essais, faute de quoi vérifier le mécanisme demanderait d'attendre des heures devant le banc d'essai.

L'éviction supprime l'objet `Node` du cluster Kubernetes, ce qui retire du même coup le membre etcd correspondant lorsque la machine était un serveur, puis efface la machine de la liste des membres Serf.
Cet effacement, plutôt qu'un départ annoncé, préserve une distinction qui compte pour la suite : le statut `left` reste réservé au décommissionnement volontaire, et ne se confond pas avec une machine évincée.

L'ordre entre l'éviction et la promotion est la partie porteuse du mécanisme.
Promouvoir un agent sans avoir retiré le membre mort ferait passer le groupe de trois membres, dont un mort et un quorum de deux, à un groupe de quatre membres, dont un mort et un quorum de trois : trois membres vivants pour un quorum de trois, c'est-à-dire toujours aucune panne supplémentaire tolérée.
La promotion n'aurait donc rien apporté.
Retirer le membre mort en premier restaure au contraire un groupe de trois qui tolère à nouveau une perte.

Il faut nommer le régime transitoire que cela impose : entre le retrait et la promotion, le cluster fonctionne avec deux membres, un quorum de deux et aucune tolérance.
C'est précisément la régression que la parité impaire cherche à éviter, ici au sein d'un chemin de reprise, et elle est inévitable puisque les changements de membres se font en série.

=== Conversion locale

Dans le cas où le cluster doit être redimensionné après l'éviction des machines mortes, le coordinateur choisit une machine à convertir.
Une fois le cluster nettoyé, le coordinateur draine la machine désignée, supprime son objet `Node`, puis diffuse un événement Serf avec un payload contenant le nom de la cible, son nouveau rôle et l'adresse à laquelle rejoindre le cluster.
La cible est l'agent au plus petit nom pour une promotion, et le serveur au plus #emph[grand] nom pour une rétrogradation : cette asymétrie garantit que le coordinateur ne se désigne jamais lui-même, ce qui reviendrait à drainer le dernier serveur du cluster.

La machine désignée désinstalle alors son K3s et le réinstalle avec l'autre rôle.

=== Portée d'un échec

Le traitement des échecs distingue deux situations.

Un tour de coordination qui échoue ne touche pas au K3s du coordinateur, qui continue de servir : un appel à `kubectl` qui échoue une fois est donc simplement retenté, et le tour repasse au serveur suivant si la machine reste incapable d'agir.
Une conversion qui échoue est en revanche définitive pour la machine concernée : son installation a été démolie et celle qui devait la remplacer n'est pas montée, si bien que plus rien de local n'est fiable.
Elle bascule alors dans un état terminal, et le reste du cluster conserve la topologie qu'il avait.

Cette conversion est enfin le premier endroit où le rôle d'une machine est réécrit après son premier démarrage.
L'état persisté n'est mis à jour qu'une fois le nouveau K3s déclaré disponible, ce qui laisse subsister une fenêtre étroite : une coupure de courant survenant entre la fin de la réinstallation et l'écriture du fichier laisserait le disque et l'état persisté en désaccord, et la machine refuserait de repartir au redémarrage suivant.
Cette fenêtre est connue et acceptée, faute d'une source de vérité extérieure à la machine elle-même.

== Reprise après un redémarrage <section-implementation-rejoin>

Les deux sections précédentes décrivent des machines vierges, qui n'ont encore rien installé.
Une machine qui redémarre est un cas différent, et c'est le plus fréquent une fois le cluster en service : une coupure de courant, un redémarrage volontaire ou un plantage se ressemblent tous vus de l'extérieur.

Ce qui distingue ce cas des autres est le fichier d'état laissé sur le disque, décrit dans la #ref(<section-implementation-persistence>, supplement: [section]).
Le chemin correspondant est implémenté dans #src("antsd/internal/cluster/rejoin.go").
Sa présence signifie que le premier démarrage a déjà eu lieu, donc que K3s est installé.
Le daemon prend alors un chemin très court, et surtout il ne réinstalle rien.
K3s est un service systemd : il redémarre tout seul avec la machine et se reconnecte à ses pairs sans que antsd ait à intervenir.
Le rôle de antsd se limite donc à vérifier que la situation est cohérente, puis à attendre.

La vérification porte sur deux points, et le premier est le plus important : le rôle inscrit dans le fichier d'état doit correspondre au rôle réellement installé sur la machine.
Ce dernier n'est pas déduit ni supposé, il est lu sur le disque, car le script d'installation de K3s laisse derrière lui une unité systemd différente selon le rôle.
Si les deux ne concordent pas, le daemon s'arrête dans un état d'échec.
Le second point est le nom : celui indiqué dans le fichier d'état doit être celui que la machine porte actuellement.
Un décalage est traité de la même manière qu'un rôle incohérent, car l'ancien nom pourrait toujours exister dans le cluster K3s.
Une fois la vérification passée, il attend simplement que le K3s local se déclare disponible, avec la sonde qui correspond à son rôle, puis retrouve son état stable.

Le point le plus important de ce chemin est ce qu'il s'interdit de faire.
Une machine ne retombe jamais sur le protocole de premier démarrage, quelle que soit la raison.
Ni un fichier d'état illisible, ni un rôle qui ne correspond pas, ni un événement de bootstrap reçu du réseau ne peuvent l'y ramener.
La raison est simple : rejouer le premier démarrage relancerait le script d'installation de K3s par-dessus des données déjà existantes.
Le daemon préfère donc s'arrêter plutôt que de risquer de détruire les données du cluster, et cette règle est vérifiée par un test dédié.

Cette attente est bornée par une échéance de dix minutes, alors qu'elle était illimitée dans une première version.
Ce revirement ne s'explique pas par la machine qui redémarre, à qui l'attente ne coûte rien, mais par le reste du cluster : cette étape fait partie de celles qui empêchent tout changement de membres, puisqu'un serveur qui revient se raccroche au quorum.
Une machine qui n'en sortirait jamais gèlerait donc les réparations de l'ensemble du cluster.
Le cas se produit réellement lorsqu'une machine évincée est remise sous tension : elle revient dans le groupe Serf comme si de rien n'était, alors que son K3s ne peut plus rejoindre une base dont il a été retiré.
Passé le délai, elle bascule dans l'état d'échec, qui ne bloque plus les réparations.
Sa seule issue reste une remise à zéro, mais elle n'immobilise plus le cluster en attendant.

== Pilotage de l'installation de K3s <section-implementation-k3s>

Une fois son rôle connu, la machine doit installer et démarrer K3s.
Cette opération est soumise à une contrainte forte, héritée de l'objectif de fonctionnement autonome : elle doit se dérouler sans le moindre accès à Internet, à partir des seuls fichiers déjà présents dans l'image système, dont la construction est décrite dans la #ref(<section-implementation-ants-os>, supplement: [section]).

Le paquet `k3s` définit pour cela une interface qui décrit les opérations attendues, présentée dans le #ref(<code_implementation_installer>), dont les commentaires ont été raccourcis pour la lecture.
On y retrouve les trois cas de figure du protocole de démarrage, la conversion utilisée par le redimensionnement, les deux attentes de disponibilité et la lecture du rôle déjà installé.

#hepia.sourced_figure(
  caption: [Interface d'installation de K3s (#src("antsd/internal/k3s/installer.go"))],
  label: <code_implementation_installer>,
  supplement: [Code],
  ```go
  // Installer abstracts the local K3s installation so the cluster workflows
  // can be tested without real K3s.
  type Installer interface {
      // InstallServerInit installs K3s as the first server of a new cluster
      // (cluster-init). Should be run by N0 only.
      InstallServerInit(ctx context.Context) error

      // InstallServerJoin installs K3s as an additional server joining the
      // existing cluster through the server at serverIP.
      InstallServerJoin(ctx context.Context, serverIP string) error

      // InstallAgent installs K3s as an agent joining the cluster through
      // the server at serverIP.
      InstallAgent(ctx context.Context, serverIP string) error

      // Convert replaces the local K3s installation with the other role.
      Convert(ctx context.Context, to node.Role, serverIP string) error

      // WaitServerReady blocks until the local K3s server reports ready.
      WaitServerReady(ctx context.Context) error

      // WaitAgentReady blocks until the cluster reports this agent node as
      // Ready.
      //
      // Readiness is role-specific because an agent hosts no Kubernetes API
      // server: the server probe cannot be reused here.
      WaitAgentReady(ctx context.Context) error

      // InstalledRole reports the K3s role already installed on this node.
      InstalledRole(ctx context.Context) (node.Role, error)
  }
  ```
)

Une seconde interface, volontairement séparée de celle-ci, rassemble les opérations qui portent sur le cluster entier plutôt que sur la machine locale : drainer une machine, supprimer son objet `Node` et vérifier si le cluster la connaît encore.
Ce sont les opérations que le redimensionnement utilise pour évincer une machine perdue ou préparer un changement de rôle.
Elles ne peuvent s'exécuter que depuis un serveur, car un agent ne dispose d'aucun fichier d'identification administrateur.
C'est exactement la même asymétrie que celle des deux attentes de disponibilité, et c'est pour cette raison que les deux interfaces restent distinctes au lieu d'en former une seule (en plus du fait que les responsabilités sont différentes).

L'implémentation réelle (#src("antsd/internal/k3s/exec.go")) s'appuie sur le script d'installation officiel de K3s, déposé dans l'image lors de sa construction.
Elle l'exécute en lui transmettant, par variables d'environnement, le mode souhaité, le jeton partagé qui autorise la machine à rejoindre le cluster et, pour les machines qui rejoignent, l'adresse du premier serveur.
Une variable en particulier mérite d'être signalée : elle indique au script de ne jamais tenter de télécharger quoi que ce soit, puisque le binaire de K3s est déjà présent sur le disque.
Sans elle, la première machine du cluster chercherait à joindre Internet et échouerait sur un site isolé.

La fin du script ne signifie pas que la machine est prête, car K3s doit encore démarrer ses composants et importer ses images de conteneurs.
L'attente de disponibilité interroge donc le système à intervalle régulier, jusqu'à obtenir une réponse positive.
La question posée dépend du rôle, et les deux formulations ne sont pas interchangeables.
Un serveur interroge le point de contrôle de santé de l'API Kubernetes qu'il héberge lui-même.
Un agent n'héberge aucune API et ne possède aucun accès administrateur : il demande donc au plan de contrôle si le cluster le considère comme disponible, en passant par les identifiants réduits que K3s lui a écrits.

L'existence même de cette interface se justifie par une seconde implémentation, qui se contente d'inscrire dans les journaux l'opération demandée avant d'attendre un court instant et de signaler une réussite.
Cette implémentation simulée rend possible le développement du daemon sur un poste de travail dépourvu de K3s, et surtout elle permet de dérouler l'intégralité du protocole de démarrage dans des tests automatisés, ce que nous détaillons dans le #ref(<chapter-tests>).
Le choix entre les deux implémentations s'opère par un simple paramètre de configuration.

Rappelons enfin un point établi lors de la conception : antsd ne s'adresse jamais au K3s d'une autre machine.
Chaque daemon ne pilote que l'instance qui s'exécute à côté de lui, et toute coordination entre machines passe exclusivement par Serf.
Cette règle évite de reconstruire dans antsd des mécanismes que K3s assure déjà.

== Persistance de l'état local <section-implementation-persistence>

Le daemon conserve sur disque une petite quantité d'informations, écrites au moment où le premier démarrage s'achève (#src("antsd/internal/node/persist.go")).

Il n'y a pas besoin de persister l'état complet du cluster, car il est déjà maintenu par K3s et par Serf.
K3s maintient son propre état dans sa base de données interne.
Dupliquer ces informations dans antsd créerait une troisième source de vérité, qu'il faudrait maintenir cohérente avec les deux autres à chaque événement.
Nous nous limitons donc au strict nécessaire : le nom de la machine, le rôle qui lui a été attribué, la date de fin du premier démarrage, la date du dernier changement de rôle et un compteur de démarrages destiné au diagnostic.

Le rôle premier de ce fichier n'est d'ailleurs pas de mémoriser son contenu, mais d'exister.
Sa présence au lancement du daemon distingue un tout premier démarrage d'un redémarrage ultérieur, et oriente donc la machine vers le protocole de bootstrap ou vers la reprise décrite dans la #ref(<section-implementation-rejoin>, supplement: [section]).

Les données ne sont pas écrites directement à leur emplacement définitif : elles sont d'abord déposées dans un fichier temporaire situé dans le même répertoire, lequel est ensuite renommé vers le nom attendu.
Le renommage étant une opération atomique du système de fichiers, le fichier d'état est à tout instant soit absent, soit complet, mais jamais tronqué.
Cette précaution se justifie par le contexte matériel : nos machines peuvent perdre leur alimentation à tout moment.
Une coupure survenant pendant une écriture directe laisserait un fichier illisible, et une machine incapable de déterminer si elle a déjà démarré une première fois.

La relecture, elle, demande de séparer deux cas que l'on pourrait croire équivalents.
Un fichier absent et un fichier illisible ne racontent pas la même histoire : le premier décrit une machine qui n'a jamais démarré, le second une machine qui a perdu la mémoire de ce qu'elle est.
Le code les distingue donc explicitement, et seul le premier cas mène au protocole de premier démarrage.
Traiter le second de la même façon reviendrait à réinstaller K3s sur une machine qui en héberge déjà un, pour la raison exposée dans la #ref(<section-implementation-rejoin>, supplement: [section]).

== Interface de contrôle et de supervision <section-implementation-admin>

L'interface HTTP décrite lors de la conception remplit deux fonctions : exposer l'état de la machine et du cluster, et recevoir les quelques actions dont l'utilisateur dispose.

Elle est construite uniquement avec la bibliothèque standard de Go, sans bibliothèque ou framework web qui serait bien trop lourd pour nos besoins.
Ces derniers se limitent ici à six points d'accès, dont un tableau de bord.
Trois d'entre eux se contentent de lire l'état du système, les trois autres correspondent aux actions de création d'un cluster.
La bibliothèque standard fournit déjà le serveur, le rendu HTML et la sérialisation JSON.

L'organisation interne de ce paquet illustre un principe de conception qui structure tout le projet.
Le paquet `admin` a besoin de lire l'état du cluster et de déclencher des actions, deux capacités qu'implémentent respectivement `serfnode` et `cluster`.
La solution naïve consisterait à importer ces deux paquets.
Nous faisons l'inverse : `admin` déclare lui-même les deux interfaces dont il a besoin, comme le montre le #ref(<code_implementation_admin-interfaces>), et ce sont les autres paquets qui viennent les satisfaire.

#hepia.sourced_figure(
  caption: [Interfaces déclarées par le paquet d'administration (#src("antsd/internal/admin/server.go"))],
  label: <code_implementation_admin-interfaces>,
  supplement: [Code],
  ```go
  // Source provides a read-only view of the local cluster state.
  type Source interface {
      // Snapshot returns the current cluster state as observed by this node.
      Snapshot() Snapshot
  }

  // Controller exposes the user actions of the node lifecycle. It is
  // implemented by the cluster manager; like Source, the interface lives here
  // so admin never depends on the cluster package.
  type Controller interface {
      State() string
      RequestBootstrap() error
      ConfirmBootstrap() error
      CancelBootstrap() error
  }
  ```
)

Le serveur HTTP ne dépend ainsi d'aucun des deux paquets qu'il utilise.
Outre l'absence de cycle d'importation, cette inversion présente l'avantage de rendre explicite, en un seul endroit, l'ensemble des opérations que l'interface d'administration peut déclencher sur le système.
Toute nouvelle action devra passer par ces interfaces, ce qui constitue une barrière utile contre la tentation d'exposer directement des mécanismes internes.

Le traitement des actions impossibles mérite également d'être mentionné.
Lorsque l'utilisateur demande une action qui n'a pas de sens dans l'état courant, par exemple confirmer une création de cluster alors qu'aucune n'a été demandée, le gestionnaire renvoie une erreur particulière que le serveur reconnaît et traduit en un code HTTP 409, qui signale un conflit avec l'état de la ressource.
L'appelant distingue ainsi une action refusée d'une véritable panne du daemon.

Le tableau de bord, rendu à partir d'un template HTML (#src("antsd/internal/admin/templates/dashboard.tmpl")), présente l'état local, la liste des membres connus avec leur état respectif, et les boutons correspondant aux actions disponibles.

#hepia.sourced_figure(
  caption: [Capture d'écran du tableau de bord d'administration],
  label: <fig_implementation_dashboard>,
  image("../assets/images/implementation_screenshot_dashboard.png"),
)

La capture d'écran de la #ref(<fig_implementation_dashboard>) présente le tableau de bord d'une machine nommée `ants06`, dans un cluster de trois serveurs et un agent.
Nous voyons que la machine est un agent, qu'elle a démarré il y a huit minutes, et que l'état du cluster est stable.
Nous y voyons par ailleurs que le cluster n'arrive plus à joindre la machine `ants05`, qui est donc en panne.

Il faut rappeler que ces points d'accès ont, dans leur forme actuelle, un statut provisoire.
Ils tiennent lieu des boutons de l'écran physique prévu sur les machines ANTS, décrit lors de la conception, et permettent de dérouler le protocole de démarrage pendant le développement sans disposer de cet écran.

== Le décommissionnement <section-implementation-decommission>

Un point d'accès de plus était prévu et ne figure pas dans cette liste : le décommissionnement, c'est-à-dire le retrait volontaire et définitif d'une machine que son propriétaire souhaite sortir du cluster, pour la remplacer ou s'en séparer.
Cette action ne fait pas partie de l'énoncé du travail, qui demande la formation du cluster, la haute disponibilité et la récupération après défaillance.
Elle a été ajoutée au périmètre en cours de projet, à la demande d'ANTS A.I. Systems, et nous avons décidé de ne pas la réaliser.
La priorité est allée aux autres points essentiels (mécanismes de réparation, comportement lorsque plusieurs machines agissent en même temps, tests, etc.), qui touchent tous à la disponibilité du produit là où le décommissionnement touche à son confort d'exploitation.
Cet arbitrage a été discuté lors des rendez-vous de suivi du projet.

Ce choix est tenable parce que le résultat visé reste atteignable sans commande dédiée.
L'utilisateur qui veut retirer une machine la débranche, et le cluster fait le reste : la machine est déclarée perdue, évincée à l'expiration de la période de grâce, puis le redimensionnement recalcule le nombre de serveurs comme après n'importe quelle panne.
Le chemin est simplement moins direct, à savoir qu'il impose l'attente de la période de grâce, et exige la réinitialisation de celle-ci avant qu'elle puisse être réutilisée ailleurs.
L'absence de cette commande coûte donc du temps et une manipulation, mais elle ne prive le système d'aucune capacité.

La conception de ce retrait a néanmoins été menée, et elle ne demanderait presque aucun mécanisme nouveau.
Le point d'accès `POST /decommission` serait traduit en commande comme les trois actions existantes, et ferait passer la machine dans un état de retrait.
Celle-ci ne peut cependant pas se supprimer elle-même du cluster, puisqu'un agent ne dispose d'aucun droit d'administration sur K3s, pour la raison exposée dans la #ref(<section-implementation-k3s>, supplement: [section]).
Elle diffuserait donc une demande au groupe et attendrait la réponse, exactement comme le fait déjà une machine réinitialisée avec le protocole d'oubli présenté dans la #ref(<part-implementation-forget-me>, supplement: [partie]).
Le coordinateur du redimensionnement traiterait cette demande avec le code qui exécute aujourd'hui les évictions, c'est-à-dire vider la machine de ses charges puis supprimer son objet dans K3s.
La machine ne désinstallerait son K3s, n'effacerait son fichier d'état et n'annoncerait son départ à Serf qu'une fois cette confirmation reçue, ce qui la laisserait vierge et directement réutilisable.

Deux points empêchent toutefois de réduire ce travail à un simple assemblage, et tous deux sont déjà identifiés.
Le premier est l'endroit où le départ d'un serveur doit être sérialisé avec les autres changements de composition de la base de données interne.
La machine qui attend sa confirmation ne doit surtout pas rejoindre pour cela la liste des états décrite dans la #ref(<part-implementation-advisory>, supplement: [partie]), et ce pour la raison qui en tient déjà le protocole d'oubli à l'écart : une attente sans échéance placée dans cette liste gèlerait les réparations du cluster entier.
La sérialisation revient donc au coordinateur, qui est déjà seul à agir et dont le tour vérifie le quorum avant toute chose, puisque c'est lui qui exécute réellement le retrait.
Le second point tient au signal que les autres machines doivent observer.
Un départ annoncé produit le statut Serf `left`, réservé depuis le début à cet usage précis, mais l'éviction pousse elle aussi un événement de départ juste avant d'effacer la machine du groupe.
La logique de décommissionnement devra donc s'appuyer sur le statut lu dans la liste des membres, et jamais sur l'arrivée de cet événement, faute de quoi une machine évincée serait traitée comme une machine partie volontairement.

== Construction de l'image ants-os <section-implementation-ants-os>

Le chapitre précédent a exposé les raisons qui nous conduisent à préparer une image système complète et préconfigurée (voir #ref(<section-conception-ants-os>, supplement: [section])).
Nous décrivons ici la manière dont cette image est effectivement construite.

La construction est décrite dans un unique fichier (#src("ants-os/ants-os.pkr.hcl")) et repose sur Packer@hashicorp_hashicorppacker_2026 ainsi que sur une extension spécialisée dans les images ARM@kaczanowski_mkaczanowskipacker-plugin-builder-arm_2026.
Cette extension part de l'image officielle de Raspberry Pi OS Lite pour l'architecture ARM64@ltd_raspberry_2026, l'agrandit à quatre gigaoctets afin de laisser la place aux ajouts, puis monte ses partitions pour y déposer des fichiers et y exécuter des commandes.

Une difficulté se pose immédiatement : le poste de développement est une machine x86, alors que les commandes de configuration doivent s'exécuter dans un environnement ARM64.
Elle est résolue par l'émulation, l'extension exécutant les commandes à l'intérieur de l'arborescence montée par l'intermédiaire de QEMU@qemu_qemu_2026.
Tout se déroule dans un conteneur Docker privilégié, ce qui évite d'installer sur le poste de développement les outils de manipulation d'images disque et les droits d'administration qu'ils exigent.

Le contenu déposé dans l'image se répartit en deux catégories.
La première regroupe les fichiers téléchargés au préalable par un script dédié (#src("ants-os/scripts/download-assets.sh")), exécuté une seule fois : le binaire de K3s pour ARM64, son script d'installation officiel et l'archive de ses images de conteneurs.
Cette archive est l'élément déterminant pour le fonctionnement hors ligne, car elle contient toutes les images que K3s doit démarrer.
Placée à l'emplacement où K3s les recherche, elle lui évite tout accès à un registre distant.
La seconde catégorie rassemble nos propres fichiers de configuration : la définition de l'interface réseau, la configuration du serveur SSH et la clé publique autorisée.

Le binaire de K3s et cette archive ne sont toutefois pas simplement copiés à l'endroit où le logiciel les attend.
Ils sont d'abord rangés dans un répertoire qui n'appartient qu'à nous, puis reliés à leur emplacement d'exécution par un lien physique.
Ce détour répond à une contrainte de la conversion de rôle décrite dans la #ref(<section-implementation-rescaling>, supplement: [section]) : la désinstallation de K3s efface le binaire ainsi que l'intégralité du répertoire de données, archive d'images comprise@k3s_uninstalling_2026, c'est-à-dire exactement ce dont une réinstallation hors ligne a besoin.
Toute conversion aurait donc laissé derrière elle une machine incapable de réinstaller quoi que ce soit.
Le répertoire réservé sert donc de coffre (#emph("vault") dans le code), auquel K3s ne touche jamais, et que antsd remet en place avant chaque installation, qu'il s'agisse d'un bootstrap, d'un joining ou d'une conversion.
Nous utilisons un lien pour accélérer l'opération et économiser de l'espace disque, car les fichiers sont volumineux.
Si le lien est impossible, antsd recopie les fichiers à la place.

Un script de configuration (#src("ants-os/scripts/provision.sh")) est ensuite exécuté à l'intérieur de l'image pour l'adapter à notre usage.
Il fixe le nom de la machine et la disposition du clavier, puis crée l'utilisateur `ants` avec accès par clé et élévation de privilèges sans mot de passe.
Cette création répond à un besoin précis : elle permet de désactiver l'assistant de première configuration de Raspberry Pi OS, qui exigerait sinon une intervention au clavier lors du tout premier démarrage, ce qui serait contraire à notre objectif.
Le script installe ensuite quelques outils de diagnostic, ajuste les permissions des binaires, et bascule la gestion du réseau de NetworkManager vers `systemd-networkd`, plus adapté à une machine sans interface graphique et configurée par fichiers.

Une étape de ce script mérite une mention particulière, car son omission provoque une panne difficile à interpréter.
Le noyau de Raspberry Pi OS ne présente pas par défaut tous les groupes de contrôle dont Kubernetes a besoin pour limiter les ressources des conteneurs.
Le script ajoute donc les options nécessaires à la ligne de commande du noyau, sans quoi K3s refuse simplement de démarrer, avec un message qui ne désigne pas immédiatement la cause.

La #ref(<fig_implementation_ants-os-build>) résume l'enchaînement complet, depuis le téléchargement des ressources jusqu'à la carte mémoire prête à être insérée dans une machine.

#hepia.sourced_figure(
  caption: [Chaîne de construction de l'image ants-os],
  label: <fig_implementation_ants-os-build>,
  image("../assets/diagrams/implementation_ants-os-build.svg", width: 90%),
)

L'image obtenue est écrite sur une carte mémoire, puis insérée dans une machine.
Toutes les machines reçoivent rigoureusement la même image. Leur seule différence à l'issue du premier démarrage provient du rôle que antsd leur attribue.

== Synthèse

Ce chapitre a montré comment l'architecture définie lors de la conception prend forme dans le code.
Trois idées structurent l'ensemble de la réalisation.
La première est la publication de l'état de chaque machine dans un tag Serf, qui rend le cluster observable sans aucune requête entre machines et sert de fondation à tous les mécanismes décrits.
La deuxième est la sérialisation de toutes les décisions dans une boucle unique, qui supprime le besoin de verrous sur l'état du cycle de vie et rend le comportement du daemon lisible.
La troisième est le recours à des interfaces aux frontières du système, envers Serf comme envers K3s, qui permet de dérouler l'intégralité du protocole dans des tests automatisés.

Les quatre situations qu'une machine peut rencontrer sont couvertes : créer un cluster, rejoindre un cluster déjà en service, revenir après un redémarrage, et changer de rôle quand la population évolue.
Le cluster ne se contente donc pas de se former, il se maintient.
Seul le décommissionnement d'une machine reste à implémenter.


Il reste maintenant à vérifier que tout ceci se comporte comme prévu sur du matériel réel, et surtout à observer ce qui se passe lorsque des machines tombent.
C'est l'objet du chapitre suivant, qui présente les moyens de vérification mis en place, le banc de six machines physiques, et les scénarios de panne qui y sont rejoués.
