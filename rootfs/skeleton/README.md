# rootfs/skeleton

This directory contains the base filesystem tree populated by build-initramfs.sh.

The script creates:
  /bin     busybox + applet symlinks
  /sbin    symlinks to busybox init, mount, etc.
  /etc     minimal config (hostname, fstab stub)
  /proc    mountpoint for procfs
  /sys     mountpoint for sysfs
  /dev     mountpoint for devtmpfs
  /data    mountpoint for persistent ext4 partition
  /tmp     tmpfs scratch space
  /init    shell script (PID 1 entry for the kernel)

None of the runtime filesystem contents are stored here; everything is
assembled at build time by scripts/build-initramfs.sh.
