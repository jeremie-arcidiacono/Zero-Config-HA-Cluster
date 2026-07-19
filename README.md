# Zero Config HA Cluster

> Travail de Bachelor réalisé dans le cadre du _Bachelor of Science : Informatique et
> systèmes de communication_ à HEPIA, Genève, en collaboration avec
> [ANTS A.I. Systems](https://ants-ai.tech).

**Documents** :

- [Énoncé](docs/enonce.md)
- [Rapport](docs/report/report.pdf) *(en cours de
  rédaction)*
- [Gradechelor](https://gradechelor.hesge.ch/2026/documents/Arcidiacono-772)

Ce dépôt contient l'implémentation d'une solution d'orchestration distribuée
« zéro-configuration » : des machines se découvrent, s'associent et forment un cluster à
haute disponibilité de manière totalement autonome et décentralisée, sans aucune
configuration manuelle. La solution s'appuie sur [Serf](https://github.com/hashicorp/serf)
(HashiCorp) pour la découverte et l'appartenance au cluster, et sur [K3s](https://k3s.io/)
pour l'orchestration applicative.

Ce travail fait suite au [projet de semestre](https://github.com/jeremie-arcidiacono/Zeroconf-Distributed-System),
qui a permis d'étudier l'état de l'art et de valider l'architecture sur un environnement
simulé.

## Structure du dépôt

```
antsd/      Daemon Go (ANTS-Daemon) qui tourne sur chaque nœud : découverte et
            appartenance au cluster via Serf, élection de rôle, installation et
            pilotage de K3s.
ants-os/    Build Packer de l'image Raspberry Pi OS personnalisée (ARM64, air-gapped)
            embarquant antsd et K3s.
ansible/    Playbooks utilisés pour déployer antsd sur les nœuds physiques pendant le
            développement (indépendant du build de l'image).
docs/       Architecture, spécification du daemon et de ses logiques, gestion de projet, et
            le mémoire.
```

## Machines

Le PoC est réalisé sur 6 Raspberry Pi 5 (8 Go), mises à disposition par HEPIA.

## Mirrors

Le dépôt principal est sur GitHub :
([`jeremie-arcidiacono/Zero-Config-HA-Cluster`](https://github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster)).

Il est également mirroré sur l'instance GitLab de la HEPIA :
([
`flg_bachelors/tb/2026/zero-config-ha-cluster`](https://gitedu.hesge.ch/flg_bachelors/tb/2026/zero-config-ha-cluster)).
