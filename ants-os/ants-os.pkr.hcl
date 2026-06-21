# =============================================================================
# Base image  : Raspberry Pi OS Lite 64-bit
# Target      : Raspberry Pi 5
# Mode        : air-gap (no internet required at boot)
# Builder     : mkaczanowski/packer-builder-arm
# =============================================================================

packer {
  # required_plugins {
  #   arm-image = {
  #     version = ">= v1.0.9"
  #     source  = "github.com/mkaczanowski/arm"
  #   }
  # }
}

# =============================================================================
# Variables
# =============================================================================

variable "k3s_version" {
  type        = string
  default     = "v1.36.1+k3s1"
  description = "k3s version to bundle"
}

variable "image_name" {
  type    = string
  default = "ants-os-rpi5-arm64"
}

variable "base_image_url" {
  type    = string
  default = "https://downloads.raspberrypi.com/raspios_lite_arm64/images/raspios_lite_arm64-2026-04-21/2026-04-21-raspios-trixie-arm64-lite.img.xz"
}

variable "base_image_checksum_url" {
  type    = string
  default = "https://downloads.raspberrypi.com/raspios_lite_arm64/images/raspios_lite_arm64-2026-04-21/2026-04-21-raspios-trixie-arm64-lite.img.xz.sha256"
}

# =============================================================================
# Source: ARM image builder
# =============================================================================

source "arm" "rpi5" {
  file_urls = [var.base_image_url]
  file_checksum_url     = var.base_image_checksum_url
  file_checksum_type    = "sha256"
  file_target_extension = "xz"
  file_unarchive_cmd = ["xz", "--decompress", "$ARCHIVE_PATH"]

  image_path = "output/${var.image_name}.img"
  image_size = "4G" # (k3s binary 70 MB + air-gap images 300 MB + OS 1.2 GB + margin)
  image_build_method = "resize"
  # image_build_method = "new"

  image_type = "dos"
  image_partitions {
    name         = "boot"
    type         = "c"
    start_sector = 8192
    filesystem   = "vfat"
    size         = "256M"
    mountpoint   = "/boot/firmware"
  }
  image_partitions {
    name         = "root"
    type         = "83"
    start_sector = 532480
    filesystem   = "ext4"
    size = "0"   # use all remaining space
    mountpoint   = "/"
  }
}

# =============================================================================
# Build: provisioning sequence
# =============================================================================

build {
  name = "ants-os"
  sources = ["source.arm.rpi5"]

  # 1. Copy binaries from pre-populated assets/ directory
  provisioner "file" {
    source      = "assets/k3s-arm64"
    destination = "/usr/local/bin/k3s"
  }
  provisioner "file" {
    sources = [
      "assets/install-k3s.sh",
    ]
    destination = "/usr/local/bin/"
  }

  provisioner "file" {
    source      = "assets/k3s-airgap-images-arm64.tar.zst"
    destination = "/tmp/k3s-airgap-images-arm64.tar.zst"
  }

  # provisioner "file" {
  #   sources = "assets/antsd"
  #   destination = "/usr/local/bin/"
  # }

  # 2. Copy configuration files
  # provisioner "file" {
  #   source      = "files/systemd/antsd.service"
  #   destination = "/etc/systemd/system/antsd.service"
  # }

  provisioner "file" {
    source      = "files/network/10-eth0.network"
    destination = "/etc/systemd/network/10-eth0.network"
  }

  # 3. System configuration
  provisioner "shell" {
    script = "scripts/provision.sh"
    environment_vars = [
      "K3S_VERSION=${var.k3s_version}",
    ]
  }
}
