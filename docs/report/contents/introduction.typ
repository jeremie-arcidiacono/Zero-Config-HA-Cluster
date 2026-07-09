#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls

De nos jours, l'engouement pour l'intelligence artificielle engendre un besoin croissant en puissance de calcul ainsi qu'en infrastructures distribuées.
Les entreprises cherchent à intégrer ces technologies de pointe pour automatiser leurs processus et améliorer leur productivité.
Cependant, elles se heurtent souvent à la complexité technique de leur mise en œuvre, en particulier concernant le déploiement de clusters.
Traditionnellement, la mise en place d'un tel environnement est une lourde tâche : elle exige des connaissances pointues, nécessite un temps d'installation conséquent et implique une maintenance continue.

C'est dans ce contexte qu'intervient l'entreprise suisse ANTS A.I. Systems, spécialisée dans la conception et l'offre de solutions d'intelligence artificielle générative.
Elle propose une approche novatrice reposant sur trois piliers : des solutions *on-premise* fonctionnant sur des machines basées sur l'architecture ARM vendues par l'entreprise, une architecture *orientée sur la confidentialité* garantissant un contrôle total des données traitées sur site, et enfin un système *Plug-and-Play*.
Ce dernier point est crucial : l'infrastructure d'IA doit être entièrement autonome afin de permettre au client final de se passer d'une équipe technique dédiée.

Malheureusement, il s'avère qu'il n'existe actuellement aucune solution permettant de déployer et de maintenir un cluster distribué de manière véritablement "zéro configuration" et clés en main. 
L'absence d'outils capables de répondre à ces besoins constitue la problématique centrale de ce projet.
L'objectif est donc de conceptualiser une nouvelle solution logicielle d'orchestration distribuée.

Ce travail s'inscrit dans le cadre du Travail de Bachelor en Informatique et Systèmes de Communication à la #acr("HEPIA"). 
Effectué en collaboration avec ANTS A.I. Systems, ce projet se déroule sur une période de 12 semaines, à hauteur de 40 heures par semaine.
Il se base sur un travail préparatoire réalisé lors du "projet de semestre"@arcidiacono_systeme_2026, réalisé entre octobre 2025 et avril 2026.
Cette base a permis de définir les besoins et les contraintes du projet, et d'identifier les solutions existantes. 

La réalisation de ce projet s'est déroulée en plusieurs étapes. Elle a d'abord commencé par la reprise du travail préparatoire du projet de semestre@arcidiacono_systeme_2026, afin de consolider les choix déjà posés et de revoir les besoins du système. Ensuite, une partie importante du travail a consisté à préciser l'architecture cible et à définir le comportement attendu des différents composants.

#highlight("TODO: modif en fonction de la suite")
À partir de là, le développement a porté sur la mise en place du logiciel, en particulier antsd et son intégration avec Serf et K3s. Cette base a ensuite servi à construire le prototype sur Raspberry Pi, à vérifier le comportement du système dans des cas simples puis dans des cas de panne. Enfin, une attention particulière a été portée sur la sécurisation du système.
Durant toute la durée du projet, des réunions de suivi ont été régulièrement organisées avec le professeur responsable, M. Florent Glück.

La majorité des sources utilisées sont issues de documentations officielles des outils étudiés ainsi que de leurs codes sources publiés sur GitHub.

Différents modèles d'IA conversationnels (notamment Gemini et Claude via la plateforme Perplexity) ont été utilisés durant ce projet afin de gagner en efficacité.
Ils ont été sollicités pour générer de la documentation technique à partir de code source, pour reformuler certains passages de ce mémoire, ou encore pour accélérer le développement logiciel.

Ce document est structuré de la façon suivante :
#highlight("TODO: modif en fonction de la suite")
Le premier chapitre présente le contexte du projet. Il revient sur Kubernetes, K3s et Serf, puis rappelle les besoins et contraintes.

Le deuxième chapitre traite de la conception et de l'architecture. Il décrit les différentes couches du système, le rôle de ants-os, le fonctionnement de antsd, ainsi que le bootstrapping et le cycle de vie d'une machine.

Le troisième chapitre est consacré à l'implémentation. Il explique comment antsd est organisé, etc 

Le quatrième chapitre aborde la sécurité. Il présente les limitations et les choix retenus pour protéger le système.

Le cinquième chapitre présente les résultats et la discussion. Il fait le bilan du travail réalisé, met en avant les limites observées et ouvre sur les améliorations possibles.


