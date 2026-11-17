# Bounded container: fork bomb and memory hog (Linux, docker)

Run with `deploy/container/stress-docker.sh` (throwaway stack: own network,
volumes and port 18931; never the live cmdrd or ~/.commander). A bounded fork
bomb (20 s) and a memory hog (node allocating 64 MiB buffers, 40 s) run as a
stray uid inside the cmdrd container (2 CPUs, 3 GiB, no swap, 1024 pids,
read-only root). Host load, MemAvailable and the API of both the container and
the live cmdrd (unauthenticated probe, 401 = answering) are sampled every second.

## Result (2026-11-17, 16-core host, load already ~12 from other agents)

```
limits: cpus=2000000000 mem=3221225472 swap=3221225472 pids=1024 readonly=true
cgroup: pids.max=1024 memory.max=3221225472 swap.max=0 cpu.max=200000 100000
start-restarts: 0
bomb pids.peak: 1024
bomb pids.events: max 8627
hog exit=137 (137 = SIGKILL: OOM killer or timeout)
memory.events: low 0 high 0 max 24 oom 1 oom_kill 1 oom_group_kill 0 
dmesg oom: OOMKilled=true 1
end: status=running restarts=0 oom=true api=401 live=401
```

- 62 samples: the live cmdrd and the container's cmdrd answered every time (0 non-401).
- Host MemAvailable never fell below 50.3 GB (baseline ~52.9 GB).
- Host load1 baseline 12.2; peak 78 during the bomb's aftermath (about 1000 runnable
  tasks counted by loadavg, but held to the 2-CPU quota); back to ~52 and falling
  within 30 s. loadavg is a task count, not CPU use.
- Fork bomb: container pids peaked at 1024 (`pids.max`), 8,627 forks refused.
- Memory hog: `oom_kill 1`, `OOMKilled=true`, the hog exited 137; cmdrd was not killed;
  restarts 0, no host swap used (swap.max 0).

## Finding fixed: run with an init process

The first run (no `--init`) left the bomb's ~1015 zombies parented to cmdrd (pid 1,
which does not reap), so pids stayed at 1024 after the bomb ended and the hog could
not even fork. A `docker restart` recovered it (pids 1024 -> 9). With `--init` the
zombies are reaped as soon as the bomb is killed (pids 10 afterwards). `run.sh` and
the stress script now pass `--init`.
