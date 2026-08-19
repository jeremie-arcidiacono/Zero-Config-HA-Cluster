#import "../lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "../globals.typ": urls, src, src_dir, pkg

== Matériel du banc d'essai <annexe_testbed_inventory>

Cette annexe recense le matériel prêté par l'école pour constituer le banc d'essai physique.

#hepia.sourced_figure(
  caption: [Matériel du banc d'essai],
  label: <table_annexe_testbed_inventory>,
  table(
    columns: (auto, 1fr),
    align: left,
    [*Quantité*], [*Matériel*],

    [6x], [Raspberry Pi 5, 8 Go de RAM],
    [6x], [Alimentation Raspberry Pi 27 W],
    [6x], [Câble Mini HDMI vers HDMI],
    [6x], [Carte SD],
    [6x], [Câble Ethernet],
    [1x], [Switch Cisco SF110-16],
    [1x], [Câble d'alimentation standard C13 - T12],
  ),
)
