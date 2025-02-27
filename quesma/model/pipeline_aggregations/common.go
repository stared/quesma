// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package pipeline_aggregations

import (
	"context"
	"quesma/logger"
	"quesma/model"
	"quesma/util"
	"strings"
)

func parseBucketsPathIntoParentAggregationName(ctx context.Context, bucketsPath string) (parentAggregationName string) {
	const delimiter = ">"
	withoutUnnecessarySuffix, _ := strings.CutSuffix(bucketsPath, delimiter+BucketsPathCount)
	lastDelimiterIdx := strings.LastIndex(withoutUnnecessarySuffix, delimiter)
	if lastDelimiterIdx+1 < len(withoutUnnecessarySuffix) {
		parentAggregationName = withoutUnnecessarySuffix[lastDelimiterIdx+1:]
	} else {
		logger.WarnWithCtx(ctx).Msgf("invalid bucketsPath: %s, withoutUnnecessarySuffix: %s. Using empty string as parent.", bucketsPath, withoutUnnecessarySuffix)
		parentAggregationName = ""
	}
	return
}

func getKey(ctx context.Context, row model.QueryResultRow, query *model.Query) any {
	if len(row.Cols) < 2 {
		logger.WarnWithCtx(ctx).Msgf("row has less than 2 columns: %v", row)
		return nil
	}
	return row.Cols[len(row.Cols)-2].Value
}

// translateSqlResponseToJsonCommon translates rows from DB (maybe postprocessed later), into JSON's format in which
// we want to return them. It is common for a lot of pipeline aggregations
func translateSqlResponseToJsonCommon(ctx context.Context, rows []model.QueryResultRow, aggregationName string) []model.JsonMap {
	if len(rows) == 0 {
		logger.WarnWithCtx(ctx).Msgf("no rows returned for %s aggregation", aggregationName)
		return []model.JsonMap{{}}
	}
	var response []model.JsonMap
	for _, row := range rows {
		response = append(response, model.JsonMap{"value": row.Cols[len(row.Cols)-1].Value})
	}
	return response
}

// calculateResultWhenMissingCommonForDiffAggregations is common for derivative/serial diff aggregations
func calculateResultWhenMissingCommonForDiffAggregations(ctx context.Context, parentRows []model.QueryResultRow, lag int) []model.QueryResultRow {
	resultRows := make([]model.QueryResultRow, 0, len(parentRows))
	if len(parentRows) == 0 {
		return resultRows
	}

	// first "lag" rows have nil value
	rowsWithNilValueCnt := min(lag, len(parentRows))
	for _, parentRow := range parentRows[:rowsWithNilValueCnt] {
		resultRow := parentRow.Copy()
		resultRow.Cols[len(resultRow.Cols)-1].Value = nil
		resultRows = append(resultRows, resultRow)
	}

	// until we find non-null row, still append nils
	firstNonNilIndex := -1
	for i, row := range parentRows[rowsWithNilValueCnt:] {
		if row.LastColValue() != nil {
			firstNonNilIndex = i + rowsWithNilValueCnt
			break
		} else {
			resultRow := row.Copy()
			resultRow.Cols[len(resultRow.Cols)-1].Value = nil
			resultRows = append(resultRows, resultRow)
		}
	}
	if firstNonNilIndex == -1 {
		return resultRows
	}

	// normal calculation at last
	if _, firstRowValueIsFloat := util.ExtractFloat64Maybe(parentRows[firstNonNilIndex].LastColValue()); firstRowValueIsFloat {
		for i, currentRow := range parentRows[firstNonNilIndex:] {
			previousRow := parentRows[i+firstNonNilIndex-rowsWithNilValueCnt]
			previousValueRaw := previousRow.LastColValue()
			previousValue, okPrevious := util.ExtractFloat64Maybe(previousValueRaw)

			currentValueRaw := currentRow.LastColValue()
			currentValue, okCurrent := util.ExtractFloat64Maybe(currentValueRaw)

			var resultValue any
			if okPrevious && okCurrent {
				resultValue = currentValue - previousValue
			} else {
				resultValue = nil
			}
			resultRow := currentRow.Copy()
			resultRow.Cols[len(resultRow.Cols)-1].Value = resultValue
			resultRows = append(resultRows, resultRow)
		}
	} else if _, firstRowValueIsInt := util.ExtractInt64Maybe(parentRows[firstNonNilIndex].LastColValue()); firstRowValueIsInt {
		for i, currentRow := range parentRows[firstNonNilIndex:] {
			previousRow := parentRows[i+firstNonNilIndex-rowsWithNilValueCnt]
			previousValueRaw := previousRow.LastColValue()
			previousValue, okPrevious := util.ExtractInt64Maybe(previousValueRaw)

			currentValueRaw := currentRow.LastColValue()
			currentValue, okCurrent := util.ExtractInt64Maybe(currentValueRaw)

			var resultValue any
			if okPrevious && okCurrent {
				resultValue = currentValue - previousValue
			} else {
				resultValue = nil
			}
			resultRow := currentRow.Copy()
			resultRow.Cols[len(resultRow.Cols)-1].Value = resultValue
			resultRows = append(resultRows, resultRow)
		}
	} else {
		logger.WarnWithCtx(ctx).Msgf("could not convert value to float or int: %v, type: %T. Returning nil values.",
			parentRows[firstNonNilIndex].LastColValue(), parentRows[firstNonNilIndex].LastColValue())
		for _, row := range parentRows[firstNonNilIndex:] {
			resultRow := row.Copy()
			resultRow.Cols[len(resultRow.Cols)-1].Value = nil
			resultRows = append(resultRows, resultRow)
		}
	}
	return resultRows
}
