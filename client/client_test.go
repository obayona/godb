package client

import (
	"path/filepath"
	"testing"

	is "github.com/stretchr/testify/require"
)

func TestClientQuery(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "client.db"))
	is.NoError(t, err)
	t.Cleanup(func() { is.NoError(t, db.Close()) })

	_, err = db.Query(`create table users (id int, name bytes, primary key (id))`)
	is.NoError(t, err)

	result, err := db.Query(`insert into users (id, name) values (1, 'Ada'), (2, 'Grace')`)
	is.NoError(t, err)
	is.Equal(t, uint64(2), result.Added)
	is.Empty(t, result.Rows)

	result, err = db.Query(`select id, name from users filter id >= 1`)
	is.NoError(t, err)
	is.Len(t, result.Rows, 2)
	is.Equal(t, int64(1), result.Rows[0].Get("id"))
	is.Equal(t, "Ada", result.Rows[0].Get("name"))
	is.Equal(t, "{id: 2, name: Grace}", result.Rows[1].String())

	result, err = db.Query(`delete from users filter id = 1`)
	is.NoError(t, err)
	is.Equal(t, uint64(1), result.Deleted)
}

func TestClientLifecycleAndQueryErrors(t *testing.T) {
	db := &Client{}
	_, err := db.Query(`select * from users`)
	is.ErrorContains(t, err, "not open")
	is.Error(t, db.Open(""))

	path := filepath.Join(t.TempDir(), "lifecycle.db")
	is.NoError(t, db.Open(path))
	is.ErrorContains(t, db.Open(path), "already open")

	_, err = db.Query(`not a statement`)
	is.ErrorContains(t, err, "execute query")

	is.NoError(t, db.Close())
	is.NoError(t, db.Close())
	_, err = db.Query(`select * from users`)
	is.ErrorContains(t, err, "not open")

	// A closed client can be reused with a new database path.
	is.NoError(t, db.Open(filepath.Join(t.TempDir(), "reopened.db")))
	is.NoError(t, db.Close())
}
