package ql

import (
	"path/filepath"
	"testing"

	"github.com/obayona/godb/btree"
	"github.com/obayona/godb/table"
	is "github.com/stretchr/testify/require"
)

func parseScan(t *testing.T, query string) *QLScan {
	t.Helper()
	stmt, err := fullStmt([]byte(query))
	is.NoError(t, err)
	selectStmt, ok := stmt.(*QLSelect)
	is.True(t, ok)
	return &selectStmt.QLScan
}

func plannerTableDef() *table.TableDef {
	return &table.TableDef{
		Name:  "users",
		Cols:  []string{"id", "email", "city", "age", "name"},
		Types: []uint32{table.TYPE_INT64, table.TYPE_BYTES, table.TYPE_BYTES, table.TYPE_INT64, table.TYPE_BYTES},
		Indexes: [][]string{
			{"id"},
			{"email", "id"},
			{"city", "age", "id"},
		},
	}
}

func TestPlannerChoosesSecondaryIndex(t *testing.T) {
	req := parseScan(t, `select * from users filter email = 'ada@example.com'`)
	sc := table.Scanner{}
	is.NoError(t, qlScanInit(req, plannerTableDef(), &sc))
	is.Equal(t, []string{"email"}, sc.Key1.Cols)
	is.Equal(t, sc.Key1, sc.Key2)
	is.Equal(t, btree.CMP_GE, sc.Cmp1)
	is.Equal(t, btree.CMP_LE, sc.Cmp2)
}

func TestPlannerChoosesCompositeRange(t *testing.T) {
	req := parseScan(t, `select * from users filter city = 'Quito' and age >= 18 and age < 30`)
	sc := table.Scanner{}
	is.NoError(t, qlScanInit(req, plannerTableDef(), &sc))
	is.Equal(t, []string{"city", "age"}, sc.Key1.Cols)
	is.Equal(t, []string{"city", "age"}, sc.Key2.Cols)
	is.Equal(t, btree.CMP_GE, sc.Cmp1)
	is.Equal(t, btree.CMP_LT, sc.Cmp2)
}

func TestPlannerFallsBackToFullScan(t *testing.T) {
	tests := []string{
		`select * from users filter email = 'a' or id = 1`,
		`select * from users filter age = 18`, // not a leading index column
		`select * from users filter name + '!' = 'Ada!'`,
	}
	for _, query := range tests {
		req := parseScan(t, query)
		sc := table.Scanner{}
		is.NoError(t, qlScanInit(req, plannerTableDef(), &sc))
		is.Empty(t, sc.Key1.Cols)
		is.Empty(t, sc.Key2.Cols)
		is.Equal(t, btree.CMP_GE, sc.Cmp1)
		is.Equal(t, btree.CMP_LE, sc.Cmp2)
	}
}

func TestIndexBySyntaxRemoved(t *testing.T) {
	_, err := fullStmt([]byte(`select * from users index by id = 1`))
	is.Error(t, err)
}

func TestAutomaticPlanningEndToEnd(t *testing.T) {
	db := table.DB{Path: filepath.Join(t.TempDir(), "planner.db")}
	is.NoError(t, db.Open())
	t.Cleanup(db.Close)

	exec := func(statement string) QLResult {
		t.Helper()
		tx := table.DBTX{}
		db.Begin(&tx)
		result, err := DBTXExecString(&tx, []byte(statement))
		if err != nil {
			db.Abort(&tx)
			is.NoError(t, err)
		}
		is.NoError(t, db.Commit(&tx))
		return result
	}

	exec(`create table users (id int, email bytes, name bytes, primary key (id), index (email))`)
	exec(`insert into users (id, email, name) values (1, 'ada@example.com', 'Ada')`)
	exec(`insert into users (id, email, name) values (2, 'grace@example.com', 'Grace')`)

	result := exec(`select id, name from users filter email = 'grace@example.com'`)
	is.True(t, result.Records.Valid())
	record := table.Record{}
	is.NoError(t, result.Records.Deref(&record))
	is.Equal(t, int64(2), record.Get("id").I64)
	is.Equal(t, []byte("Grace"), record.Get("name").Str)
	result.Records.Next()
	is.False(t, result.Records.Valid())

	result = exec(`select id from users filter name + '!' = 'Ada!'`)
	is.True(t, result.Records.Valid()) // complex predicate is evaluated after a full scan
	record = table.Record{}
	is.NoError(t, result.Records.Deref(&record))
	is.Equal(t, int64(1), record.Get("id").I64)

	result = exec(`delete from users filter id = 1`)
	is.Equal(t, uint64(1), result.Deleted)
}
