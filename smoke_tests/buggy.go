package smoke_tests

import (
	"database/sql"
	"fmt"
)

// BuggyQuery has a SQL injection vulnerability
func BuggyQuery(db *sql.DB, userInput string) (*sql.Rows, error) {
	query := fmt.Sprintf("SELECT * FROM users WHERE name = '%s'", userInput)
	return db.Query(query)
}

// NullPointerDeref dereferences a pointer without nil check
func NullPointerDeref(p *int) int {
	return *p * 2
}
