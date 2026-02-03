# How to use the Signer by Vault

## Configuration

The Signer by Vault use path to select the address to sign.

the path like:

```
http://127.0.0.1:8545/op/vaults/testsign/items/testnet/t2
```

the `http://127.0.0.1:8545` is the rpc endpoint, and `/op/vaults/testsign/items/testnet/t2` is the path to select the address to sign.

The path is composed of the following parts:

- `/op/vaults/testsign`: the vault name, note the `op/vaults` is the onepassword 's plugin path, and the `testsign` is the vault name in onepassword.
- `/items/testnet` is the item name, and `/t2` is the item path.

for this example, we can see in onepassword:

![onepassword-signer-by-vault](./onepass-image.png)

t1 and t2 is a private key item, we can use alt_publickey to get the public key:

```bash
curl -X POST http://127.0.0.1:8545/op/vaults/testsign/items/testnet/t2 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "alt_publickey",
    "params": [],
    "id": 1
  }'
{"jsonrpc":"2.0","id":1,"result":"0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"}
```

For easy use, we can use prefix:

- `OP_SIGNER_VAULT_ROOT_PATH_PREFIX` is the prefix of the path

when use this, for example `OP_SIGNER_VAULT_ROOT_PATH_PREFIX="op/vaults/testsign/items"`, the path will be:

```bash
curl -X POST http://127.0.0.1:8545/testnet/t2 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "alt_publickey",
    "params": [],
    "id": 1
  }'
{"jsonrpc":"2.0","id":1,"result":"0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"}
```

## Usage

For example, we can use the signer by vault to sign a transaction:

```bash
    --signer.address value                                                 ($OP_BATCHER_SIGNER_ADDRESS)
          Address the signer is signing requests for

    --signer.endpoint value                                                ($OP_BATCHER_SIGNER_ENDPOINT)
          Signer endpoint the client will connect to
```


Boot the signer:

```
OP_SIGNER_VAULT_ROOT_PATH_PREFIX='op/vaults/testsign/items' OP_SIGNER_VAULT_TOKEN='root' OP_SIGNER_VAULT_ADDR='http://127.0.0.1:8200' ./bin/op-signer  --rpc.port 18545  --tls.enabled=false --config ./config.local.yaml --log.level debug
```

Then we can set the batcher 's private key to 1pass with `batcher-private`, we can got:

```
curl -X POST http://127.0.0.1:18545/testnet/batcher-private \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "alt_publickey",
    "params": [],
    "id": 1
  }'
{"jsonrpc":"2.0","id":1,"result":"0xd3f2c5afb2d76f5579f326b0cd7da5f5a4126c35"}
```

Then we can use the signer:

```bash
./bin/op-batcher \
    "--l2-eth-rpc=http://127.0.0.1:9700" \
    "--rollup-rpc=http://127.0.0.1:9700" \
    "--poll-interval=1s" \
    "--sub-safety-margin=6" \
    "--num-confirmations=1" \
    "--safe-abort-nonce-too-low-count=3" \
    "--resubmission-timeout=30s" \
    "--rpc.addr=0.0.0.0" \
    "--rpc.port=8548" \
    "--rpc.enable-admin" \
    "--max-channel-duration=1" \
    "--l1-eth-rpc=http://127.0.0.1:32769" \
    "--signer.address=0xd3f2c5afb2d76f5579f326b0cd7da5f5a4126c35" \
    "--signer.tls.enabled=false" \
    "--signer.endpoint=http://127.0.0.1:18545/testnet/batcher-private" \
    "--data-availability-type=blobs" \
    "--altda.enabled=false" \
    "--altda.da-server=" \
    "--altda.da-service" \
    "--metrics.enabled" \
    "--metrics.addr=0.0.0.0" \
    "--metrics.port=9001" \
    "--pprof.enabled" \
    "--pprof.addr=0.0.0.0" \
    "--pprof.port=6060"
```

```
INFO [02-03|15:29:08.058] Added L2 block to local state            block=53f295..d5f84f:3329 tx_count=1   time=1,770,103,748
INFO [02-03|15:29:09.200] Publishing transaction                   service=batcher tx=0xadf293366fdb024506ef962ccb62ef9dacb753ed6f4b66d4f57b3dfa9178784a nonce=80 gasTipCap=1,000,000,000 gasFeeCap=3,000,000,000 gasLimit=21000 blobs=1 blobFeeCap=1,000,000,000
INFO [02-03|15:29:09.200] Created channel                          id=c8a7ee..af9272 l1Head=e12988..7ba7ed:1123 blocks_pending=85  l1OriginLastSubmittedChannel=feb7e7..2da0db:1092 batch_type=0 compression_algo=zlib target_num_frames=1 max_frame_size=130,043 use_blobs=true
INFO [02-03|15:29:09.208] Transaction successfully published       service=batcher tx=0xadf293366fdb024506ef962ccb62ef9dacb753ed6f4b66d4f57b3dfa9178784a nonce=80 gasTipCap=1,000,000,000 gasFeeCap=3,000,000,000 gasLimit=21000 blobs=1 blobFeeCap=1,000,000,000 tx=adf293..78784a
INFO [02-03|15:29:09.208] Channel closed                           id=c8a7ee..af9272 blocks_pending=55  num_frames=1 input_bytes=184,091 output_bytes=119,603 oldest_l1_origin=feb7e7..2da0db:1092 l1_origin=71318b..5cdebf:1102 oldest_l2=68e542..23a5c2:3245 latest_l2=8024a5..870347:3274 full_reason="channel full: compressor is full" compr_ratio=0.650
INFO [02-03|15:29:09.209] Building Blob transaction candidate      size=119,603 last_size=119,603 num_blobs=1
INFO [02-03|15:29:10.058] Added L2 block to local state            block=927425..b58643:3330 tx_count=1   time=1,770,103,750
```
