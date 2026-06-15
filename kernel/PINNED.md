# Kernel Pin Record

This file is written by `scripts/fetch-kernel.sh` after a successful fetch and
verification. Commit the updated file after each kernel upgrade.

| Field        | Value                                                             |
|--------------|-------------------------------------------------------------------|
| Tag          | v6.6.87                                                           |
| Version      | 6.6.87                                                            |
| Tarball      | linux-6.6.87.tar.xz                                               |
| URL          | https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.6.87.tar.xz |
| SHA256       | (populated by fetch-kernel.sh)                                    |
| GPG verify   | (populated by fetch-kernel.sh)                                    |
| Fetched      | (populated by fetch-kernel.sh)                                    |

## Signature file

https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.6.87.tar.sign

## Notes

To re-verify manually after fetch:
```
xz -cd kernel/_download/linux-6.6.87.tar.xz | gpg --verify kernel/_download/linux-6.6.87.tar.sign -
```

To re-fetch from scratch:
```
./scripts/fetch-kernel.sh --force
```
