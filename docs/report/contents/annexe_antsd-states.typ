#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls, src, src_dir, pkg

== États du cycle de vie de antsd <annexe-antsd-states>

Cette annexe recense la totalité des états définis par le daemon (#src("antsd/internal/node/state.go")).

#hepia.sourced_figure(
  caption: [États du premier démarrage],
  label: <table_annexe_states-firstboot>,
  table(
    columns: (auto, 1fr),
    align: left,
    [*État*], [*Description*],

    [`starting`],
    [Démarrage du daemon : les sous-systèmes se lancent et aucune décision n'est encore prise.],

    [`fb_discovering`],
    [Premier démarrage : la machine cherche ses voisines par mDNS et attend soit la découverte d'un cluster existant, soit une demande de création de la part de l'utilisateur.],

    [`fb_bootstrap_confirm`],
    [Une création de cluster a été demandée sur cette machine, qui attend la confirmation de l'utilisateur.],

    [`fb_bootstrap_waiting`],
    [La création est confirmée, localement ou par une autre machine. Un minuteur laisse à la liste des membres le temps de se stabiliser avant que les rôles ne soient calculés.],

    [`fb_bootstrap_install_init`],
    [La machine est N0, celle dont le nom arrive en tête : elle initialise le cluster en installant le premier K3s server.],

    [`fb_bootstrap_install_servers`],
    [La machine installe K3s en tant que server supplémentaire et rejoint N0.],

    [`fb_bootstrap_install_agent`],
    [La machine installe K3s en tant qu'agent et rejoint N0.],

    [`fb_bootstrap_failed`],
    [État terminal : la création du cluster a échoué sur cette machine, qui ne progresse plus.],

    [`fb_joining_waiting`],
    [La machine a découvert un cluster déjà en service : elle s'annonce comme candidate et laisse la liste des membres se stabiliser avant de choisir le server par lequel elle rejoindra.],

    [`fb_joining_cleanup`],
    [La machine demande au cluster d'oublier le nœud K3s qu'il connaît peut-être encore sous son nom, et attend la confirmation avant d'installer quoi que ce soit. Ce protocole d'oubli évite qu'une machine réinitialisée entre en conflit avec sa propre trace.],

    [`fb_joining_agent`],
    [La machine installe K3s en tant qu'agent du cluster découvert. Une nouvelle venue rejoint toujours en agent, quelle que soit la taille du plan de contrôle : l'agrandir relève du redimensionnement.],

    [`fb_joining_failed`],
    [État terminal : la machine n'a pas pu rejoindre le cluster découvert et ne progresse plus.],
  ),
)

#hepia.sourced_figure(
  caption: [États de reprise, de redimensionnement et d'opération],
  label: <table_annexe_states-lifecycle>,
  table(
    columns: (auto, 1fr),
    align: left,
    [*État*], [*Description*],

    [`rejoin_cluster`],
    [Un état local persisté a été trouvé, signe d'un redémarrage, d'un plantage ou d'une coupure de courant. La machine ne réinstalle rien et attend simplement que son K3s redevienne disponible.],

    [`rejoin_failed`],
    [État terminal : la reprise a échoué, parce que l'état local est illisible, que le rôle installé ne correspond pas à celui enregistré, ou que le délai d'attente est dépassé. La machine ne retombe jamais sur le premier démarrage, qui réinstallerait K3s par-dessus des données existantes.],

    [`rescale_coordinating`],
    [La machine mène un tour de redimensionnement : elle évince les machines durablement perdues puis, si la taille du plan de contrôle n'est plus la bonne, désigne celle qui doit changer de rôle.],

    [`rescale_promoting`],
    [Désignée pour agrandir le plan de contrôle, la machine convertit son agent K3s en server.],

    [`rescale_demoting`],
    [Désignée pour réduire le plan de contrôle, la machine convertit son server K3s en agent.],

    [`rescale_failed`],
    [État terminal : une conversion de rôle a échoué. Le cluster continue de fonctionner avec la topologie qu'il avait, mais cette machine ne progresse plus.],

    [`stable_server`],
    [La machine est un membre opérationnel du cluster et exécute un K3s server.],

    [`stable_agent`],
    [La machine est un membre opérationnel du cluster et exécute un K3s agent.],
  ),
)
