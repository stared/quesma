package queryparser

import (
	"github.com/stretchr/testify/assert"
	"mitmproxy/quesma/clickhouse"
	"mitmproxy/quesma/concurrent"
	"mitmproxy/quesma/quesma/config"
	"mitmproxy/quesma/testdata"
	"testing"
)

func TestQueryParserAsyncSearch(t *testing.T) {
	table := clickhouse.NewEmptyTable("tablename")
	lm := clickhouse.NewLogManager(concurrent.NewMapWith("tablename", table), config.QuesmaConfiguration{ClickHouseUrl: chUrl})
	cw := ClickhouseQueryTranslator{ClickhouseLM: lm, Table: table}
	for _, tt := range testdata.TestsAsyncSearch {
		t.Run(tt.Name, func(t *testing.T) {
			query, queryInfo := cw.ParseQueryAsyncSearch(tt.QueryJson)
			assert.True(t, query.CanParse)
			assert.Equal(t, tt.WantedParseResult, queryInfo)
		})
	}
}

// TODO this test doesn't work for now, as it's left for next (last) PR
func TestQueryParserAggregation(t *testing.T) {
	table := clickhouse.NewEmptyTable("tablename")
	lm := clickhouse.NewLogManager(concurrent.NewMapWith("tablename", table), config.QuesmaConfiguration{ClickHouseUrl: chUrl})
	cw := ClickhouseQueryTranslator{ClickhouseLM: lm, Table: table}
	for _, tt := range testdata.AggregationTests {
		t.Run(tt.TestName, func(t *testing.T) {
			cw.ParseQueryAsyncSearch(tt.QueryRequestJson)
			// fmt.Println(query, queryInfo)
			// assert.Equal(t, len(tt.WantedSqls), len(queries))
			// for i, wantedSql := range tt.WantedSqls {
			//	assert.Contains(t, wantedSql, queries[i].String())
			// }
		})
	}
}
