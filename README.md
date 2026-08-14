# GoDB

This repository contains the chapter 13/14 database implementation, split into
one-way dependency layers:

```text
utils -> btree -----> kvstore -> table -> ql -> client -> cmd/demo
          \-> freelist --/
```

- `btree/` implements page-oriented B+tree nodes, bidirectionally linked leaf
  pages, and iterators.
- `freelist/` implements the reusable, version-aware queue of free pages.
- `kvstore/` implements files, snapshots, and KV transactions by composing the
  B-tree and free list.
- `table/` implements schemas, records, indexes, and table transactions.
- `ql/` implements parsing and execution of the query language.
- `client/` provides the high-level database lifecycle and query API.
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

Applications normally need only the client package:

```go
db, err := client.Open("app.db")
if err != nil {
	return err
}
defer db.Close()

result, err := db.Query("select id, name from users filter id >= 10")
for _, row := range result.Rows {
	fmt.Println(row)
}
```

Each `Query` executes one statement in its own transaction and returns
materialized rows plus `Added`, `Updated`, and `Deleted` counts. It does not
provide placeholders or protect dynamically concatenated input from query
injection.

Queries use `FILTER` predicates without an index hint. The query layer chooses
an index for simple equality and range predicates when a leading index prefix
matches; computed, ambiguous, and otherwise unsupported predicates fall back
to a full primary-index scan.

`client.Result.Rows` contains decoded `int64` and `string` values. The lower
query and table layers remain available when byte-string columns must be
preserved as raw `[]byte` data.

The linked-leaf format uses the database signature `GoDBLinkedLeaf01` and is
not binary-compatible with database files produced by the earlier node
format.
