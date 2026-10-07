# ethrd

`ethrd` reads, persists and finalizes Ethereum logs reliably.

It ingests matching logs through two paths (see `doc/ethrd.md`):

- **subscription** (fast path): live logs from a websocket provider, stored
  speculatively and tagged `latest`.
- **scan** (source of truth): periodic `filterLogs` over `[cursor, finalized]`,
  reconciled against the speculative rows. Confirmed events are tagged
  `safe`/`finalized`; reorged-away ones are tombstoned (`removed`).

Everything is persisted to a SQLite database, with the combined ABI stored so
logs can be decoded later.

The repo is a Go library; `cmd/` contains two example binaries:

- `cmd/ethrd` - the ingest daemon.
- `cmd/view` - a TUI to explore the database.

## build

```
go build ./cmd/ethrd
go build ./cmd/view
```

## ethrd

Runs the ingest loop: connects to the provider, applies migrations, and
keeps the database in sync.

```
./ethrd -c conf.yml
```

| flag | default | description |
|------|---------|-------------|
| `-c`, `--conf` | `conf.yml` | yaml configuration file |

Configuration:

```yaml
ws: "wss://rpc.vibes.base.org/ws"        # websocket provider endpoint
db: "file:log.db?_journal_mode=WAL&_busy_timeout=5000"
ethrd:
  poll-interval: 12s                     # resubscription / scan cadence
  log-buf-size: 128                      # subscription log channel buffer
  filter:
    chunk-size: 1024                     # blocks per filterLogs chunk
    burst-limit: 1                       # rate limiter burst
    rate-limit: 100                      # rate limiter requests/second
  bootstrap:                             # first run only
    block-number: 0                      # frontier to start from
    combined-abi:
      file: "erc20-abi.json"             # map of name -> ABI, e.g. {"erc20": [...]}
```

After the first run the combined ABI is read back from the database, so
`bootstrap` can be dropped.

## view

Read-only TUI over the `log` table. Left pane lists events (block number,
log index, tags, `contract.EventName`); the right pane shows the decoded log
of the selected row. Events are decoded with the demux built from the
combined ABI stored in the database.

```
┌────────────────────────────────────────┬───────────────────────────────────────────────────────────────────────────────────────────────────┐
│      1052      13   7  erc20.Transfer  │"erc20.Transfer": {                                                                                │
│      1052      12   7  erc20.Transfer  │    "From": "0x050b84a1305f0687b4dce4e89f4376c492b81833",                                          │
│      1052       9   7  erc20.Transfer  │    "To": "0xab234cc1c75675f5b947e0840d5d4b3a97decf35",                                            │
│      1052       8   7  erc20.Transfer  │    "Value": 523647217,                                                                            │
│      1052       5   7  erc20.Transfer  │    "Raw": {                                                                                       │
│      1052       4   7  erc20.Transfer  │        "address": "0x64bd2e932fa41c9fb7451996fba3bd270d647d6d",                                   │
│      1052       1   7  erc20.Transfer  │        "topics": [                                                                                │
│      1052       0   7  erc20.Transfer  │            "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",                  │
│      1050      13   7  erc20.Transfer  │            "0x000000000000000000000000050b84a1305f0687b4dce4e89f4376c492b81833",                  │
│      1050      12   7  erc20.Transfer  │            "0x000000000000000000000000ab234cc1c75675f5b947e0840d5d4b3a97decf35"                   │
│      1050       9   7  erc20.Transfer  │        ],                                                                                         │
│      1050       8   7  erc20.Transfer  │        "data": "0x000000000000000000000000000000000000000000000000000000001f3638f1",              │
│      1050       5   7  erc20.Transfer  │        "blockNumber": "0x41c",                                                                    │
│      1050       4   7  erc20.Transfer  │        "transactionHash": "0x94ee3736ab1cc9acfedff7816069b7467fef7ae52e045cd480f90728d5d8126a",   │
│      1050       1   7  erc20.Transfer  │        "transactionIndex": "0x5",                                                                 │
│      1050       0   7  erc20.Transfer  │        "blockHash": "0x9584ef22cb8e1e65e652b57e0ef0c1b20ae523f69f2b1ec3185d765e9f2de376",         │
│      1049       1   7  erc20.Transfer  │        "blockTimestamp": "0x6abff23f",                                                            │
│      1049       0   7  erc20.Transfer  │        "logIndex": "0xd",                                                                         │
│      1048      10   7  erc20.Transfer  │        "removed": false                                                                           │
│      1048       9   7  erc20.Transfer  │    }                                                                                              │
│      1048       6   7  erc20.Transfer  │}                                                                                                  │
│      1048       5   7  erc20.Transfer  │                                                                                                   │
│      1048       2   7  erc20.Transfer  │                                                                                                   │
│      1048       1   7  erc20.Transfer  │                                                                                                   │
└────────────────────────────────────────┴───────────────────────────────────────────────────────────────────────────────────────────────────┘
```

```
./view -d log.db
```

| flag | default | description |
|------|---------|-------------|
| `-d`, `--db` | `log.db` | path to the ethrd database |

Keys:

| key | action |
|-----|--------|
| `j`/`k` | move down/up |
| `Enter` | decode the selected log |
| `n`/`p` | next/previous page |
| `g`/`G` | oldest/newest |
| `f` | toggle follow mode (periodic reload) |
| `q` | quit |
