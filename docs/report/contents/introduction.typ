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
Il se base sur un travail préparatoire réalisé lors du "projet de semestre", réalisé entre octobre 2025 et avril 2026.
Cette base a permis de définir les besoins et les contraintes du projet, et d'identifier les solutions existantes. 

La réalisation de ce projet se base sur plusieurs étapes.
#highlight("TODO")
Durant toute la durée du projet, des réunions de suivi ont été régulièrement organisées avec le professeur responsable, M. Florent Glück.

La majorité des sources utilisées sont issues de documentations officielles des outils étudiés ainsi que de leurs codes sources publiés sur GitHub.

Différents modèles d'IA conversationnels (notamment Gemini et Claude via la plateforme Perplexity) ont été utilisés durant ce projet afin de gagner en efficacité.
Ils ont été sollicités pour générer de la documentation technique à partir de code source, pour reformuler certains passages de ce mémoire, ou encore pour accélérer le développement logiciel.

Ce document est structuré de la façon suivante :
#highlight("TODO")

