// Command demo exercises the database through its SQL execution API.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/obayona/godb/ql"
	"github.com/obayona/godb/table"
)

func main() {
	const path = "demo.db"
	_ = os.Remove(path)

	database := &table.DB{Path: path}
	if err := database.Open(); err != nil {
		log.Fatal(err)
	}
	defer func() {
		database.Close()
		_ = os.Remove(path)
	}()

	statements := []string{
		`create table users (id int, name bytes, primary key (id))`,
		`insert into users (id, name) values (1, 'Ada')`,
		`insert into users (id, name) values (2, 'Grace')`,
		`delete from users index by id = 1`,
		`select id, name from users`,
	}

	for _, statement := range statements {
		tx := &table.DBTX{}
		database.Begin(tx)
		result, err := ql.DBTXExecString(tx, []byte(statement))
		if err != nil {
			database.Abort(tx)
			log.Fatalf("%s: %v", statement, err)
		}
		if err := database.Commit(tx); err != nil {
			log.Fatal(err)
		}

		fmt.Printf("> %s\n", statement)
		fmt.Printf("  added=%d updated=%d deleted=%d\n", result.Added, result.Updated, result.Deleted)
		for result.Records != nil && result.Records.Valid() {
			var record table.Record
			if err := result.Records.Deref(&record); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("  %+v\n", record)
			result.Records.Next()
		}
	}
}
