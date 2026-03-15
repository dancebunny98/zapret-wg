#!/usr/bin/env bash
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root"
  exit 1
fi

PKG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p /opt/vpngw /var/lib/vpngw /opt/vpngw/clients /etc/wireguard /etc/systemd/system /etc/systemd/system/wg-quick@wg-clients.service.d

cp -f "${PKG_DIR}/vpngw" /opt/vpngw/vpngw
cp -f "${PKG_DIR}/config.json" /opt/vpngw/config.json
cp -f "${PKG_DIR}/vpngw.service" /etc/systemd/system/vpngw.service
cp -f "${PKG_DIR}/deploy/wireguard/wg-uplink.conf" /etc/wireguard/wg-uplink.conf
cp -f "${PKG_DIR}/deploy/wireguard/wg-clients.conf" /etc/wireguard/wg-clients.conf
touch /opt/vpngw/force_vpn_ips.txt /opt/vpngw/force_vpn_domains.txt

chmod 0755 /opt/vpngw/vpngw
chmod 0600 /etc/wireguard/wg-uplink.conf /etc/wireguard/wg-clients.conf
chmod 0644 /etc/systemd/system/vpngw.service

cat >/opt/vpngw/sync_peers.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ -f /var/lib/vpngw/state.json ]] || exit 0
python3 - <<'PY'
import json,subprocess,tempfile,os
p='/var/lib/vpngw/state.json'
with open(p,'r',encoding='utf-8') as f:
    s=json.load(f)
for c in s.get('clients',{}).values():
    pub=c['public_key'].strip()
    psk=c['preshared'].strip()
    ip=c['ipv4'].strip()+'/32'
    fd,tmp=tempfile.mkstemp(prefix='wgpsk-')
    os.write(fd,(psk+'\n').encode())
    os.close(fd)
    subprocess.check_call(['wg','set','wg-clients','peer',pub,'preshared-key',tmp,'allowed-ips',ip])
    os.unlink(tmp)
print('peers synced')
PY
wg show wg-clients
EOF
chmod 0755 /opt/vpngw/sync_peers.sh

cat >/etc/systemd/system/wg-quick@wg-clients.service.d/10-vpngw-sync.conf <<'EOF'
[Service]
ExecStartPost=/bin/bash -lc '/opt/vpngw/sync_peers.sh'
EOF

if grep -q "__WG_CLIENTS_PRIVATE_KEY__" /etc/wireguard/wg-clients.conf; then
  WG_PRIV="$(wg genkey)"
  sed -i "s|__WG_CLIENTS_PRIVATE_KEY__|${WG_PRIV}|g" /etc/wireguard/wg-clients.conf
fi
if grep -q "__WG_UPLINK_PRIVATE_KEY__" /etc/wireguard/wg-uplink.conf; then
  WG_UPLINK_PRIV="$(wg genkey)"
  sed -i "s|__WG_UPLINK_PRIVATE_KEY__|${WG_UPLINK_PRIV}|g" /etc/wireguard/wg-uplink.conf
fi

WG_CLIENTS_PUB="$(awk -F'=' '/PrivateKey/{gsub(/ /, "", $2); print $2}' /etc/wireguard/wg-clients.conf | wg pubkey)"

python3 - "$WG_CLIENTS_PUB" <<'PY'
import json, sys
p = "/opt/vpngw/config.json"
pub = sys.argv[1].strip()
with open(p, "r", encoding="utf-8") as f:
    cfg = json.load(f)
cfg.setdefault("wireguard", {})
cfg["wireguard"]["server_public_key"] = pub
with open(p, "w", encoding="utf-8") as f:
    json.dump(cfg, f, ensure_ascii=False, indent=2)
PY

cat >/etc/sysctl.d/99-vpngw.conf <<EOF
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
EOF
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

systemctl daemon-reload
systemctl enable --now wg-quick@wg-clients
ip link set dev wg-clients txqueuelen 1000 || true

UPLINK_READY=1
if grep -Eq 'REQUIRED_|YOUR_|SET_ME|__WG_' /etc/wireguard/wg-uplink.conf; then
  UPLINK_READY=0
  echo "wg-uplink.conf still contains placeholders; skipping wg-uplink autostart."
else
  systemctl enable --now wg-quick@wg-uplink
  ip link set dev wg-uplink txqueuelen 1000 || true
fi

CONFIG_READY=1
if ! python3 - <<'PY'
import json, sys
markers = ("REQUIRED_", "YOUR_", "SET_ME", "__WG_")
with open("/opt/vpngw/config.json", "r", encoding="utf-8") as f:
    cfg = json.load(f)
for v in (cfg["dns"]["direct_upstream"], cfg["dns"]["vpn_upstream"]):
    if any(m in str(v).upper() for m in markers):
        sys.exit(1)
PY
then
  CONFIG_READY=0
  echo "config.json still contains placeholder DNS upstreams; skipping vpngw autostart."
fi

CLIENT_READY=1
if ! python3 - <<'PY'
import json, sys
markers = ("REQUIRED_", "YOUR_", "SET_ME", "__WG_")
with open("/opt/vpngw/config.json", "r", encoding="utf-8") as f:
    cfg = json.load(f)
v = cfg["wireguard"]["server_endpoint"]
if any(m in str(v).upper() for m in markers):
    sys.exit(1)
PY
then
  CLIENT_READY=0
  echo "wireguard.server_endpoint still contains placeholders; skipping client generation."
fi

if [[ "${UPLINK_READY}" -eq 1 && "${CONFIG_READY}" -eq 1 ]]; then
  systemctl enable --now vpngw
fi

if [[ "${CONFIG_READY}" -eq 1 && "${CLIENT_READY}" -eq 1 ]]; then
  for n in 1 2 3; do
    /opt/vpngw/vpngw gen-client -config /opt/vpngw/config.json -name "router-${n}" >"/opt/vpngw/clients/router-${n}.txt"
    awk 'f{print} /^$/{f=1}' "/opt/vpngw/clients/router-${n}.txt" >"/opt/vpngw/clients/router-${n}.conf"
  done
fi

echo "Done."
echo "Client configs:"
ls -1 /opt/vpngw/clients/router-*.conf 2>/dev/null || true
