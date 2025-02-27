// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package config

type operationMode string

const (
	Proxy                            operationMode = "proxy"
	ProxyInspect                     operationMode = "proxy-inspect"
	DualWriteQueryElastic            operationMode = "dual-write-query-elastic"
	DualWriteQueryClickhouse         operationMode = "dual-write-query-clickhouse"
	DualWriteQueryClickhouseVerify   operationMode = "dual-write-query-clickhouse-verify"
	DualWriteQueryClickhouseFallback operationMode = "dual-write-query-clickhouse-fallback"
	ClickHouse                       operationMode = "clickhouse"
)

func (o operationMode) String() string {
	return string(o)
}
