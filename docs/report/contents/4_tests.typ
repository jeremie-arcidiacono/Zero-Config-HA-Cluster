#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls, src, src_dir, pkg

= Tests et PoC <chapter-tests>

Le chapitre précédent décrit un système complet, mais rien n'y démontre encore qu'il fonctionne.
Un logiciel qui coordonne plusieurs machines pose d'ailleurs une difficulté particulière : ses comportements les plus intéressants n'apparaissent qu'à plusieurs.
Ce chapitre traite donc de la vérification, et il se lit en deux temps.

Nous présentons d'abord les moyens mis en place pour éprouver le système, depuis les tests qui s'exécutent en quelques secondes sur un poste de travail jusqu'au banc de six machines physiques.
Nous rejouons ensuite sur ce banc les situations que le cluster est censé encaisser, en observant ce qu'il fait réellement, combien de temps il y met, et à partir de quel moment il ne s'en sort plus.
L'interprétation de ces observations par rapport aux objectifs du projet est réservée au #ref(<chapter-results>) : ce chapitre-ci rapporte des faits.

== Les moyens de vérification <section-tests-environment>

Le développement d'un logiciel destiné à un ensemble de machines physiques pose un problème pratique évident.
Reconstruire une image système complète et réécrire six cartes mémoire à chaque modification du code représenterait plusieurs dizaines de minutes pour un changement d'une ligne, ce qui est incompatible avec un rythme de travail raisonnable.
Nous avons donc mis en place trois moyens complémentaires de vérifier le comportement du système, du plus rapide au plus fidèle.
Aucun ne remplace les deux autres : le premier vérifie la logique, le deuxième le comportement d'ensemble, et seul le troisième dit la vérité sur le matériel réel.

=== Les tests automatisés

Le premier moyen, et le plus utile durant la réalisation, tient au fait que les scénarios complets du cycle de vie s'exécutent dans des tests automatisés, sans réseau, sans K3s et sans matériel.

Cette possibilité découle directement de deux décisions présentées au chapitre précédent.
D'une part, le gestionnaire de cluster ne manipule pas Serf directement, mais à travers l'interface réduite du #ref(<code_implementation_serfapi>).
D'autre part, l'installation de K3s passe elle aussi par une interface, dont il existe une implémentation simulée.

Les tests fournissent une implémentation de l'interface Serf adossée à un bus de diffusion simulé en mémoire.
Ce bus reproduit le comportement observable de Serf : un événement applicatif est remis à toutes les machines, expéditeur compris, et une mise à jour de l'état d'une machine est notifiée à toutes les autres.
Il devient alors possible d'instancier plusieurs gestionnaires dans un même processus, de les relier au même bus, et de dérouler la chorégraphie complète.
Ces tests occupent les fichiers en `_test.go` du paquet #src_dir("antsd/internal/cluster", body: [`cluster`]), à raison d'un fichier par scénario.

Le test le plus représentatif met en scène quatre machines (fonction `TestBootstrapFourNodes` dans le fichier #src("antsd/internal/cluster/bootstrap_test.go")).
Il déclenche la création du cluster depuis la première, puis vérifie que les trois machines dont le nom vient en tête deviennent serveurs, que la quatrième devient agent, et que chacune a bien enregistré son rôle sur le disque.

Ces tests du paquet #pkg("cluster") sont complétés par d'autres tests plus simples dans les autres paquets.
L'ensemble compte aujourd'hui presque une centaine de fonctions de test qui s'exécutent en une dizaine de secondes, l'essentiel de cette durée revenant aux observations qui attendent qu'une machine simulée atteigne l'état voulu.
Cette rapidité tient à deux réglages propres aux tests : l'installation de K3s simulée rend la main en quelques millisecondes au lieu de plusieurs secondes, et les périodes d'attente du protocole sont également raccourcies.

Ces tests ont permis de vérifier la non-régression et servent de garde-fou pour les règles que le système ne doit jamais enfreindre.
Par exemple, le refus de retomber sur le protocole de premier démarrage et l'interdiction d'annoncer un départ à l'arrêt, tous deux exposés au chapitre précédent, sont ainsi surveillés par des tests dédiés.

La couverture globale de ces tests est d'environ 62 % du code source, et près de 90 % pour le code du paquet #pkg("cluster").

Il faut cependant nommer la limite de ce moyen, car elle explique pourquoi les deux suivants existent.
Un test de ce type vérifie que la logique de décision est juste, en supposant que le reste se comporte comme prévu.
Il ne dit rien du temps que prend une installation réelle, ni de la manière dont Serf se comporte sur un vrai réseau, ni de la manière dont K3s réagit quand une machine disparaît brutalement.

=== L'exécution locale de plusieurs instances

Le deuxième moyen consiste à exécuter plusieurs daemons complets sur un même poste de travail.
Chaque instance reçoit un nom, un port Serf, un port HTTP et un fichier d'état distincts, et utilise l'installation simulée de K3s.
Contrairement aux tests, la communication passe ici par le véritable Serf et par la découverte #acr("mDNS").

Cette configuration sert surtout à observer le système en fonctionnement, en particulier à suivre les transitions d'état sur les tableaux de bord des différentes instances.
C'est d'ailleurs la raison pour laquelle l'installation simulée attend quelques secondes au lieu de rendre la main immédiatement : sans ce délai, les états intermédiaires défileraient trop vite pour être observés.

=== Le déploiement sur les machines physiques <part-tests-deployment>

Le troisième moyen s'adresse au matériel réel.
Un ensemble de playbooks Ansible@ansible_ansible_2026 (#src_dir("ansible/playbooks")) permet de contrôler et de vérifier le comportement du système sur le banc d'essai physique.

Un playbook (#src("ansible/playbooks/deploy-antsd.yml")) compile le daemon pour ARM64 sur le poste de développement, puis distribue le binaire obtenu à toutes les machines de l'inventaire et redémarre le service correspondant.
Cette solution prend quelques secondes, contre plusieurs dizaines de minutes pour une reconstruction complète de l'image.

Il faut souligner que l'ensemble de la partie Ansible du projet est un outil de développement et non un mécanisme du produit final.
Dans le système livré, antsd est intégré à l'image ants-os et aucune machine ne reçoit de logiciel par le réseau.
Il n'y a donc aucune dépendance à Ansible.

Trois autres playbooks complètent cette solution.
Le premier prépare une machine fraîchement flashée, en fixant son nom et en installant le service du daemon.
Le second est d'une autre nature, car il ne vérifie pas antsd mais le cluster lui-même.
Il déploie une petite application hello-world sur chaque nœud du cluster K3s (donc sur chaque machine disponible), puis interroge toutes les machines l'une après l'autre et affiche quelle instance a répondu.
L'intérêt est qu'une machine interrogée peut parfaitement répondre par une instance hébergée ailleurs, ce qui prouve que le réseau interne de Kubernetes fonctionne d'une machine à l'autre.
C'est la seule vérification qui atteste que le cluster rend réellement un service, là où tout le reste se contente de constater que antsd a déclaré K3s installé.
L'image utilisée est d'ailleurs choisie parmi celles que ants-os embarque déjà, afin que cette vérification n'introduise aucun accès à Internet.
Le troisième playbook est un utilitaire de maintenance, qui permet de réinitialiser le cluster en supprimant tous les fichiers d'état et en supprimant les installations K3s.

== Le banc d'essai <section-tests-testbed>

Le banc d'essai physique est constitué de six Raspberry Pi 5 équipés de huit gigaoctets de mémoire vive, reliés par un switch.
La liste du matériel prêté par l'école est donnée dans l'#ref(<annexe_testbed_inventory>).
Il faut souligner une différence : le switch prévu n'est pas un modèle administrable, et nous utilisons donc un modèle plus complet.
Cela nous permet de placer les six machines dans un #acr("VLAN") isolé, et ainsi de momentanément couper l'accès à Internet sur ce segment pour s'assurer que le cluster ne dépend pas de ressources externes.

Ce matériel correspond à l'ordre de grandeur des machines commercialisées par ANTS A.I. Systems, tout en restant d'un coût modeste.
Le nombre de six machines permet de former un cluster comportant à la fois des serveurs et des agents, et de retirer plusieurs machines tout en conservant un quorum.

Le réseau est plat et ne comporte qu'un seul domaine de diffusion.
Ce n'est pas un détail de montage mais une hypothèse du système : la découverte repose sur #acr("mDNS"), donc sur le multicast local, et toute mesure présentée ici suppose que les six machines partagent le même segment.
Les adresses sont attribuées par #acr("DHCP") et stockées dans l'inventaire Ansible, pour que les outils de mesure sachent où joindre chaque machine.

Les noms des machines sont attribués par l'inventaire plutôt que dérivés de l'adresse MAC comme le fait le produit final, afin de rester lisibles pendant les essais.
Cet écart a une conséquence directe sur la lecture des résultats : les noms sont ordonnés, donc la machine dont le nom vient en tête est à la fois la machine de rang zéro au premier démarrage et le coordinateur du redimensionnement tant qu'elle est un serveur vivant.
Plusieurs scénarios distinguent pour cette raison le débranchement de cette machine de celui de n'importe quelle autre.
La contrepartie de ce confort est que la dérivation du nom depuis l'adresse MAC n'est jamais exercée pendant cette campagne, et fait donc partie d'une vérification à part, qui n'est pas présentée ici.

La #ref(<fig_tests_testbed>) montre le banc tel qu'il est monté.

#hepia.sourced_figure(
  caption: [Banc d'essai : six Raspberry Pi 5 posés sur le switch],
  label: <fig_tests_testbed>,
  image("../assets/images/rpi_test_bench.jpg", width: 90%),
)

Une différence matérielle mérite d'être signalée dès maintenant, car elle porte sur toutes les durées publiées dans ce chapitre.
Ces machines démarrent sur une carte mémoire, là où les machines d'ANTS embarquent un stockage NVMe (voir #ref(<section-conception-ants-os>, supplement: [section])).
Or la base de données interne de K3s est particulièrement sensible à la latence d'écriture, comme exposé au #ref(<section-context-kubernetes>, supplement: [section]).
Les durées mesurées ici sont donc des durées de banc, obtenues sur le stockage le plus lent des deux.

=== Conduite d'un essai <part-tests-method>

Chaque scénario est décrit par une fiche, qui porte un identifiant et fixe les mêmes champs pour tous : ce que la fiche valide, l'état du banc exigé avant de commencer, les réglages du daemon, l'action exacte qui déclenche le scénario, la définition des deux instants entre lesquels la durée est mesurée, le résultat attendu et les traces à conserver.
Ces identifiants sont stables et ne sont jamais réattribués, si bien que les suites présentent des trous : la relecture du protocole a retiré plusieurs scénarios dont la démonstration ne valait pas la peine.

Les pannes sont provoquées physiquement, ce qui est précisément ce que le banc apporte.
Débrancher l'alimentation d'une machine reproduit la coupure de courant et le plantage matériel.
Débrancher le câble réseau produit une situation différente, car la machine continue de fonctionner en aveugle, ce qui permet d'observer séparément les deux côtés d'une coupure.
En pratique, la coupure réseau est parfois réalisée grâce à des routes "blackhole" sur les machines, ce qui permet de couper le réseau sans intervention physique et de garder la machine contrôlable à distance.
Un redémarrage propre, l'arrêt d'un service ou la corruption volontaire du fichier d'état complètent ces injections lorsque le scénario vise un mécanisme précis du daemon plutôt qu'une panne réaliste.

Pour la remise à zéro entre deux scénarios, la procédure consiste donc à désinstaller K3s et à supprimer le fichier d'état, ce qu'automatise un playbook dédié (voir #ref(<part-tests-deployment>, supplement: [section])).

La campagne enchaîne la plupart des scénarios sans remise à zéro intermédiaire, chacun partant de l'état laissé par le précédent.
Ce choix est motivé par l'économie de temps, car une remise à zéro suivie d'un premier démarrage coûte plusieurs minutes, ce qui est démesuré au vu du nombre de scénarios à exécuter.

Reste la question de l'observation, à laquelle le matériel choisi oppose deux obstacles.
Le premier est que Raspberry Pi OS conserve le journal du système en mémoire vive afin d'épargner la carte mémoire, si bien que ce journal disparaît au redémarrage.
Le second est que ces machines ne possèdent pas d'horloge sauvegardée par pile : elles démarrent avec une date erronée, puis la synchronisation réseau corrige l'heure d'un seul coup, causant un saut important.
Comparer les horodatages de deux machines sans précaution n'a donc aucun sens.

Ces deux obstacles ont chacun leur réponse.
Le journal est rendu persistant dans ants-os.
Ensuite, la chronologie de référence n'est pas construite à partir des journaux des machines mais depuis le poste de travail : un script d'observation (#src("docs/protocoles-tests/outils/observe.sh")) interroge une fois par seconde le point de statut d'une machine et écrit une ligne par membre du cluster, horodatée par l'horloge du poste.
Une seule machine interrogée suffit à voir tout le cluster, puisque chaque membre publie son état dans un tag Serf propagé à tous, et cette économie est délibérée : interroger les six machines ajouterait de la charge dans la fenêtre même que l'on cherche à caractériser.
Un second observateur prend simplement le relais lorsque la machine interrogée est celle que le scénario débranche.
S'y ajoutent, selon les scénarios, le compteur de redémarrages du service K3s, une sonde qui interroge en boucle la disponibilité de l'API, et une autre qui interroge l'application de test.

=== Règles de mesure <part-tests-metrology>

Une durée brute ne dit presque rien du système mesuré.
Annoncer que la récupération après la perte d'un serveur prend deux minutes revient surtout à publier la valeur d'un paramètre choisi arbitrairement pour la campagne, alors que le travail réellement accompli par le logiciel tient dans les quelques secondes restantes.

Toute durée dans ce chapitre est donc décomposée en quatre parts.
La première rassemble les constantes du protocole, codées en dur et identiques d'un essai à l'autre.
La deuxième rassemble les délais réglables via la configuration de antsd, dont la valeur de campagne diffère volontairement de celle du produit.
La troisième est le travail réellement mesuré, c'est-à-dire la durée du script d'installation, celle de la sonde de disponibilité, celle d'un tour de coordination ou d'une conversion de rôle, et le délai de propagation d'une information par Serf.
La quatrième n'apparaît que dans les scénarios de redémarrage : le temps propre de la machine, c'est-à-dire le micrologiciel, systemd et l'arrêt de K3s, sur lequel antsd n'a aucune prise.
La séparer est utile, puisqu'un essai dédié mesure la reprise du daemon seul et permet de la chiffrer.
Seule la troisième part est une propriété de l'implémentation, et c'est donc la seule que nous commentons.

Le #ref(<table_tests_constants>) donne les constantes qui interviennent dans les mesures, telles qu'elles figurent dans le code du daemon.

#hepia.sourced_figure(
  caption: [Constantes du protocole intervenant dans les durées mesurées],
  label: <table_tests_constants>,
  table(
    columns: (1fr, auto),
    align: left,
    [*Ce que la constante borne*], [*Valeur*],
    [Attente commune avant le signal de départ du premier démarrage], [10 s],
    [Attente avant de choisir la machine à rejoindre], [10 s],
    [Échéance d'un premier démarrage, script d'installation et sonde compris], [10 min],
    [Échéance d'une reprise après redémarrage], [10 min],
    [Échéance d'une conversion de rôle], [10 min],
    [Échéance d'un tour de coordination], [5 min],
    [Drainage d'une machine avant sa conversion], [2 min],
    [Intervalle entre deux sondes de disponibilité de K3s], [3 s],
    [Réémission d'une demande d'oubli restée sans réponse], [15 s],
    [Interrogation mDNS du réseau à la recherche de pairs], [5 s],
  ),
)

Les deux délais réglables méritent d'être présentés séparément, car ils dominent presque toutes les mesures de résilience.
La période de grâce avant éviction vaut 12 h dans le produit, ce qui est délibéré puisqu'une éviction est irréversible pour la machine concernée et qu'un redémarrage ou une maintenance ne doivent jamais en provoquer une.
Le délai anti-rebond, qui empêche de réagir à un plan de contrôle momentanément déséquilibré, vaut 30 s.
Ces deux valeurs sont respectivement ramenées à 45 s et 15 s pour la campagne, faute de quoi vérifier un seul mécanisme d'éviction demanderait de rester une demi-journée devant le banc.
Trois échéances du tableau ci-dessus sont raccourcies de la même manière, à cinq minutes pour le premier démarrage comme pour la reprise, et à sept minutes pour une conversion de rôle.
Elles ne bornent que des chemins d'échec, et n'interviennent donc que dans un seul chiffre publié plus loin.
Toute durée présentée ensuite doit être lue en gardant à l'esprit que ces parts sont des réglages d'essai, et non des caractéristiques du produit.

Une même panne produit par ailleurs trois indisponibilités de nature et de durée très différentes, qu'il ne faut jamais confondre en un seul chiffre.
La première est celle de la topologie vue par antsd, c'est-à-dire le temps qu'il faut pour retrouver le nombre de serveurs visé.
La deuxième est celle du plan de contrôle de Kubernetes, mesurée en interrogeant en boucle la disponibilité de l'API depuis un serveur survivant.
La troisième est celle de l'application, mesurée en interrogeant en boucle le service de test.
Cette dernière est en partie déterminée par une valeur qui n'appartient pas à ce projet : Kubernetes gère seul le redémarrage des pods, et la durée de cette opération dépend de sa propre configuration.

Enfin, chaque scénario n'est exécuté qu'une fois, sauf si le résultat s'écarte de l'attendu, cas où il faut départager l'accident du comportement normal.

== Les scénarios <section-tests-scenarios>

Les scénarios sont regroupés en cinq familles, qui suivent le cycle de vie d'une machine puis les pannes qu'elle peut subir.
Chacune correspond à un chemin de la machine d'états présentée au #ref(<chapter-conception>).

=== Création du cluster <part-tests-bootstrap>

Le premier scénario est celui de la mise en service : des machines vierges, aucun cluster sur le réseau, et l'unique action utilisateur de tout le système.
Il est rejoué à deux tailles, trois machines (`TB-01`) puis six machines (`TB-02`), qui sont les deux points intéressants de la règle de dimensionnement.
Trois machines donnent trois serveurs et aucun agent, ce qui est le plancher de la haute disponibilité.
Six machines donnent cinq serveurs et un agent, ce qui est la plus grande taille que le banc permet, et le seul cas où un agent est installé pendant la création du cluster.
Les tailles intermédiaires ne font pas l'objet d'un premier démarrage dédié, la suite complète des cibles étant déjà vérifiée par un test automatisé.

L'injection consiste à demander la création du cluster depuis le tableau de bord de la première machine, à vérifier que le nombre de machines découvertes correspond à ce qui est branché, puis à confirmer.
La mesure court de l'instant où cette machine entre en attente jusqu'à l'instant où la dernière machine du lot atteint son état stable.
Le déroulé attendu est celui de la #ref(<fig_conception_bootstrap-sequence>) : les serveurs s'installent strictement l'un après l'autre, et les agents ne démarrent qu'une fois le plan de contrôle complet.

Trois grandeurs sont relevées séparément, car elles ne se comportent pas de la même façon quand le cluster grandit : la durée du script d'installation, celle de la sonde de disponibilité qui suit, et l'écart entre deux installations de serveur successives.
La comparaison des deux tailles donne le coût d'échelle du protocole, c'est-à-dire le prix payé pour ajouter les membres un par un plutôt que tous à la fois.

Une quatrième grandeur est relevée alors qu'elle est invisible pour l'utilisateur : le nombre de redémarrages du service K3s à l'issue du premier démarrage.
Un premier démarrage qui aboutit avec un redémarrage n'est en effet pas le même résultat qu'un premier démarrage sans aucun, et cette différence documente un défaut connu, observé et instrumenté sur le banc lors d'une création de cluster à quatre machines.
La machine initiale se déclare stable à l'instant où son API répond pour la première fois, alors que sa base de données est encore saturée par les écritures de son propre démarrage.
Le serveur suivant se présente 0,35 s plus tard, la latence d'écriture locale grimpe jusqu'à près de deux secondes pour une seule écriture, le service K3s de la machine initiale perd son élection de leader et s'arrête, et le serveur qui rejoignait échoue avant de repartir de lui-même.
Les quatre machines finissent malgré tout dans leur état stable et l'utilisateur ne voit rien, mais le plan de contrôle a été retardé d'une quarantaine de secondes.
Ce compteur est donc la seule mesure qui rende ce défaut visible, et l'analyse de sa cause comme les corrections envisagées appartiennent au #ref(<chapter-results>).

Un dernier essai de cette famille vérifie un refus plutôt qu'une réussite (`TB-07`).
Une machine sur laquelle K3s est installé mais dont le fichier d'état a été supprimé doit refuser d'installer par-dessus les données existantes et s'arrêter dans un état d'échec en nommant la reprise attendue.
C'est exactement la situation que produit un premier démarrage ayant échoué tardivement.

Les deux créations sont conformes.
Trois machines forment leur cluster en 1 min 57 s et six machines en 3 min 46 s, dont respectivement 106 s et 216 s de travail effectif.
Le coût par serveur reste du même ordre dans les deux cas, entre trente-cinq et quarante secondes, ce qui donne une croissance linéaire avec la taille du plan de contrôle.
C'est le prix de la sérialisation, et il a le mérite d'être prévisible.
La sonde de disponibilité, en revanche, ne coûte rien du tout : le script d'installation ne rend la main qu'une fois K3s démarré, si bien que les trois grandeurs annoncées plus haut se réduisent en pratique à deux.

Le compteur de redémarrages sépare nettement les deux tailles.
Il vaut zéro partout à trois machines, et un sur la machine initiale à six machines.
Le défaut décrit ci-dessus ne se manifeste donc qu'à partir d'une certaine charge, ce qui explique qu'il ait pu passer inaperçu pendant une bonne partie du développement.

Le refus attendu de `TB-07` est obtenu en 20 s, et avant toute diffusion sur le réseau.
Le journal nomme la cause, `k3s is already installed as "agent" while this node has no persisted state: a factory reset is required`, et le tableau de bord affiche "#emph[First boot failed on this machine. It needs a factory reset, the other machines do not.]".
C'est exactement la distinction dont l'utilisateur a besoin, puisqu'elle lui dit à la fois quoi faire et sur quelle machine.

=== Rattachement d'une machine <part-tests-joining>

La deuxième famille couvre l'autre chemin de premier démarrage, celui d'une machine vierge branchée à côté d'un cluster qui tourne déjà.
C'est la démonstration la plus directe de la promesse du produit, puisque ce chemin ne demande aucune action à l'utilisateur.

Le scénario principal (`TJ-01`) allume simultanément toutes les machines vierges restantes.
Trois propriétés y sont observées ensemble.
Les machines qui arrivent s'installent en parallèle, ce que prouve le recouvrement de leurs fenêtres d'installation, puisqu'un agent ne touche pas à la composition de la base de données interne et n'a donc personne à attendre.
Elles terminent toutes agents, quelle que soit la taille du plan de contrôle.
Si la nouvelle population appelle davantage de serveurs, le coordinateur les promeut ensuite une par une.

Cette promotion demande une précaution de lecture, sans quoi un essai correct passerait pour une anomalie.
Le compteur anti-rebond ne tourne que sur le coordinateur et repart de zéro dès qu'une machine change d'état, si bien que la promotion n'arrive pas mécaniquement quinze secondes après le branchement.
Elle arrive quinze secondes après que le coordinateur a observé un déséquilibre stable, ce qui peut être légèrement plus tard.

Le deuxième scénario (`TJ-04`) joue une situation qui avait posé un problème avec une conception antérieure : une machine vierge est allumée alors qu'un serveur du cluster est en panne.
La machine qui démarre après la panne n'apprend jamais l'existence du membre tombé, parce que memberlist ignore l'annonce de mort d'un nœud dont il n'a jamais entendu parler.
La version précédente comptait donc un serveur de moins, croyait un emplacement libre, s'installait en serveur, et se heurtait au refus de la base de données interne tant que le membre mort était présent, tout en bloquant l'éviction qui aurait débloqué la situation.
L'essai vérifie que la machine s'installe désormais en agent sans blocage, alors même que le plan de contrôle est incomplet, et que le décompte de la période de grâce continue de tourner pendant son installation.

Le troisième scénario (`TJ-05`) est celui du protocole d'oubli décrit dans la #ref(<part-implementation-forget-me>, supplement: [partie]).
Une machine qui vient d'être évincée est réinitialisée localement puis rebranchée, ce qui reproduit exactement ce que fait le bouton de remise à zéro d'une machine ANTS.
Elle doit repartir en premier démarrage, demander au cluster d'oublier ce qu'il sait d'elle, obtenir la confirmation, puis s'installer en agent sous le même nom que celui sous lequel elle avait été évincée.
Deux vérifications comptent autant que la durée : la liste des nœuds Kubernetes ne doit montrer aucun doublon, et aucune erreur ne doit apparaître, ce qui serait la preuve que le fantôme n'a pas été effacé.

Les trois scénarios sont conformes.
Le rattachement de deux machines vierges demande 1 min 48 s, promotions comprises.
Les deux agents entrent en installation à la même seconde, ce qui établit le parallélisme annoncé, puis les deux promotions s'enchaînent en série, comme tout changement de composition de la base de données interne.
L'application de test répond ensuite depuis les six machines.

Le rattachement pendant la panne d'un serveur aboutit en 32 s.
La preuve recherchée est négative et se lit dans la liste des membres de l'arrivante : le serveur tombé n'y figure pas du tout, ni vivant ni en panne.
La machine s'installe bien en agent sans rien bloquer, et le décompte de la période de grâce continue de tourner pendant son installation.

Le protocole d'oubli, enfin, ne coûte qu'une seconde sur les 35 s du rattachement complet, la machine réinitialisée n'ayant en l'occurrence plus rien à faire effacer puisque son éviction l'avait déjà retirée du cluster.
Elle revient sous son nom d'origine et la liste des nœuds Kubernetes ne montre aucun doublon.

=== Redémarrage et coupure de courant <part-tests-rejoin>

La troisième famille couvre la reprise, c'est-à-dire le cas le plus fréquent une fois le cluster en service.
Une machine qui redémarre ne réinstalle rien : K3s repart seul avec la machine, et antsd se contente de vérifier la cohérence puis d'attendre la sonde de son rôle.

Les deux premiers scénarios redémarrent un agent (`TR-01`) puis un serveur (`TR-02`), et vérifient d'abord une propriété qui n'est pas une durée.
La machine absente doit être vue en panne par les autres et jamais comme partie (`left`), car c'est cette distinction qui permet à un seul chemin de reprise de couvrir le redémarrage, le plantage et la coupure de courant.
Le fichier d'état est relevé au retour : le rôle est inchangé, la date du premier démarrage est conservée et le compteur de démarrages est incrémenté.
Le cas du serveur ajoute une mesure d'indisponibilité de l'API, prise depuis un serveur survivant, puisque le cluster doit continuer de servir avec deux serveurs sur trois.

Le troisième scénario (`TR-03`) coupe l'alimentation des six machines en même temps, puis la rétablit.
C'est le scénario nominal de la reprise, et sa mesure s'arrête au retour du dernier serveur et non du premier, car aucune sonde de serveur ne peut réussir avant que le quorum ne soit reformé.
Un point de vigilance accompagne cet essai : une machine à la traîne gèle tout redimensionnement du cluster tant qu'elle est en reprise, y compris l'éviction qui la débloquerait.

Un quatrième scénario (`TR-04`) ne redémarre que le daemon, sans toucher ni à la machine ni à K3s.
Il sert à isoler la part qui revient réellement à antsd dans les durées précédentes.

Le dernier scénario (`TR-05`) corrompt volontairement le fichier d'état d'une machine.
Elle doit s'arrêter dans un état d'échec terminal et ne jamais repartir sur le protocole de premier démarrage, puisque celui-ci réinstallerait K3s par-dessus des données existantes.
La conséquence à vérifier est qu'une fois dans cet état, elle ne gèle plus aucune réparation du cluster, contrairement à la reprise dont elle sort.

Les quatre scénarios sont conformes.
Le redémarrage d'un agent occupe 1 min 52 s et celui d'un serveur 2 min 17 s, mais antsd n'en porte que 6 s et 19 s.
Tout le reste appartient à la machine, dont environ 90 s consacrées au seul arrêt de K3s.
Le redémarrage du daemon seul le confirme, puisque la reprise s'y achève dans la seconde et que les autres machines ne l'ont même pas vue en panne.
Du côté de antsd, l'écart entre les deux rôles tient entièrement à la sonde de disponibilité, celle d'un serveur devant attendre que son API réponde.

La propriété qui n'est pas une durée est vérifiée dans les deux cas : la machine absente est vue en panne et jamais comme partie, son rôle et sa date de premier démarrage sont conservés, et son compteur de démarrages est incrémenté.
Le cluster n'a par ailleurs jamais cessé de servir pendant le redémarrage d'un serveur, avec 384 sondes de disponibilité consécutives sans un seul échec depuis un serveur survivant.

La coupure générale est plus rapide que ces redémarrages propres, avec 58 s jusqu'au retour du dernier des cinq serveurs, dont 33 s d'attente que le quorum se reforme.
Le paradoxe n'est qu'apparent : une coupure d'alimentation n'arrête pas K3s, elle le tue, et c'est précisément cet arrêt ordonné que paie un redémarrage propre.
Les six machines retrouvent leur rôle sans rien réinstaller.

Le fichier d'état corrompu conduit à l'état terminal en 4 s, sans jamais passer par le protocole de premier démarrage : la transition va directement de l'état initial à l'échec de reprise, et le journal nomme la ligne fautive du fichier.
Le coordinateur journalise un déséquilibre du plan de contrôle deux secondes plus tard, ce qui établit la propriété recherchée, à savoir que cet état terminal ne gèle plus les réparations.

=== Redimensionnement <part-tests-rescale>

La quatrième famille est celle du mécanisme de redimensionnement.
Elle demande de garder en tête que la population compte les machines en panne, une machine débranchée conservant sa place et son emplacement de serveur tant qu'elle n'est pas évincée.
Rien ne bouge donc avant la fin de la période de grâce, et ce n'est pas un délai de réaction mais le mécanisme lui-même.

Le premier scénario (`TS-01`) débranche un agent alors que la population est déjà à la cible.
L'agent doit être vu en panne en quelques secondes, le rester pendant toute la période de grâce, puis disparaître complètement de la liste des membres et de la liste des nœuds Kubernetes, sans qu'aucune conversion ne suive.

Le deuxième (`TS-02`) débranche un serveur d'un cluster de quatre machines et enchaîne les deux opérations : éviction du membre mort, puis promotion de l'agent restant.
Cet ordre est le point de l'essai.
Le total mesuré entre le débranchement et le retour à trois serveurs est le chiffre que ce mémoire présente comme temps de récupération après la perte d'un serveur.

Le troisième (`TS-04`) est le chemin qui produit une rétrogradation, et il demande six machines au départ.
Débrancher un serveur et l'agent fait tomber la population de six à quatre, donc la cible de cinq à trois, alors que quatre serveurs restent installés.
Le surplus déclenche la conversion du serveur dont le nom vient en dernier.
L'application de test doit survivre, la machine étant drainée avant d'être supprimée.

Le détail d'une conversion (`TS-03`) est extrait des essais ci-dessus plutôt que provoqué séparément.
Il découpe l'opération en réception de l'ordre, désinstallation, réinstallation et sonde du nouveau rôle, et il sert surtout à établir un point sur lequel repose tout le fonctionnement hors ligne : la réinstallation ne tente aucun téléchargement et travaille depuis le coffre d'assets de l'image, alors que la désinstallation de K3s a effacé le binaire et les archives d'images.
La comparaison des deux sens de conversion est intéressante en elle-même, un serveur portant une base de données et un plan de contrôle là où un agent n'en porte pas.

Le dernier scénario (`TS-05`) vérifie qu'une panne plus courte que la période de grâce ne produit aucune éviction, et surtout que le décompte repart de zéro au retour de la machine : deux coupures successives ne doivent jamais se cumuler.
C'est l'essai qui justifie que la période de grâce du produit se compte en heures, le coût d'une éviction abusive étant une réinstallation complète de la machine.

Les cinq scénarios sont conformes.
L'éviction d'un agent absent aboutit en 54 s, dont 45 s de période de grâce et une seule seconde de travail, et n'entraîne aucune conversion puisque la population retombe exactement sur sa cible.
La perte d'un serveur demande 1 min 50 s, dont 43 s de travail, et l'ordre des deux opérations se lit directement dans les journaux de la base de données : le membre mort est retiré, le nouveau n'est ajouté que 32 s plus tard et d'abord comme simple apprenant, puis promu.
La rétrogradation prend 2 min 05 s, vise bien le serveur dont le nom vient en dernier, et le coordinateur ne répond à aucun moment par une promotion au déséquilibre passager qu'elle provoque.
L'application de test survit, la machine ayant été drainée avant sa conversion.

Les deux sens de conversion ne coûtent pas la même chose, avec 39 s pour passer d'agent à serveur contre 24 s dans l'autre sens, ce qui est cohérent avec le fait qu'un serveur porte une base de données et un plan de contrôle là où un agent n'en porte pas.
Le découpage montre où va ce temps : moins d'une seconde pour recevoir l'ordre, deux secondes pour désinstaller, moins d'une seconde pour restaurer les assets, et tout le reste pour réinstaller.
Le fonctionnement hors ligne est établi au passage : aucun téléchargement n'est tenté, le script signale explicitement qu'il saute cette étape, et le binaire réinstallé porte deux liens vers le coffre de l'image, donc il a été rétabli sans même être recopié.

Le dernier scénario confirme que le décompte de la période de grâce repart de zéro à chaque retour.
Deux pannes successives de 26 s et 27 s sur la même machine totalisent 53 s pour une grâce de 45 s, sans qu'aucune éviction ne soit déclenchée.

=== Pannes et disponibilité <part-tests-failures>

La dernière famille ne mesure plus la mécanique interne mais ce que la panne coûte à l'utilisateur.
Plusieurs de ces essais figent volontairement la topologie en désactivant le redimensionnement, sans quoi une éviction se déclencherait au milieu de la mesure et déplacerait ce que l'on croit mesurer.

Le premier (`TP-01`) débranche un serveur sur trois et vérifie la haute disponibilité elle-même : le plan de contrôle continue de servir avec deux serveurs, une brève interruption restant possible pendant que la base de données interne réélit son leader.
Le deuxième (`TP-03`) mesure la disponibilité de l'application pendant cette même panne, en interrogeant le service depuis une machine survivante.

Le troisième (`TP-05`) coupe le lien réseau d'une machine sans l'éteindre, ce qui la laisse fonctionner en aveugle, et se lit des deux côtés de la coupure.
Sa seconde partie vise un garde-fou ajouté à la suite d'un incident rencontré lors d'un essai antérieur : une machine isolée du reste du réseau s'était vue seule serveur vivant dans sa propre vue, s'était élue coordinatrice, et avait tenté d'évincer les trois machines de la majorité restée saine.
L'essai débranche donc le câble du coordinateur, et rien d'autre.
Du côté majoritaire, un nouveau coordinateur doit se désigner et son tour progresser normalement.
Du côté minoritaire, la vérification de quorum doit échouer immédiatement et le tour être abandonné sans qu'aucune suppression ne soit tentée contre des machines bien vivantes.

Le quatrième (`TP-02`) débranche deux serveurs sur trois et perd donc le quorum, ce qui est la limite structurelle du système : plus aucune auto-réparation n'est possible, le coordinateur ne peut plus rien supprimer, et ses tours échouent en boucle.
Ce qui est consigné ici compte autant que la mesure, à savoir ce qu'affichent le tableau de bord et le point de statut de la machine survivante, et si un observateur extérieur peut comprendre la situation sans lire les journaux.

Le cinquième (`TP-04`) joue le retrait volontaire d'une machine par un utilisateur qui la débranche définitivement, faute de commande de décommissionnement (voir #ref(<section-implementation-decommission>, supplement: [section])).
Il ne s'agit pas seulement de vérifier que le cluster se rétablit, mais de chiffrer ce que l'absence de cette fonctionnalité coûte réellement : le temps d'attente imposé par la période de grâce, les données qui restent sur la machine retirée, et la réinitialisation exigée avant de la réutiliser ailleurs.

Les deux derniers (`TP-06` et `TP-07`) séparent les deux cycles de vie qui cohabitent sur une machine.
Tuer antsd pendant que K3s tourne doit être sans effet sur le cluster Kubernetes, ce qui confirme que le daemon n'est pas dans le chemin des données, mais rend la machine invisible pour les autres et donc évinçable au bout de la période de grâce, alors qu'elle est parfaitement saine.
Arrêter K3s pendant que antsd tourne révèle la situation inverse, et un écart connu : antsd ne surveille pas la santé de son K3s local une fois stable, si bien qu'il continue de se déclarer serveur alors que Kubernetes voit la machine indisponible.
Ces deux essais ne mesurent donc pas une performance mais l'écart entre deux vues du même cluster.

La haute disponibilité tient.
Pendant la perte d'un serveur sur trois, une seule sonde de disponibilité échoue sur 526, soit moins d'une seconde d'interruption, et elle ne survient pas au moment de la bascule mais plus tard, pendant la replanification des charges.
La mesure la plus parlante de cet essai est cependant ailleurs : Serf voit la machine en panne en 7,5 s là où Kubernetes met une quarantaine de secondes à la déclarer indisponible, soit un rapport de un à six entre les deux couches.

Du côté de l'application, la même panne ouvre une fenêtre de dégradation de 45 s, pendant laquelle 12 requêtes échouent.
Ces échecs correspondent à la part que le routage interne envoyait encore aux trois pods perdus (le serveur hébergeait trois pods, ce qui explique ce nombre) sur les six du service.
Il n'y a donc à aucun moment de coupure totale, et le service retrouve son taux de réponse normal avant même que Kubernetes ne replanifie les charges.

La partition réseau se lit des deux côtés comme prévu.
La détection d'une machine devenue injoignable a été mesurée cinq fois entre 7,3 et 8,7 s, valeur qui appartient aux bibliothèques de propagation et non à antsd, et la machine isolée voit exactement l'image miroir, à savoir toutes les autres en panne et elle seule vivante.
La période de grâce a été délibérément allongée du côté majoritaire pour cet essai, afin d'observer le refus attendu sans provoquer d'éviction réelle.
Du côté minoritaire, le coordinateur isolé décide bien d'évincer la majorité, mais abandonne son tour aussitôt faute de quorum, puis recommence à chaque expiration du délai anti-rebond, soit toutes les quinze secondes ici : cinq tours ont été observés, et aucune suppression n'a jamais été tentée.

Le retrait volontaire d'une machine ramène le cluster à un état sain en 1 min 50 s, sans aucune commande.
Ce qui chiffre le coût de la fonctionnalité manquante est ce qui reste sur la machine retirée : K3s installé, 130 Mo de base de données et un fichier d'état qui la déclare serveur, donc une réinitialisation obligatoire avant tout réemploi.

Les deux derniers essais donnent les deux écarts de vue attendus.
Tuer le daemon laisse K3s intact et le nœud disponible, ce qui confirme qu'il n'est pas dans le chemin des données, mais rend la machine invisible pour les autres en une dizaine de secondes.
Arrêter K3s produit l'inverse : Kubernetes déclare la machine indisponible en une cinquantaine de secondes alors que antsd continue de s'annoncer serveur sans aucune limite de temps, si bien que le redimensionnement raisonne sur un serveur de plus qu'il n'en existe.

La perte du quorum, enfin, se déroule en deux temps qu'il faut séparer.

Pendant l'incident, le comportement est conforme : l'API cesse de répondre, le coordinateur abandonne ses tours en boucle, et rien de plus n'est cassé.
Mais rien ne le dit à l'utilisateur.
Le tableau de bord de la machine survivante annonce un serveur stable et Serf disponible, et son point de statut retourne les mêmes valeurs.
Deux machines y apparaissent en panne, ce qui ne permet pas de distinguer un cluster qui tient d'un cluster perdu.

La reprise, en revanche, n'est pas conforme, et c'est le seul écart de toute la campagne.
Au rebranchement, la base de données retrouve son quorum en 7,3 s par simple reconnexion, alors que Serf met 17,7 s à repasser ses membres en vie.
Dans cette fenêtre de 10,4 s, deux machines détiennent en même temps le quorum et une vue périmée, c'est-à-dire exactement la combinaison que la vérification de quorum devait rendre impossible.
Leur période de grâce étant écoulée depuis longtemps, la décision d'éviction part sans le moindre amortissement.
Les deux ordres de suppression sont réellement émis, à douze millisecondes d'écart, chacun visant l'autre camp : l'issue ne tient qu'à la latence de deux appels.
À la fin de l'essai, la liste des nœuds Kubernetes ne compte plus qu'une seule machine, les autres étant devenues des fantômes.

Conformément à la règle énoncée plus haut, cet essai a été rejoué sur un banc entièrement remis à zéro, et le résultat est identique, avec une fenêtre mesurée à 13 s cette fois.
Aucun correctif n'est livré, faute de temps, et le cas est repris dans la section suivante.

Cependant, il faut nuancer ce constat.
Les délais très courts de la campagne ont amplifié le problème.
Dans un cas réel, les délais bien plus longs de la période de grâce et du délai anti-rebond réduisent fortement la probabilité d'une telle collision.
S'il est acceptable d'augmenter davantage ces délais par rapport à nos valeurs choisies arbitrairement (ce qui est une décision qu'ANTS peut prendre), il est possible de réduire encore la probabilité.

=== Synthèse des mesures <part-tests-results>

Le #ref(<table_tests_measures>) rassemble les durées les plus significatives de la campagne, chacune décomposée selon la règle énoncée plus haut.
C'est ce tableau, et non les chiffres bruts cités dans les paragraphes précédents, qui sert de référence au #ref(<chapter-results>).

#hepia.sourced_figure(
  caption: [Durées mesurées sur le banc, décomposées en constantes, délais réglés, travail effectif et temps machine],
  label: <table_tests_measures>,
  table(
    columns: (1fr, auto, auto, auto, auto),
    align: left,
    [*Opération mesurée*], [*Total*], [*Constantes*], [*Délais réglés*], [*Travail*],
    [Création, trois machines (`TB-01`)], [1 min 57 s], [11 s], [aucun], [106 s],
    [Création, six machines (`TB-02`)], [3 min 46 s], [10 s], [aucun], [216 s],
    [Rattachement en agent (`TJ-04`)], [32 s], [10 s], [aucun], [22 s],
    [Rattachement groupé et promotions (`TJ-01`)], [1 min 48 s], [12 s], [16 s], [80 s],
    [Rattachement après réinitialisation (`TJ-05`)], [35 s], [10 s], [aucun], [25 s],
    [Reprise d'un agent redémarré (`TR-01`)], [1 min 52 s], [aucune], [aucun], [6 s (+ 106 s machine)],
    [Reprise d'un serveur redémarré (`TR-02`)], [2 min 17 s], [aucune], [aucun], [19 s (+ 118 s machine)],
    [Reprise après une coupure générale (`TR-03`)], [58 s], [aucune], [aucun], [33 s (+ 25 s machine)],
    [Reprise du daemon seul (`TR-04`)], [moins d'1 s], [aucune], [aucun], [moins d'1 s],
    [Éviction d'une machine absente (`TS-01`)], [54 s], [9 s], [45 s], [1 s],
    [Récupération après la perte d'un serveur (`TS-02`)], [1 min 50 s], [7 s], [60 s], [43 s],
    [Promotion en serveur (`TS-03`)], [39 s], [aucune], [aucun], [39 s],
    [Rétrogradation en agent (`TS-04`)], [2 min 05 s], [7 s], [60 s], [58 s],
    [Détection d'une machine débranchée (`TP-05`)], [7 à 9 s], [7 à 9 s], [aucun], [aucun],
  ),
)

Trois lectures accompagnent ce tableau.
Les lignes de reprise portent entre parenthèses le temps propre de la machine, qui n'est pas du travail de antsd mais qui doit figurer pour que le total reste juste.
La dernière ligne est intégralement une constante des bibliothèques de propagation, ce qui veut dire que le délai de détection d'une panne ne dépend d'aucun code écrit pour ce projet.
Enfin, toutes ces valeurs sont relevées à la seconde, si bien qu'un total peut s'écarter d'une unité de la somme de ses parts.

== Cas supportés et cas non supportés <section-tests-limits>

Les scénarios précédents décrivent ce que le système encaisse.
Il reste à dire ce qu'il n'encaisse pas, car un chapitre de tests qui ne montre que des réussites ne démontre rien.
Les cas rassemblés ici proviennent soit d'un incident réellement rencontré sur le banc, soit d'une limite assumée lors de la conception.

La campagne établit que le système encaisse sans aucune commande toute panne qui laisse un quorum de serveurs vivants, qu'il s'agisse d'une machine débranchée, d'un redémarrage propre, d'une coupure générale des six machines ou d'un retrait définitif.
Elle établit également qu'il encaisse l'ajout de machines à un cluster en service, y compris plusieurs à la fois et pendant qu'un serveur est en panne.

Une réserve doit cependant être apportée à cet énoncé, et elle vient du seul essai non conforme de la campagne.
Une partition réseau est encaissée tant qu'elle dure, mais pas au moment où elle guérit, et le quorum n'y change rien puisque c'est justement son retour qui ouvre la fenêtre du problème.
C'est un problème rare, mais qui doit être corrigé.

Le #ref(<table_tests_limits>) rassemble les cas non supportés.

#hepia.sourced_figure(
  caption: [Cas non supportés, comportement observé et reprise attendue],
  label: <table_tests_limits>,
  table(
    columns: (1.1fr, 1.5fr, 1fr),
    align: left,
    [*Cas*], [*Conséquence*], [*Action requise*],
    [Machine évincée qui redémarre],
    [Bloquée en reprise jusqu'à l'échéance, et plus aucune réparation nulle part pendant ce temps],
    [Réinitialiser la machine revenue],

    [Éviction mutuelle à la guérison d'une partition],
    [Le cluster tombe à une seule machine, sans aucun message],
    [Réinitialiser le cluster entier],

    [Perte du quorum],
    [API morte, coordination retentée en boucle, machines restantes annoncées saines],
    [Réinitialiser le cluster entier],

    [Fichier d'état corrompu],
    [Échec terminal de la reprise, affiché sur la machine],
    [Réinitialiser cette machine],

    [antsd mort sur une machine saine],
    [Machine vue en panne, puis évincée alors qu'elle fonctionne],
    [Relancer le daemon avant la fin de la grâce],

    [K3s arrêté, antsd vivant],
    [Le cluster surcompte ses serveurs, la machine est annoncée saine à tort],
    [Redémarrer K3s à la main],

    [Machine non vierge en tête du premier démarrage],
    [Refus d'installer, cohorte en attente, et aucun message],
    [Réinitialiser la machine fautive],

    [Échec d'installation d'un serveur],
    [Machine terminale, rangs suivants bloqués],
    [Réinitialiser, puis recommencer],

    [Conversion de rôle échouée],
    [Échec terminal, K3s local détruit, le reste du cluster continue],
    [Réinitialiser cette machine],
  ),
)

Les corrections que nous recommandons pour ces cas sont les suivantes.

- *Machine évincée qui redémarre* : détecter au démarrage que le cluster ne connaît plus cette machine, et s'arrêter en échec aussitôt plutôt que d'attendre l'échéance complète.
- *Éviction mutuelle* : recouper la vue Serf avec la liste des nœuds Kubernetes avant toute éviction, et refuser d'agir pendant un temps de cooldown après le retour du quorum.
- *Perte du quorum* : la détecter et l'afficher, au lieu de retenter en silence.
- *Fichier d'état corrompu* : sauvegarde de secours du fichier, ou reconstruction depuis le rôle installé.
- *antsd mort sur une machine saine* : redémarrage automatique du service, aujourd'hui désactivé pour les besoins du développement.
- *K3s arrêté sous un daemon vivant* : surveillance de l'état stable, qui est la première amélioration à apporter.
- *Machine non vierge et échec d'installation* : une échéance sur l'attente du premier démarrage, puis recalcul de la cohorte et des rangs sans la machine en échec.
- *Conversion de rôle échouée* : tentative de retour au rôle précédent, ou à défaut un message nommant l'étape échouée.

Trois de ces cas sortent du lot et méritent d'être commentés.

Le premier est celui d'une machine évincée que l'on rebranche sans l'avoir réinitialisée.
Elle rejoint le groupe Serf comme si de rien n'était, retrouve son fichier d'état et prend le chemin de la reprise, mais son K3s ne peut plus rejoindre une base de données dont il a été retiré.
L'échéance de dix minutes posée sur la reprise ne répare pas cette machine, dont la seule issue reste une réinitialisation, mais elle empêche une machine morte d'immobiliser toutes les autres.
L'essai en donne le prix, avec une échéance ramenée à cinq minutes pour la campagne : le cluster est resté 4 min 26 s sans pouvoir se réparer, et une seconde machine tombée pendant ce temps n'a été évincée qu'avec 3 min 41 s de retard, à la seconde même où la première a renoncé.

Le deuxième est la perte du quorum, qui est une limite structurelle et non un défaut d'implémentation.
Sans quorum, la base de données ne sert plus aucune écriture ni aucune lecture, donc le coordinateur ne peut plus rien supprimer ni convertir.
L'essai confirme que le système ne se répare pas mais ne casse rien de plus tant que dure l'incident, la vérification de quorum placée en tête de chaque tour de coordination refusant toute action.
Il confirme aussi que rien n'en est dit à l'utilisateur, ce qui est plus gênant que la panne elle-même : le tableau de bord de la machine survivante annonce un serveur en bonne santé, et seul le journal permet de comprendre la situation.
Supporter un tel cas de figure de manière automatique est complexe, et n'est pas prévu avec la conception actuelle.

Le troisième est l'éviction mutuelle à la guérison d'une partition, le plus coûteux des trois puisqu'il détruit un cluster par ailleurs en bon état.
Il montre la limite exacte de cette même vérification de quorum, qui répond à la question "suis-je du côté majoritaire ?" mais pas à la question "la vue sur laquelle je m'apprête à agir est-elle contemporaine de cette réponse ?".
Or les deux couches ne guérissent pas à la même vitesse, et c'est la plus rapide des deux qui autorise ici une action décidée à partir de la plus lente.
La leçon dépasse ce cas particulier : la couche qui subit la conséquence d'une décision devrait être celle qui en fournit la donnée d'entrée, une éviction retirant après tout un nœud Kubernetes et un membre de sa base de données.

Il reste enfin à dire ce qu'implique la réinitialisation citée dans presque toutes les lignes du tableau.
Une réinitialisation d'usine consiste à désinstaller K3s et à supprimer le fichier d'état.
Le point important pour l'utilisateur final est qu'elle ne concerne jamais que la machine dont l'écran affiche l'échec, à la seule exception des deux cas qui touchent le cluster entier : les autres machines forment un cluster qui fonctionne et n'ont pas à être touchées.
C'est ce qui rend cette procédure acceptable pour un client sans compétence technique, et c'est ce qui a été retenu lors des réunions de suivi.
