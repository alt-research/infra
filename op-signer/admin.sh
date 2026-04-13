#!/bin/sh
set -eu

usage() {
  cat <<'USAGE'
Usage: /app/admin.sh [OPTIONS] <COMMAND>

Options:
  --password <val>       Admin password (or set ADMIN_PASSWORD env var)
  --chain <name>         Chain name: ethereum, sepolia, bsc, bsc-testnet
  --namespace <val>      Namespace identifier (required)
  --endpoint <url>       Admin endpoint (default: OP_SIGNER_ADMIN_ENDPOINT or https://localhost:9545)
  --dry-run              Print commands without executing
  --continue-on-error    Continue after command failure

Commands:
  add-sequencer          Add sequencer config
  add-batcher            Add batcher config
  add-proposer           Add proposer config
  add-challenger-resolver  Add challenger-resolver config
  add-main               Add batcher + proposer + challenger-resolver
  add-all                Add all four components
  remove-sequencer       Remove sequencer config
  remove-batcher         Remove batcher config
  remove-proposer        Remove proposer config
  remove-challenger-resolver  Remove challenger-resolver config
  remove-all             Remove all four components
USAGE
}

# Defaults
PASSWORD="${ADMIN_PASSWORD:-}"
CHAIN=""
NAMESPACE=""
ENDPOINT="${OP_SIGNER_ADMIN_ENDPOINT:-https://localhost:9545}"
DRY_RUN=false
CONTINUE_ON_ERROR=false
COMMAND=""

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    --password)
      [ $# -ge 2 ] || { echo "Error: --password requires a value" >&2; exit 1; }
      PASSWORD="$2"; shift 2 ;;
    --chain)
      [ $# -ge 2 ] || { echo "Error: --chain requires a value" >&2; exit 1; }
      CHAIN="$2"; shift 2 ;;
    --namespace)
      [ $# -ge 2 ] || { echo "Error: --namespace requires a value" >&2; exit 1; }
      NAMESPACE="$2"; shift 2 ;;
    --endpoint)
      [ $# -ge 2 ] || { echo "Error: --endpoint requires a value" >&2; exit 1; }
      ENDPOINT="$2"; shift 2 ;;
    --dry-run)
      DRY_RUN=true; shift ;;
    --continue-on-error)
      CONTINUE_ON_ERROR=true; shift ;;
    -h|--help)
      usage; exit 0 ;;
    -*)
      echo "Error: unknown option '$1'" >&2; usage; exit 1 ;;
    *)
      if [ -n "$COMMAND" ]; then
        echo "Error: unexpected argument '$1'" >&2; exit 1
      fi
      COMMAND="$1"; shift ;;
  esac
done

# Validate
if [ -z "$NAMESPACE" ]; then
  echo "Error: --namespace is required" >&2
  exit 1
fi

if [ -z "$PASSWORD" ]; then
  echo "Error: --password or ADMIN_PASSWORD env var is required" >&2
  exit 1
fi

if [ -z "$COMMAND" ]; then
  echo "Error: command is required" >&2; usage; exit 1
fi

# Chain ID mapping
resolve_chain_id() {
  case "$1" in
    ethereum)    CHAIN_ID="1" ;;
    sepolia)     CHAIN_ID="11155111" ;;
    bsc)         CHAIN_ID="56" ;;
    bsc-testnet) CHAIN_ID="97" ;;
    *)           echo "Error: invalid chain '$1'. Must be: ethereum, sepolia, bsc, bsc-testnet" >&2; return 1 ;;
  esac
}

# Validate command and chain
case "$COMMAND" in
  add-sequencer|add-batcher|add-proposer|add-challenger-resolver|add-main|add-all)
    if [ -z "$CHAIN" ]; then
      echo "Error: --chain is required for add commands" >&2
      exit 1
    fi
    resolve_chain_id "$CHAIN" || exit 1
    ;;
  remove-sequencer|remove-batcher|remove-proposer|remove-challenger-resolver|remove-all)
    if [ -n "$CHAIN" ]; then
      echo "Warning: --chain is ignored for remove commands" >&2
    fi
    CHAIN_ID=""
    ;;
  *)
    echo "Error: unknown command '$COMMAND'" >&2; usage; exit 1
    ;;
esac

# Build command list — store components, not full command strings
CMD_COUNT=0

add_to_list() {
  CMD_COUNT=$((CMD_COUNT + 1))
  eval "CMD_TYPE_${CMD_COUNT}=\$1"      # "add" or "remove"
  eval "CMD_CN_${CMD_COUNT}=\$2"        # cn suffix (e.g. "sequencer")
  eval "CMD_KEY_${CMD_COUNT}=\$3"       # key name (e.g. "sequencer-prikey")
  eval "CMD_LABEL_${CMD_COUNT}=\$4"     # display label (e.g. "add-config sequencer")
}

case "$COMMAND" in
  add-sequencer)
    add_to_list add sequencer sequencer-prikey "add-config sequencer" ;;
  add-batcher)
    add_to_list add batcher batcher-prikey "add-config batcher" ;;
  add-proposer)
    add_to_list add proposer proposer-prikey "add-config proposer" ;;
  add-challenger-resolver)
    add_to_list add challenger challenger-resolver-prikey "add-config challenger" ;;
  add-main)
    add_to_list add batcher batcher-prikey "add-config batcher"
    add_to_list add proposer proposer-prikey "add-config proposer"
    add_to_list add challenger challenger-resolver-prikey "add-config challenger"
    ;;
  add-all)
    add_to_list add sequencer sequencer-prikey "add-config sequencer"
    add_to_list add batcher batcher-prikey "add-config batcher"
    add_to_list add proposer proposer-prikey "add-config proposer"
    add_to_list add challenger challenger-resolver-prikey "add-config challenger"
    ;;
  remove-sequencer)
    add_to_list remove sequencer sequencer-prikey "remove-config sequencer" ;;
  remove-batcher)
    add_to_list remove batcher batcher-prikey "remove-config batcher" ;;
  remove-proposer)
    add_to_list remove proposer proposer-prikey "remove-config proposer" ;;
  remove-challenger-resolver)
    add_to_list remove challenger challenger-resolver-prikey "remove-config challenger" ;;
  remove-all)
    add_to_list remove sequencer sequencer-prikey "remove-config sequencer"
    add_to_list remove batcher batcher-prikey "remove-config batcher"
    add_to_list remove proposer proposer-prikey "remove-config proposer"
    add_to_list remove challenger challenger-resolver-prikey "remove-config challenger"
    ;;
esac

# Print command preview with password masked
print_preview() {
  echo ""
  echo "=== Commands to execute ==="
  echo ""
  _i=1
  while [ "$_i" -le "$CMD_COUNT" ]; do
    eval "_type=\$CMD_TYPE_${_i}"
    eval "_cn=\$CMD_CN_${_i}"
    eval "_key=\$CMD_KEY_${_i}"
    if [ "$_type" = "add" ]; then
      echo "[$_i] /app/op-signer admin --admin.endpoint $ENDPOINT --admin.password **** add-config --parent-chain-id $CHAIN_ID --allowed-client-cn \"${NAMESPACE}-seq-${_cn}\" --path \"${NAMESPACE}-operator-keys/${_key}\""
    else
      echo "[$_i] /app/op-signer admin --admin.endpoint $ENDPOINT --admin.password **** remove-config-by-path --path \"${NAMESPACE}-operator-keys/${_key}\""
    fi
    echo ""
    _i=$((_i + 1))
  done
}

# Print preview
print_preview

# Dry run — exit after preview
if [ "$DRY_RUN" = true ]; then
  echo "(dry-run mode, not executing)"
  exit 0
fi

# Confirm
printf "Proceed? [y/N] "
read -r _answer
case "$_answer" in
  y|Y) ;;
  *)   echo "Aborted."; exit 0 ;;
esac

# Execute
_succeeded=0
_failed=0
_skipped=0
_i=1
while [ "$_i" -le "$CMD_COUNT" ]; do
  eval "_type=\$CMD_TYPE_${_i}"
  eval "_cn=\$CMD_CN_${_i}"
  eval "_key=\$CMD_KEY_${_i}"
  eval "_label=\$CMD_LABEL_${_i}"

  # Skip if prior failure and not continue-on-error
  if [ "$_failed" -gt 0 ] && [ "$CONTINUE_ON_ERROR" = false ]; then
    eval "RESULT_${_i}=SKIP"
    _skipped=$((_skipped + 1))
    _i=$((_i + 1))
    continue
  fi

  echo ""
  echo "--- [$_i] $_label ---"
  _exit_code=0
  if [ "$_type" = "add" ]; then
    /app/op-signer admin \
      --admin.endpoint "$ENDPOINT" \
      --admin.password "$PASSWORD" \
      add-config \
      --parent-chain-id "$CHAIN_ID" \
      --allowed-client-cn "${NAMESPACE}-seq-${_cn}" \
      --path "${NAMESPACE}-operator-keys/${_key}" \
      || _exit_code=$?
  else
    /app/op-signer admin \
      --admin.endpoint "$ENDPOINT" \
      --admin.password "$PASSWORD" \
      remove-config-by-path \
      --path "${NAMESPACE}-operator-keys/${_key}" \
      || _exit_code=$?
  fi

  if [ "$_exit_code" -eq 0 ]; then
    eval "RESULT_${_i}=OK"
    _succeeded=$((_succeeded + 1))
  else
    eval "RESULT_${_i}=\"FAIL:${_exit_code}\""
    _failed=$((_failed + 1))
  fi

  _i=$((_i + 1))
done

# Summary
echo ""
echo "=== Summary ==="
_i=1
while [ "$_i" -le "$CMD_COUNT" ]; do
  eval "_label=\$CMD_LABEL_${_i}"
  eval "_result=\"\$RESULT_${_i}\""
  case "$_result" in
    OK)
      echo "[$_i] [OK]   $_label"
      ;;
    SKIP)
      echo "[$_i] [SKIP] $_label"
      ;;
    FAIL:*)
      _code="${_result#FAIL:}"
      echo "[$_i] [FAIL] $_label (exit code $_code)"
      ;;
  esac
  _i=$((_i + 1))
done

_total=$CMD_COUNT
echo ""
echo "Result: ${_succeeded}/${_total} succeeded, ${_failed} failed, ${_skipped} skipped"

# Exit with failure if any command failed
if [ "$_failed" -gt 0 ]; then
  exit 1
fi
