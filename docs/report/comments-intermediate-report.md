# Commentaires et retours de M. Gluck sur le rapport

## Premier retour - 06 juillet 2026

- [x] Les références sont indiquées entre parenthèses (x). Pensez-vous pouvoir changer le style pour que cela soit des
  crochets [x] ? En effet, c'est le style utilisé habituellement dans les articles scientifiques.
- [x] Référence manquante au projet Memberlist
- [x] Référence manquante au protocole SWIM (aussi, signification de l'acronyme ?)
- [x] Fig2 pas très convaincante: à revoir/améliorer
- [x] p8, vous mentionnez antsd alors que le lecteur n'a aucune idée de quoi il s'agit. Il serait bien de très
  brièvement décrire son rôle (possiblement dans la même phrase)
- [x] Fig4: texte dans les disque pas terrible visuellement car prends bcp de place pour rien
- [x] Figures: éviter le texte par dessus les flèches
- [x] sources figures: quand c'est vous, ne rien indiquer (sauf une phrase à qqpart au début du rapport)
- [x] Pensez à mettre des couleurs dans vos diagrammes

## Deuxième retour - 13 aout 2026

- [x] Assurez-vous de bien tout référencer (ex. Serf, K3s, Perplexity, etc.)
- [x] Ajoutez une photo d'un noeud ANTS actuel dans le rapport, cf. pièce-jointe (et indiquez le copyright ANTS en
  légende)
- [x] Remplacez les occurences de "faire" par un mot plus spécifique, p.ex. "réaliser" (à ajuster, cela dépend du
  contexte)
- [x] Remplacez "faire tourner" par exécuter
- [x] Faites en sorte que pour chaque extrait de code, celui-ci soit clickable et pointe sur le fichier source sur votre
  git. De même, lorsque vous parlez d'interface, fichier source, etc. ajoutez des références à ce dont vous parlez avec
  un lien sur le fichier correspondant sur votre git
- [x] p1 "Cette base a ensuite servi à construire le prototype sur Raspberry Pi" -> préciser la version de la rpi (car
  en l'état cela semble être la rpi 1)
- [x] Soyez plus détaillé/précis dans la description de comment vous avez utilisé l'IA: "pour accélérer le développement
  logiciel" est trop vague/général
- [x] p3 "Kubernetes[2], aussi connu sous le nom de « K8s », intervient en tant qu’orchestrateur" -> reforumler: "
  intervient" n'est pas le bon verbe ici
- [x] k3s: y-a-t-il des inconvénients à ce que des noeuds server jouent également le rôle d'agent ? Ceci est une
  question légitime qui n'est pas posée/analysée dans le rapport
- [x] p7 "Le client a malgré tout besoin d’une fenêtre sur son cluster" -> pas convaincu du terme "fenêtre" ici.
  Reformuler.
- [x] Fig2: étrange que le "Cluster ANTS" soit représenté par un personnage. Pas convaincu du rôle de cet acteur "
  Cluster ANTS" tel que présenté dans la figure
- [x] p12: "tout en restant proche des machines réelles de ANTS A.I. Systems" -> mentionner (ici ou ailleurs) que la
  plateforme de ANTS system est basée sur la plateforme Nvidia Jetson Orin Nano
- [x] "Fig. 3. – Architecture d’une machine dans le cluster" -> le mot architecture n'est pas vraiment correct à mon
  sens. Plutôt qu'une architecture, il s'agit de la pile de couches logicielles sur chaque noeud du cluster
- [x] "est un daemon Go qui s’exécute" -> démon écrit en Go ...
- [x] Fig5 (et dans le texte): "Calcul du rôle" -> "détermination du rôle"
- [x] Pour la justification d'utiliser le langage Go, vous pouvez aussi mentionner (si vous en avez envie) que c'est un
  langage compilé et que contrairement à des langages interprétés, il fourni de meilleures performances et une détection
  d'erreurs à la compilation (= garantie une meilleure fiabilité et maintenance à long terme). Il dispose de features
  modernes contraiement plus moderne que à C, tout en offrant de bonnes perfomances (dans le cas d'un tel projet le C n'
  a pas vraiment d'avantages). P/r à Rust, il offre une facilité de développement et courbe d'apprentissage bien plus
  simple/rapide.
- [x] p25: "La réponse la plus immédiate consisterait à protéger cet état par un verrou." -> pourquoi cet état doit-il
  être protégé ?
- [x] p30: "b) Tolérante aux doublons et au désordre" -> "b) Tolérance aux doublons et au désordre"
- [x] p31: "les deux informations empruntent des chemins différents dans le réseau épidémique" -> épidémique ?
- [x] Référencez votre script packer dans "3.10. Construction de l’image ants-os", ainsi l'expert peut voir à quoi il
  ressemble. De même pour tout autre élément décrit dans votre rapport. Chaque fois que vous décrivez quelque chose que
  vous avez implémenté ou écrit (script, etc.), référencez cela vers le source dans votre git
- [x] Globalement, le rapport manque un petit peu de schémas/diagrammes (après le ch. conception). Essayez vraiment d'en
  ajouter où cela est possible

- [x] Fig4, Fig5, Fig7, etc.: lisibilité : évitez d'avoir du texte par dessus une flèche ou derrière un élément
