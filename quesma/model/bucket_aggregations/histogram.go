package bucket_aggregations

import "mitmproxy/quesma/model"

type QueryTypeHistogram struct{}

func (qt QueryTypeHistogram) IsBucketAggregation() bool {
	return true
}

func (qt QueryTypeHistogram) TranslateSqlResponseToJson(rows []model.QueryResultRow) []model.JsonMap {
	var response []model.JsonMap
	for _, row := range rows {
		response = append(response, model.JsonMap{
			"key":           row.Cols[0].Value,
			"doc_count":     row.Cols[1].Value,
			"key_as_string": 1, // TODO fill this
		})
	}
	return response
}

func (qt QueryTypeHistogram) String() string {
	return "histogram"
}
