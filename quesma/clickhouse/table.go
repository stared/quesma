// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package clickhouse

import (
	"context"
	"fmt"
	"quesma/logger"
	"quesma/model"
	"quesma/quesma/config"
	"quesma/util"
	"sort"
	"strconv"
	"strings"
)

type Table struct {
	Name         string
	DatabaseName string `default:""`
	Cluster      string `default:""`
	Cols         map[string]*Column
	Config       *ChTableConfig
	Created      bool // do we need to create it during first insert
	indexes      []IndexStatement
	aliases      map[string]string //deprecated
	// we should use aliases directly from configuration, not store them here
	Comment          string // this human-readable comment
	CreateTableQuery string
	TimestampColumn  *string
}

func (t *Table) GetFulltextFields() []string {
	var res = make([]string, 0)
	for _, col := range t.Cols {
		if col.IsFullTextMatch {
			res = append(res, col.Name)
		}
	}
	return res
}

func (t *Table) createTableOurFieldsString() []string {
	rows := make([]string, 0)
	if t.Config.hasOthers {
		_, ok := t.Cols[othersFieldName]
		if !ok {
			rows = append(rows, fmt.Sprintf("%s\"%s\" JSON", util.Indent(1), othersFieldName))
		}
	}
	if t.Config.hasTimestamp {
		_, ok := t.Cols[timestampFieldName]
		if !ok {
			defaultStr := ""
			if t.Config.timestampDefaultsNow {
				defaultStr = " DEFAULT now64()"
			}
			rows = append(rows, fmt.Sprintf("%s\"%s\" DateTime64(3)%s", util.Indent(1), timestampFieldName, defaultStr))
		}
	}
	if len(t.Config.attributes) > 0 {
		for _, a := range t.Config.attributes {
			_, ok := t.Cols[a.KeysArrayName]
			if !ok {
				rows = append(rows, fmt.Sprintf("%s\"%s\" Array(String)", util.Indent(1), a.KeysArrayName))
			}
			_, ok = t.Cols[a.ValuesArrayName]
			if !ok {
				rows = append(rows, fmt.Sprintf("%s\"%s\" Array(%s)", util.Indent(1), a.ValuesArrayName, a.Type.String()))
			}
		}
	}
	return rows
}

// it will be removed soon,
// we should rely on metadata from clickhouse
// And we shouldn't use '*'. All columns should be explicitly defined.
func (t *Table) applyTableSchema(query *model.Query) {
	var newColumns []model.Expr
	var hasWildcard bool

	for _, selectColumn := range query.SelectCommand.Columns {

		if selectColumn == model.NewWildcardExpr {
			hasWildcard = true
		} else {
			newColumns = append(newColumns, selectColumn)
		}
	}

	if hasWildcard {

		cols := make([]string, 0, len(t.Cols))
		for _, col := range t.Cols {
			cols = append(cols, col.Name)
		}
		sort.Strings(cols)

		for _, col := range cols {
			newColumns = append(newColumns, model.NewColumnRef(col))
		}
	}

	query.SelectCommand.Columns = newColumns
}

func (t *Table) extractColumns(query *model.Query, addNonSchemaFields bool) ([]string, error) {

	N := len(query.SelectCommand.Columns)
	if query.SelectCommand.IsWildcard() {
		N = len(t.Cols)
	}
	cols := make([]string, 0, N)
	if query.SelectCommand.IsWildcard() {
		for _, col := range t.Cols {
			cols = append(cols, col.Name)
		}
	} else {
		for _, selectColumn := range query.SelectCommand.Columns {
			switch selectCol := selectColumn.(type) {
			case model.ColumnRef:
				colName := selectCol.ColumnName
				_, ok := t.Cols[colName]
				if !ok {
					return nil, fmt.Errorf("column %s not found in table %s", selectCol, t.Name)
				}

				cols = append(cols, colName)
			default:
				cols = append(cols, model.AsString(selectCol))
			}
		}
	}
	return cols, nil
}

func (t *Table) createTableString() string {
	s := "CREATE TABLE IF NOT EXISTS " + t.FullTableName() + " (\n"
	rows := make([]string, 0)
	for _, col := range t.Cols {
		rows = append(rows, col.createTableString(1))
	}
	rows = append(rows, t.createTableOurFieldsString()...)
	for _, index := range t.indexes {
		rows = append(rows, util.Indent(1)+index.statement())
	}
	return s + strings.Join(rows, ",\n") + "\n)\n" + t.Config.CreateTablePostFieldsString()
}

// FullTableName returns full table name with database name if it's not empty.
// in a format: ["database".]"table" as it seems to work for all cases in Clickhouse.
// You can use it in any query to Clickhouse, e.g. in FROM ... clause.
func (t *Table) FullTableName() string {
	if t.DatabaseName != "" {
		return strconv.Quote(t.DatabaseName) + "." + strconv.Quote(t.Name)
	} else {
		return strconv.Quote(t.Name)
	}
}

// GetDateTimeType returns type of a field (currently DateTime/DateTime64), if it's a DateTime type. Invalid otherwise.
// Timestamp from config defaults to DateTime64.
func (t *Table) GetDateTimeType(ctx context.Context, fieldName string) DateTimeType {
	if col, ok := t.Cols[fieldName]; ok {
		typeName := col.Type.String()
		// hasPrefix, not equal, because we can have DateTime64(3) and we want to catch it
		if strings.HasPrefix(typeName, "DateTime64") {
			return DateTime64
		}
		if strings.HasPrefix(typeName, "DateTime") {
			return DateTime
		}
	}
	if t.Config.hasTimestamp && fieldName == timestampFieldName {
		return DateTime64
	}
	return Invalid
}

// applyIndexConfig applies full text search and alias configuration to the table
func (t *Table) applyIndexConfig(configuration config.QuesmaConfiguration) {
	for _, c := range t.Cols {
		c.IsFullTextMatch = configuration.IsFullTextMatchField(t.Name, c.Name)
	}

	aliasFields := configuration.AliasFields(t.Name)
	if len(aliasFields) > 0 {
		t.aliases = make(map[string]string)
		for _, alias := range aliasFields {
			if _, ok := t.Cols[alias.TargetFieldName]; !ok {
				logger.Warn().Msgf("target field '%s' for field '%s' not found in table '%s'",
					alias.TargetFieldName, alias.SourceFieldName, t.Name)
				continue
			}
			t.aliases[alias.SourceFieldName] = alias.TargetFieldName
		}
	}
	if v, ok := configuration.IndexConfig[t.Name]; ok {
		t.TimestampColumn = v.TimestampField
	}

}

func (t *Table) HasColumn(ctx context.Context, fieldName string) bool {
	return t.Cols[fieldName] != nil
}

func (t *Table) AliasFields(ctx context.Context) []*Column {
	aliasFields := make([]*Column, 0)
	for key, val := range t.aliases {
		col := t.Cols[val]
		if col == nil {
			logger.ErrorWithCtx(ctx).Msgf("alias '%s' for field '%s' not found in table '%s'", val, key, t.Name)
			continue
		}
		aliasFields = append(aliasFields, &Column{
			Name:            key,
			Type:            col.Type,
			Modifiers:       col.Modifiers,
			IsFullTextMatch: col.IsFullTextMatch,
		})
	}
	return aliasFields
}

func (t *Table) AliasList() []config.FieldAlias {
	result := make([]config.FieldAlias, 0)
	for key, val := range t.aliases {
		result = append(result, config.FieldAlias{
			SourceFieldName: key,
			TargetFieldName: val,
		})
	}
	return result
}

func (t *Table) GetAttributesList() []Attribute {
	return t.Config.attributes
}

// TODO Won't work with tuples, e.g. trying to access via tupleName.tupleField will return NotExists,
// instead of some other response. Fix this when needed (we seem to not need tuples right now)
func (t *Table) GetFieldInfo(ctx context.Context, fieldName string) FieldInfo {
	col, ok := t.Cols[fieldName]
	if !ok {
		return NotExists
	}
	if col.isArray() {
		return ExistsAndIsArray
	}
	return ExistsAndIsBaseType
}

func (t *Table) GetTimestampFieldName() (string, error) {
	if t.TimestampColumn != nil {
		return *t.TimestampColumn, nil
	} else {
		return "", fmt.Errorf("no timestamp field configured for table %s", t.Name)
	}
}
