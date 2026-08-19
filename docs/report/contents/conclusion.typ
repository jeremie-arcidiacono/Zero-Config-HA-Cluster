#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

Ce Travail de Bachelor visait à passer de la liste d'outils étudiée durant le projet de semestre@arcidiacono_systeme_2026 à une conception complète et à une implémentation fonctionnelle : un cluster à haute disponibilité capable de se former et de se maintenir sans intervention manuelle, condition nécessaire pour qu'un client d'ANTS A.I. Systems@ants_ants_2026 sans compétence technique puisse déployer une infrastructure d'intelligence artificielle en la branchant simplement.

Pour y parvenir, l'architecture en couches posée lors du projet de semestre a été conservée : une couche basse fondée sur Serf, chargée de la découverte des nœuds et de la détection des pannes, et une couche haute confiée à K3s pour l'orchestration des applications.
Le pont entre les deux, qui restait entièrement à concevoir, a constitué le cœur de ce travail : antsd, le daemon qui pilote localement l'installation et la reconfiguration de K3s selon ce que Serf observe du cluster.
Complétée par ants-os, l'image système air-gapée qui embarque à la fois antsd et K3s, la solution a été validée sur un banc de six Raspberry Pi 5, où le cluster se forme seul, absorbe redémarrages et pannes sans intervention, et se reconfigure lorsque sa population change.

D'un point de vue personnel, le changement le plus marquant par rapport au projet de semestre a été de quitter un environnement simulé pour du matériel réel.
Cela s'accompagne du très grand nombre d'états possibles à la fois pour le cluster et pour chaque machine, et de la difficulté à les imaginer et à les reproduire tous.
Les oublier conduit à revenir sur des hypothèses posées tôt dans la conception.

Ce travail a aussi été l'occasion d'entrer bien plus loin dans le fonctionnement interne de K3s et d'etcd, et de mesurer à quel point un protocole de reprise après panne, simple en apparence, exige d'anticiper des cas de figure qui ne se révèlent souvent qu'à l'usage.

La principale difficulté n'a d'ailleurs pas été technique, mais méthodologique : face à des mécanismes qui s'enchevêtrent (élection de rôle, membership etcd, tolérance aux pannes, cycle de vie d'une machine), il a fallu apprendre à isoler un problème à la fois sans perdre de vue ses effets de bord sur le reste du système, et à documenter chaque décision au fur et à mesure plutôt que de la reconstruire après coup une fois le code écrit.
Ce travail confirme, à une échelle plus grande que le projet de semestre, qu'un système distribué se construit autant par itérations sur banc que par la conception qui les précède.

Les pistes d'amélioration identifiées en cours de route sont détaillées au chapitre précédent (#ref(<section-results-improvements>)).
Ce travail livre à ANTS A.I. Systems une base fonctionnelle, testée sur du matériel réel et documentée, sur laquelle la suite peut maintenant s'appuyer : la sécurisation du cluster, puis l'intégration de la couche applicative qui exploitera enfin la puissance que ce cluster met à disposition.

