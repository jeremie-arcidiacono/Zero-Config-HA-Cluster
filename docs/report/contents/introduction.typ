#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls, repo_url

De nos jours, l'engouement pour l'intelligence artificielle engendre un besoin croissant en puissance de calcul ainsi qu'en infrastructures distribuées.
Les entreprises cherchent à intégrer ces technologies de pointe pour automatiser leurs processus et améliorer leur productivité.
Cependant, elles se heurtent souvent à la complexité technique de leur mise en œuvre, en particulier concernant le déploiement de clusters.
Traditionnellement, la mise en place d'un tel environnement est une lourde tâche : elle exige des connaissances pointues, nécessite un temps d'installation conséquent et implique une maintenance continue.

C'est dans ce contexte qu'intervient l'entreprise suisse ANTS A.I. Systems@ants_ants_2026, spécialisée dans la conception et l'offre de solutions d'intelligence artificielle générative.
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
À partir de là, le développement a porté sur la mise en place du logiciel, en particulier antsd (le daemon conçu durant ce travail, qui s'exécute sur chaque machine et automatise la formation puis le maintien du cluster) et son intégration avec Serf et K3s. Cette base a ensuite servi à construire le prototype sur Raspberry Pi 5, à vérifier le comportement du système dans des cas simples puis dans des cas de panne. Enfin, une attention particulière a été portée sur la sécurisation du système.
Durant toute la durée du projet, des réunions de suivi ont été régulièrement organisées avec le professeur responsable, M. Florent Glück.

La majorité des sources utilisées sont issues de documentations officielles des outils étudiés ainsi que de leurs codes sources publiés sur GitHub.
Concernant les illustrations, sauf mention explicite d'une source sous la figure, toutes les figures et tous les diagrammes de ce document ont été réalisés par l'auteur.

L'intégralité de ce travail est publiée dans un dépôt Git public, à l'adresse #link(repo_url).
Les extraits de code, les fichiers et les scripts cités dans ce document pointent tous vers ce dépôt.
Le dépôt a également un mirroir sur le GitLab de la HES-SO Genève #link("https://gitedu.hesge.ch/flg_bachelors/tb/2026/zero-config-ha-cluster").

Quelques conventions accompagnent le dépôt depuis le début du projet.
Les grandes étapes fonctionnelles sont développées sur des branches dédiées dont le nom reprend la nature du changement et le domaine concerné, par exemple `feat/antsd-add-rescaling` (convention #emph("Conventional Branch")@conventional_branch_conventional_2026).
Les changements plus courts, qui tiennent en un seul commit, sont en revanche déposés directement sur la branche principale : cela évite d'alourdir inutilement l'historique.
Les messages de commit suivent la convention #emph("Conventional Commits")@conventional-commits_conventional_2026, avec un ensemble fixe de portées calquées sur les répertoires du dépôt.

Différents modèles d'IA conversationnels ont été employés durant ce projet, principalement les modèles de Claude et Gemini, utilisés via la plateforme Perplexity@perplexity_perplexity_2026, ainsi que celle de Claude directement@anthropic_ai_2026.
Ils ont servi à trois usages.
Le premier est la recherche documentaire, pour obtenir une synthèse d'une documentation ou d'un code source tiers.
Le deuxième est la rédaction, pour la reformulation de certains passages de ce mémoire et la relecture orthographique.
Le troisième est le développement, pour produire du code, en particulier sur les aspects "aide au développement" comme les tests automatisés et les playbooks Ansible, et pour obtenir une relecture critique du code écrit.
L'architecture, les décisions de conception et la structure de ce document n'ont en revanche pas été délégués.
Le code et le texte générés ont été systématiquement relus.

Ce document est structuré de la façon suivante :
#highlight("TODO: modif en fonction de la suite")
Le premier chapitre présente le contexte du projet. Il revient sur Kubernetes, K3s et Serf, puis rappelle les besoins et contraintes.

Le deuxième chapitre traite de la conception et de l'architecture. Il décrit les différentes couches du système, le rôle de ants-os, le fonctionnement de antsd, ainsi que le bootstrapping et le cycle de vie d'une machine.

Le troisième chapitre est consacré à l'implémentation. Il explique comment antsd est organisé, etc 

Le quatrième chapitre aborde la sécurité. Il présente les limitations et les choix retenus pour protéger le système.

Le cinquième chapitre présente les résultats et la discussion. Il dresse le bilan du travail réalisé, met en avant les limites observées et ouvre sur les améliorations possibles.


