// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package quesma

import (
	"fmt"
	"quesma/clickhouse"
	"quesma/logger"
	"quesma/model"
	"quesma/schema"
	"strings"
)

//
//
// Do not use `arrayJoin` here. It's considered harmful.
//
//
//

type ArrayTypeVisitor struct {
	tableName string
	table     *clickhouse.Table

	// deps
	schemaRegistry schema.Registry
	logManager     *clickhouse.LogManager
	schema         schema.Schema
}

func (v *ArrayTypeVisitor) visitChildren(args []model.Expr) []model.Expr {
	var newArgs []model.Expr
	for _, arg := range args {
		if arg != nil {
			newArgs = append(newArgs, arg.Accept(v).(model.Expr))
		}
	}
	return newArgs
}

func (v *ArrayTypeVisitor) dbColumnType(fieldName string) string {

	//
	// This is a HACK to get the column database type from the schema
	//
	//
	fieldName = strings.TrimSuffix(fieldName, ".keyword")

	tableColumnName := strings.ReplaceAll(fieldName, ".", "::")
	col, ok := v.table.Cols[tableColumnName]
	if ok {
		return col.Type.String()
	}

	return ""
}

func (v *ArrayTypeVisitor) VisitLiteral(e model.LiteralExpr) interface{} { return e }
func (v *ArrayTypeVisitor) VisitInfix(e model.InfixExpr) interface{} {

	column, ok := e.Left.(model.ColumnRef)
	if ok {
		dbType := v.dbColumnType(column.ColumnName)

		if strings.HasPrefix(dbType, "Array") {

			op := strings.ToUpper(e.Op)

			switch {

			case (op == "ILIKE" || op == "LIKE") && dbType == "Array(String)":

				variableName := "x"
				lambda := model.NewLambdaExpr([]string{variableName}, model.NewInfixExpr(model.NewLiteral(variableName), op, e.Right.Accept(v).(model.Expr)))
				return model.NewFunction("arrayExists", lambda, e.Left)

			case e.Op == "=":
				return model.NewFunction("has", e.Left, e.Right.Accept(v).(model.Expr))

			default:
				logger.Warn().Msgf("Unhandled array infix operation  %s, column %v (%v)", e.Op, column.ColumnName, dbType)
			}
		}
	}

	left := e.Left.Accept(v).(model.Expr)
	right := e.Right.Accept(v).(model.Expr)

	return model.NewInfixExpr(left, e.Op, right)
}

func (v *ArrayTypeVisitor) VisitPrefixExpr(e model.PrefixExpr) interface{} {

	args := v.visitChildren(e.Args)

	return model.NewPrefixExpr(e.Op, args)

}
func (v *ArrayTypeVisitor) VisitFunction(e model.FunctionExpr) interface{} {

	if len(e.Args) == 1 {
		arg := e.Args[0]
		column, ok := arg.(model.ColumnRef)
		if ok {
			dbType := v.dbColumnType(column.ColumnName)
			if strings.HasPrefix(dbType, "Array") {
				switch {

				case e.Name == "sumOrNull" && dbType == "Array(Int64)":
					fnName := model.LiteralExpr{Value: fmt.Sprintf("'%s'", e.Name)}
					wrapped := model.NewFunction("arrayReduce", fnName, column)
					wrapped = model.NewFunction(e.Name, wrapped)
					return wrapped

				default:
					logger.Warn().Msgf("Unhandled array function %s, column %v (%v)", e.Name, column.ColumnName, dbType)

				}
			}
		}
	}

	args := v.visitChildren(e.Args)
	return model.NewFunction(e.Name, args...)
}
func (v *ArrayTypeVisitor) VisitColumnRef(e model.ColumnRef) interface{} {

	return e
}

func (v *ArrayTypeVisitor) VisitNestedProperty(e model.NestedProperty) interface{} {

	return model.NestedProperty{
		ColumnRef:    e.ColumnRef.Accept(v).(model.ColumnRef),
		PropertyName: e.PropertyName.Accept(v).(model.LiteralExpr),
	}
}
func (v *ArrayTypeVisitor) VisitArrayAccess(e model.ArrayAccess) interface{} {
	return model.ArrayAccess{
		ColumnRef: e.ColumnRef.Accept(v).(model.ColumnRef),
		Index:     e.Index.Accept(v).(model.Expr),
	}
}
func (v *ArrayTypeVisitor) VisitMultiFunction(e model.MultiFunctionExpr) interface{} {

	args := v.visitChildren(e.Args)
	return model.MultiFunctionExpr{Name: e.Name, Args: args}
}

func (v *ArrayTypeVisitor) VisitString(e model.StringExpr) interface{} { return e }
func (v *ArrayTypeVisitor) VisitOrderByExpr(e model.OrderByExpr) interface{} {

	exprs := v.visitChildren(e.Exprs)

	return model.NewOrderByExpr(exprs, e.Direction)

}
func (v *ArrayTypeVisitor) VisitDistinctExpr(e model.DistinctExpr) interface{} {

	return model.NewDistinctExpr(e.Expr.Accept(v).(model.Expr))
}
func (v *ArrayTypeVisitor) VisitTableRef(e model.TableRef) interface{} {
	return model.NewTableRef(e.Name)
}
func (v *ArrayTypeVisitor) VisitAliasedExpr(e model.AliasedExpr) interface{} {
	return model.NewAliasedExpr(e.Expr.Accept(v).(model.Expr), e.Alias)
}
func (v *ArrayTypeVisitor) VisitWindowFunction(e model.WindowFunction) interface{} {

	return model.NewWindowFunction(e.Name, v.visitChildren(e.Args), v.visitChildren(e.PartitionBy), e.OrderBy.Accept(v).(model.OrderByExpr))

}

func (v *ArrayTypeVisitor) VisitSelectCommand(e model.SelectCommand) interface{} {

	if v.schemaRegistry == nil {
		logger.Error().Msg("Schema registry is not set")
		return e
	}
	sch, exists := v.schemaRegistry.FindSchema(schema.TableName(v.tableName))

	if !exists {
		logger.Error().Msgf("Schema fot table %s not found", v.tableName)
		return e
	}
	v.schema = sch

	table := v.logManager.FindTable(v.tableName)
	if table == nil {
		logger.Error().Msgf("Table %s not found", v.tableName)
		return e
	}

	v.table = table

	// check if the query has array columns

	var allColumns []model.ColumnRef
	for _, expr := range e.Columns {
		allColumns = append(allColumns, model.GetUsedColumns(expr)...)
	}
	if e.WhereClause != nil {
		allColumns = append(allColumns, model.GetUsedColumns(e.WhereClause)...)
	}

	hasArrayColumn := false
	for _, col := range allColumns {
		dbType := v.dbColumnType(col.ColumnName)
		if strings.HasPrefix(dbType, "Array") {
			hasArrayColumn = true
			break
		}
	}
	// no array columns, no need to transform
	if !hasArrayColumn {
		return e
	}

	// transformation

	var groupBy []model.Expr
	for _, expr := range e.GroupBy {
		groupBy = append(groupBy, expr.Accept(v).(model.Expr))
	}

	var columns []model.Expr
	for _, expr := range e.Columns {
		columns = append(columns, expr.Accept(v).(model.Expr))
	}

	var fromClause model.Expr
	if e.FromClause != nil {
		fromClause = e.FromClause.Accept(v).(model.Expr)
	}

	var whereClause model.Expr
	if e.WhereClause != nil {
		whereClause = e.WhereClause.Accept(v).(model.Expr)
	}

	return model.NewSelectCommand(columns, groupBy, e.OrderBy,
		fromClause, whereClause, e.Limit, e.SampleLimit, e.IsDistinct)

}

func (v *ArrayTypeVisitor) VisitParenExpr(p model.ParenExpr) interface{} {
	var exprs []model.Expr
	for _, expr := range p.Exprs {
		exprs = append(exprs, expr.Accept(v).(model.Expr))
	}
	return model.NewParenExpr(exprs...)
}

func (v *ArrayTypeVisitor) VisitLambdaExpr(e model.LambdaExpr) interface{} {
	return model.NewLambdaExpr(e.Args, e.Body.Accept(v).(model.Expr))
}
