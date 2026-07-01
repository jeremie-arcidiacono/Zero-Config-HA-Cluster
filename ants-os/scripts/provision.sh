#!/usr/bin/env bash
# =============================================================================
# Main system configuration
# Executed in the image chroot via QEMU
# =============================================================================
set -euo pipefail

step=1
total_steps=11
echo_step() {
    echo "=====> [$step/$total_steps] $1"
    step=$((step + 1))
}

echo_step "Set hostname"
echo "ants" > /etc/hostname
sed -i 's/127.0.1.1.*/127.0.1.1\tants/' /etc/hosts

echo_step "Set keyboard layout : swiss french"
cat > /etc/default/keyboard << 'EOF'
XKBMODEL="pc105"
XKBLAYOUT="ch"
XKBVARIANT="fr"
XKBOPTIONS=""
BACKSPACE="guess"
EOF

echo_step "Create default user to remove first-boot wizard"
systemctl disable userconfig.service || true
useradd -m -G adm,sudo,users,plugdev,netdev -s /bin/bash ants
echo "ants:ants" | chpasswd

# Ask user to set a new password on first login
#passwd --expire ants

# Because userconfig.service is disabled, we need to explicitly enable the getty service for tty1 to allow login from the console
systemctl enable getty@tty1.service

# Setup sudoers
echo "ants ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/010_ants-nopasswd
chmod 440 /etc/sudoers.d/010_ants-nopasswd

# Setup SSH key authentication
mkdir -p /home/ants/.ssh
chmod 700 /home/ants/.ssh
mv /tmp/authorized_keys /home/ants/.ssh/authorized_keys
chmod 600 /home/ants/.ssh/authorized_keys
chown -R ants:ants /home/ants/.ssh

echo_step "Install required packages"
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y \
    htop \
    net-tools \
    vim

echo_step "Set binaries permissions"
chmod 755 /usr/local/bin/k3s
chmod 755 /usr/local/bin/install-k3s.sh
# chmod 755 /usr/local/bin/antsd

echo_step "Setup SSH server"
systemctl enable ssh
chmod 600 /etc/ssh/sshd_config.d/00-ants-hardening.conf

# Remove userconf-pi SSH banner, irrelevant since we provision a custom user directly
rm -f /etc/ssh/sshd_config.d/rename_user.conf
rm -f /usr/share/userconf-pi/sshd_banner

echo_step "Move k3s air-gap images"
mkdir -p /var/lib/rancher/k3s/agent/images
mv /tmp/k3s-airgap-images-arm64.tar.zst /var/lib/rancher/k3s/agent/images/

echo_step "Configure cgroups for k3s (required on Raspberry Pi)"
cmdline="/boot/firmware/cmdline.txt"
if [ -f "$cmdline" ]; then
    if ! grep -q "cgroup_memory=1" "$cmdline"; then
        sed -i 's/$/ cgroup_memory=1 cgroup_enable=memory/' "$cmdline"
        echo "     cgroups added to $cmdline"
    fi
else
    echo "     WARNING: $cmdline not found"
fi

echo_step "Enable systemd-networkd (network management)"
# Switch from NetworkManager to systemd-networkd
systemctl disable NetworkManager || true
systemctl enable systemd-networkd
chmod 644 /etc/systemd/network/10-eth0.network

echo_step "Enable antsd"
systemctl enable antsd || true

echo_step "Apt cleanup to reduce image size"
apt clean
rm -rf /var/lib/apt/lists/*

echo "=====> Provisioning completed successfully"
