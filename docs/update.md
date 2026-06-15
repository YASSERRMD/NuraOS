# NuraOS A/B Rootfs Update

NuraOS uses a dual-slot (A/B) rootfs strategy for safe over-the-air updates.
One slot runs the live OS; the other receives the update. If the updated slot
fails to boot, the system falls back to the previous slot automatically.

## Slot layout

```
/boot/
  rootfs-a.ext4    -- slot A rootfs image
  rootfs-b.ext4    -- slot B rootfs image
/data/etc/
  active-slot      -- contains 'a' or 'b'; read by the QEMU boot script
  update-state.json  -- update history and pending reboot status
```

The QEMU boot script (`scripts/run-qemu.sh`) reads `active-slot` and passes
the corresponding rootfs to the `-drive` flag.

## Checking the active slot

```sh
bash scripts/slot-select.sh
```

Other sub-commands:

```sh
bash scripts/slot-select.sh inactive      # print the inactive slot
bash scripts/slot-select.sh set b         # manually set active slot to b
bash scripts/slot-select.sh toggle        # switch to the other slot
```

## Installing an update

```sh
bash scripts/update.sh --url https://example.com/nuraos-rootfs.ext4 \
    --sha256 <hex>
```

Steps performed:
1. Detect the inactive slot (opposite of `active-slot`).
2. Download the image to a temp staging file.
3. Verify SHA-256 if `--sha256` is provided.
4. Copy staging to `/boot/rootfs-<inactive>.ext4`.
5. Write `/data/etc/update-state.json` with `last_result: pending_reboot`.

The active slot is NOT changed yet. After verifying the update is staged:

```sh
bash scripts/slot-select.sh set <inactive-slot>
reboot
```

### Using a local image

```sh
bash scripts/update.sh --local /path/to/nuraos-rootfs.ext4
```

### Dry run

```sh
bash scripts/update.sh --url https://example.com/nuraos-rootfs.ext4 --dry-run
```

Prints the plan without downloading or writing anything.

## Rolling back

If the updated slot fails to boot or behaves incorrectly, revert:

```sh
bash scripts/update.sh --rollback
reboot
```

This switches `active-slot` back to the previously active slot and records
`last_result: rolled_back` in the state file.

## Update state file

`/data/etc/update-state.json` schema:

```json
{
  "active_slot":   "a",
  "pending_slot":  "b",
  "last_update":   "2026-01-01T00:00:00Z",
  "last_result":   "pending_reboot",
  "boot_attempts": 0
}
```

`last_result` values:

| Value | Meaning |
|---|---|
| `pending_reboot` | Update staged; waiting for reboot to activate |
| `rolled_back` | Slot was reverted to the previous one |
| `success` | Update activated and boot confirmed healthy |

## Gateway endpoint

```
GET /update/status
```

Returns the current slot state and update-state file contents:

```json
{
  "active_slot":   "a",
  "inactive_slot": "b",
  "update_state": {
    "active_slot":   "a",
    "pending_slot":  "b",
    "last_update":   "2026-01-01T00:00:00Z",
    "last_result":   "pending_reboot",
    "boot_attempts": 0
  }
}
```

`update_state` is `null` when no update has been performed yet.

Override paths via environment variables:

```sh
ACTIVE_SLOT_FILE=/custom/active-slot UPDATE_STATE=/custom/state.json ./gateway
```

## Boot confirmation

After booting into the new slot, mark the update successful by writing the
state file directly:

```sh
jq '.last_result = "success" | .pending_slot = null | .boot_attempts = 0' \
    /data/etc/update-state.json > /tmp/s.json && \
    mv /tmp/s.json /data/etc/update-state.json
```

A future phase will add automatic boot counting and watchdog-triggered
rollback when `boot_attempts` exceeds a threshold.
