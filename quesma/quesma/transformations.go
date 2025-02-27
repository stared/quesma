// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package quesma

import (
	"quesma/model"
	"quesma/plugins"
)

type TransformationPipeline struct {
	transformers []plugins.QueryTransformer
}

func (o *TransformationPipeline) Transform(queries []*model.Query) ([]*model.Query, error) {
	for _, transformer := range o.transformers {
		queries, _ = transformer.Transform(queries)
	}
	return queries, nil
}
