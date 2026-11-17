#!/bin/sh
# One-time setup of the backend server, run as root. Safe to re-run.
#
# Creates the `deploy` user that Ansible connects as, lets it run Docker, and
# hands it the app directory. Every argument is an SSH public key to authorize
# for `deploy` (the CI deploy key, plus your own for manual runs):
#
#   ssh root@tx.griffin-pantone.ts.net "sh -s -- '$(cat ci_deploy.pub)' '$(cat ~/.ssh/id_ed25519.pub)'" \
#     < deploy/bootstrap-server.sh
set -eu

# Must match APP_UID/APP_GID in the Dockerfile and app_uid/app_gid in Ansible
DEPLOY_UID=10001
DEPLOY_GID=10001
APP_DIR=/opt/tunnel-manager

if [ "$(id -u)" -ne 0 ]; then
	echo "Run this as root" >&2
	exit 1
fi
if [ "$#" -eq 0 ]; then
	echo "Pass at least one SSH public key to authorize for deploy" >&2
	exit 1
fi
docker compose version >/dev/null

getent group deploy >/dev/null || groupadd --gid "$DEPLOY_GID" deploy
id deploy >/dev/null 2>&1 || useradd --create-home --uid "$DEPLOY_UID" --gid deploy --shell /bin/bash deploy
if [ "$(id -u deploy)" != "$DEPLOY_UID" ] || [ "$(id -g deploy)" != "$DEPLOY_GID" ]; then
	echo "User deploy exists with a different UID/GID than $DEPLOY_UID:$DEPLOY_GID" >&2
	exit 1
fi
# Docker access is root-equivalent; deploy exists so CI never logs in as root
usermod --append --groups docker deploy

home=$(getent passwd deploy | cut -d: -f6)
install -d -m 0700 -o deploy -g deploy "$home/.ssh"
keys="$home/.ssh/authorized_keys"
touch "$keys"
for key in "$@"; do
	grep -qxF "$key" "$keys" || printf '%s\n' "$key" >>"$keys"
done
chown deploy:deploy "$keys"
chmod 0600 "$keys"

# Only the directory itself (and the pre-Ansible compose file, which the first
# deploy renames) change owner; older files inside stay as they are
install -d -m 0750 "$APP_DIR"
chown deploy:deploy "$APP_DIR"
chmod 0750 "$APP_DIR"
if [ -f "$APP_DIR/docker-compose.yml" ]; then
	chown deploy:deploy "$APP_DIR/docker-compose.yml"
fi

echo "deploy user ready: uid=$(id -u deploy) groups=$(id -Gn deploy)"
