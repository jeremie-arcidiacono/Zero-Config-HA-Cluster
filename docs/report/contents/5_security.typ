#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

= Sécurisation <chapter-security>

L'énoncé de ce travail demande une investigation des méthodes et des recommandations permettant de sécuriser l'architecture du cluster.
Ce chapitre y répond.

La sécurité a été volontairement minimisée de la conception comme de l'implémentation présentées jusqu'ici : le système ne comporte ni authentification, ni chiffrement de sa couche basse, ni notion d'identité vérifiable d'une machine.
Ce choix est assumé, puisqu'il a permis de traiter d'abord le cœur du sujet, à savoir la formation autonome du cluster et sa capacité à se réparer.
Ce chapitre existe donc pour que cette absence apparaisse comme une décision documentée plutôt que comme un oubli, et pour donner à ANTS A.I. Systems une base de travail en vue d'un déploiement réel.
Ce qui est décrit n'est implémenté.

Nous posons d'abord le cadre de l'analyse, c'est-à-dire ce que l'on protège et contre qui, puis nous dressons l'état actuel du système sous la forme d'une liste de constats.
Nous proposons ensuite les mesures qui y répondent, avant de terminer par la question de conception qui commande la valeur de presque toutes les autres : celle de l'enrôlement d'une machine neuve.


== Cadre de l'analyse <section-security-scope>

=== Contexte et biens à protéger

Trois propriétés du déploiement visé, déjà décrites au #ref(<chapter-context>), comptent davantage que les autres pour la sécurité.
Le cluster est hors ligne, à l'exception de quelques services de base comme la synchronisation de l'heure, ce qui retire beaucoup de surfaces classiques : ni dépendance téléchargée à l'exécution, ni exposition directe depuis Internet, ni contrôle distant.
L'installation est faite par le client, sans compétence particulière, ce qui est toute la raison d'être du projet : il n'y a personne pour saisir une configuration, gérer des certificats ou retenir un mot de passe, et toute mesure qui suppose un administrateur compétent sur place est hors sujet.
Le matériel est enfin livré par ANTS, donc l'image système et son contenu sont maîtrisés à la fabrication.
C'est le seul moment où un secret peut être injecté sans aucun effort de la part du client, ce qui rendra tentante, plus loin, l'option d'une clé déposée dans l'image.

Viennent ensuite les biens à protéger, par ordre décroissant de gravité en cas de compromission.
Le token d'enrôlement de K3s vaut le contrôle total du cluster, puisque celui qui le possède peut rejoindre le cluster comme serveur, donc devenir membre du plan de contrôle, lire l'intégralité de la base de données et ordonnancer n'importe quelle charge sur n'importe quelle machine.
Le contenu de cette base de données vient ensuite, car il contient les secrets Kubernetes de toutes les charges déployées, donc en pratique les identifiants applicatifs.
Un accès en lecture à ces données est d'autant plus grave, car il permettrait là aussi de prendre le contrôle du cluster.
La disponibilité du plan de contrôle mérite d'être listée comme un bien à part entière, car plusieurs faiblesses identifiées plus loin ne permettent de voler quoi que ce soit, mais permettent de casser la haute disponibilité, ce qui revient à annuler l'intérêt du produit.
L'intégrité du binaire antsd enfin, car un daemon modifié est un point de contrôle idéal : il tourne en root sur chaque machine, installe K3s et pilote le cycle de vie du cluster.

Une précision de méthode s'impose pour terminer.
Le banc d'essai est constitué de six Raspberry Pi 5 démarrant sur carte mémoire, alors que les machines qu'ANTS produira auront un matériel maîtrisé et un stockage interne, si bien qu'une partie des observations faites sur le banc ne survivra pas au passage en production.
Chaque constat et chaque mesure porte pour cette raison une portée : `PoC`, `production`, ou `les deux` lorsqu'il tient au logiciel ou au protocole, indépendamment du matériel.

=== Profils d'attaquant et hypothèses <part-security-assumptions>

Le système actuel repose sur une frontière de confiance unique et implicite : tout ce qui se trouve sur le réseau local est de confiance, et un membre du gossip Serf est cru sur parole, aussi bien sur son identité que sur les ordres qu'il émet.
Cette hypotèse est bien sûr trop optimise.

Le premier profil d'attaquant est celui auquel on pense spontanément : une machine branchée sur le même réseau que le cluster, ce qui suffit à exploiter la majorité des constats qui suivent.
Le scénario réaliste n'est d'ailleurs pas forcément celui d'un attaquant déterminé, car chez un client, un poste compromis ou une personne de passage dans le local suffisent.

Le second concèrne les applications finales.
Les charges d'intelligence artificielle générative d'ANTS tournent dans le cluster, donc sur ce réseau : un conteneur en `hostNetwork`, un pod aux privilèges trop larges ou une évasion de conteneur classique se retrouvent directement en position d'attaquant adjacent, sur chacune des machines, sans avoir eu besoin d'un accès physique.
Ce profil est d'autant plus pertinent que le produit d'ANTS consiste à exécuter du code de modèles et différentes applications qui pourrait avoir de nombreuses dépendances (par exemple écosystème Python, dont la chaîne d'approvisionnement est un vecteur connu).
Il a néanmoins une limite : tant qu'ANTS décide seule de ce qui est déployé et que les charges restent peu nombreuses, le risque reste contenu, et il changerait de nature si le catalogue d'applications venait à s'ouvrir à des tiers.

Un troisième profil, celui de l'accès physique à une machine, doit être qualifié plus soigneusement, car il est trivial sur le banc d'essai alors qu'en production la difficulté dépendra entièrement de choix (matériels entre autres) qui ne sont pas les nôtres.
Nous retenons donc le principe, à savoir qu'un secret présent en clair sur un stockage non chiffré est un secret exposé, sans tirer de conclusion sur la difficulté réelle de l'exploiter.

Deux hypothèses encadrent le reste du chapitre.
La première est que le client n'est pas hostile et que la chaîne de fabrication d'ANTS est saine, ce qui a une conséquence : le modèle protège contre un tiers, pas contre le propriétaire du matériel.
La seconde est nettement plus structurante : une machine réellement admise dans le cluster est considérée comme de confiance.
Elle détient de toute façon les secrets et les droits nécessaires pour nuire, et sa compromission est un scénario perdu quoi qu'il arrive, si bien que nous ne cherchons pas à nous défendre contre un membre légitime devenu hostile.
Cette hypothèse simplifie considérablement l'analyse, mais sa contrepartie est que la sécurité de la couche basse repose alors sur le contrôle d'accès à l'entrée, sans aucune défense en profondeur derrière.
Il n'y a pas de second rempart si la porte cède, ce qui donne à la fermeture de cette porte une importance qu'elle n'aurait pas dans un modèle plus défensif.

Trois sujets sont enfin écartés explicitement : les attaques purement réseau (comme l'usurpation d'adresses MAC, l'épuisement de pool DHCP, etc.), qui ne visent pas la solution en particulier et relèvent de l'infrastructure du client, le logiciel d'administration final, qu'ANTS développe avec sa propre interface authentifiée, et la sécurité des charges applicatives elles-mêmes, qui ne nous intéresse que là où ces charges deviennent un attaquant contre l'infrastructure.

Enfin, le projet a une propriété centrale à défendre : on branche une machine et elle rejoint le cluster, sans intervention.
Or presque toute mesure de sécurité consiste à exiger une preuve avant d'accepter un nouveau venu, ce qui est exactement l'inverse.
Chaque mesure est donc jugée sur cet axe, selon qu'elle préserve le zéro-configuration, qu'elle le dégrade en faisant apparaître une action ponctuelle, ou qu'elle le casse.

== État actuel du système <section-security-current>

Il faut d'abord dire ce que le système possède déjà.
K3s met en place tout seul une infrastructure de certificats et chiffre ses communications internes : l'API, les échanges des agents avec le plan de contrôle, ainsi que le trafic client et pair de sa base de données.
Cette partie, la plus délicate à réaliser correctement, fonctionne sans que le projet ait eu à s'en occuper, y compris dans un déploiement hors ligne.
La couche haute de l'architecture est donc raisonnablement protégée par défaut, et l'essentiel de ce qui suit concerne la couche basse, celle que nous avons écrite.

Les constats sont regroupés sous huit identifiants, de `SEC-01` à `SEC-08`.
Ce découpage est volontairement court, car beaucoup de symptômes différents remontent à la même cause.

=== Le cluster est ouvert à toute machine du réseau (`SEC-01`)

`SEC-01` est le constat central de la liste, et de loin le plus important.

Notre encapsulation de Serf ne fournit jamais de clé de chiffrement à la bibliothèque.
Le trafic de gossip circule donc en clair, et surtout aucun secret n'est exigé pour rejoindre le cluster : une machine branchée sur le réseau est acceptée dans le gossip, et à partir de là elle est traitée exactement comme une machine légitime, puisque ses annonces sont crues et ses événements exécutés.
C'est la faiblesse fondatrice, puisque Serf est la couche sur laquelle repose toute la logique du cluster : il n'existe aujourd'hui aucun contrôle d'accès entre un inconnu et le pilotage de ce cluster.

La #ref(<fig_security_trust-boundary>) résume cette asymétrie : la couche haute exige un secret de tout arrivant, la couche basse n'exige rien, et les deux profils d'attaquant décrits plus haut atteignent directement cette dernière.
Les flèches pleines représentent un accès obtenu sans aucun secret, les flèches pointillées un accès refusé

#hepia.sourced_figure(
  caption: [Frontière de confiance actuelle.],
  label: <fig_security_trust-boundary>,
  image("../assets/diagrams/security_trust-boundary.svg", width: 80%),
)

Le cas le plus grave est le détournement d'un ordre de redimensionnement.
Le coordinateur diffuse un événement qui désigne la machine à convertir, son nouveau rôle et l'adresse à laquelle rejoindre le cluster, comme décrit dans la #ref(<section-implementation-rescaling>, supplement: [section]).
La machine visée vérifie que l'ordre lui est bien destiné et que son état correspond au rôle attendu, mais ces gardes protègent contre un ordre incohérent, pas contre un ordre malveillant : les événements applicatifs de Serf ne transportent aucune identité d'émetteur.

Un attaquant présent dans le gossip diffuse donc un ordre visant un serveur du plan de contrôle, avec le rôle d'agent et une adresse qu'il contrôle.
La machine visée désinstalle son serveur K3s, puis réinstalle un agent pointant vers l'infrastructure de l'attaquant.
Répétée, l'opération démonte le plan de contrôle machine par machine, et chaque machine convertie exécute ensuite les charges que l'attaquant lui envoie.

Deux autres conséquences du même mécanisme méritent également une brève mention.
Les événements du protocole de premier démarrage sont forgeables de la même façon, ce qui permet de perturber la mise en service d'un lot de machines neuves, l'impact restant moindre puisque les gardes refusent d'agir sur une machine où K3s est déjà installé.
La disponibilité peut par ailleurs être attaquée directement, ce qui vise la propriété même que le produit doit garantir : le nombre de serveurs visé étant déterminé de la population observée, un attaquant qui ajoute et retire des membres à volonté fait osciller la cible entre un, trois, cinq et sept, et chaque conversion qui en résulte désinstalle puis réinstalle K3s sur une machine réelle.

Deux précisions expliquent pourquoi ce constat n'est pas décliné davantage.
La première tient à l'hypothèse de confiance posée plus haut, qui fait que fermer l'entrée referme du même coup toutes les conséquences ci-dessus.
C'est aussi la raison pour laquelle l'identité des machines ne fait pas l'objet d'un constat séparé : une machine annonce elle-même son nom, et toute la répartition des rôles repose sur l'ordre lexicographique de ces noms, si bien que se nommer `aaa` suffit à prendre la tête de tous les classements, élection du coordinateur comprise.
Sous l'hypothèse retenue, ce n'est pas une faiblesse en soi, mais une illustration de l'absence de défense en profondeur.
La seconde est que la découverte mDNS n'est pas la porte du cluster : n'importe qui peut annoncer le service que nous recherchons, mais un pair découvert et incapable d'entrer dans le gossip échoue simplement à rejoindre.
Le filtrage mis en place dans le paquet `discovery`, décrit dans la #ref(<section-implementation-serf>, supplement: [section]), relève de la robustesse et non de la sécurité, car il n'arrête aucun attaquant.

=== Secrets, stockage et image système

Le deuxième constat (`SEC-02`) est que les secrets n'ont pas de cycle de vie.
Le système repose sur un secret unique, fixé une fois pour toutes et jamais renouvelé : le token de K3s est un paramètre de démarrage du daemon, transmis tel quel à chaque installation, identique sur toutes les machines et pour toute la durée de vie du cluster.
Ce qui fait la gravité du constat n'est pas l'unicité mais la portée de ce secret.
K3s distingue le token serveur du token agent et prévoit que le second soit configuré séparément, faute de quoi le token agent est simplement le token serveur@k3s_token_2026.
Comme antsd ne configure que le premier, chaque agent du cluster détient un secret suffisant pour rejoindre le cluster en tant que serveur, c'est-à-dire pour entrer dans le plan de contrôle et lire toute la base de données : la machine la moins privilégiée du cluster stocke en permanence les identifiants les plus privilégiés.
L'absence de rotation est plus complexe à résoudre, car elle impose notamment de gérer le fait que chaque servers et agents doivent obtenir le nouveau token puis redémarrer pour être de nouveau opérationnel après une rotation.

Le troisième (`SEC-03`) est que le stockage d'une machine expose tout ce qu'il contient, faute de chiffrement du disque et de vérification de l'intégrité de ce qui démarre.
Trois éléments méritent d'être nommés.
Le token de K3s d'abord, dont on vient de voir qu'il vaut un enrôlement en tant que serveur : lire le disque de la machine la moins importante du cluster suffit donc à prendre le contrôle de l'ensemble.
Les sauvegardes de la base de données ensuite, qui contiennent l'intégralité de l'état du cluster, les certificats, les secrets Kubernetes des charges, etc. 
etcd ne chiffre jamais ces données sur le disque@etcd_transport_2026 et K3s sait au maximum chiffrer les secrets Kubernetes mais pas le reste des données.
Enfin, le fichier d'état local, dont la validation porte sur la forme et non sur l'authenticité, alors qu'il gouverne l'arbitrage entre un premier démarrage et un redémarrage décrit dans la #ref(<section-implementation-persistence>, supplement: [section]) : modifier le rôle qui y est enregistré revient à manipuler cet arbitrage.

Le quatrième (`SEC-04`) est que l'image est identique sur toute la flotte.
Une image fabriquée en chaîne reproduit par construction le même contenu sur chaque machine livrée, donc tout identifiant qu'elle contient est un identifiant de flotte : le premier exemplaire extrait compromet toutes les machines déployées, chez tous les clients.
L'image crée aujourd'hui un utilisateur avec un mot de passe par défaut et un `sudo` sans mot de passe, et la console locale est active, si bien qu'un clavier et un écran donnent un accès root.
Cela reste de portée `PoC` puisqu'ANTS gérera les comptes de ses machines autrement.
Une clé publique SSH commune est également intégrée à l'image, en l'occurrence celle qui pilote le banc d'essai par Ansible, et cet exemple-là ne disparaît pas en production : savoir quelle clé, si elle existe, est autorisée sur une machine livrée à un client est une véritable question de conception.
La configuration SSH mérite en revanche d'être signalée comme correcte et renforcée, avec une authentification par clé uniquement, sans connexion root ni mot de passe.

Le cinquième (`SEC-05`) n'est pas une faiblesse mais une contrainte à établir : antsd ne peut pas être confiné au-delà d'un certain point.
Le daemon écrit dans `/usr/local/bin` lorsqu'il restaure les fichiers de K3s depuis le coffre de l'image (voir #highlight("TODO: mettre ref vers chapitre")) ainsi que dans `/var/lib/rancher`, le script d'installation qu'il lance écrit son unit dans `/etc/systemd/system`, et il pilote systemd.
Il restera donc un processus privilégié disposant d'un accès en écriture à des emplacements sensibles.

=== K3s, charges hébergées et interface d'administration

Le sixième constat (`SEC-06`) est que K3s est livré sans les options de durcissement que la distribution laisse à l'exploitant : le chiffrement des secrets au repos, la journalisation d'audit de l'API (pour laquelle K3s ne crée par défaut ni le répertoire ni la politique), le contrôle d'admission Pod Security et le durcissement des paramètres du kernel.
K3s publie un guide dédié qui documente la configuration correspondant aux exigences du référentiel #acr("CIS")@k3s_cis_2026, le sujet est donc documenté et la seule question est celle du dosage.

Le septième (`SEC-07`) est que l'infrastructure reste joignable depuis les charges hébergées, rien n'isolant les charges applicatives des services d'infrastructure des machines : un pod en `hostNetwork`, ou une évasion de conteneur, atteint Serf et l'interface d'administration sur chacune d'elles.
Ce constat compte parce qu'il change la difficulté d'exploitation de tout le reste des constats, puisque sans lui il faut un accès direct au réseau du client, alors qu'avec lui une charge applicative compromise suffit.

Le dernier (`SEC-08`) est que l'interface de contrôle et de supervision est ouverte, puisqu'elle écoute en HTTP clair, sans authentification, et expose la topologie complète du cluster ainsi que des actions de contrôle.
Il est posé puis sorti du périmètre pour la raison donnée plus haut, ces points d'accès étant assumés comme des remplaçants temporaires des boutons physiques et de l'interface web final que ANTS crééra, comme expliqué dans la #ref(<section-conception-antsd>, supplement: [section]).

Le #ref(<table_security_findings>) récapitule ces constats, leur portée et la mesure principale qui leur répond.
Il sert de fil conducteur à la section suivante, qui ne revient donc pas sur les problèmes eux-mêmes.

#hepia.sourced_figure(
  caption: [Registre des constats de sécurité],
  label: <table_security_findings>,
  table(
    columns: (auto, 1fr, auto, 1fr, auto),
    align: left,
    [*ID*], [*Constat*], [*Portée*], [*Réponse principale*], [*Zéro-config*],
    [`SEC-01`], [Le cluster est ouvert à toute machine du réseau], [Les deux], [Chiffrer et authentifier le gossip], [à trancher],
    [`SEC-02`], [Les secrets n'ont pas de cycle de vie], [Les deux], [Token agent distinct, rotation], [préserve],
    [`SEC-03`], [Le stockage expose tout ce qu'il contient], [Production], [Chiffrement des disques], [préserve],
    [`SEC-04`], [L'image est identique sur toute la flotte], [Les deux], [Aucun identifiant valable dans l'image], [dégrade],
    [`SEC-05`], [antsd a un plancher de privilèges élevé], [Les deux], [Unit systemd alignée sur ce plancher], [préserve],
    [`SEC-06`], [K3s est livré sans ses options de durcissement], [Les deux], [Guide de durcissement de K3s], [préserve],
    [`SEC-07`], [L'infrastructure est joignable depuis les charges], [Les deux], [Politique réseau, écoute restreinte], [préserve],
    [`SEC-08`], [L'interface d'administration est ouverte], [PoC], [Hors périmètre], [préserve],
  ),
)

== Mesures envisageables <section-security-measures>

Les mesures suivent l'ordre du tableau et commencent par celle qui commande toutes les autres.

=== Fermer l'entrée du cluster

C'est la mesure la plus importante du chapitre.
Sous l'hypothèse retenue, où une machine admise est de confiance, fermer l'entrée referme d'un seul coup le détournement d'ordre de conversion, la falsification des événements de premier démarrage et le déni de service par oscillation de la population.

Techniquement, elle est presque triviale.
Serf chiffre son trafic dès qu'une clé symétrique lui est fournie dans la configuration qu'il transmet à memberlist@hashicorp_hashicorpserf_2026, et un pair qui ne possède pas cette clé ne peut ni lire le gossip, ni se faire accepter.
Le cluster ouvert d'aujourd'hui devient alors un cluster fermé.

Deux éléments de la bibliothèque comptent pour la conception.
Le trousseau permet de détenir plusieurs clés à la fois, la première chiffrant les messages sortants et les suivantes ne servant qu'au déchiffrement, ce qui rend une rotation possible sans interruption et permet à une machine redémarrée de retrouver un cluster dont la clé a changé pendant son absence.
La rotation elle-même se pilote depuis le cluster, Serf diffusant la nouvelle clé par le gossip et suivant les réponses de chaque membre : il n'y a donc rien à inventer, seulement à décider qui la déclenche, et le coordinateur de redimensionnement est le candidat naturel puisqu'il est déjà désigné pour les décisions à l'échelle du cluster.

Toute la difficulté est ailleurs : d'où vient cette clé, et comment une machine neuve l'obtient.
C'est l'objet de la #ref(<section-security-enrollment>, supplement: [section]), et c'est ce qui détermine si la mesure préserve le zéro-configuration ou le dégrade.

Il faut en revanche résister à la tentation de sécuriser la découverte, car le protocole mDNS n'est pas conçu pour cela et une machine qui annonce un service qu'elle n'offre pas ne cause aucun dommage tant que l'étape suivante la refuse.
La découverte reste ouverte, le contrôle d'accès est au gossip.

Nous pensons qu'une mesure complémentaire mérite d'être discuté car nous l'avons étudié, bien qu'elle s'avère peu utile si l'on considère que l'entrée du cluster est sécurisé et que l'on ne souhaite pas de défense en profondeur.
Elle exploite une propriété que la conception possède déjà : le coordinateur légitime est déterministe, et chaque machine sait le calculer depuis sa propre vue du cluster.
Il suffit donc que les ordres du coordinateur nomment son émetteur pour que le récepteur confronte ce nom à sa propre observation et rejette un ordre venu d'ailleurs.
Cette vérification devient indispensable si la clé de gossip est commune à toute la flotte (dans le cas où un node légitime venait à être compromis), et reste utile dans le cas contraire au titre de la défense en profondeur.
Un point de vigilance pour sa réalisation : la vue du récepteur peut différer de celle de l'émetteur, notamment juste après une panne, et il faut donc s'assurer que ce décalage mène à un rejet temporaire suivi d'une nouvelle tentative plutôt qu'à un blocage.

Un dernier garde-fou relève autant de la robustesse que de la sécurité.
Il consiste à limiter le rythme des conversions, en refusant d'en enchaîner plus d'un certain nombre sur une fenêtre de temps plutôt que de suivre indéfiniment une population qui bouge, la date du dernier changement de rôle figurant déjà dans l'état persisté.

=== Cycle de vie des secrets

Séparer le token agent du token serveur est le meilleur rapport entre coût et bénéfice de toute la couche K3s.
Le mécanisme existe et se configure par un simple paramètre@k3s_token_2026, après quoi le secret stocké sur un agent ne permet plus que de rejoindre comme agent.
L'effet est important et réduit directement la portée de `SEC-03`.

Une difficulté propre au projet est toutefois à signaler.
Le redimensionnement convertit des agents en serveurs, et une machine promue a besoin du token serveur au moment de sa promotion : ce secret cesse donc d'être un paramètre préinstallé pour devenir une information à transmettre, ce qui s'articule naturellement avec l'ordre de conversion que le coordinateur diffuse déjà.

Rendre les secrets renouvelables est le second objectif.
K3s fournit une commande de rotation du token serveur, qui impose de redémarrer les serveurs et les agents avec le nouveau token@k3s_token_2026, ce qui est exactement le genre d'opération qu'antsd sait orchestrer puisqu'il pilote déjà l'installation et la conversion de chaque machine, et le trousseau de Serf joue le même rôle pour la clé de gossip.
Un point documenté par K3s ne doit pas être oublié : une sauvegarde antérieure à une rotation nécessite l'ancien token pour être restaurée, et une procédure de rotation doit donc traiter les sauvegardes en conséquence.

=== Stockage, image et privilèges du daemon

Le stockage est le domaine où ce travail peut le moins conclure, puisque la faisabilité dépend du matériel qu'ANTS retiendra.
Le principe à transmettre est en revanche clair : un secret en clair sur un stockage non protégé est un secret exposé, et le chiffrement du disque n'a de valeur que si la clé n'est pas rangée à côté.
La solution habituelle consiste à sceller cette clé dans un composant matériel dédié (#acr("TPM")), qui ne la libère que si la chaîne de démarrage est intacte, ce qui protège à la fois contre la lecture du stockage et contre le remplacement du binaire antsd.
Rien de tout cela n'est disponible par défaut sur un Raspberry Pi 5, ce qui explique que le banc d'essai ne puisse pas le démontrer.
Cette mesure reste transparente pour l'utilisateur, et elle a un effet de levier sur le reste du chapitre puisqu'elle donne un endroit sûr où déposer les secrets dont les autres mesures ont besoin.

Sur le principe, une image livrée ne doit par ailleurs contenir ni mot de passe par défaut, ni clé publique commune à la flotte.
Si un accès de maintenance est nécessaire, il doit être propre à chaque machine, ce qui suppose une étape de personnalisation en fabrication et fait donc apparaître une action qui n'existe pas aujourd'hui.
Cette étape de personnalisation peut être complexe à mettre en œuvre, car elle doit être compatible avec la production et la distribution en masse de machines identiques.
Ce n'est, à ce jour, pas souhaibale par ANTS.

Le durcissement de l'unit systemd de antsd doit enfin partir du plancher de privilèges établi par `SEC-05`.
Ce qui peut être durci sont des choses comme la protection des répertoires personnels, la restriction des familles d'adresses ou la limitation des capacités, tandis que la protection du système de fichiers doit être ramenée à un niveau compatible avec les écritures nécessaires.
La démarche compte ici plus que le résultat, car une directive de durcissement copiée sans vérification donne l'illusion de la sécurité tout en cassant la fonctionnalité.

Une autre possibilité consiste à scinder le daemon en deux parties, l'une privilégiée et l'autre non.
Le petit process privilégié expose une API réstrinte (par exemple avec un socket Unix) que la partie non privilégiée (qui contient toute la logique métier) appelle pour réaliser les opérations sensibles.
Des checks de sécurité sont effectués à l'entrée de cette API, tel que des allow-lists, vérification de checksum, etc.
De cette manière, si la partie non privilégiée est compromise (dépendances, bug, etc.), l'attaquant n'a pas un accès complet.
Naturellement, cette option demande des changements importants de conception et d'implémentation.

=== K3s et isolation des charges

Le guide de durcissement de K3s décrit la configuration correspondant aux exigences CIS, mais tout appliquer sur un cluster destiné à un usage particulier serait une erreur de dosage.
Trois éléments se distinguent par leur rapport entre valeur et risque dans notre contexte.

Le chiffrement des secrets au repos est le meilleur candidat@k3s_secrets_2026, puisqu'il traite le cas des sauvegardes décrit en `SEC-03` et s'active à l'installation d'un serveur, sans configuration supplémentaire, K3s générant lui-même la clé.

Le contrôle d'admission Pod Security@kubernetes_pod_2026 répond quant à lui à `SEC-07`, en empêchant les charges de réclamer les privilèges qui leur permettraient d'attaquer la machine hôte.
La prudence est ici de mise, car une politique trop stricte peut empêcher les charges légitimes d'ANTS de démarrer. 
ANTS aura besoin d'investiguer ces politiques pour trouver le juste équilibre, en fonction de la nature des applications qu'elle déploiera.

Le journal d'audit de l'API suppose enfin de créer le répertoire et la politique.
Sur un système sans administrateur et sans collecte centralisée de journaux, sa valeur réelle est discutable et il consomme de l'écriture disque, ce qui le rend optionnel, à l'inverse du durcissement des paramètres du noyau et des réglages de suites cryptographiques, qui sont peu coûteux et sans effet visible.

L'isolation des charges se joue pour sa part à deux niveaux qui se complètent.
Au niveau de Kubernetes, une politique réseau refusant aux pods l'accès aux ports d'infrastructure des machines, combinée au contrôle d'admission ci-dessus qui interdit `hostNetwork` aux charges ordinaires.
Au niveau du système, la restriction des interfaces d'écoute : l'interface d'administration ainsi que le port de Serf n'ont aucune raison d'écouter `0.0.0.0` au lieu de l'interface physique, ce qui les écarte du réseau des pods.
Ce second point est peu coûteux et mérite d'être retenu, car la configuration actuelle écoute sur toutes les interfaces, y compris celles que K3s crée.

== Enrôlement : d'où vient le premier secret <section-security-enrollment>

=== Le problème et l'observation qui le débloque

Toute mesure qui ferme le cluster repose sur un secret, qu'il s'agisse d'une clé de gossip ou d'un token d'enrôlement.
La question n'est pas de savoir comment ce secret protège, c'est un problème résolu, mais comment une machine neuve l'obtient, sachant qu'il n'y aucun personnel technique pour le saisir.

Le raisonnement se boucle sur lui-même.
Une machine qui vient d'être branchée doit prouver qu'elle a le droit de rejoindre, elle ne peut le prouver qu'avec un secret, et ce secret doit bien venir de quelque part.
Or les deux seuls endroits d'où il peut venir sont l'image système, qui est identique pour toute la flotte, ou le cluster lui-même, auquel elle n'a pas encore le droit de parler.

C'est ce cercle qui fait que le chiffrement du gossip n'est pas un gain gratuit.
Avoir une clé propre au cluster le ferme, y compris à des machines parfaitement légitimes que le client branchera six mois plus tard. 
La promesse du produit passe donc de "branchez la machine, elle rejoint" à "enrôlez la machine", ce qui n'est pas souhaitable.
Et comme une machine admise est considérée de confiance, il n'y a aucun second rempart derrière cette porte, donc le secret qui la garde porte à lui seul toute la sécurité de la couche basse.

Le protocole de premier démarrage contient pourtant déjà une réponse partielle, malgré qu'elle n'a pas été conçue pour la sécurité.
L'écran de confirmation présenté dans la #ref(<part-conception-bootstrap>, supplement: [partie]) affiche le nombre de machines découvertes et demande à l'utilisateur de confirmer lorsque ce nombre correspond à ce qu'il attend, pour une raison purement fonctionnelle : éviter d'attendre un long délai avant de figer la composition du cluster initial.
Cette interaction est une attestation humaine, par laquelle l'utilisateur déclare que ces machines sont les siennes.
C'est le seul moment du cycle de vie où une personne de confiance se prononce sur la composition du cluster, et il est déjà là, déjà accepté par la conception, déjà implémenté.

La conséquence est importante pour la suite : l'enrôlement de la cohorte initiale est gratuit, puisque les machines présentes au moment de la confirmation peuvent recevoir un secret propre au cluster sans aucune action supplémentaire de l'utilisateur.
La bonne question n'est donc plus comment fermer le cluster sans rien demander à personne, mais plutôt quel est le prix minimal pour ajouter une machine à un cluster déjà fermé.

=== Trois options

La première option consiste à déposer le secret dans l'image, ce qui est la situation actuelle pour le token de K3s et serait la variante la plus simple pour la clé de gossip.
Le zéro-configuration y est parfait dans les deux sens, puisque la cohorte initiale se forme sans rien demander et qu'une machine ajoutée bien plus tard rejoint tout aussi silencieusement, en portant le même secret depuis sa fabrication.
Le défaut est structurel : le secret est le même pour tous les clients d'ANTS, il est présent sur chaque disque livré, et il ne peut pas être changé sans refabriquer les images.
Une seule machine analysée, n'importe où, compromet toutes les installations déployées.
La sécurité obtenue est donc surtout apparente, puisqu'elle arrête une personne peu déterminée mais pas quelqu'un qui a acheté ou récupéré une machine.

La deuxième option consiste à engendrer un secret propre au cluster, accompagné d'une cérémonie d'appairage.
Ce secret est produit au bootstrap par la machine qui initialise le cluster, puis partagé avec la cohorte attestée à l'écran de confirmation, si bien que chaque cluster possède un secret unique qu'aucune image ne contient.
Pour la cohorte initiale, le coût est nul, et toute la difficulté est reportée sur l'ajout ultérieur d'une machine, qui devient une opération d'appairage : le schéma classique consiste à afficher un code court sur une machine du cluster et à le faire confirmer sur celle qui arrive, ce qui prouve que la personne a un accès physique aux deux, et le protocole possède déjà les écrans capables de porter cette interaction.
Cette option a une propriété qui compte beaucoup au vu du modèle de menace : être dans le gossip vaut alors preuve d'appartenance au cluster, donc l'hypothèse de confiance entre membres s'applique pleinement et le modèle reste simple à expliquer comme à vérifier.
Son coût est tout aussi réel : rejoindre un cluster existant ne demande aujourd'hui aucune action de l'utilisateur, ce qui est même une propriété mise en avant par le projet, et cette option en ajoute une.

La troisième option est hybride, et part d'un constat : les deux secrets du système ne protègent pas la même chose et n'ont pas la même valeur, la clé de gossip participe à la couche basse alors que le token de K3s vaut le contrôle total du cluster.
Une clé de flotte, présente dans l'image, sert donc au gossip et à la découverte, ce qui permet à une machine neuve de parler à ses pairs à tout moment et préserve intégralement le zéro-configuration.
Un secret propre au cluster, engendré au bootstrap, protège ce qui compte vraiment, à savoir l'entrée dans K3s, et ne quitte jamais le cluster sous sa forme durable : une machine qui arrive obtient à la place un jeton d'enrôlement à durée de vie limitée, émis à la demande par le plan de contrôle, ce que K3s prend en charge nativement@k3s_token_2026.
L'articulation avec l'existant est directe, puisqu'une machine qui arrive rejoint toujours en agent et que le coordinateur est déjà celui qui lui transmet l'adresse à joindre.

Une condition ne doit pas être manquée dans cette dernière option.
La clé de gossip y est partagée par toute la flotte, donc être dans le gossip ne prouve plus l'appartenance à un cluster donné : les conséquences décrites en `SEC-01`, à commencer par le détournement d'un ordre de conversion, redeviennent atteignables par quiconque a extrait la clé de flotte d'une machine.
Cette option n'est donc cohérente qu'accompagnée d'une sécurité en profondeur (tel que la vérification de l'émetteur des ordres décrit plus haut), ce qui est le prix de sa souplesse.

Le #ref(<table_security_enrollment>) résume la comparaison des trois options.

#hepia.sourced_figure(
  caption: [Comparaison des trois options d'enrôlement],
  label: <table_security_enrollment>,
  table(
    columns: (1fr, auto, auto, auto),
    align: left,
    [*Critère*], [*1. Image*], [*2. Appairage*], [*3. Hybride*],
    [Cluster initial], [aucune action], [aucune action], [aucune action],
    [Ajout d'une machine plus tard], [aucune action], [code d'appairage], [aucune action],
    [Secret dans l'image], [oui, critique], [non], [oui, pour le gossip seulement],
    [Le gossip prouve l'appartenance], [non], [oui], [non],
    [Vérification d'émetteur nécessaire], [oui], [non], [oui],
    [Compromission d'une machine], [toute la flotte], [un cluster], [un cluster, et le gossip de la flotte],
    [Rotation possible], [non], [oui], [oui],
    [Zéro-configuration], [préserve], [dégrade], [préserve],
    [Complexité de réalisation], [faible], [élevée], [élevée],
  ),
)

#highlight("REVIEW HERE")


=== Ce que la conception laisse ouvert

La première option est écartée, car elle ne fait que déplacer le problème et n'offre aucune rotation.
Le choix se joue donc entre l'appairage et l'hybride, et il n'est pas tranché ici parce qu'il ne repose pas sur un critère technique.
Le critère décisif est commercial : quelle est la fréquence réelle d'ajout d'une machine à un cluster déjà installé, et qui est présent quand cela arrive.
Si l'ajout d'une machine est une opération rare, le coût de l'appairage est négligeable et son modèle de confiance nettement plus simple.
Si à l'inverse une machine doit pouvoir être branchée sans notice ni assistance, l'appairage revient à casser la promesse du produit sur le seul cas où elle compte vraiment, et l'option hybride s'impose.

Les deux se défendent, et elles ne diffèrent pas par leur niveau de sécurité mais par l'endroit où elles placent la complexité : l'appairage garde un modèle simple et paie en ergonomie, l'hybride garde l'ergonomie intacte et paie en complexité de modèle, puisqu'il impose deux niveaux de confiance et rend obligatoire des vérifications qui seraient facultatives autrement.

Notre recommandation penche en faveur de l'appairage, car il permet de garder un modèle simple et plus logique.
D'après nos informations, l'ajout d'une machine est une opération rare, et un simple code d'appairage à saisir reste une opération ponctuelle et basique, déjà présente dans d'autres appareils ou logiciels du quotidien (appareils connectés, applications de messagerie, etc.).

Deux questions restent enfin sans réponse.

La première est celle de la politique d'émission des jetons.
L'option hybride déplace le contrôle d'accès depuis la possession d'un secret vers une décision prise par le cluster, ce qui est un progrès, mais cette décision doit s'appuyer sur quelque chose.
Une politique purement automatique équivaut à la première option, une politique demandant une confirmation équivaut à la deuxième, et l'espace intermédiaire est probablement là où se trouve la bonne réponse : par exemple une fenêtre d'ajout ouverte temporairement par l'utilisateur, ou une acceptation automatique tant que la population reste sous le nombre de machines attesté.
Cela reste moins contraignant pour l'utilisateur que l'appairage, qui n'a pas un code à saisir par machine.

La deuxième est celle de la révocation, qu'aucune des trois options ne traite dans le cas d'une machine volée ou mise au rebut.
La rotation des secrets permet d'exclure une machine, mais suppose une opération sur toutes les autres, et une révocation ciblée supposerait une identité par machine, que le protocole n'a pas aujourd'hui puisqu'une machine se contente d'annoncer son nom.

== Synthèse et priorités <section-security-priorities>

Le chantier central est la fermeture de l'entrée du cluster.
C'est la seule mesure qui referme d'un coup toute une classe de faiblesses, puisque le détournement d'ordre de conversion, la falsification des événements de premier démarrage et le déni de service par oscillation supposent tous d'être dans le gossip.
Le code à écrire est court, Serf faisant tout le travail, et ce qui coûte est de décider d'où vient la clé et comment une machine neuve l'obtient, question qui touche directement à la promesse du produit.
C'est donc un sujet de conception à trancher avec ANTS avant d'être un sujet d'implémentation.

Viennent ensuite les mesures à faible coût et fort effet, qui ne dépendent d'aucun arbitrage.
Le token agent distinct est un paramètre, et il retire les identifiants du plan de contrôle de toutes les machines qui n'en font pas partie.
Le chiffrement des secrets au repos est une option d'installation, et la restriction des adresses d'écoute comme les permissions du fichier d'état sont de la même veine.
La vérification de l'émetteur d'un ordre est courte à écrire, et elle devient indispensable si la clé de gossip est une clé de flotte.

Le reste appartient à ANTS, parce qu'il est hors du périmètre de ce travail et dépend de décisions qui ne sont pas les nôtres : tout ce qui touche au matériel de production et au chiffrement du stockage, la gestion des comptes livrés dans l'image, le réglage du contrôle d'admission en fonction des applications réellement déployées, et le logiciel d'administration final.
