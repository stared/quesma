package queryparser

import (
	"fmt"
	"log"
	"mitmproxy/quesma/clickhouse"
	"mitmproxy/quesma/model"
	"time"
)

type ClickhouseQueryTranslator struct {
	ClickhouseLM *clickhouse.LogManager
}

type ClickhouseResultReader struct {
}

func NewClickhouseResultReader() *ClickhouseResultReader {
	return &ClickhouseResultReader{}
}

// TODO come back to (int, error) return type?
func (cw *ClickhouseQueryTranslator) Write(buf []byte) model.Query {
	query := cw.parseQuery(string(buf))
	return query
}

func (cw *ClickhouseQueryTranslator) WriteAsyncSearch(buf []byte) (model.Query, model.QueryInfo) {
	log.Println("ClickhouseQueryTranslator.WriteAsyncSearch, buf: ", string(buf))
	query, queryInfo := cw.parseQueryAsyncSearch(string(buf))
	log.Printf("ClickhouseQueryTranslator.WriteAsyncSearch, queryInfo: %+v, query: %+v", queryInfo, query)
	return query, queryInfo
}

func MakeResponse[T fmt.Stringer](ResultSet []T, header string) ([]byte, error) {
	var newBuf []byte
	newBuf = append(newBuf, []byte("{\n")...)
	newBuf = append(newBuf, []byte(`"`+header+`": [`)...)
	numRows := len(ResultSet)
	i := 0
	for _, row := range ResultSet {
		newBuf = append(newBuf, []byte(row.String())...)
		if i < numRows-1 {
			newBuf = append(newBuf, []byte(",\n")...)
		}
		i++
	}
	newBuf = append(newBuf, []byte("]}")...)
	return newBuf, nil
}

func (cw *ClickhouseQueryTranslator) GetAttributesList(tableName string) []clickhouse.Attribute {
	return cw.ClickhouseLM.GetAttributesList(tableName)
}

func (cw *ClickhouseQueryTranslator) GetFieldInfo(tableName string, fieldName string) clickhouse.FieldInfo {
	return cw.ClickhouseLM.GetFieldInfo(tableName, fieldName)
}

// TODO flatten tuples, I think (or just don't support them for now, we don't want them at the moment in production schemas)
func (cw *ClickhouseQueryTranslator) GetFieldsList(tableName string) []string {
	return []string{"message"}
}

func (cw *ClickhouseQueryTranslator) QueryClickhouse(query model.Query) ([]clickhouse.QueryResultRow, error) {
	return cw.ClickhouseLM.ProcessSelectQuery(query)
}

// fieldName = "*" -> we query all, otherwise only this 1 field
func (cw *ClickhouseQueryTranslator) GetNMostRecentRows(tableName, fieldName, timestampFieldName, originalSelectStmt string, limit int) ([]clickhouse.QueryResultRow, error) {
	return cw.ClickhouseLM.GetNMostRecentRows(tableName, fieldName, timestampFieldName, originalSelectStmt, limit)
}

func (cw *ClickhouseQueryTranslator) GetHistogram(tableName string) ([]clickhouse.HistogramResult, error) {
	return cw.ClickhouseLM.GetHistogram(tableName, "@timestamp", 15*time.Minute)
}

//lint:ignore U1000 Not used yet
func (cw *ClickhouseQueryTranslator) GetAutocompleteSuggestions(tableName, fieldName string, prefix string, limit int) ([]clickhouse.QueryResultRow, error) {
	return cw.ClickhouseLM.GetAutocompleteSuggestions(tableName, fieldName, prefix, limit)
}

func (cw *ClickhouseQueryTranslator) GetFacets(tableName, fieldName, originalSelectStmt string, limit int) ([]clickhouse.QueryResultRow, error) {
	return cw.ClickhouseLM.GetFacets(tableName, fieldName, originalSelectStmt, limit)
}
