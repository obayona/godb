# GoDB

This repository contains the chapter 13/14 database implementation, split into
one-way dependency layers:

```text
utils -> btree -----> kvstore -> table -> ql -> cmd/demo
          \-> freelist --/
```

- `btree/` implements page-oriented B+tree nodes, bidirectionally linked leaf
  pages, and iterators.
- `freelist/` implements the reusable, version-aware queue of free pages.
- `kvstore/` implements files, snapshots, and KV transactions by composing the
  B-tree and free list.
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

The linked-leaf format uses the database signature `GoDBLinkedLeaf01` and is
not binary-compatible with database files produced by the earlier node
format.
