#!/usr/bin/env bash
set -euo pipefail
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