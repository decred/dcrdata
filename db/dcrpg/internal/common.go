// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package internal

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
