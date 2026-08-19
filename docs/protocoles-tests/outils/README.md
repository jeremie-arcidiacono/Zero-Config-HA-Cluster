# Outils de mesure

## observe.sh

Produit la chronologie des états du cluster, horodatée par l'horloge du poste de travail.

C'est utile pour une raison qui n'a rien à voir avec antsd : l'horloge de RPi sautent parfois au démarrage.
Un CSV produit depuis une seule horloge résout cela.

### Prérequis

`bash`, `curl` et `jq`, sous WSL.
Le service `antsd` doit exposer son interface d'administration sur le port 9000, ce qui est le défaut.

### Usage

```bash
cd docs/protocoles-tests/outils

# Vérification avant une session : un seul sondage
./observe.sh --once

# Observation d'un run, en écrivant dans le dossier de résultats
./observe.sh --out ../resultats/2026-08-01-TB-01-run1/states.csv

# Interroger explicitement d'autres machines
./observe.sh --hosts 10.10.9.31,10.10.9.73 --interval 2
```

Le fichier de sortie est ouvert en ajout, donc relancer le script sur le même fichier ne perd rien.

### Choix des observateurs

Le script interroge le premier observateur qui répond, dans l'ordre donné.
Une seule machine suffit à voir tout le cluster.
Le second observateur sert au cas où la machine interrogée est justement celle que l'on débranche.

Interroger les six machines est possible (`--hosts` accepte autant d'adresses que voulu) mais ce n'est pas le défaut.

Quand aucun observateur ne répond, le script écrit une ligne `NONE`.
Ces lignes sont une utile : elles montrent lorsque plus aucune machine observée n'est joignable.

### Format de sortie

```csv
ts_local,observer,observer_state,member,member_status,member_state
"2026-08-12T14:03:11,204512300+02:00","10.10.9.24","stable_server","ants01","alive","stable_server"
"2026-08-12T14:03:11,204512300+02:00","10.10.9.24","stable_server","ants02","alive","fb_bootstrap_install_servers"
```

| Colonne          | Contenu                                                         |
|------------------|-----------------------------------------------------------------|
| `ts_local`       | Horodatage du sondage                                           |
| `observer`       | Machine interrogée pour ce sondage                              |
| `observer_state` | État de la machine interrogée, tel qu'elle le connaît elle-même |
| `member`         | Nom du membre décrit par la ligne                               |
| `member_status`  | Statut Serf : `alive`, `failed`, `left`                         |
| `member_state`   | État du cycle de vie du membre, lu dans son tag Serf            |

