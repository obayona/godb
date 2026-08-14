// Package client provides the high-level API for opening a database and
// executing query-language statements without managing transactions directly.
package client

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/obayona/godb/ql"
	"github.com/obayona/godb/table"
)

// Client owns an open database. Its zero value is ready to use with Open.
type Client struct {
	mu     sync.Mutex
	db     table.DB
	opened bool
}

// Result contains materialized rows and mutation counts returned by Query.
type Result struct {
	Rows    []Row
	Added   uint64
	Updated uint64
	Deleted uint64
}

// Row is a query result decoded into Go-native int64 and string values.
// Columns and Values retain the projection order from the query.
type Row struct {
	Columns []string
	Values  []any
}

// Get returns a value by column name, or nil if the column is absent.
func (row Row) Get(column string) any {
	for i, name := range row.Columns {
		if name == column {
			return row.Values[i]
		}
	}
	return nil
}

// String formats a row as an ordered, human-readable column/value object.
func (row Row) String() string {
	parts := make([]string, len(row.Columns))
	for i, column := range row.Columns {
		parts[i] = fmt.Sprintf("%s: %v", column, row.Values[i])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Open creates and opens a client for path.
func Open(path string) (*Client, error) {
	client := &Client{}
	if err := client.Open(path); err != nil {
		return nil, err
	}
	return client, nil
}

// Open opens or creates the database file at path.
func (client *Client) Open(path string) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.opened {
		return errors.New("client is already open")
	}
	if path == "" {
		return errors.New("database path is empty")
	}
	client.db = table.DB{Path: path}
	if err := client.db.Open(); err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	client.opened = true
	return nil
}

// Query executes one query-language statement in its own transaction. Result
// rows are decoded and materialized before the transaction commits. This API
// deliberately performs no placeholder binding or SQL-injection protection;
// callers must not concatenate untrusted input into statements.
func (client *Client) Query(statement string) (Result, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if !client.opened {
		return Result{}, errors.New("client is not open")
	}

	tx := table.DBTX{}
	client.db.Begin(&tx)
	result, err := ql.DBTXExecString(&tx, []byte(statement))
	if err != nil {
		client.db.Abort(&tx)
		return Result{}, fmt.Errorf("execute query: %w", err)
	}

	out := Result{
		Added:   result.Added,
		Updated: result.Updated,
		Deleted: result.Deleted,
	}
	rows := result.Rows()
	for rows.Valid() {
		row, err := rows.Deref()
		if err != nil {
			client.db.Abort(&tx)
			return Result{}, fmt.Errorf("decode query row: %w", err)
		}
		out.Rows = append(out.Rows, Row{
			Columns: append([]string(nil), row.Cols...),
			Values:  append([]any(nil), row.Vals...),
		})
		rows.Next()
	}
	if err := client.db.Commit(&tx); err != nil {
		return Result{}, fmt.Errorf("commit query: %w", err)
	}
	return out, nil
}

// Close closes the database. Calling Close on an already closed client is
// safe.
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if !client.opened {
		return nil
	}
	client.db.Close()
	client.opened = false
	return nil
}
