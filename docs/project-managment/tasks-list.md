# Liste des tâches du projet

Date : mai 2026 (début du projet)

## Préparation

- Découpages des tâches et diagrammes Gantt prévisionnelle
- Conception de l'architecture (global, antsd)
- Réception des machines

## Mémoire

- Rédaction énoncé
- Rédaction mémoire

## Système d'exploitation

- Sélection d'une distribution Linux et des outils/paquets qui devront être installés
- Configuration de base
    - Réseau : IPv6 et SLAAC
    - Démarrage automatique
- Intégration des composants logiciels (Serf, pré-installation K3s, etc.)
- Automatisation du processus de "création d'image flashable"
- (Prévoir un moyen de reset les machines facilement ?)

## ants daemon

- Detection initiale de tous les noeuds du cluster via Serf
- Installation du cluster K3s
    - On part du principe que durant l'initialisation (définir une durée en minutes), il n'y a pas de
      perturbation/pannes. Si c'est le cas, on considère que le client doit reset les machines et recommencer.
- Module de communication avec K3s
- Detection de panne d'un noeud
- Test sur machines physiques des concepts de base de Serf (semblable au PoC du Projet de Semestre)

## K3s

- Appliqué une config de base à K3s
- Déploiement d'un programme générique pour la démo

## Résilience

- Etude et implémentation des logiques de réponses à incidents
- Test sur machines physiques des scénarios de pannes (semblable au PoC du Projet de Semestre)
    - Classifier les cas supportés et non-supportés
    - Documenter les réactions prévues
        - états du cluster après chaque incident
        - temps nécéssaire pour retrouver un état stable

## Sécurité

Idées brouillons à affiner et explorer :

- Chiffrement des communications inter-noeuds (pour Serf)
- Si RPC vers Serf remote (donc pas que local): authentification RPC (via Serf)
- Si communication API K3s remote : auth API ?
- Mécanisme d'auth des nœuds pour rejoindre le cluster
- Réflexion : origine des clés ?
    - Par défaut, clés communes mis dans l'OS.
    - Possibilité pour l'utilisateur de BYOK ?
    - Clé sym ou asym ? Mix des deux (comme https: initial en asym puis passage en sym) ?

## TODO - Divers

- CI
    - Tests et validation de qualité
    - Compilation du mémoire Typst
    - Génération auto de l'image OS flashable ?
- Tests end-to-end
- Observabilité (pour démo et tests) : comment voir l'état du cluster ?