# GoDB

This repository contains the chapter 13/14 database implementation, split into
one-way dependency layers:

```text
utils -> btree -> kvstore -> table -> ql -> cmd/demo
```

- `btree/` implements page-oriented B+tree nodes and iterators.
- `kvstore/` implements files, free-page management, snapshots, and KV
  transactions.
- `table/` implements schemas, records, indexes, and table transactions.
- `ql/` implements parsing and execution of the query language.
- `utils/` contains shared internal assertions.

Transactions live with the layer whose state they manage: `KVTX` is in
`kvstore/`, while `DBTX` is in `table/`. This keeps package dependencies
acyclic.

The command-line walkthrough can be run with:

```sh
go run ./cmd/demo
```

The demo creates a table, inserts rows, deletes one row, and selects the
remaining data.
