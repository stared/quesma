// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package queryparser

import (
	"context"
	"github.com/stretchr/testify/assert"
	"quesma/clickhouse"
	"quesma/concurrent"
	"quesma/quesma/config"
	"quesma/schema"
	"testing"
)

type parseRangeTest struct {
	name             string
	rangePartOfQuery QueryMap
	createTableQuery string
	expectedWhere    string
}

var parseRangeTests = []parseRangeTest{
	{
		"DateTime64",
		QueryMap{
			"timestamp": QueryMap{
				"format": "strict_date_optional_time",
				"gte":    "2024-02-02T13:47:16.029Z",
				"lte":    "2024-02-09T13:47:16.029Z",
			},
		},
		`CREATE TABLE ` + tableName + `
		( "message" String, "timestamp" DateTime64(3, 'UTC') )
		ENGINE = Memory`,
		`("timestamp">=parseDateTime64BestEffort('2024-02-02T13:47:16.029Z') AND "timestamp"<=parseDateTime64BestEffort('2024-02-09T13:47:16.029Z'))`,
	},
	{
		"parseDateTimeBestEffort",
		QueryMap{
			"timestamp": QueryMap{
				"format": "strict_date_optional_time",
				"gte":    "2024-02-02T13:47:16.029Z",
				"lte":    "2024-02-09T13:47:16.029Z",
			},
		},
		`CREATE TABLE ` + tableName + `
		( "message" String, "timestamp" DateTime )
		ENGINE = Memory`,
		`("timestamp">=parseDateTimeBestEffort('2024-02-02T13:47:16.029Z') AND "timestamp"<=parseDateTimeBestEffort('2024-02-09T13:47:16.029Z'))`,
	},
	{
		"numeric range",
		QueryMap{
			"time_taken": QueryMap{
				"gt": "100",
			},
		},
		`CREATE TABLE ` + tableName + `
		( "message" String, "timestamp" DateTime, "time_taken" UInt32 )
		ENGINE = Memory`,
		`"time_taken">100`,
	},
	{
		"DateTime64",
		QueryMap{
			"timestamp": QueryMap{
				"format": "strict_date_optional_time",
				"gte":    "2024-02-02T13:47:16",
				"lte":    "2024-02-09T13:47:16",
			},
		},
		`CREATE TABLE ` + tableName + `
		( "message" String, "timestamp" DateTime64(3, 'UTC') )
		ENGINE = Memory`,
		`("timestamp">=parseDateTime64BestEffort('2024-02-02T13:47:16') AND "timestamp"<=parseDateTime64BestEffort('2024-02-09T13:47:16'))`,
	},
}

func Test_parseRange(t *testing.T) {
	s := staticRegistry{
		tables: map[schema.TableName]schema.Schema{
			"logs-generic-default": {
				Fields: map[schema.FieldName]schema.Field{
					"host.name":         {PropertyName: "host.name", InternalPropertyName: "host.name", Type: schema.TypeObject},
					"type":              {PropertyName: "type", InternalPropertyName: "type", Type: schema.TypeText},
					"name":              {PropertyName: "name", InternalPropertyName: "name", Type: schema.TypeText},
					"content":           {PropertyName: "content", InternalPropertyName: "content", Type: schema.TypeText},
					"message":           {PropertyName: "message", InternalPropertyName: "message", Type: schema.TypeText},
					"host_name.keyword": {PropertyName: "host_name.keyword", InternalPropertyName: "host_name.keyword", Type: schema.TypeKeyword},
					"FlightDelay":       {PropertyName: "FlightDelay", InternalPropertyName: "FlightDelay", Type: schema.TypeText},
					"Cancelled":         {PropertyName: "Cancelled", InternalPropertyName: "Cancelled", Type: schema.TypeText},
					"FlightDelayMin":    {PropertyName: "FlightDelayMin", InternalPropertyName: "FlightDelayMin", Type: schema.TypeText},
					"_id":               {PropertyName: "_id", InternalPropertyName: "_id", Type: schema.TypeText},
				},
			},
		},
	}
	for _, test := range parseRangeTests {
		t.Run(test.name, func(t *testing.T) {
			table, err := clickhouse.NewTable(test.createTableQuery, clickhouse.NewNoTimestampOnlyStringAttrCHConfig())
			if err != nil {
				t.Fatal(err)
			}
			assert.NoError(t, err)
			lm := clickhouse.NewLogManager(concurrent.NewMapWith(tableName, table), config.QuesmaConfiguration{})
			cw := ClickhouseQueryTranslator{ClickhouseLM: lm, Table: table, Ctx: context.Background(), SchemaRegistry: s}

			simpleQuery := cw.parseRange(test.rangePartOfQuery)
			assert.Equal(t, test.expectedWhere, simpleQuery.WhereClauseAsString())
		})
	}
}
