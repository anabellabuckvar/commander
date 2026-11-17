#!/usr/bin/env bash
# Run Commander's engine bounded: cmdrd beside its PostgreSQL, with CPU,
# memory, pids and disk limits. The API is published on the host's loopback
# only. Re-running replaces the containers and keeps the data.
#
#   deploy/container/run.sh              build the image and start it
#   deploy/container/run.sh --no-build   start from the image already built
#   deploy/container/run.sh --down       stop and remove the containers (data kept)
#
# Limits: CMDR_CPUS (2), CMDR_MEMORY (3g, no swap), CMDR_PIDS (1024 for cmdrd),
# CMDR_HOME_SIZE (20G), CMDR_PG_SIZE (10G); CMDR_PORT (8797) on 127.0.0.1.
#
# Engines:
# - Podman (macOS, or Linux with podman): one pod; CPU and memory cap the whole
#   pod; disk caps are XFS project quotas on the volumes, which need rootful
#   Podman (on macOS the Podman machine's root connection).
# - Docker (Linux without podman, or CMDR_ENGINE=docker): no pods, so cmdrd
#   and PostgreSQL share a small network-holder container; cmdrd gets
#   CMDR_CPUS/CMDR_MEMORY and PostgreSQL its own CMDR_PG_CPUS (1) and
#   CMDR_PG_MEMORY (1g). Disk caps are ext4 image files loop-mounted under
#   CMDR_DATA_DIR (needs sudo).
set -euo pipefail
cd "$(dirname "$0")/../.."

NAME=${CMDR_POD:-commander-bounded}
PORT=${CMDR_PORT:-8797}
CPUS=${CMDR_CPUS:-2}
MEMORY=${CMDR_MEMORY:-3g}
PIDS=${CMDR_PIDS:-1024}
HOME_SIZE=${CMDR_HOME_SIZE:-20G}
PG_SIZE=${CMDR_PG_SIZE:-10G}
IMAGE=${CMDR_IMAGE:-localhost/commander-engine:latest}
PG_IMAGE=${CMDR_PG_IMAGE:-docker.io/library/postgres:17}
[[ $PORT == 8787 ]] && { echo "refusing 8787: that is the live Commander's port" >&2; exit 2; }

ENGINE=${CMDR_ENGINE:-}
if [[ -z $ENGINE ]]; then
  if [[ -n ${CMDR_PODMAN:-} ]] || command -v podman >/dev/null 2>&1; then ENGINE=podman
  elif command -v docker >/dev/null 2>&1; then ENGINE=docker
  else echo "need podman or docker" >&2; exit 2; fi
fi

# The database password lives only in the data: generated once, kept in the
# pg data's secret file and in cmdrd's private state.postgres.
init_data() { # $1 = pg mount, $2 = home mount (as -v specs)
  "${CTR[@]}" run --rm --user 0 --entrypoint bash -v "$1" -v "$2" "$IMAGE" -c '
    set -e
    mkdir -p /pg/data /h/.commander
    if [ ! -s /pg/password ]; then head -c 24 /dev/urandom | od -An -tx1 | tr -d " \n" > /pg/password; fi
    chmod 600 /pg/password
    printf "postgres://commander:%s@127.0.0.1:5432/commander?sslmode=disable\n" "$(cat /pg/password)" > /h/.commander/state.postgres
    chown -R 1000:1000 /h
    chmod 700 /h/.commander; chmod 600 /h/.commander/state.postgres
    chown 999:999 /pg/data /pg/password; chmod 700 /pg/data'
}

wait_pg() {
  for _ in $(seq 60); do
    "${CTR[@]}" exec "$NAME-postgres" pg_isready -q -h 127.0.0.1 -U commander && return 0
    sleep 1
  done
  echo "PostgreSQL did not become ready" >&2; return 1
}

if [[ $ENGINE == podman ]]; then
  if [[ -n ${CMDR_PODMAN:-} ]]; then read -r -a CTR <<<"$CMDR_PODMAN"
  elif [[ $(uname -s) == Darwin ]]; then CTR=(podman --connection podman-machine-default-root)
  else CTR=(podman); fi

  if [[ ${1:-} == --down ]]; then
    "${CTR[@]}" pod rm -f "$NAME" >/dev/null 2>&1 || true
    echo "removed pod $NAME (volumes $NAME-home and $NAME-pg kept)"; exit 0
  fi
  [[ ${1:-} == --no-build ]] || "${CTR[@]}" build -t "$IMAGE" -f deploy/container/Containerfile .
  for v in "$NAME-home:$HOME_SIZE" "$NAME-pg:$PG_SIZE"; do
    "${CTR[@]}" volume exists "${v%%:*}" || "${CTR[@]}" volume create --opt o=size="${v##*:}" "${v%%:*}" >/dev/null
  done
  "${CTR[@]}" pod rm -f "$NAME" >/dev/null 2>&1 || true
  "${CTR[@]}" pod create --name "$NAME" --publish "127.0.0.1:$PORT:8787" \
    --cpus "$CPUS" --memory "$MEMORY" --memory-swap "$MEMORY" >/dev/null
  init_data "$NAME-pg:/pg" "$NAME-home:/h"
  "${CTR[@]}" run -d --pod "$NAME" --name "$NAME-postgres" --pids-limit 256 \
    -v "$NAME-pg:/pg" -e PGDATA=/pg/data -e POSTGRES_USER=commander -e POSTGRES_DB=commander \
    -e POSTGRES_PASSWORD_FILE=/pg/password "$PG_IMAGE" -c listen_addresses=127.0.0.1 >/dev/null
  wait_pg
  "${CTR[@]}" run -d --init --pod "$NAME" --name "$NAME-cmdrd" --pids-limit "$PIDS" \
    -v "$NAME-home:/home/commander" "$IMAGE" >/dev/null
  echo "Commander engine: http://127.0.0.1:$PORT (pod $NAME: ${CPUS} CPUs, ${MEMORY} memory, ${PIDS} pids, home ${HOME_SIZE}, db ${PG_SIZE})"
  echo "API token: ${CTR[*]} exec $NAME-cmdrd cat /home/commander/.commander/api-token"
  exit 0
fi

# Docker.
CTR=(docker)
DATA=${CMDR_DATA_DIR:-$HOME/.local/share/commander-bounded/$NAME}
PG_CPUS=${CMDR_PG_CPUS:-1}
PG_MEMORY=${CMDR_PG_MEMORY:-1g}

if [[ ${1:-} == --down ]]; then
  docker rm -f "$NAME-cmdrd" "$NAME-postgres" "$NAME-net" >/dev/null 2>&1 || true
  for d in home pg; do mountpoint -q "$DATA/$d" && sudo umount "$DATA/$d"; done
  echo "removed $NAME (data images in $DATA kept)"; exit 0
fi
[[ ${1:-} == --no-build ]] || docker build -t "$IMAGE" -f deploy/container/Containerfile .

# Each data area is an ext4 image of a fixed size, so it can never grow past it.
mkdir -p "$DATA"
for v in "home:$HOME_SIZE" "pg:$PG_SIZE"; do
  d=${v%%:*}; img=$DATA/$d.img
  if [[ ! -f $img ]]; then truncate -s "${v##*:}" "$img"; mkfs.ext4 -q -F "$img"; fi
  mkdir -p "$DATA/$d"
  mountpoint -q "$DATA/$d" || sudo mount -o loop "$img" "$DATA/$d"
done

docker rm -f "$NAME-cmdrd" "$NAME-postgres" "$NAME-net" >/dev/null 2>&1 || true
docker run -d --name "$NAME-net" --publish "127.0.0.1:$PORT:8787" \
  --cpus 0.1 --memory 32m --pids-limit 8 --entrypoint sleep "$IMAGE" infinity >/dev/null
init_data "$DATA/pg:/pg" "$DATA/home:/h"
docker run -d --name "$NAME-postgres" --network "container:$NAME-net" \
  --cpus "$PG_CPUS" --memory "$PG_MEMORY" --memory-swap "$PG_MEMORY" --pids-limit 256 \
  -v "$DATA/pg:/pg" -e PGDATA=/pg/data -e POSTGRES_USER=commander -e POSTGRES_DB=commander \
  -e POSTGRES_PASSWORD_FILE=/pg/password "$PG_IMAGE" -c listen_addresses=127.0.0.1 >/dev/null
wait_pg
docker run -d --init --name "$NAME-cmdrd" --network "container:$NAME-net" \
  --cpus "$CPUS" --memory "$MEMORY" --memory-swap "$MEMORY" --pids-limit "$PIDS" \
  -v "$DATA/home:/home/commander" ${CMDR_EXTRA_RUN_ARGS:-} "$IMAGE" >/dev/null
echo "Commander engine: http://127.0.0.1:$PORT ($NAME: cmdrd ${CPUS} CPUs, ${MEMORY} memory, ${PIDS} pids; postgres ${PG_CPUS} CPUs, ${PG_MEMORY}; home ${HOME_SIZE}, db ${PG_SIZE} in $DATA)"
echo "API token: docker exec $NAME-cmdrd cat /home/commander/.commander/api-token"
