package internal

import (
	"bytes"
	"strconv"
)

const (
	IndexExists = `SELECT 1
		FROM   pg_class c
		JOIN   pg_namespace n ON n.oid = c.relnamespace
		WHERE  c.relname = $1 AND n.nspname = $2;`

	IndexIsUnique = `SELECT indisunique
		FROM   pg_index i
		JOIN   pg_class c ON c.oid = i.indexrelid
		JOIN   pg_namespace n ON n.oid = c.relnamespace
		WHERE  c.relname = $1 AND n.nspname = $2`

	CreateTestingTable = `CREATE TABLE IF NOT EXISTS testing (
		id SERIAL8 PRIMARY KEY,
		timestamp TIMESTAMP,
		timestamptz TIMESTAMPTZ
	);`
)

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

func makeARRAYOfUnquotedTEXT(text []string) string {
	if len(text) == 0 {
		return "ARRAY[]"
	}
	buffer := bytes.NewBufferString("ARRAY[")
	for i, txt := range text {
		if i == len(text)-1 {
			buffer.WriteString(txt)
			break
		}
		buffer.WriteString(txt + `, `)
	}
	buffer.WriteString("]")

	return buffer.String()
}

func makeARRAYOfBIGINTs(ints []uint64) string {
	if len(ints) == 0 {
		return "ARRAY[]::BIGINT[]"
	}

	buffer := bytes.NewBufferString("ARRAY[")
	for i, v := range ints {
		u := strconv.FormatUint(v, 10)
		if i == len(ints)-1 {
			buffer.WriteString(u)
			break
		}
		buffer.WriteString(u + `, `)
	}
	buffer.WriteString("]")

	return buffer.String()
}
