package ql

import (
	"fmt"
	"strings"

	"github.com/obayona/godb/table"
)

// Row is a query result decoded into Go-native values. Integer columns become
// int64 values and byte-string columns become strings. Cols and Vals retain
// the query's projection order.
type Row struct {
	Cols []string
	Vals []any
}

// Get returns a decoded value by column name, or nil when the column does not
// exist. Use QLResult.Records when binary []byte values must be preserved.
func (row Row) Get(column string) any {
	for i, name := range row.Cols {
		if name == column {
			return row.Vals[i]
		}
	}
	return nil
}

// String formats a row as an ordered, human-readable column/value object.
func (row Row) String() string {
	parts := make([]string, len(row.Cols))
	for i, column := range row.Cols {
		parts[i] = fmt.Sprintf("%s: %v", column, row.Vals[i])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// DecodeRecord converts a low-level table record into a query Row.
func DecodeRecord(record table.Record) (Row, error) {
	if len(record.Cols) != len(record.Vals) {
		return Row{}, fmt.Errorf("record column/value length mismatch")
	}
	row := Row{
		Cols: append([]string(nil), record.Cols...),
		Vals: make([]any, len(record.Vals)),
	}
	for i, value := range record.Vals {
		switch value.Type {
		case table.TYPE_INT64:
			row.Vals[i] = value.I64
		case table.TYPE_BYTES:
			row.Vals[i] = string(value.Str)
		default:
			return Row{}, fmt.Errorf("unsupported value type %d for column %s", value.Type, record.Cols[i])
		}
	}
	return row, nil
}

// RowIter adapts raw query records into decoded rows.
type RowIter struct {
	records RecordIter
}

// Valid reports whether the iterator currently points to a row.
func (iter *RowIter) Valid() bool {
	return iter != nil && iter.records != nil && iter.records.Valid()
}

// Next advances to the next decoded row.
func (iter *RowIter) Next() {
	if iter.Valid() {
		iter.records.Next()
	}
}

// Deref returns the current decoded row.
func (iter *RowIter) Deref() (Row, error) {
	if !iter.Valid() {
		return Row{}, fmt.Errorf("row iterator is not valid")
	}
	record := table.Record{}
	if err := iter.records.Deref(&record); err != nil {
		return Row{}, err
	}
	return DecodeRecord(record)
}

// Rows returns a decoded view of the result records. Mutation statements have
// an empty iterator. The Records field remains available for binary-safe,
// low-level access.
func (result QLResult) Rows() *RowIter {
	return &RowIter{records: result.Records}
}
