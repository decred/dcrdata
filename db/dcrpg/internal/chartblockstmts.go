package internal

const (
	// InsertChartBlock holds DML for Chart block data inserting.
	InsertChartBlock = `INSERT INTO chartblocks VALUES ($1, $2, $3);`

	// SelectChartBlockByTimeRangeSQL holds the query string for selecting chart data by a time range.
	SelectChartBlockByTimeRangeSQL = `SELECT * FROM chartblocks WHERE time BETWEEN $1 AND $2 ORDER BY time LIMIT $3;`

	// SelectAllChartBlocks holds the query string for selecting chart data.
	SelectAllChartBlocks = `SELECT * FROM chartblocks ORDER BY time;`

	// CreateChartBlockTable holds DDL string for creating the chart data table.
	CreateChartBlockTable = `CREATE TABLE IF NOT EXISTS chartblocks (
		height INT4 PRIMARY KEY,
		sbits INT8,
		time INT8
	);`

	// IndexChartBlockTableOnTime is DDL for indexing the chart data table on the time column.
	IndexChartBlockTableOnTime = `CREATE INDEX chartblocks_time_idx ON chartblocks (time);`

	// DeindexChartBlockTableOnTime is DDL for dropping existing index on the chart data table.
	DeindexChartBlockTableOnTime = `DROP INDEX chartblocks_time_idx;`
)
