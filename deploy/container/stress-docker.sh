#!/usr/bin/env bash
# Prove the bounded engine container protects its host (Linux, docker).
# Starts a throwaway stack (own network, volumes, port, no ~/.commander),
# runs a bounded fork bomb and a memory hog as a stray uid inside the cmdrd
# container, and samples host load, MemAvailable and two API probes (the
# container's and the live cmdrd's, unauthenticated: 401 = answering) every
# second. Output: $OUT/{samples.csv,summary.txt}. Never touches the live cmdrd.
#
#   CMDR_IMAGE=commander-fngn10:test CMDR_PORT=18931 deploy/container/stress-docker.sh
set -uo pipefail
cd "$(dirname "$0")/../.."
N=${CMDR_STRESS_NAME:-cmdrstress}; PORT=${CMDR_PORT:-18931}; I=${CMDR_IMAGE:-commander-engine:latest}
LIVE=${LIVE_URL:-http://127.0.0.1:8787}; BOMB_S=${BOMB_S:-20}; HOG_S=${HOG_S:-40}
OUT=${OUT:-$(mktemp -d)}; mkdir -p "$OUT"
[[ $PORT == 8787 ]] && { echo "refusing the live port" >&2; exit 2; }
CT=$N-cmdrd; PG=$N-postgres

up() {
  docker network create "$N-net" >/dev/null 2>&1 || true
  docker volume create "$N-home" >/dev/null; docker volume create "$N-pg" >/dev/null
  docker run --rm --user 0 --entrypoint bash -v "$N-pg:/pg" -v "$N-home:/h" "$I" -c "
    set -e; mkdir -p /pg/data /h/.commander
    [ -s /pg/password ] || head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n' > /pg/password
    chmod 600 /pg/password
    printf 'postgres://commander:%s@$PG:5432/commander?sslmode=disable\n' \"\$(cat /pg/password)\" > /h/.commander/state.postgres
    chown -R 1000:1000 /h; chmod 700 /h/.commander; chmod 600 /h/.commander/state.postgres
    chown 999:999 /pg/data /pg/password; chmod 700 /pg/data" || return 1
  docker rm -f "$CT" "$PG" >/dev/null 2>&1
  docker run -d --name "$PG" --network "$N-net" --pids-limit 256 --memory 1g --memory-swap 1g \
    -v "$N-pg:/pg" -e PGDATA=/pg/data -e POSTGRES_USER=commander -e POSTGRES_DB=commander \
    -e POSTGRES_PASSWORD_FILE=/pg/password postgres:17 >/dev/null
  sleep 8
  docker run -d --init --name "$CT" --network "$N-net" -p "127.0.0.1:$PORT:8787" --restart unless-stopped \
    --cpus 2 --memory 3g --memory-swap 3g --pids-limit 1024 --read-only --tmpfs /tmp:size=256m \
    --cap-drop ALL --security-opt no-new-privileges -v "$N-home:/home/commander" "$I" >/dev/null
  sleep 10
}
down() { docker rm -f "$CT" "$PG" >/dev/null 2>&1; docker volume rm "$N-home" "$N-pg" >/dev/null 2>&1; docker network rm "$N-net" >/dev/null 2>&1; }
[[ ${1:-} == --down ]] && { down; exit 0; }

code() { curl -s -o /dev/null -m 3 -w '%{http_code}' "$1/api/snapshot" 2>/dev/null; }
CID_FILE=$OUT/cid
sampler() {
  echo "t,phase,load1,mem_avail_mb,live_api,ctr_api,ctr_pids,ctr_mem_mb" > "$OUT/samples.csv"
  while [[ -e $OUT/run ]]; do
    cg=/sys/fs/cgroup/system.slice/docker-$(cat "$CID_FILE").scope
    echo "$(date +%T),$(cat "$OUT/phase"),$(cut -d' ' -f1 /proc/loadavg),$(awk '/MemAvailable/{print int($2/1024)}' /proc/meminfo),$(code "$LIVE"),$(code "http://127.0.0.1:$PORT"),$(cat $cg/pids.current 2>/dev/null),$(( $(cat $cg/memory.current 2>/dev/null || echo 0) / 1048576 ))" >> "$OUT/samples.csv"
    sleep 1
  done
}

up || { echo "stack failed"; exit 1; }
docker inspect -f '{{.Id}}' "$CT" > "$CID_FILE"
{
  echo "limits: $(docker inspect "$CT" --format 'cpus={{.HostConfig.NanoCpus}} mem={{.HostConfig.Memory}} swap={{.HostConfig.MemorySwap}} pids={{.HostConfig.PidsLimit}} readonly={{.HostConfig.ReadonlyRootfs}}')"
  cg=/sys/fs/cgroup/system.slice/docker-$(cat "$CID_FILE").scope
  echo "cgroup: pids.max=$(cat $cg/pids.max) memory.max=$(cat $cg/memory.max) swap.max=$(cat $cg/memory.swap.max) cpu.max=$(cat $cg/cpu.max)"
  echo "start-restarts: $(docker inspect -f '{{.RestartCount}}' "$CT")"
} > "$OUT/summary.txt"
touch "$OUT/run"; echo before > "$OUT/phase"; sampler & SP=$!
sleep 10

echo forkbomb > "$OUT/phase"
# uid 1001 is a stray user: cleanup kills only its processes, never cmdrd (uid 1000).
docker exec -u 1001 "$CT" bash -c "timeout -s KILL $BOMB_S bash -c ':(){ :|:& };:'" >/dev/null 2>&1 &
sleep $((BOMB_S + 2))
echo "bomb pids.peak: $(cat /sys/fs/cgroup/system.slice/docker-$(cat "$CID_FILE").scope/pids.peak 2>/dev/null)" >> "$OUT/summary.txt"
echo "bomb pids.events: $(cat /sys/fs/cgroup/system.slice/docker-$(cat "$CID_FILE").scope/pids.events 2>/dev/null)" >> "$OUT/summary.txt"
docker exec -u 1001 "$CT" bash -c 'kill -9 -1' >/dev/null 2>&1
echo after-bomb > "$OUT/phase"; sleep 10

echo memhog > "$OUT/phase"
docker exec -u 1001 "$CT" timeout -s KILL "$HOG_S" node -e 'const a=[];for(;;){a.push(Buffer.alloc(64*1048576,1));}' >"$OUT/hog.txt" 2>&1
echo "hog exit=$? (137 = SIGKILL: OOM killer or timeout)" >> "$OUT/summary.txt"
echo "memory.events: $(tr '\n' ' ' < /sys/fs/cgroup/system.slice/docker-$(cat "$CID_FILE").scope/memory.events)" >> "$OUT/summary.txt"
echo "dmesg oom: $(docker inspect -f 'OOMKilled={{.State.OOMKilled}}' "$CT") $(dmesg 2>/dev/null | grep -c 'Memory cgroup out of memory' || true)" >> "$OUT/summary.txt"
echo after-hog > "$OUT/phase"; sleep 15
echo "end: status=$(docker inspect -f '{{.State.Status}} restarts={{.RestartCount}} oom={{.State.OOMKilled}}' "$CT") api=$(code http://127.0.0.1:$PORT) live=$(code "$LIVE")" >> "$OUT/summary.txt"
rm -f "$OUT/run"; wait $SP 2>/dev/null
cat "$OUT/summary.txt"; echo "samples: $OUT/samples.csv"
