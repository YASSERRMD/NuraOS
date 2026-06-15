# NuraOS Inference

NuraOS runs local inference via `llama-server`, the HTTP server bundled with
[llama.cpp](https://github.com/ggerganov/llama.cpp). The server starts
before the agent (see `rootfs/sbin/supervisor`) and listens on
`127.0.0.1:8081` inside the guest.

## Model selection

Models are stored in `/data/models/` as GGUF files and described by the
manifest at `/data/model.json`. The agent reads this manifest to discover the
active model path at startup.

Download a model with:

```sh
bash scripts/fetch-model.sh
```

To use a different model set `MODEL_URL` and `MODEL_NAME` before running:

```sh
MODEL_URL=https://huggingface.co/.../my-model-Q4_K_M.gguf \
MODEL_NAME=my-model-q4_k_m \
bash scripts/fetch-model.sh
```

## Model size vs RAM budget

The QEMU default is 512 MB. The table below shows peak RSS measured with
llama-server loaded but idle. Inference adds roughly 100 MB per concurrent
request at Q4_K_M quantisation.

| Model | Params | Quant | GGUF size | Peak RSS | Min RAM |
|-------|--------|-------|-----------|----------|---------|
| SmolLM2-1.7B-Instruct | 1.7B | Q4_K_M | ~1.0 GB | ~900 MB | 2 GB |
| Qwen2-0.5B-Instruct | 0.5B | Q4_K_M | ~400 MB | ~350 MB | 1 GB |
| Phi-3-mini-4k-instruct | 3.8B | Q4_K_M | ~2.2 GB | ~2.0 GB | 4 GB |
| Llama-3.2-1B-Instruct | 1.0B | Q4_K_M | ~660 MB | ~600 MB | 1.5 GB |

**Recommended for 2 GB QEMU**: SmolLM2-1.7B-Instruct Q4_K_M (the default in
`scripts/fetch-model.sh`).

**Recommended for testing on 1 GB**: Qwen2-0.5B Q4_K_M. Set env vars before
`fetch-model.sh` (see above).

## QEMU memory flag

```sh
bash scripts/run-qemu.sh --mem 2048   # 2 GB RAM
bash scripts/run-qemu.sh --mem 4096   # 4 GB RAM (needed for Phi-3-mini)
```

## llama.cpp build

Source is fetched at the pinned SHA from `scripts/VERSIONS.env`:

```sh
bash scripts/fetch-llama.sh   # clone at LLAMA_SHA
bash scripts/build-llama.sh   # cmake, CPU-only, static musl
```

Build flags used:
- `-DGGML_NATIVE=OFF` for reproducible cross-compilation
- `-DGGML_AVX=ON` baseline x86_64 SIMD (QEMU qemu64 CPU supports AVX)
- `-DGGML_AVX2=OFF` disabled for broadest guest CPU compatibility
- `-DBUILD_SHARED_LIBS=OFF` and `-DCMAKE_EXE_LINKER_FLAGS=-static` for musl
- Server binary installed to `rootfs/staging/sbin/llama-server`

## model.json schema

```json
{
  "name":                "smollm2-1.7b-instruct-q4_k_m",
  "path":                "/data/models/smollm2-1.7b-instruct-q4_k_m.gguf",
  "size_bytes":          1048576000,
  "size_mb":             1000,
  "quantization":        "Q4_K_M",
  "context_length":      2048,
  "parameters_billions": 1.7,
  "architecture":        "smollm2"
}
```

The agent reads `path` to pass to llama-server's `--model` flag and
`context_length` to cap the `max_tokens` budget per turn.

## llama-server startup flags

The supervisor starts llama-server with:

```sh
llama-server \
  --model "$(jq -r .path /data/model.json)" \
  --ctx-size "$(jq -r .context_length /data/model.json)" \
  --host 127.0.0.1 \
  --port 8081 \
  --log-disable
```

The agent waits for the health endpoint (`GET /health`) to return `200` before
serving its first turn.

## Quantisation guide

| Quant | Bits/weight | Quality | Size vs FP16 |
|-------|-------------|---------|--------------|
| Q4_K_M | 4.5 | Good for chat | ~28% |
| Q5_K_M | 5.5 | Better reasoning | ~34% |
| Q8_0 | 8.0 | Near-lossless | ~50% |
| F16 | 16.0 | Reference | 100% |

Q4_K_M is the recommended default for embedded and low-RAM scenarios.

## Model management

### List installed models

```sh
bash scripts/model-list.sh
```

Output shows all `.gguf` files in `/data/models` with their sizes.
The active model (from `/data/model.json`) is marked with `*`.

Override defaults:

```sh
bash scripts/model-list.sh --model-dir /custom/models --manifest /data/model.json
```

### Switch the active model

After downloading a second model with `fetch-model.sh`, activate it:

```sh
bash scripts/model-activate.sh qwen2-0.5b-instruct-q4_k_m \
    --quantization Q4_K_M \
    --context-length 2048 \
    --params-billions 0.5 \
    --architecture qwen2
```

The script writes a new `/data/model.json` manifest. Restart nura-agent to
load the new model (or send `SIGHUP` if hot-reload is configured).

Preview without writing:

```sh
bash scripts/model-activate.sh qwen2-0.5b-instruct-q4_k_m --dry-run
```

### Gateway model endpoint

```
GET /models
```

Returns the active model manifest and a list of discovered `.gguf` files:

```json
{
  "active": {
    "name": "smollm2-1.7b-instruct-q4_k_m",
    "path": "/data/models/smollm2-1.7b-instruct-q4_k_m.gguf",
    "size_bytes": 1048576000,
    "size_mb": 1000,
    "quantization": "Q4_K_M",
    "context_length": 2048,
    "parameters_billions": 1.7,
    "architecture": "smollm2"
  },
  "available": [
    {
      "name": "smollm2-1.7b-instruct-q4_k_m",
      "path": "/data/models/smollm2-1.7b-instruct-q4_k_m.gguf",
      "size_bytes": 1048576000,
      "size_mb": 1000
    },
    {
      "name": "qwen2-0.5b-instruct-q4_k_m",
      "path": "/data/models/qwen2-0.5b-instruct-q4_k_m.gguf",
      "size_bytes": 398000000,
      "size_mb": 379
    }
  ]
}
```

`active` is `null` when no manifest exists (fresh install before first
`fetch-model.sh` run). `available` is an empty array when the models
directory is missing or empty.

Override the paths read by the gateway:

```sh
MODEL_MANIFEST=/custom/model.json MODEL_DIR=/custom/models ./gateway
```

## Privacy and data residency

All inference runs on-device. No prompts, completions, or model weights leave
the machine unless the routing policy is set to `remote_first` or
`remote_only` in `agent.toml` and a cloud provider API key is configured.
See [config.md](config.md) for routing policy options.
