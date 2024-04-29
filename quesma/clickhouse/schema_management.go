package clickhouse

import (
	"database/sql"
	"mitmproxy/quesma/logger"
)

type SchemaManagement struct {
	chDb *sql.DB
}

func NewSchemaManagement(chDb *sql.DB) *SchemaManagement {
	return &SchemaManagement{chDb: chDb}
}

func (s *SchemaManagement) readTables(database string) (map[string]map[string]string, error) {
	logger.Debug().Msgf("describing tables: %s", database)

	rows, err := s.chDb.Query("SELECT table, name, type FROM system.columns WHERE database = ?", database)
	if err != nil {
		return map[string]map[string]string{}, err
	}
	defer rows.Close()
	columnsPerTable := make(map[string]map[string]string)
	for rows.Next() {
		var table, colName, colType string
		if err := rows.Scan(&table, &colName, &colType); err != nil {
			return map[string]map[string]string{}, err
		}
		if _, ok := columnsPerTable[table]; !ok {
			columnsPerTable[table] = make(map[string]string)
		}
		columnsPerTable[table][colName] = colType
	}

	return columnsPerTable, nil
}

func (s *SchemaManagement) tableComment(database, table string) (comment string) {

	err := s.chDb.QueryRow("SELECT comment FROM system.tables WHERE database = ? and table = ? ", database, table).Scan(&comment)

	if err != nil {
		logger.Error().Msgf("could not get table comment: %v", err)
	}
	return comment
}
