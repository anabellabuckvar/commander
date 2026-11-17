#!/usr/bin/env bash
# Run Commander's engine bounded: one pod holding cmdrd and its PostgreSQL,
# with CPU and memory limits on the pod, a pids limit per container and size
# limits on both data volumes. The API is published on the host's loopback
# only. Re-running replaces the containers and keeps the volumes.
#
#   deploy/container/run.sh              build the image and start the pod
#   deploy/container/run.sh --no-build   start from the image already built
#   deploy/container/run.sh --down       stop and remove the pod (volumes kept)
#
# Limits: CMDR_CPUS (2), CMDR_MEMORY (3g, no swap), CMDR_PIDS (1024 for cmdrd),
# CMDR_HOME_SIZE (20G), CMDR_PG_SIZE (10G); CMDR_PORT (8797) on 127.0.0.1.
set -euo pipefail
cd "$(dirname "$0")/../.."

# Disk limits use XFS project quotas, which only rootful Podman can set. On
# macOS that is the Podman machine's root connection; on Linux run it as root
# or point CMDR_PODMAN at a rootful Podman.
if [[ -n ${CMDR_PODMAN:-} ]]; then
  read -r -a PODMAN <<<"$CMDR_PODMAN"
elif [[ $(uname -s) == Darwin ]]; then
  PODMAN=(podman --connection podman-machine-default-root)
else
  PODMAN=(podman)
fi

NAME=${CMDR_POD:-commander-bounded}
PORT=${CMDR_PORT:-8797}
CPUS=${CMDR_CPUS:-2}
MEMORY=${CMDR_MEMORY:-3g}
PIDS=${CMDR_PIDS:-1024}
HOME_SIZE=${CMDR_HOME_SIZE:-20G}
PG_SIZE=${CMDR_PG_SIZE:-10G}
IMAGE=${CMDR_IMAGE:-localhost/commander-engine:latest}
PG_IMAGE=${CMDR_PG_IMAGE:-docker.io/library/postgres:17}

if [[ ${1:-} == --down ]]; then
  "${PODMAN[@]}" pod rm -f "$NAME" >/dev/null 2>&1 || true
  echo "removed pod $NAME (volumes $NAME-home and $NAME-pg kept)"
  exit 0
fi
if [[ ${1:-} != --no-build ]]; then
  "${PODMAN[@]}" build -t "$IMAGE" -f deploy/container/Containerfile .
fi

for v in "$NAME-home:$HOME_SIZE" "$NAME-pg:$PG_SIZE"; do
  if ! "${PODMAN[@]}" volume exists "${v%%:*}"; then
    "${PODMAN[@]}" volume create --opt o=size="${v##*:}" "${v%%:*}" >/dev/null
  fi
done

"${PODMAN[@]}" pod rm -f "$NAME" >/dev/null 2>&1 || true
"${PODMAN[@]}" pod create --name "$NAME" --publish "127.0.0.1:$PORT:8787" \
  --cpus "$CPUS" --memory "$MEMORY" --memory-swap "$MEMORY" >/dev/null

# The database password lives only inside the volumes: generated once, kept
# in the pg volume's secret file and in cmdrd's private state.postgres.
"${PODMAN[@]}" run --rm --user 0 --entrypoint bash -v "$NAME-pg:/pg" -v "$NAME-home:/h" "$IMAGE" -c '
  set -e
  mkdir -p /pg/data /h/.commander
  if [ ! -s /pg/password ]; then head -c 24 /dev/urandom | od -An -tx1 | tr -d " \n" > /pg/password; fi
  chmod 600 /pg/password
  printf "postgres://commander:%s@127.0.0.1:5432/commander?sslmode=disable\n" "$(cat /pg/password)" > /h/.commander/state.postgres
  chown -R 1000:1000 /h
  chmod 700 /h/.commander; chmod 600 /h/.commander/state.postgres
  chown 999:999 /pg/data /pg/password; chmod 700 /pg/data'

"${PODMAN[@]}" run -d --pod "$NAME" --name "$NAME-postgres" --pids-limit 256 \
  -v "$NAME-pg:/pg" -e PGDATA=/pg/data -e POSTGRES_USER=commander -e POSTGRES_DB=commander \
  -e POSTGRES_PASSWORD_FILE=/pg/password "$PG_IMAGE" -c listen_addresses=127.0.0.1 >/dev/null

for _ in $(seq 60); do
  "${PODMAN[@]}" exec "$NAME-postgres" pg_isready -q -h 127.0.0.1 -U commander && break
  sleep 1
done

"${PODMAN[@]}" run -d --init --pod "$NAME" --name "$NAME-cmdrd" --pids-limit "$PIDS" \
  -v "$NAME-home:/home/commander" "$IMAGE" >/dev/null

echo "Commander engine: http://127.0.0.1:$PORT (pod $NAME: ${CPUS} CPUs, ${MEMORY} memory, ${PIDS} pids, home ${HOME_SIZE}, db ${PG_SIZE})"
echo "API token: ${PODMAN[*]} exec $NAME-cmdrd cat /home/commander/.commander/api-token"
