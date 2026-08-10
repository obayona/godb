package ql

import (
	"testing"

	"github.com/obayona/godb/table"
	is "github.com/stretchr/testify/require"
)

func TestDecodeRecord(t *testing.T) {
	record := table.Record{
		Cols: []string{"id", "name"},
		Vals: []table.Value{
			{Type: table.TYPE_INT64, I64: 2},
			{Type: table.TYPE_BYTES, Str: []byte("Grace")},
		},
	}

	row, err := DecodeRecord(record)
	is.NoError(t, err)
	is.Equal(t, []string{"id", "name"}, row.Cols)
	is.Equal(t, []any{int64(2), "Grace"}, row.Vals)
	is.Equal(t, int64(2), row.Get("id"))
	is.Equal(t, "Grace", row.Get("name"))
	is.Nil(t, row.Get("missing"))
	is.Equal(t, "{id: 2, name: Grace}", row.String())
}

func TestDecodeRecordRejectsUnknownType(t *testing.T) {
	_, err := DecodeRecord(table.Record{
		Cols: []string{"bad"},
		Vals: []table.Value{{Type: table.TYPE_ERROR}},
	})
	is.Error(t, err)
}
