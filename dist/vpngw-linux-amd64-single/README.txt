Single binary build (Linux ELF).
Default install target: /root/proga
Run as root:
  chmod +x vpngw
  ./vpngw bootstrap -clients 3

Bundle contents:
- vpngw: single Linux binary with bootstrap and gen-client commands
- README.txt: quick usage note for this minimal package

Notes:
- bootstrap no longer auto-installs packages unless you add -allow-net-install
- fresh configs contain REQUIRED_* placeholders for uplink peer and DNS values
- wg-clients can be prepared locally before any external endpoint is configured
- this package is intentionally minimal; the full staged release layout lives in dist/vpngw-linux-amd64
