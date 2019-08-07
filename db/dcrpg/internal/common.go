// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package internal

import (
	"bytes"
	"strconv"
)

const (
	// IndexExists checks if an index with a given name in certain namespace
	// (schema) exists.
	IndexExists = `SELECT 1
		FROM   pg_class c
		JOIN   pg_namespace n ON n.oid = c.relnamespace
		WHERE  c.relname = $1 AND n.nspname = $2;`

	// IndexIsUnique checks if an index with a given name in certain namespace
	// (schema) exists, and is a UNIQUE index.
	IndexIsUnique = `SELECT indisunique
		FROM   pg_index i
		JOIN   pg_class c ON c.oid = i.indexrelid
		JOIN   pg_namespace n ON n.oid = c.relnamespace
		WHERE  c.relname = $1 AND n.nspname = $2`

	// CreateTestingTable creates the testing table.
	CreateTestingTable = `CREATE TABLE IF NOT EXISTS testing (
		id SERIAL8 PRIMARY KEY,
		timestamp TIMESTAMP,
		timestamptz TIMESTAMPTZ
	);`
)

// makeARRAYOfTEXT parses the string slice into a PostgreSQL-syntax ARRAY
// string.
func makeARRAYOfTEXT(text []string) string {
	if len(text) == 0 {
		return "ARRAY['']"
	}
	buffer := bytes.NewBufferString("ARRAY[")
	for i, txt := range text {
		if i == len(text)-1 {
			buffer.WriteString(`'` + txt + `'`)
			break
		}
		buffer.WriteString(`'` + txt + `', `)
	}
	buffer.WriteString("]")

	return buffer.String()
}

func makeARRAYOfBIGINTs(ints []uint64) string {
	if len(ints) == 0 {
		return "'{}'::BIGINT[]" // cockroachdb: "ARRAY[]:::BIGINT[]"
	}

	buffer := bytes.NewBufferString("'{")
	for i, v := range ints {
		u := strconv.FormatUint(v, 10)
		if i == len(ints)-1 {
			buffer.WriteString(u)
			break
		}
		buffer.WriteString(u + `, `)
	}
	buffer.WriteString("}'::BIGINT[]")

	return buffer.String()
}
