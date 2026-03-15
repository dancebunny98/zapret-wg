#!/usr/bin/env bash
set -euo pipefail

WG_CLIENTS_IF="${WG_CLIENTS_IF:-wg-clients}"
WG_UPLINK_IF="${WG_UPLINK_IF:-wg-uplink}"

nft list table inet vpngw >/dev/null 2>&1 || nft add table inet vpngw
nft list chain inet vpngw mss_clamp >/dev/null 2>&1 || nft add chain inet vpngw mss_clamp '{ type filter hook forward priority mangle; policy accept; }'
nft flush chain inet vpngw mss_clamp
nft add rule inet vpngw mss_clamp oifname "$WG_UPLINK_IF" tcp flags syn tcp option maxseg size set rt mtu
nft add rule inet vpngw mss_clamp oifname "$WG_CLIENTS_IF" tcp flags syn tcp option maxseg size set rt mtu

cat >/etc/sysctl.d/99-vpngw-tune.conf <<EOF
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
ip link set dev "$WG_UPLINK_IF" txqueuelen 1000 || true
ip link set dev "$WG_CLIENTS_IF" txqueuelen 1000 || true
nft list chain inet vpngw mss_clamp
sysctl net.core.default_qdisc net.core.rmem_max net.core.wmem_max net.core.optmem_max net.core.netdev_max_backlog net.ipv4.tcp_mtu_probing
