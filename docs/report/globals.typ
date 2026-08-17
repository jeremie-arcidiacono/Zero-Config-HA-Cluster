// Lister dans cette table les URLs des figures
#let urls = (
  "https://www.vecteezy.com/vector-art/17057754-big-data-icon-suitable-for-a-wide-range-of-digital-creative-projects",
  "https://kubernetes.io/images/docs/components-of-kubernetes.svg",
)

// Repo public du projet
// Sert de base pour tous les liens de code source cité dans le texte
#let repo_url = "https://github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster"
#let repo_ref = "main"

// Lien vers un fichier dans le repo : #src("antsd/internal/cluster/manager.go")
#let src(path, body: none) = link(
  repo_url + "/blob/" + repo_ref + "/" + path,
  if body == none { raw(path) } else { body },
)

// Lien vers un répertoire dans le repo : #src_dir("ants-os")
#let src_dir(path, body: none) = link(
  repo_url + "/tree/" + repo_ref + "/" + path,
  if body == none { raw(path) } else { body },
)

// Raccourci pour les package Go du daemon : #pkg("cluster")
#let pkg(name) = src_dir("antsd/internal/" + name, body: raw(name))