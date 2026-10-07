# overview

`ethrd`: reads, persists and finalizes ethereum logs reliably.

Components:
```

┌───────────────────┐
│ ethereum-provider │
└─────────┬─────────┘
          │ subscription
          │ scan
      ┌───▼───┐  ┌──────────┐
      │ ethrd ◄──► database │
      └───────┘  └──────────┘
```

## commitment levels (and block finality)

Commitment levels indicate how likely a blockchain reorganization (reorg) is
to happen for a given block.

```

       ┌─── observed ────┐  ┌─── speculated ───┐
       ▼                 ▼  ▼                  ▼
       o◄─ ... o◄─ ... ◄─o◄─o◄─... ◄─o◄─ ... ◄─o
       ▲       ▲         ▲           ▲         ▲
   genesis <= cursor <= finalized <= safe <= latest

```

EVM based chains categorize them into discrete levels:

| commitment  |  latency  | description            |
|------------:|----------:|-----------------------:|
| `flash`     |    ~200ms | high chance of reorgs  |
| `latest`    |     ~12s  | high chance of reorgs  |
| `safe`      |     ~6m*  | low chance of reorgs   |
| `finalized` |     ~13m  | effectively immutable  |
*after fast confirmation rule (FCR) drops to about 13s.

Each commitment level is better suited for a different use case. For instance,
when dealing with financial values one would lean towards lower reorg chances.
While UX work may go in the opposite direction, towards faster response times.

For maximum flexibility, we'll provide the same event multiple times, with
varying commitment levels. This way consumers can decide how to handle them.

The implementation associates a tag with events in discrete levels: `latest`,
`safe`, or `finalized`. Same values as the table above.

`flash` blocks can be tested on base test chain (same as `latest` there).
`finalized` blocks also mark `safe`.

Consumers dealing with non-`finalized` events must be able to undo their side
effects. They are speculative and may be removed due to reorgs.

## two paths of ingestion

### scan (source of truth)

Periodically, scan `[cursor, finalized]` with `filterLogs`, in chunks, paced
by the rate limiter. These are the canonical, finalized events. The scan is
the complete stream: it delivers every event in its `finalized` form, even
ones the fast path dropped or never saw. Any speculative state retrieved by
the subscription must be reconciled to them.

Note: `finalized` as a block number is a snapshot taken at the start of the
tick, not the live, updating value.

### subscription (fast path)

- `log.Removed == false` → insert entry as `live`, emit `event:latest`.
- `log.Removed == true`  → ignored.

The fast path is best effort and additive: only the scan writes tombstones, and
only at finalized heights, so a tombstone is final by construction. Removals
surface to consumers as `event:finalized+removed`, when the scan settles the
height.

Why not tombstone here: A block reorged out can regain canonicity before
finalization. A phenomenon called fork oscillation. The consequence is that an
early tombstone requires a revival process as well. We sidestep the revival
issue by only tombstoning finalized logs.

## Reconcile

Upon reading the canonical state from the blockchain, the system must reconcile
what actually happened to its speculative state:
- Observed events that don't exist as speculation must be created.
- Events that have been speculated but not observed must be tombstoned.
- Observed events that have been speculated must have their tags updated.

### Log identity

Log identity must exclude re-mines. The expected behavior of reorged events is:
- new(A), del(A), new(B).

This is important for observability, payload consistency and auditability.

### algorithm

Reconcile is a merge-join set reconciliation between two views of the same
block range, `[cursor, finalized]`:

- **O (observed)** - canonical logs read by the scan.
- **S (speculative)** - live rows (no `removed` bit) held by the database for
  that range (applies to all rows not yet finalized by construction).

The scan is the arbiter: everything in O is true; everything in S that O does
not confirm is false. The diff yields three buckets:

```
O \ S   →  insert      observed, never speculated
S \ O   →  tombstone   speculated, reorged away
O ∩ S   →  tag update  speculation confirmed
```

The diff is scoped to `[cursor, finalized]`, the newly finalized region. Rows
below `cursor` are settled: they were confirmed when the frontier passed, and
finalized blocks do not reorg. Rows above `finalized` are still speculative and
belong to a later round.

Identity is `(block_hash, log_index)`. `block_hash` excludes re-mines and
log_index is unique within the block. A reorg reads as `new(A), del(A),
new(B)`, never an in-place update. Both sides are merged in `merge key`
`(block_number, log_index, block_hash)` order, so edits are emitted in
non-decreasing block order: a block N tombstone never follows a block N+1
confirm. An identity is a lifetime key: a tombstoned row is dead forever, so
a late write for it must be a no-op.

Each scanned chunk is reconciled in a single transaction: upsert its O,
tombstone its S \ O, advance `cursor` past it. A crash resumes at the last
committed chunk; re-running a chunk is a no-op, since every write is
idempotent.
