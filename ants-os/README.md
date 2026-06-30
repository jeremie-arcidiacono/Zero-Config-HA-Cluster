# ants-os - Image OS Raspberry Pi 5 Custom

Image Raspberry Pi OS Lite ARM64 personnalisée pour les Raspberry Pi, représentant des machines ANTS dans le PoC.
Construit avec [Packer](https://www.packer.io/) + plugin `mkaczanowski/packer-builder-arm`.

## Prérequis (machine de build)

```bash
docker pull mkaczanowski/packer-builder-arm:latest
```

## Structure du projet

```
ants-os/
├── ants-os.pkr.hcl             # Fichier principal Packer
├── assets/                     # Binaires pré-téléchargés (gitignore)
│   ├── k3s-arm64               # Binaire k3s pour ARM64
│   ├── k3s-airgap-images-arm64.tar.zst  # Images container k3s (air-gap)
│   ├── install-k3s.sh          # Script d'installation k3s officiel
│   ├── antsd                   # [TODO] Binaire antsd compilé pour ARM64
├── files/
│   ├── systemd/
│   │   └── antsd.service       # Unité systemd pour antsd
│   └── network/
│       └── 10-eth0.network     # Config réseau (IPv4 DHCP + IPv6 SLAAC)
├── scripts/
│   ├── download-assets.sh      # Téléchargement des assets
│   └── provision.sh            # Script de provisioning exécuté dans le chroot
└── output/
    └── ants-os-rpi5-arm64.img  # Image générée
```

## Workflow de build

### 1. Télécharger les assets (une seule fois)

```bash
./scripts/download-assets.sh
```

### 2. Initialiser le plugin Packer

```bash
packer init ants-os.pkr.hcl
```

### Clé SSH

La clé publique utilisée par défaut est définie dans `files/ssh/authorized_keys`.

### 3. Construire l'image

```bash
# Le plugin packer-builder-arm requiert les droits root (montage loop + chroot)
docker run --rm --privileged -v /dev:/dev -v ${PWD}:/build packer-builder-arm:latest build ants-os.pkr.hcl
```

L'image est générée dans `output/`.

### 5. Flasher sur SD card

Pour l'instant, seul l'écriture via le logiciel Raspberry Pi Imager a été testée.

Pour ce faire :

1. Sélectionner l'appareil : Raspberry Pi 5
2. Sélectionner l'image : Utiliser image personnalisée
3. Sélectionner la carte SD
4. Débuter l'écriture

Par la suite, il est prévu d'utiliser l'outil `dd` pour remplacer ce logiciel GUI.

## Configuration réseau

L'image est configurée pour démarrer avec :

- **IPv4** : DHCP automatique
- **IPv6** : SLAAC (auto-configuration via Router Advertisement)
