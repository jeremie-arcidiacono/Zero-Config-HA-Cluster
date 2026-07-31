# Premier démarrage d'une machine

Dans ce fichier, on décrit ce qui se passe lorsqu'une machine démarre pour la première fois.  
On traite 2 cas :

1. Le client vient d'installer sa/ses premières machines, il n'y a donc pas de cluster existant.
2. Le client a déjà un cluster opérationnel, il ajoute une nouvelle machine à ce cluster.

Le cas suivant n'est pas traité ici :

- La machine a déjà effectué un démarrage auparavant : antsd prend le chemin `rejoin_cluster` et ne fait aucune des
  étapes décrites dans ce fichier.

## Étapes principales

Après le démarrage de la machine, antsd et Serf sont exécutés, puis :

1. Attente de la découverte de l'ensemble des autres machines du réseau local, via Serf et le protocole mDNS
2. Une fois la découverte terminée soit :
    - Un cluster existe déjà : la machine le rejoint.
    - Aucun cluster n'existe : on lance le [processus de bootstrapping](#mécanisme-de-bootstrapping).

Ce qui nous donne les états suivants :

- `fb_discovering` : découverte des autres machines, affichage sur écran
- `fb_joining` : la machine a découvert un cluster, installe K3s et est en train de rejoindre le cluster
- `fb_joining_failed` : échec du processus de joining. la machine ne progresse plus
- `stable_XXXX` : la machine fait partie d'un cluster K3s, cet état ne fait plus partie du protocole de 1er démarrage
    - `stable_server` : la machine est un server K3s
    - `stable_agent` : la machine est un agent K3s
- `fb_bootstrap_XXXX` : la machine n'a découvert aucun cluster ET l'utilisateur à demander ce mode, elle est donc en
  train de lancer le processus de
  bootstrapping pour créer un nouveau cluster
    - `fb_bootstrap_confirm` : l'utilisateur a demandé la création d'un nouveau cluster, on attend sa confirmation
    - `fb_bootstrap_waiting` : confirmation ok, on attend avant de commencer le processus de bootstrapping pour
      s'assurer que toutes les machines aient eu l'info
    - `fb_bootstrap_install_init` : la machine N0 installe la toute première instance de K3s
    - `fb_bootstrap_install_servers` : les machines N1 et N2 installent K3s en mode server, en rejoignant le cluster de
      N0
    - `fb_bootstrap_install_agent` : les machines N3+ installent K3s en mode agent, en rejoignant le cluster de N0
    - `fb_bootstrap_failed` : échec du processus de bootstrapping, la machine ne progresse plus. Soit : le
      script d'installation K3s retourne une erreur, ou le K3s fraîchement installé ne se déclare pas prêt dans les
      5 minutes.

Tous les états sont préfixés par "fb-" pour "first boot", afin de les différencier des états globaux du reste du cycle
de vie d'antsd.

```mermaid
flowchart TD
    A[Serf effectue la découverte via mDNS\nÉcran A]
    A -- Boucle --> A
    A -- Utilisateur demande création d'un nouveau cluster --> B[Attente utilisateur...\nÉcran C]
    B -- Confirmation --> C[On déclenche le processus de bootstrapping:\n on broadcast l'info et on passe en mode fb_bootstrap_waiting\nÉcran D]
    A -- Détection d'un node en mode fb_bootstrap_waiting --> D[On passe en mode fb_bootstrap_waiting\nÉcran D]
    A -- Détection d'un node en mode stable_XXXX --> E[Attente X sec\nÉcran B]
    E --> J{Nb node en état stable_server\n< 3 ?}
%% TODO: garder cette logique simple et faire intervenir le rescaling plus tard, ou directement appliquer la logique complexe ici ?
    J -- Oui --> K[Installation de \nk3s server]
    J -- Non --> L[Installation de \nk3s agent]
    style C fill: #664600, color: #000
    style D fill: #664600, color: #000
```

La branche du milieu n'est pas implémenté pour le moment.

## Interaction utilisateur avec l'écran embarqué sur la machine ANTS

Écran A:
> Status : découverte...  
> Nombre de machines découvertes : XX machines
>
> BTN --> Création d'un nouveau cluster ?

Écran B:
> Status : joining existing ANTS cluster...  
> Nombre de machines découvertes : XX machines

Écran C:
> Status : attente création d'un nouveau cluster ?  
> Nombre de machines découvertes : XX machines
>
> BTN --> Lorsque votre nombre de machines correspond à celui ci-dessus, appuyer ici...
> BTN --> Retour

L'écran C sert à la fois de confirmation, et permet d'être sûr qu'on commence le bootstrapping lorsque tous les nodes
ont été découverts (plutot que de se baser sur un long timer).

Écran D:
> Status : création d'un nouveau cluster...  
> Nombre de machines découvertes : XX machines


**Implémentation sur Pis** : l'écran physique étant inexistant, les boutons sont simulés par des endpoints HTTP :

- `POST /bootstrap` = bouton de l'écran A (demande de création)
- `POST /bootstrap/confirm` = bouton de confirmation de l'écran C
- `POST /bootstrap/cancel` = bouton retour de l'écran C

# Mécanisme de bootstrapping

Le mécanisme de bootstrapping est une sous-étape qui est déclenchée lorsqu'une machine démarre pour la première
fois, et qu'elle n'a découvert aucun cluster existant.

Dès qu'un dès node décide qu'il est nécéssaire de lancer le processus de bootstrapping, il en informe tous les autres
par broadcast, et tous les nodes passe donc en `fb_bootstrap_waiting`.  
En passant en mode `fb_bootstrap_waiting`, un timer local est démarré.  
La première machine dont le timer expire broadcast le signal de départ : chaque node calcule alors son rôle.  
Seul N0 passe en `fb_bootstrap_install_init`, les autres restent en `fb_bootstrap_waiting` jusqu'au signal server-ready
de
N0.

Actuellement, les machines N3+ sont bloqués indéfiniment en `fb_bootstrap_waiting` si le quorum de servers n'est pas
atteint.

### Événements Serf du protocole

Le protocole repose sur 3 user events Serf (broadcast à tout le monde, y compris l'émetteur ; les handlers sont
idempotents : un événement reçu dans un état inattendu est ignoré, ce qui absorbe les doublons) :

| Événement                   | Émetteur                            | Payload  | Effet                                                                       |
|-----------------------------|-------------------------------------|----------|-----------------------------------------------------------------------------|
| `antsd:bootstrap-requested` | le node dont l'utilisateur confirme | -        | tous les nodes en premier démarrage passent en `fb_bootstrap_waiting`       |
| `antsd:bootstrap-start`     | 1er node dont le timer expire       | -        | chaque node calcule son rôle (rang dans la liste triée des membres vivants) |
| `antsd:server-ready`        | N0, une fois son K3s prêt           | IP de N0 | N1/N2 installent K3s server ; les agents surveillent le quorum              |

Le nombre de servers vaut `min(3, N)` : avec 1 ou 2 machines, il n'y a simplement pas d'agent.

Ce diagramme montre le processus à partir de `fb_bootstrap_install_init`.

Tous les "Serf Event" sont en réalité en parallèle.

```mermaid
sequenceDiagram
    participant N0 as Nœud 0 (l'identifiant le + faible)
    participant N1 as Nœud 1
    participant N2 as Nœud 2
    participant Nx as Nœud N+\n(futurs nœuds)
    note over N0, Nx: Chaque nœud calcule son rôle selon sa position
    N0 ->> N0: Installation de k3s Server, en mode initialisation de cluster
    N0 ->> N0: Attendre que K3s soit prêt
    N0 ->> N0: Passage en stable_server
    N0 ->> N1: Serf Event(antsd:server-ready, N0ip: XXXX)
    N0 ->> N2: Serf Event(antsd:server-ready, N0ip: XXXX)
    N0 ->> Nx: Serf Event(antsd:server-ready, N0ip: XXXX)

    par N1 et N2 en parallèle
        N1 ->> N1: Installation de k3s Server, en rejoignant N0
        N2 ->> N2: Installation de k3s Server, en rejoignant N0
    end

    N1 ->> N1: Passage en stable_server
    N2 ->> N2: Passage en stable_server
    note over N0, N2: Quorum etcd atteint: cluster HA opérationnel
    note over Nx: Nx attend d'observer N membres en stable_server
    Nx ->> Nx: Installation de k3s Agent, en rejoignant N0
    Nx ->> Nx: Passage en stable_agent
```