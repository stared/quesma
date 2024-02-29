package clickhouse

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/ClickHouse/clickhouse-go/v2"
	"mitmproxy/quesma/concurrent"
	"mitmproxy/quesma/index"
	"mitmproxy/quesma/jsonprocessor"
	"mitmproxy/quesma/logger"
	"mitmproxy/quesma/quesma/config"
	"mitmproxy/quesma/util"
	"regexp"
	"strings"
)

const (
	timestampFieldName = "@timestamp"
	othersFieldName    = "others"
)

type (
	LogManager struct {
		chDb             *sql.DB
		tableDefinitions TableMap
		cfg              config.QuesmaConfiguration
	}
	TableMap  = *concurrent.Map[string, *Table]
	SchemaMap = map[string]interface{} // TODO remove
	Attribute struct {
		KeysArrayName   string
		ValuesArrayName string
		Type            BaseType
	}
	ChTableConfig struct {
		hasTimestamp bool // does table have 'timestamp' field
		// allow_suspicious_ttl_expressions=1 to enable TTL without date field (doesn't work for me!)
		// also be very cautious with it and test it beforehand, people say it doesn't work properly
		// TODO make sure it's unique in schema (there's no other 'timestamp' field)
		// I (Krzysiek) can write it quickly, but don't want to waste time for it right now.
		timestampDefaultsNow bool
		engine               string // "Log", "MergeTree", etc.
		orderBy              string // "" if none
		partitionBy          string // "" if none
		primaryKey           string // "" if none
		settings             string // "" if none
		ttl                  string // of type Interval, e.g. 3 MONTH, 1 YEAR
		// look https://clickhouse.com/docs/en/sql-reference/data-types/special-data-types/interval
		// "" if none
		hasOthers bool // has additional "others" JSON field for out of schema values
		// TODO make sure it's unique in schema (there's no other 'others' field)
		// I (Krzysiek) can write it quickly, but don't want to waste time for it right now.
		attributes                            []Attribute
		castUnsupportedAttrValueTypesToString bool // if we have e.g. only attrs (String, String), we'll cast e.g. Date to String
		preferCastingToOthers                 bool // we'll put non-schema field in [String, String] attrs map instead of others, if we have both options
	}
)

func (lm *LogManager) Start() {
	if err := lm.initConnection(); err != nil {
		logger.Error().Msgf("could not connect to clickhouse. error: %v", err)
	}

	lm.loadTables() // TODO fetch from config
}

func (lm *LogManager) loadTables() {
	configuredTables := make(map[string]map[string]string)
	databaseName := "default"
	if lm.cfg.ClickHouseDatabase != nil {
		databaseName = *lm.cfg.ClickHouseDatabase
	}
	if tables, err := lm.describeTables(databaseName); err != nil {
		logger.Error().Msgf("could not describe tables: %v", err)
		return
	} else {
		for table, columns := range tables {
			if indexConfig, found := lm.cfg.GetIndexConfig(table); found {
				if indexConfig.Enabled {
					configuredTables[table] = columns
				} else {
					logger.Debug().Msgf("table '%s' is disabled\n", table)
				}
			} else {
				logger.Info().Msgf("table '%s' not configured explicitly\n", table)
			}
		}
	}

	logger.Info().Msgf("discovered tables: [%s]", strings.Join(util.MapKeys(configuredTables), ","))

	populateTableDefinitions(configuredTables, databaseName, lm)
}

func (lm *LogManager) describeTables(database string) (map[string]map[string]string, error) {
	logger.Debug().Msgf("describing tables: %s", database)

	if err := lm.initConnection(); err != nil {
		return map[string]map[string]string{}, err
	}
	rows, err := lm.chDb.Query("SELECT table, name, type FROM system.columns WHERE database = ?", database)
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

func (lm *LogManager) Close() {
	_ = lm.chDb.Close()
}

func withDefault(optStr *string, def string) string {
	if optStr == nil {
		return def
	}
	return *optStr
}

func (lm *LogManager) initConnection() error {
	if lm.chDb == nil {
		options := clickhouse.Options{Addr: []string{lm.cfg.ClickHouseUrl.Host}}
		if lm.cfg.ClickHouseUser != nil || lm.cfg.ClickHousePassword != nil || lm.cfg.ClickHouseDatabase != nil {
			options.TLS = &tls.Config{
				InsecureSkipVerify: true, // TODO: fix it
			}

			options.Auth = clickhouse.Auth{
				Username: withDefault(lm.cfg.ClickHouseUser, ""),
				Password: withDefault(lm.cfg.ClickHousePassword, ""),
				Database: withDefault(lm.cfg.ClickHouseDatabase, ""),
			}
		}

		lm.chDb = clickhouse.OpenDB(&options)
	}
	return lm.chDb.Ping()
}

func (lm *LogManager) matchIndex(indexNamePattern, indexName string) bool {
	r, err := regexp.Compile("^" + strings.Replace(indexNamePattern, "*", ".*", -1) + "$")
	if err != nil {
		logger.Error().Msgf("invalid index name pattern [%s]: %s", indexNamePattern, err)
		return false
	}
	return r.MatchString(indexName)
}

// Indexes can be in a form of wildcard, e.g. "index-*"
// If we have such index, we need to resolve it to a real table name.
func (lm *LogManager) ResolveTableName(index string) string {
	for k := range lm.tableDefinitions.Snapshot() {
		if lm.matchIndex(index, k) {
			return k
		}
	}
	return ""
}

// updates also Table TODO stop updating table here, find a better solution
func addOurFieldsToCreateTableQuery(q string, config *ChTableConfig, table *Table) string {
	if !config.hasOthers && len(config.attributes) == 0 {
		_, ok := table.Cols[timestampFieldName]
		if !config.hasTimestamp || ok {
			return q
		}
	}

	othersStr, timestampStr, attributesStr := "", "", ""
	if config.hasOthers {
		_, ok := table.Cols[othersFieldName]
		if !ok {
			othersStr = fmt.Sprintf("%s\"%s\" JSON,\n", util.Indent(1), othersFieldName)
			table.Cols[othersFieldName] = &Column{Name: othersFieldName, Type: NewBaseType("JSON")}
		}
	}
	if config.hasTimestamp {
		_, ok := table.Cols[timestampFieldName]
		if !ok {
			defaultStr := ""
			if config.timestampDefaultsNow {
				defaultStr = " DEFAULT now64()"
			}
			timestampStr = fmt.Sprintf("%s\"%s\" DateTime64(3)%s,\n", util.Indent(1), timestampFieldName, defaultStr)
			table.Cols[timestampFieldName] = &Column{Name: timestampFieldName, Type: NewBaseType("DateTime64")}
		}
	}
	if len(config.attributes) > 0 {
		for _, a := range config.attributes {
			_, ok := table.Cols[a.KeysArrayName]
			if !ok {
				attributesStr += fmt.Sprintf("%s\"%s\" Array(String),\n", util.Indent(1), a.KeysArrayName)
				table.Cols[a.KeysArrayName] = &Column{Name: a.KeysArrayName, Type: CompoundType{Name: "Array", BaseType: NewBaseType("String")}}
			}
			_, ok = table.Cols[a.ValuesArrayName]
			if !ok {
				attributesStr += fmt.Sprintf("%s\"%s\" Array(%s),\n", util.Indent(1), a.ValuesArrayName, a.Type.String())
				table.Cols[a.ValuesArrayName] = &Column{Name: a.ValuesArrayName, Type: a.Type}
			}
		}
	}

	i := strings.Index(q, "(")
	return q[:i+2] + othersStr + timestampStr + attributesStr + q[i+1:]
}

func (lm *LogManager) sendCreateTableQuery(query string) error {
	if err := lm.initConnection(); err != nil {
		return err
	}
	if _, err := lm.chDb.Exec(query); err != nil {
		return fmt.Errorf("error in sendCreateTableQuery: query: %s\nerr:%v", query, err)
	}
	return nil
}

func (lm *LogManager) ProcessCreateTableQuery(query string, config *ChTableConfig) error {
	table, err := NewTable(query, config)
	if err != nil {
		return err
	}

	// if exists only then createTable
	noSuchTable := lm.addSchemaIfDoesntExist(table)
	if !noSuchTable {
		return fmt.Errorf("table %s already exists", table.Name)
	}

	return lm.sendCreateTableQuery(addOurFieldsToCreateTableQuery(query, config, table))
}

func buildCreateTableQueryNoOurFields(tableName, jsonData string, config *ChTableConfig) (string, error) {
	m := make(SchemaMap)
	err := json.Unmarshal([]byte(jsonData), &m)
	if err != nil {
		logger.Error().Msgf("Can't unmarshall, json: %s\nerr:%v", jsonData, err)
		return "", err
	}
	createTableCmd := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s"
(
	%s
)
%s`,
		tableName, FieldsMapToCreateTableString("", m, 1, config)+Indexes(m),
		config.CreateTablePostFieldsString())
	return createTableCmd, nil
}

func Indexes(m SchemaMap) string {
	var result strings.Builder
	for col := range m {
		index := getIndexStatement(col)
		if index != "" {
			result.WriteString(",\n")
			result.WriteString(util.Indent(1))
			result.WriteString(index.statement())
		}
	}
	result.WriteString(",\n")
	return result.String()
}

func (lm *LogManager) CreateTableFromInsertQuery(name, jsonData string, config *ChTableConfig) error {
	// TODO fix lm.addSchemaIfDoesntExist(name, jsonData)

	query, err := buildCreateTableQueryNoOurFields(name, jsonData, config)
	if err != nil {
		return err
	}

	err = lm.ProcessCreateTableQuery(query, config)
	if err != nil {
		return err
	}
	return nil
}

func (lm *LogManager) BuildInsertJson(tableName, js string, config *ChTableConfig) (string, error) {
	if !config.hasOthers && len(config.attributes) == 0 {
		return js, nil
	}
	// we find all non-schema fields
	m, err := JsonToFieldsMap(js)
	if err != nil {
		return "", err
	}

	t := lm.GetTable(tableName)
	onlySchemaFields := RemoveTypeMismatchSchemaFields(m, t)
	schemaFieldsJson, err := json.Marshal(onlySchemaFields)

	if err != nil {
		return "", err
	}

	mDiff := DifferenceMap(m, t) // TODO change to DifferenceMap(m, t)

	if len(mDiff) == 0 && string(schemaFieldsJson) == js { // no need to modify, just insert 'js'
		return js, nil
	}
	var attrsMap map[string][]interface{}
	var othersMap SchemaMap
	if len(config.attributes) > 0 {
		attrsMap, othersMap, _ = BuildAttrsMapAndOthers(mDiff, config)
	} else if config.hasOthers {
		othersMap = mDiff
	} else {
		return "", fmt.Errorf("no attributes or others in config, but received non-schema fields: %s", mDiff)
	}
	nonSchemaStr := ""
	if len(attrsMap) > 0 {
		attrs, err := json.Marshal(attrsMap) // check probably bad, they need to be arrays
		if err != nil {
			return "", err
		}
		nonSchemaStr = string(attrs[1 : len(attrs)-1])
	}
	if len(othersMap) > 0 {
		others, err := json.Marshal(othersMap)
		if err != nil {
			return "", err
		}
		if nonSchemaStr != "" {
			nonSchemaStr += "," // need to watch out where we input commas, CH doesn't tolerate trailing ones
		}
		nonSchemaStr += fmt.Sprintf(`"%s":%s`, othersFieldName, others)
	}
	onlySchemaFields = RemoveNonSchemaFields(m, t)
	schemaFieldsJson, err = json.Marshal(onlySchemaFields)
	if err != nil {
		return "", err
	}
	comma := ""
	if nonSchemaStr != "" && len(schemaFieldsJson) > 2 {
		comma = "," // need to watch out where we input commas, CH doesn't tolerate trailing ones
	}
	return fmt.Sprintf("{%s%s%s", nonSchemaStr, comma, schemaFieldsJson[1:]), nil
}

func (lm *LogManager) GetTableConfig(tableName, jsonData string) (*ChTableConfig, error) {
	table := lm.GetTable(tableName)
	var config *ChTableConfig
	if table == nil {
		config = NewOnlySchemaFieldsCHConfig()
		err := lm.CreateTableFromInsertQuery(tableName, jsonData, config)
		if err != nil {
			logger.Error().Msgf("error ProcessInsertQuery, can't create table: %v", err)
			return nil, err
		}
		return config, nil
	} else if !table.Created {
		err := lm.sendCreateTableQuery(table.CreateTableString())
		if err != nil {
			return nil, err
		}
		config = table.Config
	} else {
		config = table.Config
	}
	return config, nil
}

func (lm *LogManager) ProcessInsertQuery(tableName string, jsonData []string) error {
	if config, err := lm.GetTableConfig(tableName, jsonData[0]); err != nil {
		return err
	} else {
		return lm.Insert(tableName, jsonData, config)
	}
}

func (lm *LogManager) Insert(tableName string, jsons []string, config *ChTableConfig) error {
	if err := lm.initConnection(); err != nil {
		return err
	}

	var jsonsReadyForInsertion []string
	for _, jsonValue := range jsons {
		preprocessedJson := preprocess(jsonValue, NestedSeparator)
		insertJson, err := lm.BuildInsertJson(tableName, preprocessedJson, config)
		if err != nil {
			logger.Error().Msgf("error BuildInsertJson, tablename: %s\nerror: %v\njson:%s", tableName, err, PrettyJson(insertJson))
		}
		jsonsReadyForInsertion = append(jsonsReadyForInsertion, insertJson)
	}

	insertValues := strings.Join(jsonsReadyForInsertion, ", ")

	insert := fmt.Sprintf("INSERT INTO \"%s\" FORMAT JSONEachRow %s", tableName, insertValues)

	_, err := lm.chDb.Exec(insert)
	if err != nil {
		return fmt.Errorf("error on Insert, tablename: [%s]\nerror: [%v]", tableName, err)
	} else {
		return nil
	}
}

func (lm *LogManager) GetTable(tableName string) *Table {
	tableNamePattern := index.TableNamePatternRegexp(tableName)
	for name, table := range lm.tableDefinitions.Snapshot() {
		if tableNamePattern.MatchString(name) {
			return table
		}
	}

	table, _ := lm.tableDefinitions.Load(tableName)
	return table
}

func (lm *LogManager) GetTableDefinitions() TableMap {
	return lm.tableDefinitions
}

// Returns if schema wasn't created (so it needs to be, and will be in a moment)
func (lm *LogManager) addSchemaIfDoesntExist(table *Table) bool {
	t := lm.GetTable(table.Name)
	if t == nil {
		table.Created = true
		lm.tableDefinitions.Store(table.Name, table)
		return true
	}
	wasntCreated := !t.Created
	t.Created = true
	return wasntCreated
}

func NewLogManager(tables TableMap, cfg config.QuesmaConfiguration) *LogManager {
	return &LogManager{chDb: nil, tableDefinitions: tables, cfg: cfg}
}

// right now only for tests purposes
func NewLogManagerWithConnection(db *sql.DB, tables TableMap) *LogManager {
	return &LogManager{chDb: db, tableDefinitions: tables}
}

func NewLogManagerEmpty() *LogManager {
	return &LogManager{tableDefinitions: concurrent.NewMap[string, *Table]()}
}

func NewOnlySchemaFieldsCHConfig() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:                          true,
		timestampDefaultsNow:                  true,
		engine:                                "MergeTree",
		orderBy:                               "(" + `"@timestamp"` + ")",
		partitionBy:                           "",
		primaryKey:                            "",
		ttl:                                   "",
		hasOthers:                             false,
		attributes:                            []Attribute{NewDefaultStringAttribute()},
		castUnsupportedAttrValueTypesToString: false,
		preferCastingToOthers:                 false,
	}
}

func NewDefaultCHConfig() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:         true,
		timestampDefaultsNow: true,
		engine:               "MergeTree",
		orderBy:              "(" + `"@timestamp"` + ")",
		partitionBy:          "",
		primaryKey:           "",
		ttl:                  "",
		hasOthers:            false,
		attributes: []Attribute{
			NewDefaultInt64Attribute(),
			NewDefaultFloat64Attribute(),
			NewDefaultBoolAttribute(),
			NewDefaultStringAttribute(),
		},
		castUnsupportedAttrValueTypesToString: true,
		preferCastingToOthers:                 true,
	}
}

func NewNoTimestampOnlyStringAttrCHConfig() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:         false,
		timestampDefaultsNow: false,
		engine:               "MergeTree",
		orderBy:              "(" + `"@timestamp"` + ")",
		partitionBy:          "",
		primaryKey:           "",
		ttl:                  "",
		hasOthers:            false,
		attributes: []Attribute{
			NewDefaultStringAttribute(),
		},
		castUnsupportedAttrValueTypesToString: true,
		preferCastingToOthers:                 true,
	}
}

func NewChTableConfigNoAttrs() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:                          false,
		timestampDefaultsNow:                  false,
		engine:                                "MergeTree",
		orderBy:                               "(" + `"@timestamp"` + ")",
		hasOthers:                             false,
		attributes:                            []Attribute{},
		castUnsupportedAttrValueTypesToString: true,
		preferCastingToOthers:                 true,
	}
}

func NewChTableConfigFourAttrs() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:         false,
		timestampDefaultsNow: true,
		engine:               "MergeTree",
		orderBy:              "(" + "`@timestamp`" + ")",
		hasOthers:            false,
		attributes: []Attribute{
			NewDefaultInt64Attribute(),
			NewDefaultFloat64Attribute(),
			NewDefaultBoolAttribute(),
			NewDefaultStringAttribute(),
		},
		castUnsupportedAttrValueTypesToString: true,
		preferCastingToOthers:                 true,
	}
}

func NewChTableConfigTimestampStringAttr() *ChTableConfig {
	return &ChTableConfig{
		hasTimestamp:                          true,
		timestampDefaultsNow:                  true,
		attributes:                            []Attribute{NewDefaultStringAttribute()},
		engine:                                "MergeTree",
		orderBy:                               "(" + "`@timestamp`" + ")",
		hasOthers:                             false,
		castUnsupportedAttrValueTypesToString: true,
		preferCastingToOthers:                 true,
	}
}

func preprocess(jsonStr string, nestedSeparator string) string {
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(jsonStr), &data)

	resultJSON, _ := json.Marshal(jsonprocessor.FlattenMap(data, nestedSeparator))
	return string(resultJSON)
}
