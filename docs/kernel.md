# Linux Kernel

## Chosen LTS line: 6.6.y

NuraOS pins the Linux 6.6 LTS branch (tag v6.6.87).

### Rationale

**Stability window.** The 6.6 branch is maintained through December 2026,
giving us a multi-year window for security backports without chasing mainline
churn. For an embedded appliance that needs infrequent, predictable maintenance
cycles this is preferable to a short-lived stable series.

**Driver maturity.** virtio device support (console, block, net) that NuraOS
relies on is stable and well-tested in 6.6. There are no regressions in the
virtio paths for x86-64 QEMU targets.

**Upstream BTF and eBPF tooling.** While NuraOS does not currently use eBPF,
6.6 ships BTF-enabled kernel objects out of the box. This leaves the option
open for future observability tooling without a kernel rebuild.

**Compared to alternatives:**
- 5.15 (LTS until 2026-12): older driver stack, missing some virtio-serial
  improvements that simplify the serial console setup.
- 6.1 (LTS until 2028-01): viable alternative; 6.6 preferred for better
  BPF-assisted tracing primitives.
- 6.12 (LTS candidate): too new; documentation and backport rhythm not yet
  established.

## Source

The authoritative source is the kernel.org stable git repository:
```
https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git
```

Signed tarballs from `cdn.kernel.org` are used for the fetch to avoid
cloning the full git history. Signatures are verified against the kernel
developer PGP keyring (Linus Torvalds, Greg KH).

See [kernel/PINNED.md](../kernel/PINNED.md) for the exact tag and verification record.
See [docs/toolchain.md](toolchain.md) for the full version manifest.

## Config approach

The configuration starts from `tinyconfig` (the smallest possible valid config)
and adds exactly what NuraOS needs. Loadable modules are disabled; everything
is compiled in. This minimises attack surface and eliminates the module loader.

Config fragment: [kernel/configs/nuraos_x86_64_defconfig](../kernel/configs/nuraos_x86_64_defconfig)

### Enabled options

| Option                        | Reason                                      |
|-------------------------------|---------------------------------------------|
| CONFIG_64BIT                  | x86-64 target                               |
| CONFIG_BINFMT_ELF             | run ELF binaries (BusyBox, agent, server)   |
| CONFIG_PROC_FS / SYSFS        | /proc and /sys required by userland         |
| CONFIG_TMPFS                  | /tmp, /dev/shm                              |
| CONFIG_DEVTMPFS               | automatic device node creation              |
| CONFIG_EXT4_FS                | /data persistent partition                  |
| CONFIG_BLK_DEV_INITRD         | load initramfs from kernel command line     |
| CONFIG_SERIAL_8250_CONSOLE    | ttyS0 serial console in QEMU               |
| CONFIG_VIRTIO_PCI / BLK / NET | virtio block (/data) and network in QEMU   |
| CONFIG_VIRTIO_CONSOLE         | virtio serial console alternative          |
| CONFIG_INET                   | TCP/IP for agent HTTP API and providers    |

### Disabled options

| Option             | Reason                                           |
|--------------------|--------------------------------------------------|
| CONFIG_MODULES     | no module loader; everything built in            |
| CONFIG_VT          | no virtual terminal; serial only                 |
| CONFIG_DEBUG_KERNEL | strip debug overhead from the appliance image   |
| CONFIG_IPV6        | not needed yet; reduces surface area            |
| CONFIG_NETFILTER   | no firewall needed at this stage                |
| CONFIG_SCSI        | virtio-blk does not need SCSI layer             |

## Hardening options (Phase 40)

The following security hardening options are added in the Phase 40 pass.
They are part of the same config fragment and require no separate step.

| Option                       | Effect                                                         |
|------------------------------|----------------------------------------------------------------|
| STRICT_KERNEL_RWX            | Kernel text is R-X, data is RW- (no W+X pages)               |
| RANDOMIZE_BASE + MEMORY      | KASLR: randomize kernel and physical-memory load addresses     |
| STACKPROTECTOR_STRONG        | Per-function stack canary via GCC `-fstack-protector-strong`  |
| FORTIFY_SOURCE               | Replace unsafe C string ops with bounds-checked versions       |
| SLAB_FREELIST_HARDENED       | Randomize slab freelist; detect double-free                    |
| HARDENED_USERCOPY            | Validate every kernel-to/from-userspace copy size             |
| INIT_ON_ALLOC_DEFAULT_ON     | Zero-initialize all kernel allocations (mitigates info leaks) |
| SHUFFLE_PAGE_ALLOCATOR       | Randomize page-allocator freelist order                        |
| RETPOLINE + CPU_IBPB_ENTRY   | Spectre v2 mitigation via retpoline and IBPB entry barriers   |
| SECCOMP + SECCOMP_FILTER     | Allow processes to install BPF syscall filters                |
| PROC_KCORE=n                 | Disable /proc/kcore (no raw physical memory access)           |
| SECURITY_DMESG_RESTRICT      | Restrict dmesg to CAP_SYSLOG / privileged processes           |
| USER_NS=n                    | Disable user namespaces (no container workloads)              |
| BUG_ON_DATA_CORRUPTION       | Kernel panic on detected data corruption                      |

### Hardening trade-offs

- **INIT_ON_ALLOC_DEFAULT_ON** adds ~1-3% overhead on allocation-heavy paths.
  Acceptable for an appliance running a single-tenant workload.
- **KASLR** requires a small entropy source at boot; QEMU provides sufficient
  entropy via the virtio-rng device (add `-device virtio-rng-pci` to QEMU args
  for production use; not required in the current run-qemu.sh test config).
- **USER_NS=n** prevents unprivileged namespace abuse (CVE-2020-8835 class).
  If a future phase needs containers, re-enable and add seccomp profiles.
- **SECCOMP_FILTER** is needed for the gateway process in a future seccomp
  profile (Phase 41). Having it in the kernel now costs nothing.

## Build

```sh
./scripts/fetch-kernel.sh    # Download and verify
./scripts/kernel-config.sh   # Apply NuraOS defconfig (includes hardening)
./scripts/build-kernel.sh    # Produce bzImage
```

## bzImage size

Recorded here after each build. Target: below 6 MB for the initial tinyconfig
baseline.

| Phase | Config           | Size (bzImage) | Notes               |
|-------|------------------|----------------|---------------------|
| 04    | nuraos_x86_64    | (TBD)          | first build attempt |
