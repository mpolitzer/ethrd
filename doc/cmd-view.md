# cmd/view

TUI to explore `log` db using the library: tview.
Reads the db read-only (`-d <path>`, default `file:log.db?mode=ro`);
events are decoded with the demux built from `state.combined_abi` in the database.

## basic

```
┌───────┬───────┬──────┬────────────────┐
│ block │ event │ LSFR │   DETAIL       │
├───────┼───────┼──────┼────────────────┤
├───────┼───────┼──────┤       .        │
├───────┼───────┼──────┤       .        │
│◄─── selected line ──►│    <json>      │
├───────┼───────┼──────┤       .        │
├───────┼───────┼──────┤       .        │
├───────┼───────┼──────┤       .        │
├───┬───┴───────┴──────┴────────────────┤
│ * │ j/k move ...                      │
└───┴───────────────────────────────────┘
```

Two panes in a tview grid,

On the left, a table:
- each row having block number, `contract.EventName`, tags (one position per tag: letter when set, `.` when absent, e.g. `L...`)

Bottom Row:
- `*` (asterisk) indicates loading.
- followed by a terse version of the available commands.

On the right the decoded (Demux) log of the highlighted entry from the left table.

keys:
- g/G: jump to the oldest/newest page (and reload entries from database).
- f: toggles a follow mode to periodically reload entries from database.
- j/k: move up/down in the left pane.
- n/p: load next/previous page.
- q: quit

## details

- TUI lib: https://github.com/rivo/tview
- 
