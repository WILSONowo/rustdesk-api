#!/bin/sh
set -eu

deploy_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
domain=${DOMAIN:?Set DOMAIN to your public API hostname}
case "$domain" in *[!a-zA-Z0-9.-]*|.*|-*|*..*|*.) printf '%s\n' 'Invalid DOMAIN' >&2; exit 1;; esac
challenge_root=/var/www/rustdesk-acme
probe_path=/.well-known/acme-challenge/rustdesk-origin-check
install -d -m 0755 "$challenge_root/.well-known/acme-challenge"
expected=$(cat /proc/sys/kernel/random/uuid)
printf '%s' "$expected" > "$challenge_root$probe_path"
chmod 0644 "$challenge_root$probe_path"
actual=$(curl --fail --silent --show-error --location --connect-timeout 10 --max-time 30 "http://$domain$probe_path")
if [ "$actual" != "$expected" ]; then
    printf '%s\n' 'Public ACME probe did not reach this origin. Check DNS and EdgeOne HTTP:80 challenge routing.' >&2
    exit 1
fi

certbot certonly --webroot -w "$challenge_root" -d "$domain" \
    --non-interactive --agree-tos --register-unsafely-without-email --keep-until-expiring

config=/etc/nginx/conf.d/rustdesk-accounts.conf
if [ -f "$config" ]; then cp -p "$config" "$config.before-https"; fi
sed "s/remote\.example\.com/$domain/g" "$deploy_dir/nginx.edgeone.conf.example" > "$config"
if ! nginx -t; then
    if [ -f "$config.before-https" ]; then mv "$config.before-https" "$config"; else rm -f "$config"; fi
    exit 1
fi
systemctl reload nginx
install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/reload-nginx <<'HOOK'
#!/bin/sh
set -eu
nginx -t
systemctl reload nginx
HOOK
chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/reload-nginx
systemctl enable --now certbot.timer
curl --fail --silent --show-error --resolve "$domain:443:127.0.0.1" "https://$domain/api/version"
printf '\nTesting automatic renewal...\n'
certbot renew --dry-run
