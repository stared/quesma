package config

const (
	Proxy OperationMode = iota
	ProxyInspect
	DualWriteQueryElastic
	DualWriteQueryClickhouse
	DualWriteQueryClickhouseVerify
	DualWriteQueryClickhouseFallback
	ClickHouse
)

func (m OperationMode) String() string {
	return [...]string{"proxy", "proxy-inspect", "dual-write-query-elastic", "dual-write-query-clickhouse", "dual-write-query-clickhouse-verify", "dual-write-query-clickhouse-fallback", "clickhouse"}[m]
}

func parseOperationMode(str string) OperationMode {
	switch str {
	case "proxy":
		return Proxy
	case "proxy-inspect":
		return ProxyInspect
	case "dual-write-query-elastic":
		return DualWriteQueryElastic
	case "dual-write-query-clickhouse":
		return DualWriteQueryClickhouse
	case "dual-write-query-clickhouse-verify":
		return DualWriteQueryClickhouseVerify
	case "dual-write-query-clickhouse-fallback":
		return DualWriteQueryClickhouseFallback
	case "clickhouse":
		return ClickHouse
	default:
		return -1
	}
}
