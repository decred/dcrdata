package internal

import "fmt"

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
)

func makeARRAYOfTEXT(text []string) string {
	if len(text) == 0 {
		return "ARRAY['']"
	}
	sTEXTARRAY := "ARRAY["
	for i, txt := range text {
		if i == len(text)-1 {
			sTEXTARRAY += fmt.Sprintf(`'%s'`, txt)
			break
		}
		sTEXTARRAY += fmt.Sprintf(`'%s', `, txt)
	}
	sTEXTARRAY += "]"
	return sTEXTARRAY
}

func makeARRAYOfUnquotedTEXT(text []string) string {
	if len(text) == 0 {
		return "ARRAY[]"
	}
	sTEXTARRAY := "ARRAY["
	for i, txt := range text {
		if i == len(text)-1 {
			sTEXTARRAY += txt
			break
		}
		sTEXTARRAY += fmt.Sprintf(`%s, `, txt)
	}
	sTEXTARRAY += "]"
	return sTEXTARRAY
}

func makeARRAYOfBIGINTs(ints []uint64) string {
	if len(ints) == 0 {
		return "ARRAY[]::BIGINT[]"
	}
	ARRAY := "ARRAY["
	for i, v := range ints {
		if i == len(ints)-1 {
			ARRAY += fmt.Sprintf(`%d`, v)
			break
		}
		ARRAY += fmt.Sprintf(`%d, `, v)
	}
	ARRAY += "]"
	return ARRAY
}
