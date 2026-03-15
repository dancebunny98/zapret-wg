#!/usr/bin/env bash
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "run as root"
  exit 1
fi

cat >/etc/sysctl.d/99-vpngw.conf <<'EOF'
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
EOF
cat >/etc/sysctl.d/99-vpngw-tune.conf <<'EOF'
net.core.default_qdisc=fq
net.core.rmem_max=67108864
net.core.wmem_max=67108864
net.core.rmem_default=262144
net.core.wmem_default=262144
net.core.optmem_max=4194304
net.core.netdev_max_backlog=16384
net.ipv4.udp_rmem_min=16384
net.ipv4.udp_wmem_min=16384
net.ipv4.tcp_mtu_probing=1
EOF
if sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null | tr ' ' '\n' | grep -qx bbr; then
  echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.d/99-vpngw-tune.conf
fi
sysctl --system >/dev/null

echo "Ensure wg interfaces are up before starting vpngw:"
echo "  - wg-clients: interface for end clients"
echo "  - wg-uplink : upstream interface for egress bypass"
echo
echo "Install service:"
echo "  cp deploy/systemd/vpngw.service /etc/systemd/system/vpngw.service"
echo "  systemctl daemon-reload && systemctl enable --now vpngw"
