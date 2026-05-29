# ZERO CONFIG HIGH-AVAILABILITY CLUSTER

ORIENTATION : INFORMATIQUE LOGICIELLE

## Descriptif :

Ce travail de Bachelor s’inscrit dans le prolongement du projet de semestre réalisé en collaboration avec l'entreprise
ANTS A.I. Systems (https://ants-ai.tech), spécialisée dans l'offre de solutions d'intelligence artificielle générative
on-premise. Face aux besoins grandissants en puissance de calcul, le déploiement manuel de clusters distribués reste une
tâche fastidieuse qui exige des compétences pointues et une maintenance continue. L’objectif de ce projet est de
réaliser une solution logicielle d'orchestration distribuée « zéro-configuration » (Plug-and-Play), permettant à des
machines de se découvrir, de s'associer et de former un cluster à haute disponibilité de manière totalement autonome
grâce à un fonctionnement décentralisé, sans aucune configuration manuelle de la part du client. En s'appuyant sur des
outils open source tels que Serf (développé par HashiCorp) et Kubernetes, la solution développée devra être capable de
découvrir les nœuds, construire le cluster, assurer la haute disponibilité et se reconfigurer en cas de pannes, le tout,
avec une intervention minimale de l’utilisateur.

## Travail demandé :

- Mettre en place l'autoconfiguration réseau des nœuds sans serveur externe.
- Concevoir et développer la logique logicielle tirant parti de Serf et K3s pour créer le cluster initial. Lors de
  l'intégration d'un nouveau nœud, K3s devra s'installer et se configurer de manière autonome et transparente.
- Création d’une image du système d’exploitation initial pour l’architecture cible (ARM64), complet, configuré et ne
  requérant aucune connexion Internet, pouvant être flashée sur tout nœud physique.
- Réalisation d’un Proof of Concept (PoC) fonctionnel sur des nœuds physiques à architecture ARM64 (spécifications
  similaires aux noeuds ANTS).
- Réaliser des tests de résilience et développer la logique de récupération en cas de défaillance d'un ou plusieurs
  nœuds du cluster.
- Investigation de méthodes ou recommandations pour sécuriser l'architecture du cluster et si le temps le permet, les
  mettre en place.

## Infos

Filière d’études : ISC   
En collaboration avec : ANTS A.I. Systems  
Travail de bachelor soumis à une convention de stage en entreprise : non  
Travail de bachelor soumis à un contrat de confidentialité : non  
