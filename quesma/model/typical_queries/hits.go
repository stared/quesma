// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package typical_queries

import (
	"context"
	"fmt"
	"quesma/clickhouse"
	"quesma/elasticsearch"
	"quesma/logger"
	"quesma/model"
	"strconv"
	"time"
)

// Hits is a struct responsible for returning hits part of response.
// There's actually no such aggregation in Elastic.
//
// We still have a couple of distinct handlers for different types of requests, and Hits is one of them.
// We treat it here as if it was a normal aggregation, even though it's technically not completely correct.
// But it works, and because of that we can unify response creation part of Quesma, so it's very useful.
type Hits struct {
	ctx            context.Context
	table          *clickhouse.Table
	highlighter    *model.Highlighter
	sortFieldNames []string
	addSource      bool // true <=> we add hit.Source field to the response
	addScore       bool // true <=> we add hit.Score field to the response (whose value is always 1)
	addVersion     bool // true <=> we add hit.Version field to the response (whose value is always 1)
}

func NewHits(ctx context.Context, table *clickhouse.Table, highlighter *model.Highlighter,
	sortFieldNames []string, addSource, addScore, addVersion bool) Hits {

	return Hits{ctx: ctx, table: table, highlighter: highlighter, sortFieldNames: sortFieldNames,
		addSource: addSource, addScore: addScore, addVersion: addVersion}
}

const (
	defaultScore   = 1 // if we add "score" field, it's always 1
	defaultVersion = 1 // if we  add "version" field, it's always 1
)

func (query Hits) IsBucketAggregation() bool {
	return false
}

func (query Hits) TranslateSqlResponseToJson(rows []model.QueryResultRow, level int) []model.JsonMap {
	hits := make([]model.SearchHit, 0, len(rows))
	for i, row := range rows {
		hit := model.NewSearchHit(query.table.Name)
		if query.addScore {
			hit.Score = defaultScore
		}
		if query.addVersion {
			hit.Version = defaultVersion
		}
		if query.addSource {
			hit.Source = []byte(rows[i].String(query.ctx))
		}
		query.addAndHighlightHit(&hit, &row)

		hit.ID = query.computeIdForDocument(hit, strconv.Itoa(i+1))
		for _, fieldName := range query.sortFieldNames {
			if val, ok := hit.Fields[fieldName]; ok {
				hit.Sort = append(hit.Sort, elasticsearch.FormatSortValue(val[0]))
			} else {
				logger.WarnWithCtx(query.ctx).Msgf("field %s not found in fields", fieldName)
			}
		}
		hits = append(hits, hit)
	}

	return []model.JsonMap{{
		"hits": model.SearchHits{
			Total: &model.Total{
				Value:    len(rows),
				Relation: "eq", // TODO fix in next PR
			},
			Hits: hits,
		},
		"shards": model.ResponseShards{
			Total:      1,
			Successful: 1,
			Failed:     0,
		},
	}}
}

func (query Hits) addAndHighlightHit(hit *model.SearchHit, resultRow *model.QueryResultRow) {
	for _, col := range resultRow.Cols {
		if col.Value == nil {
			continue // We don't return empty value
		}
		columnName := col.ColName
		hit.Fields[columnName] = []interface{}{col.Value}
		if query.highlighter.ShouldHighlight(columnName) {
			// check if we have a string here and if so, highlight it
			switch valueAsString := col.Value.(type) {
			case string:
				hit.Highlight[columnName] = query.highlighter.HighlightValue(columnName, valueAsString)
			case *string:
				if valueAsString != nil {
					hit.Highlight[columnName] = query.highlighter.HighlightValue(columnName, *valueAsString)
				}
			default:
				logger.WarnWithCtx(query.ctx).Msgf("unknown type for hit highlighting: %T, value: %v", col.Value, col.Value)
			}
		}
	}

	// TODO: highlight and field checks
	for _, alias := range query.table.AliasList() {
		if v, ok := hit.Fields[alias.TargetFieldName]; ok {
			hit.Fields[alias.SourceFieldName] = v
		}
	}
}

func (query Hits) computeIdForDocument(doc model.SearchHit, defaultID string) string {
	tsFieldName, err := query.table.GetTimestampFieldName()
	if err != nil {
		return defaultID
	}

	var pseudoUniqueId string

	if v, ok := doc.Fields[tsFieldName]; ok {
		if vv, okk := v[0].(time.Time); okk {
			// At database level we only compare timestamps with millisecond precision
			// However in search results we append `q` plus generated digits (we use q because it's not in hex)
			// so that kibana can iterate over documents in UI
			pseudoUniqueId = fmt.Sprintf("%xq%s", vv, defaultID)
		} else {
			logger.WarnWithCtx(query.ctx).Msgf("failed to convert timestamp field [%v] to time.Time", v[0])
			return defaultID
		}
	}
	return pseudoUniqueId
}

func (query Hits) String() string {
	return fmt.Sprintf("hits(table: %v)", query.table.Name)
}

func (query Hits) PostprocessResults(rowsFromDB []model.QueryResultRow) []model.QueryResultRow {
	return rowsFromDB
}
