package quesma

import (
	"context"
	"errors"
	"mitmproxy/quesma/clickhouse"
	"mitmproxy/quesma/quesma/config"
	"mitmproxy/quesma/quesma/mux"
	"mitmproxy/quesma/quesma/routes"
	"mitmproxy/quesma/quesma/ui"
	"strings"
)

func configureRouter(config config.QuesmaConfiguration, lm *clickhouse.LogManager, console *ui.QuesmaManagementConsole) *mux.PathRouter {
	router := mux.NewPathRouter()
	router.RegisterPath(routes.ClusterHealthPath, "GET", func(_ context.Context, body string, _ string, params map[string]string) (string, error) {
		return `{"cluster_name": "quesma"}`, nil
	})
	router.RegisterPath(routes.BulkPath, "POST", func(ctx context.Context, body string, _ string, params map[string]string) (string, error) {
		dualWriteBulk(ctx, "", body, lm, config)
		return "", nil
	})
	router.RegisterPathMatcher(routes.IndexDocPath, "POST", matchedAgainstConfig(config), func(ctx context.Context, body string, _ string, params map[string]string) (string, error) {
		dualWrite(ctx, params["index"], body, lm, config)
		return "", nil
	})
	router.RegisterPathMatcher(routes.IndexBulkPath, "POST", matchedAgainstConfig(config), func(ctx context.Context, body string, _ string, params map[string]string) (string, error) {
		dualWriteBulk(ctx, params["index"], body, lm, config)
		return "", nil
	})
	router.RegisterPathMatcher(routes.IndexSearchPath, "POST", matchedAgainstPattern(config), func(ctx context.Context, body string, _ string, params map[string]string) (string, error) {
		if strings.Contains(params["index"], ",") {
			return "", errors.New("multi index search is not yet supported")
		} else {
			responseBody, err := handleSearch(ctx, params["index"], []byte(body), lm, console)
			if err != nil {
				return "", err
			}
			return string(responseBody), nil
		}
	})
	router.RegisterPathMatcher(routes.IndexAsyncSearchPath, "POST", matchedAgainstPattern(config), func(ctx context.Context, body string, _ string, params map[string]string) (string, error) {
		if strings.Contains(params["index"], ",") {
			return "", errors.New("multi index search is not yet supported")
		} else {
			responseBody, err := handleAsyncSearch(ctx, params["index"], []byte(body), lm, console)
			if err != nil {
				return "", err
			}
			return string(responseBody), nil
		}
	})
	return router
}

func matchedAgainstConfig(config config.QuesmaConfiguration) mux.MatchPredicate {
	return func(m map[string]string) bool {
		indexConfig, exists := config.GetIndexConfig(m["index"])
		return exists && indexConfig.Enabled
	}
}

func matchedAgainstPattern(configuration config.QuesmaConfiguration) mux.MatchPredicate {
	return func(m map[string]string) bool {

		var candidates []string
		for _, tableName := range clickhouse.Tables() {
			if config.MatchName(m["index"], tableName) {
				candidates = append(candidates, tableName)
			}
		}

		if len(candidates) > 0 {
			// TODO multi-index support
			indexConfig, exists := configuration.GetIndexConfig(candidates[0])
			return exists && indexConfig.Enabled
		} else {
			return false
		}
	}
}
