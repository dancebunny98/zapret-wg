#!/usr/bin/env bash
set -euo pipefail

systemctl stop vpngw || true
systemctl stop wg-quick@wg-clients || true

WG_PRIV="$(wg genkey)"
WG_PUB="$(printf '%s' "$WG_PRIV" | wg pubkey)"

cat > /etc/wireguard/wg-clients.conf <<EOF
[Interface]
Address = 10.70.0.1/24, fd42:42:70::1/64
ListenPort = 51820
PrivateKey = ${WG_PRIV}
PostUp = ip link set dev %i txqueuelen 1000
EOF

python3 - <<PY
import json
p='/root/proga/config.json'
with open(p,'r',encoding='utf-8') as f:
    c=json.load(f)
c.setdefault('wireguard',{})['server_endpoint']='192.168.1.105:51820'
c.setdefault('wireguard',{})['server_public_key']='${WG_PUB}'
with open(p,'w',encoding='utf-8') as f:
    json.dump(c,f,ensure_ascii=False,indent=2)
print('config updated')
PY

rm -f /var/lib/vpngw/state.json /root/proga/clients/router-*.conf /root/proga/clients/router-*.txt

systemctl start wg-quick@wg-clients
/root/proga/vpngw bootstrap -clients 3 -force -server-endpoint 192.168.1.105:51820
