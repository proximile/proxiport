#!/usr/bin/env bash
# Install proxiportd on a fresh Linux host with systemd + certbot.
#
# Expects four files staged at $STAGE before running:
#   $STAGE/proxiportd            (linux/amd64 binary)
#   $STAGE/proxiportd.conf       (rendered config — secrets baked in, mode 0640)
#   $STAGE/proxiportd.service    (systemd unit, this directory)
#
# Required env:
#   HOST   = public hostname (e.g. proxiport.example.com)
#   EMAIL  = contact email for Let's Encrypt
#   STAGE  = directory holding the three files above (default /tmp/proxiport)
set -euo pipefail

: "${HOST:?HOST=public.hostname required}"
: "${EMAIL:?EMAIL=contact@example.com required}"
STAGE="${STAGE:-/tmp/proxiport}"

echo "[1/9] apt: certbot + acl"
sudo DEBIAN_FRONTEND=noninteractive apt-get update -q
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q certbot acl

echo "[2/9] proxiport user + dirs"
if ! getent passwd proxiport >/dev/null; then
    sudo useradd -r -d /var/lib/proxiport -m -s /usr/sbin/nologin -U \
        -c "ProxiPort server" proxiport
fi
sudo install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
sudo install -d -o proxiport -g proxiport -m 0750 /var/log/proxiport
sudo install -d -o root -g root -m 0755 /etc/proxiport

echo "[3/9] ssl-cert group + add proxiport"
if ! getent group ssl-cert >/dev/null; then
    sudo groupadd --system ssl-cert
fi
sudo usermod -aG ssl-cert proxiport

echo "[4/9] install binary, conf, unit"
sudo install -o root -g root -m 0755 "$STAGE/proxiportd" /usr/local/bin/proxiportd
sudo install -o root -g proxiport -m 0640 "$STAGE/proxiportd.conf" /etc/proxiport/proxiportd.conf
sudo install -o root -g root -m 0644 "$STAGE/proxiportd.service" /etc/systemd/system/proxiportd.service

echo "[5/9] cert: certbot standalone (port 80 must be free)"
if [ ! -d "/etc/letsencrypt/live/$HOST" ]; then
    sudo certbot certonly --standalone --non-interactive --agree-tos \
        -m "$EMAIL" -d "$HOST"
else
    echo "  cert already exists, skipping issuance"
fi

echo "[6/9] grant ssl-cert group read on /etc/letsencrypt"
sudo chgrp -R ssl-cert /etc/letsencrypt/live /etc/letsencrypt/archive
sudo chmod -R g+rX /etc/letsencrypt/live /etc/letsencrypt/archive

echo "[7/9] cert renewal: deploy + pre/post hooks"
sudo install -d -m 0755 \
    /etc/letsencrypt/renewal-hooks/deploy \
    /etc/letsencrypt/renewal-hooks/pre \
    /etc/letsencrypt/renewal-hooks/post

sudo tee /etc/letsencrypt/renewal-hooks/deploy/proxiportd-restart.sh >/dev/null <<'HOOK'
#!/usr/bin/env bash
set -e
chgrp -R ssl-cert /etc/letsencrypt/live /etc/letsencrypt/archive
chmod -R g+rX /etc/letsencrypt/live /etc/letsencrypt/archive
systemctl restart proxiportd
HOOK

echo -e '#!/usr/bin/env bash\nsystemctl stop proxiportd' | \
    sudo tee /etc/letsencrypt/renewal-hooks/pre/proxiportd-stop.sh >/dev/null
echo -e '#!/usr/bin/env bash\nsystemctl start proxiportd' | \
    sudo tee /etc/letsencrypt/renewal-hooks/post/proxiportd-start.sh >/dev/null
sudo chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/*.sh \
    /etc/letsencrypt/renewal-hooks/pre/*.sh \
    /etc/letsencrypt/renewal-hooks/post/*.sh

echo "[8/9] systemd: enable + start"
sudo systemctl daemon-reload
sudo systemctl enable proxiportd
sudo systemctl restart proxiportd

echo "[9/9] status"
sleep 2
sudo systemctl --no-pager --full status proxiportd | head -25
echo
echo "listening:"
sudo ss -tlnp | grep -E ':(80|443|2[0-9]{4})\s' || true
echo
echo "log tail:"
sudo tail -20 /var/log/proxiport/proxiportd.log 2>/dev/null || echo "(no log yet)"
