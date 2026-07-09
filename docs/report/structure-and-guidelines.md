# Informations diverses concernant la rédaction du mémoire

## Structure du document

1. Introduction (1 à 3 pages)
    - Intro
        - cadre bachelor, HEPIA, entreprise, encadrement, durée et dates
        - Ants AI Systems : présentation de l'entreprise et son activité
        - Problématique : manque de solution pour cluster simple / zéro config
        - Objectifs :
    - Méthodologie :
        - expliciter utilisation IA
        - "le projet a commencé par", "ensuite j'ai réalisé", ...
        - mentionner brièvement les recherches de source sur google/github/blog
        - mentionner que l'essentiel des sources est de la documentation officielle des outils étudiés
    - Plan du mémoire : "le chapitre 1 parle de...", "le chapitre 2 parle de...", etc.
2. Chapitre 1 : Contexte
    - Présentation de Kubernetes (PS)
    - Présentation de Serf plus petit que durant le PS (PS)
    - Définition des besoins/contraintes (partiellement PS)
    - Reférence a état de l'art du PS
    - Référence à état de l'art du PS pour Serf + Memberlist
3. Chapitre 2 : Conception et architecture (mécanisme)
    - Exigence et features
        - autonomie du cluster
        - tolérance aux pannes
        - limites de dimensionnement (3 à 7 serveurs) => mécanisme de rescaling
        - contraintes matérielles
        - diagram use case => montre client final qui branche puis utilise le système final web
    - Archi globale en couche (os, antsd + serf, k3s, web+ceph)
    - ants-os (pourquoi, Packer)
    - Archi logicielle antsd
        - exposition des points Control et Monitoring
        - diagram composants antsd (serf, module de communication et control k3s, boucles, ...)
        - diagram antsd state machine (= workflow)
4. Chapitre 3 : Implémentation et PoC ??? (comment c'est implémenté)
    - structure go (séparation responsabilité)
    - encapsulation / wrapper serf
    - persistance fichier local
5. Chapitre 4 : Sécurité ???
6. Chapitre 5 : Résultats et Discussion
    - est-ce que ca fonctionne ?
    - est-ce que l'objectif est atteint ?
    - problèmes rencontrés
    - améliorations futures
7. Conclusion
    - Synthèse
    - Retour personnel : ce que j'ai appris, difficultés des choix, etc...
    - ouverture...

## Questions concernant la structure du mémoire :

## Questions diverses :

## Consignes de rédaction :

- Utiliser des titres de niveau 1, 2 et 3 uniquement (titre de niveau 4 interdit)
- Toujours citer les sources des figures (utiliser `#hepia.sourced_figure(...)` avec `source:...`)
- Légende et numéro de référence pour code, table, image
- Chaque code, table, image est introduite par du texte, pas juste posé

- Écrire les chiffres en toutes lettres (0-9) sauf quand on référence une table/figure (FIG 5)
- Lier les chapitres entre eux : quelques lignes en début/fin de chapitre pour faire une transition
    - Avoir une continuité logique, pas de chapitre qui sort de nulle part, fil rouge
- Cohérence des temps : écrire au présent, ne pas mélanger les temps (passé/futur avec le présent)
- Soit première personne, soit troisième, ne pas mélanger les deux (voir avec Perrot la préférence, égal pour Gluck)
- Ne pas oublier de relire l'orthographe
- Éviter les anglicismes (à moins que ce soit un terme technique ou un terme qui est habituellement utilisé en anglais
  dans le domaine)

- De manière générale, faites attention à bien introduire toute nouvelle section ou chapitre. Il faut vraiment que la
  lecture soit fluide et continue et que rien ne semble "tomber du ciel".
- Essayez d'éviter d'avoir un mémoire avec de nombreux points énumérés ("items"). Plutôt qu'avoir des item lists,
  essayez de mieux décrire le contenu avec des mots qui décrivent aussi complètement que possible les idées et concepts.

Pensez à voir
les [remarques données lors du mémoire du projet de semestre](https://github.com/jeremie-arcidiacono/Zeroconf-Distributed-System/blob/main/docs/report/comments.md).

