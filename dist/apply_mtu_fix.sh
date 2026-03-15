#!/usr/bin/env bash
set -euo pipefail

if ! grep -q '^MTU =' /etc/wireguard/wg-clients.conf; then
  echo 'MTU = 1360' >> /etc/wireguard/wg-clients.conf
else
  sed -i 's/^MTU =.*/MTU = 1360/' /etc/wireguard/wg-clients.conf
fi

for f in /root/proga/clients/router-1.conf /root/proga/clients/router-2.conf /root/proga/clients/router-3.conf; do
  if [[ -f "$f" ]]; then
    sed -i 's/^MTU = .*/MTU = 1360/' "$f"
  fi
done

python3 - <<'PY'
import json
p='/root/proga/config.json'
with open(p,'r',encoding='utf-8') as f:
    c=json.load(f)
c.setdefault('wireguard',{})['mtu']=1360
with open(p,'w',encoding='utf-8') as f:
    json.dump(c,f,ensure_ascii=False,indent=2)
print('config mtu set 1360')
PY

systemctl restart wg-quick@wg-clients
systemctl restart vpngw

systemctl is-active wg-quick@wg-clients vpngw

grep -n '^MTU' /etc/wireguard/wg-clients.conf /root/proga/clients/router-1.conf