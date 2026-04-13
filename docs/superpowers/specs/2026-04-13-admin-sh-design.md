# op-signer admin.sh Script Spec

## Overview

A POSIX sh script (`/app/admin.sh`) for batch registration and removal of op-signer config entries. Used interactively inside the `op-remote-signer-admin-client` K8s pod.

## Approach

POSIX `/bin/sh` script — no bash dependency, no Dockerfile changes for additional packages. The final Alpine image already has `/bin/sh`.

## Interface

```
/app/admin.sh [OPTIONS] <COMMAND>
```

### Options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| `--password <val>` | No | `ADMIN_PASSWORD` env var | Admin password. Error if neither provided. |
| `--chain <name>` | For `add-*` | None | Chain name (see mapping below). Not needed for `remove-*`. |
| `--namespace <val>` | Yes | None | Namespace identifier, e.g. `op-demo-testnet`. |
| `--endpoint <url>` | No | `OP_SIGNER_ADMIN_ENDPOINT` env, fallback `https://localhost:9545` | Admin API endpoint. |
| `--dry-run` | No | false | Print commands and exit without executing. |
| `--continue-on-error` | No | false | Continue executing remaining commands after a failure. |

### Chain Mapping

| `--chain` value | `--parent-chain-id` |
|-----------------|---------------------|
| `ethereum` | 1 |
| `sepolia` | 11155111 |
| `bsc` | 56 |
| `bsc-testnet` | 97 |

### Commands

| Command | Description |
|---------|-------------|
| `add-sequencer` | Add sequencer config |
| `add-batcher` | Add batcher config |
| `add-proposer` | Add proposer config |
| `add-challenger-resolver` | Add challenger-resolver config |
| `add-main` | = add-batcher + add-proposer + add-challenger-resolver |
| `add-all` | = add-sequencer + add-batcher + add-proposer + add-challenger-resolver |
| `remove-sequencer` | Remove sequencer config |
| `remove-batcher` | Remove batcher config |
| `remove-proposer` | Remove proposer config |
| `remove-challenger-resolver` | Remove challenger-resolver config |
| `remove-all` | = all four remove commands |

## Component Mapping

| Component | CN suffix (`--allowed-client-cn`) | Key path suffix (`--path`) |
|-----------|----------------------------------|----------------------------|
| sequencer | `<ns>-seq-sequencer` | `<ns>-operator-keys/sequencer-prikey` |
| batcher | `<ns>-seq-batcher` | `<ns>-operator-keys/batcher-prikey` |
| proposer | `<ns>-seq-proposer` | `<ns>-operator-keys/proposer-prikey` |
| challenger-resolver | `<ns>-seq-challenger` | `<ns>-operator-keys/challenger-resolver-prikey` |

Note: challenger-resolver uses `challenger` (not `challenger-resolver`) as the CN suffix.

## Generated Commands

### add-config template

```sh
/app/op-signer admin \
  --admin.endpoint <endpoint> \
  --admin.password <password> \
  add-config \
  --parent-chain-id <chain_id> \
  --allowed-client-cn "<namespace>-seq-<component>" \
  --path "<namespace>-operator-keys/<key_name>"
```

### remove-config template

```sh
/app/op-signer admin \
  --admin.endpoint <endpoint> \
  --admin.password <password> \
  remove-config-by-path \
  --path "<namespace>-operator-keys/<key_name>"
```

No `--parent-chain-id` or `--allowed-client-cn` needed for remove.

## Execution Flow

1. Parse arguments (`while/case` loop)
2. Validate: namespace non-empty, `add-*` requires `--chain`, password available
3. Generate command list based on command
4. Print command preview (password masked as `****`)
5. If `--dry-run`: exit 0
6. Prompt `Proceed? [y/N]` — only `y` or `Y` proceeds
7. Execute commands sequentially, transparently streaming stdout/stderr
8. On failure: stop unless `--continue-on-error` is set
9. Print result summary

## Output Format

### Command Preview

```
=== Commands to execute ===

[1] /app/op-signer admin --admin.endpoint https://... \
      --admin.password **** add-config \
      --parent-chain-id 11155111 \
      --allowed-client-cn "op-demo-testnet-seq-batcher" \
      --path "op-demo-testnet-operator-keys/batcher-prikey"

[2] ...

Proceed? [y/N]
```

### Execution Output

```
--- [1] add-config batcher ---
<raw op-signer stdout/stderr>

--- [2] add-config proposer ---
<raw op-signer stdout/stderr>
```

### Result Summary

```
=== Summary ===
[1] [OK]   add-config batcher
[2] [FAIL] add-config proposer (exit code 1)
[3] [SKIP] add-config challenger-resolver

Result: 1/3 succeeded, 1 failed, 1 skipped
```

Uses `[OK]`, `[FAIL]`, `[SKIP]` for portability (no UTF-8 dependency).

## Dockerfile Change

Add to the final stage, after the existing COPY line:

```dockerfile
COPY ./op-signer/admin.sh /app/admin.sh
RUN chmod +x /app/admin.sh
```

## Example Usage

```sh
# Add all components for a testnet namespace
/app/admin.sh --password mypass --chain sepolia --namespace op-demo-testnet add-all

# Using env var for password
export ADMIN_PASSWORD=mypass
/app/admin.sh --chain bsc --namespace op-prod-bsc add-main

# Dry run
/app/admin.sh --chain ethereum --namespace op-prod --dry-run add-all

# Remove all configs (no --chain needed)
/app/admin.sh --namespace op-demo-testnet remove-all

# Continue on error
/app/admin.sh --chain sepolia --namespace op-demo-testnet --continue-on-error add-all
```
