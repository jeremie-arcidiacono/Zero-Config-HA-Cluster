# Premier démarrage d'une machine

Dans cet exemple, une machine démarre pour la première fois.  
On traite 2 cas :

1. Le client vient d'installer sa/ses premières machines, il n'y a donc pas de cluster existant
2. Le client a déjà un cluster opérationnel, il ajoute une nouvelle machine à ce cluster

Les cas suivant ne sont pas traité :

- La machine a déjà effectué un démarrage auparavant : antsd n'aura pas le même comportement.

## Étapes principales

1. Démarrage de la machine, antsd et Serf sont exécutés
2. Attente de la découverte de l'ensemble des autres machines du réseau local, via Serf et le protocole mDNS
3. Une fois la découverte terminée (timer de X secondes après le dernier nouveau member découvert) soit :
    - Un cluster existe déjà : la machine le rejoint.
    - Aucun cluster n'existe : on attend encore une fois, afin que toutes les machines ait eu le temps de démarrer, puis
      on lance le [processus de bootstrapping](#mécanisme-de-bootstrapping).

Ce qui nous donne les états suivants :

- `starting` : étape 1
- `discovering` : étape 2, découverte des autres machines
- `joining` : étape 3, la machine a découvert un cluster, installe K3s et est en train de rejoindre le cluster
- `joining-failed` : échec du processus de joining. la machine ne progresse plus
- `stable-XXXX` : la machine fait partie d'un cluster K3s
    - `stable-server` : la machine est un server K3s
    - `stable-agent` : la machine est un agent K3s
- `bootstrap-XXXX` : la machine n'a découvert aucun cluster, elle est en train de lancer le processus de bootstrapping
  pour créer un nouveau cluster
    - `bootstrap-waiting` : on attend avant de commencer le processus de bootstrapping, pour s'assurer que toutes les
      machines aient eu le temps de démarrer
    - `bootstrap-install-init` : la machine N0 installe la toute première instance de K3s
    - `bootstrap-install-servers` : les machines N1 et N2 installent K3s en mode server, en rejoignant le cluster de N0

```mermaid
flowchart TD
    A([Nœud démarre]) --> B[Lecture adresse MAC pour construction de l'ID unique]
    B --> C[Démarrage de antsd et Serf]
    C --> D[Serf effectue la découverte via mDNS\n+ démarrage du timer]
    D -- Nouveau membre détecté = reset du timer --> D
%%    D --> E{Nouveaux membres\ndétectés ?}
%%    E -- Non, attente --> D
%%    E -- Oui, reset du timer --> D
    D --> F[Timer expire]
    F --> G[Serf Query :\nexiste-t-il des nodes en état stable ?]
    G -- Oui:\ncluster existant --> I[Lire le nombre de node en état stable-server]
    I --> J{Nb node en état stable-server\n< 3 ?}
    J -- Oui --> K[Installation de \nk3s server]
    J -- Non --> L[Installation de \nk3s agent]
    G -- Non:\naucun cluster --> M{Serf Query :\nexiste-t-il des nodes en état bootstrap-waiting ?}
    M -- Oui --> P[On passe en mode bootstrap-waiting]
    M -- Non --> N[On déclenche le processus de bootstrapping:\n on broadcast l'info et on passe en mode bootstrap-waiting]
    style P fill: #664600, color: #000
    style N fill: #664600, color: #000
```

# Mécanisme de bootstrapping

Dès qu'un dès node décide qu'il est nécéssaire de lancer le processus de bootstrapping, il en informe tous les autres
par broadcast, et tous les nodes passe donc en bootstrap-waiting.
En passant en mode bootstrap-waiting, un timer local est démarré.
La première machine dont le timer expire (donc la première à etre passée en bootstrap-waiting) informe tous les autres
de passer en mode bootstrap-install-init.

Ce diagramme montre le processus à partir de bootstrap-install-init.

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
    N0 ->> N1: Serf Event(move to: bootstrap-install-servers, N0ip: XXXX)
    N0 ->> N2: Serf Event(move to: bootstrap-install-servers, N0ip: XXXX)
    N0 ->> Nx: Serf Event(move to: bootstrap-install-servers, N0ip: XXXX)

    par N1 et N2 en parallèle
        N1 ->> N1: Installation de k3s Server, en rejoignant N0
        N2 ->> N2: Installation de k3s Server, en rejoignant N0
    end

    N1 ->> N1: Broadcaster tag k3s_state=server
    N2 ->> N2: Broadcaster tag k3s_state=server
    note over N0, N2: Quorum etcd atteint - cluster HA opérationnel
    Nx ->> Nx: Installation de k3s Agent, en rejoignant N0
    Nx ->> Nx: Broadcaster tag k3s_state=agent
```