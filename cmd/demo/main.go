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
		`create table users (id int, name bytes, email bytes, age int, primary key (id), index (email), index (age))`,
		`insert into users (id, name, email, age) values (1, 'Ada', 'ada@example.com', 36)`,
		`insert into users (id, name, email, age) values (2, 'Grace', 'grace@example.com', 28)`,
		`insert into users (id, name, email, age) values (3, 'Linus', 'linus@example.com', 33)`,
		// Uses the secondary email index automatically.
		`select id, name from users filter email = 'grace@example.com'`,
		// Uses the secondary age index as a bounded range.
		`select id, name, age from users filter age >= 30 and age < 40`,
		// Computed predicates are correct via the full-scan fallback.
		`select id, name from users filter name + '!' = 'Ada!'`,
		`delete from users filter id = 1`,
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
