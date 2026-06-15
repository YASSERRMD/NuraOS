# Suite T10 - storage

Verifies the NuraOS persistent data layer: artifact existence, ext4 mount
behaviour, and gateway accessibility to the models directory.

## Cases

| Case | Source | Pass condition |
|------|--------|----------------|
| `data-img-exists` | `image/out/data.img` | File present on host |
| `data-mount-check` | Fresh QEMU boot + serial | `/data mounted` seen in serial within 90 s; **skip** if data.img absent |
| `models-dir-accessible` | GET /models | 200 response (gateway reads `/data/models`) |

## Notes

`data-mount-check` boots a **separate** QEMU instance with `DataImage` set.
It does not reuse the pre-booted instance provided by the runner.

## Running

```
go run ./cmd/run-suite -- storage
```
