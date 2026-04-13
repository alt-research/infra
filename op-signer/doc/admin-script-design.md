# op-signer Admin Script Design

## Goal

Create a shell script to be added to the op-signer Docker image, used inside the `op-remote-signer-admin-client` Kubernetes pod. The script simplifies batch registration and removal of op-signer config entries for rollup components (sequencer, batcher, proposer, challenger-resolver).

## Background & Context

### Where this script lives

- **Source repo**: `alt-research/infra`, branch `op-signer`
- **Path**: `op-signer/Dockerfile` builds the op-signer image
- **Dockerfile** (current):
  ```dockerfile
  FROM dhi.io/golang:1.26-alpine3.23-dev AS builder
  COPY ./op-signer /app
  WORKDIR /app
  RUN apk --no-cache add make jq bash git alpine-sdk
  RUN make build

  FROM dhi.io/alpine-base:3.23
  WORKDIR /app
  COPY --from=builder /app/bin/op-signer /app/
  RUN ["/app/op-signer", "--help"]
  ENTRYPOINT ["/app/op-signer"]
  ```
- The script should be COPY'd into the image (e.g., `/app/admin.sh`)
- The base image is Alpine, so the script should use `#!/bin/sh` or `#!/bin/bash` (bash is available in builder stage but may not be in final stage — verify `alpine-base:3.23`)

### How it's used

- Users SSH into the `op-remote-signer-admin-client-xxxxxx` pod (namespace `op-remote-signer`)
- They manually run `/app/op-signer admin ...` commands to register/remove signing keys
- This script automates the repetitive parts of that workflow
- The script is **interactively invoked** by the operator, not run as an entrypoint or init container

### Related project

- **op-remote-signer repo**: `alt-research/op-remote-signer` (on GitHub)
  - Contains: docker-compose.yml, nginx config, vault setup, cert-sync, scripts, README
  - README documents the full usage workflow including the admin commands this script automates

## Script Specification

### Input Parameters

| Parameter | Source | Description |
|-----------|--------|-------------|
| `--password` | CLI arg or `ADMIN_PASSWORD` env var | Admin password for `--admin.password`. **Must never be printed in command preview.** |
| `--network` | CLI arg (required) | `testnet` or `mainnet`. Maps to `--parent-chain-id`: testnet=`11155111`, mainnet=`1` |
| `--namespace` | CLI arg (required) | String identifier (e.g., `op-demo-testnet`). Used in `--allowed-client-cn` and `--path` |

### Commands

The script accepts a single positional command argument:

#### add-config subcommands

| Subcommand | Description |
|------------|-------------|
| `add-sequencer` | Register sequencer config |
| `add-batcher` | Register batcher config |
| `add-proposer` | Register proposer config |
| `add-challenger-resolver` | Register challenger-resolver config |
| `add-main` | Run add-batcher + add-proposer + add-challenger-resolver |
| `add-all` | Run add-sequencer + add-batcher + add-proposer + add-challenger-resolver |

#### remove-config subcommands

| Subcommand | Description |
|------------|-------------|
| `remove-sequencer` | Remove sequencer config |
| `remove-batcher` | Remove batcher config |
| `remove-proposer` | Remove proposer config |
| `remove-challenger-resolver` | Remove challenger-resolver config |
| `remove-all` | Run all four remove-* commands |

### Exact Commands Generated

#### add-config commands

**add-sequencer:**
```bash
/app/op-signer admin --admin.password <password> add-config \
  --parent-chain-id <network> \
  --allowed-client-cn "<namespace>-seq-sequencer" \
  --path "<namespace>-operator-keys/sequencer-prikey"
```

**add-batcher:**
```bash
/app/op-signer admin --admin.password <password> add-config \
  --parent-chain-id <network> \
  --allowed-client-cn "<namespace>-seq-batcher" \
  --path "<namespace>-operator-keys/batcher-prikey"
```

**add-proposer:**
```bash
/app/op-signer admin --admin.password <password> add-config \
  --parent-chain-id <network> \
  --allowed-client-cn "<namespace>-seq-proposer" \
  --path "<namespace>-operator-keys/proposer-prikey"
```

**add-challenger-resolver:**
```bash
/app/op-signer admin --admin.password <password> add-config \
  --parent-chain-id <network> \
  --allowed-client-cn "<namespace>-seq-challenger" \
  --path "<namespace>-operator-keys/challenger-resolver-prikey"
```

#### remove-config commands

**remove-sequencer:**
```bash
/app/op-signer admin --admin.password <password> remove-config-by-path \
  --path "<namespace>-operator-keys/sequencer-prikey"
```

**remove-batcher:**
```bash
/app/op-signer admin --admin.password <password> remove-config-by-path \
  --path "<namespace>-operator-keys/batcher-prikey"
```

**remove-proposer:**
```bash
/app/op-signer admin --admin.password <password> remove-config-by-path \
  --path "<namespace>-operator-keys/proposer-prikey"
```

**remove-challenger-resolver:**
```bash
/app/op-signer admin --admin.password <password> remove-config-by-path \
  --path "<namespace>-operator-keys/challenger-resolver-prikey"
```

### Behavior Requirements

1. **Dry-run first**: The script must print the commands it will execute **before** running them. The password must be masked in the printed output (e.g., shown as `****` or `[REDACTED]`).
2. **Confirmation**: After printing, prompt the user for confirmation before executing.
3. **Password from env**: If `--password` is not provided as a CLI arg, read from `ADMIN_PASSWORD` environment variable. If neither is set, exit with error.
4. **Validation**: Validate that `--network` is either `testnet` or `mainnet`. Validate that `--namespace` is non-empty.

### Example Usage

```bash
# Using CLI password
/app/admin.sh --password mypass --network testnet --namespace op-demo-testnet add-all

# Using env var for password
export ADMIN_PASSWORD=mypass
/app/admin.sh --network testnet --namespace op-demo-testnet add-batcher

# Remove all configs
/app/admin.sh --network mainnet --namespace op-prod remove-all
```

### Example Output (dry-run preview)

```
=== Commands to execute ===

[1] /app/op-signer admin --admin.password **** add-config \
      --parent-chain-id 11155111 \
      --allowed-client-cn "op-demo-testnet-seq-sequencer" \
      --path "op-demo-testnet-operator-keys/sequencer-prikey"

[2] /app/op-signer admin --admin.password **** add-config \
      --parent-chain-id 11155111 \
      --allowed-client-cn "op-demo-testnet-seq-batcher" \
      --path "op-demo-testnet-operator-keys/batcher-prikey"

[3] /app/op-signer admin --admin.password **** add-config \
      --parent-chain-id 11155111 \
      --allowed-client-cn "op-demo-testnet-seq-proposer" \
      --path "op-demo-testnet-operator-keys/proposer-prikey"

[4] /app/op-signer admin --admin.password **** add-config \
      --parent-chain-id 11155111 \
      --allowed-client-cn "op-demo-testnet-seq-challenger" \
      --path "op-demo-testnet-operator-keys/challenger-resolver-prikey"

Proceed? [y/N]
```

## Implementation Notes

### Dockerfile Change

Add the script to the final stage:
```dockerfile
COPY ./op-signer/admin.sh /app/admin.sh
RUN chmod +x /app/admin.sh
```

Note: Ensure `bash` or `sh` is available in the final image (`dhi.io/alpine-base:3.23`). If only `sh` is available, write the script in POSIX sh.

### Open Questions (to decide during implementation)

1. Should `--network` be omitted for `remove-config` commands? (remove-config-by-path doesn't use `--parent-chain-id`)
2. Should the script support a `--dry-run` flag that only prints without prompting to execute?
3. Should execution results be summarized (success/failure per command)?
