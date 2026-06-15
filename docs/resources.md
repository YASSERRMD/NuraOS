# NuraOS Resource Control

NuraOS uses Linux cgroup v2 (unified hierarchy) to enforce per-service CPU,
memory, and I/O limits. A service that exhausts its memory budget is OOM-killed
and restarted (per its unit restart policy) without affecting other services or
the service manager.

## Kernel configuration

The following options are enabled in `kernel/configs/nuraos_x86_64_defconfig`:

```
CONFIG_CGROUPS=y
CONFIG_CGROUP_SCHED=y
CONFIG_FAIR_GROUP_SCHED=y
CONFIG_CFS_BANDWIDTH=y
CONFIG_CGROUP_CPUACCT=y
CONFIG_CGROUP_FREEZER=y
CONFIG_MEMCG=y
CONFIG_BLKCG=y
CONFIG_CGROUP_PIDS=y
```

## Hierarchy

The init script mounts the unified cgroup v2 filesystem and enables the cpu,
memory, and io controllers at the root:

```sh
mount -t cgroup2 -o nosuid,nodev,noexec none /sys/fs/cgroup
printf "+cpu +memory +io" > /sys/fs/cgroup/cgroup.subtree_control
```

The service manager (`nura-manager`) creates the following layout at startup:

```
/sys/fs/cgroup/
  nura.slice/                           <- parent slice
    nura-agent.service/                 <- per-service leaf cgroup
    gateway.service/
    llama-server.service/
```

Each service cgroup is created before the service process starts. The process
PID is written to `cgroup.procs` immediately after fork.

## Per-service limits

Limits are declared in each unit's `[resources]` section:

```toml
[resources]
cpu_weight = 100    # proportional CPU weight (1-10000, default 100)
memory_max = "256M" # hard memory limit; "0" means unlimited
io_weight  = 0      # I/O weight (0 = unset; >0 enables io.weight)
```

| Field | cgroup v2 file | Format | Notes |
|-------|---------------|--------|-------|
| `cpu_weight` | `cpu.weight` | 1-10000 | Proportional scheduling weight |
| `memory_max` | `memory.max` | bytes or "max" | Hard limit; OOM-kills on breach |
| `io_weight` | `io.weight` | `default N` | I/O proportional weight; only written if > 0 |

Memory values accept human-readable suffixes (K, M, G) in unit files; they are
converted to byte counts before writing to `memory.max`.

### Current assignments

| Service | cpu_weight | memory_max | io_weight |
|---------|-----------|-----------|----------|
| nura-agent | 100 | 256 MiB | - |
| gateway | 100 | 128 MiB | - |
| llama-server | 200 | unlimited | - |

llama-server has `memory_max = "0"` (unlimited) because model inference memory
usage varies greatly with model size and quantisation.

## Memory pressure and OOM handling

When a service exceeds `memory.max`, the kernel OOM-kills one of its processes.
The service manager's OOM watcher goroutine polls `memory.events` every 5
seconds and logs an error event to the journal when `oom_kill` increases:

```
[ERROR] OOM kill detected in service cgroup service=nura-agent oom_kills=1 new_kills=1
```

The service is restarted according to its `[restart]` policy. Because the kill
is scoped to the service cgroup, other services and the service manager are
unaffected.

## Per-service resource metrics

The `/metrics` endpoint exposes cgroup stats in Prometheus format:

```
# HELP nura_cgroup_cpu_usage_seconds_total Total CPU time consumed by a service cgroup.
# TYPE nura_cgroup_cpu_usage_seconds_total counter
nura_cgroup_cpu_usage_seconds_total{service="nura-agent"} 12.345678
nura_cgroup_cpu_usage_seconds_total{service="gateway"} 0.123456

# HELP nura_cgroup_memory_bytes Current memory usage of a service cgroup in bytes.
# TYPE nura_cgroup_memory_bytes gauge
nura_cgroup_memory_bytes{service="nura-agent"} 52428800

# HELP nura_cgroup_memory_max_bytes Configured memory hard limit (0 = unlimited).
# TYPE nura_cgroup_memory_max_bytes gauge
nura_cgroup_memory_max_bytes{service="nura-agent"} 268435456

# HELP nura_cgroup_oom_kills_total Total OOM kills in a service cgroup.
# TYPE nura_cgroup_oom_kills_total counter
nura_cgroup_oom_kills_total{service="nura-agent"} 0
```

Data is read directly from `/sys/fs/cgroup/nura.slice/<service>.service/` on
each `/metrics` request. No polling goroutine is needed for reads.

## Adjusting limits at runtime

cgroup v2 limits can be updated without restarting the service:

```sh
# Increase llama-server memory limit to 8 GiB:
echo 8589934592 > /sys/fs/cgroup/nura.slice/llama-server.service/memory.max

# Raise gateway CPU weight:
echo 200 > /sys/fs/cgroup/nura.slice/gateway.service/cpu.weight
```

Changes take effect immediately but are not persisted. Update the unit TOML
file and restart the manager to make the change permanent.

## Graceful fallback

If the cgroup v2 filesystem is not mounted (e.g., kernel built without cgroup
support, or running in a restricted container), all cgroup operations log a
warning and continue. The services run without resource limits. The `/metrics`
endpoint omits the `nura_cgroup_*` families when no cgroup data is available.
