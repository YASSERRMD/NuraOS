# NuraOS Userland

All userland binaries in NuraOS are compiled as fully static executables linked
against musl libc. No dynamic linker is present in the initramfs.

## Toolchain

| Component  | Version | Purpose                                      |
|------------|---------|----------------------------------------------|
| musl libc  | 1.2.5   | C standard library for static linking        |
| musl-gcc   | wrapper | GCC frontend that links against musl         |
| cc-musl.sh | n/a     | NuraOS canonical compile wrapper (always static) |

### Setup

```sh
./scripts/fetch-musl.sh      # download, build, install musl to third_party/musl-install/
./scripts/cc-musl.sh -O2 src.c -o out   # compile a static binary
```

### musl install layout

```
third_party/musl-install/
    bin/musl-gcc         musl-gcc wrapper (gcc front end pointing at musl headers/libs)
    lib/                 musl libc static archive and crt objects
    include/             musl C headers
```

The directory is gitignored (build output). Run `fetch-musl.sh` on each fresh checkout.

## Smoke test

A minimal hello-world binary verifies the toolchain:

```sh
./rootfs/tests/build-hello.sh
```

Expected output:
```
[build-hello] compiling hello.c ...
[build-hello] OK: binary is statically linked
[build-hello] binary: rootfs/tests/hello (N kB)
[build-hello] output: hello from NuraOS musl static build
[build-hello] smoke test PASSED.
```

The compiled binary is gitignored; only the source `rootfs/tests/hello.c` is
tracked.

## BusyBox

BusyBox is built in Phase 06. It provides sh, init, mount, ip, ps, and other
essential applets as a single static binary. See the applet list and binary size
recorded below after Phase 06 completes.

| Applet      | Purpose                               |
|-------------|---------------------------------------|
| sh          | POSIX shell for /init and scripts     |
| init        | PID 1 bootstrap                       |
| mount       | mount proc, sysfs, devtmpfs, /data    |
| ls, cat     | basic file inspection                 |
| ip          | network configuration                 |
| ping        | network connectivity check            |
| ps          | process listing                       |
| mkdir, ln   | filesystem setup in /init             |
| mknod       | device node creation                  |
| switch_root | pivot from initramfs to /data         |
| halt, poweroff | clean shutdown                     |
| udhcpc      | DHCP client for network bringup       |

BusyBox binary size: (recorded after Phase 06 build)
