// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package schema

import (
	"reflect"
	"testing"
)

func TestSchema_ResolveField(t *testing.T) {
	tests := []struct {
		name          string
		fieldName     string
		schema        Schema
		resolvedField Field
		exists        bool
	}{
		{
			name:      "empty schema",
			fieldName: "field",
			schema:    Schema{},
			exists:    false,
		},
		{
			name:      "should resolve field",
			fieldName: "message",
			schema: Schema{
				Fields: map[FieldName]Field{
					"message": {PropertyName: "message", InternalPropertyName: "message", Type: TypeText},
				},
			},
			resolvedField: Field{PropertyName: "message", InternalPropertyName: "message", Type: TypeText},
			exists:        true,
		},
		{
			name:      "should not resolve field",
			fieldName: "foo",
			schema: Schema{
				Fields: map[FieldName]Field{
					"message": {PropertyName: "message", InternalPropertyName: "message", Type: TypeText},
				},
			},
			resolvedField: Field{},
			exists:        false,
		},
		{
			name:      "should resolve aliased field",
			fieldName: "message_alias",
			schema: Schema{
				Fields:  map[FieldName]Field{"message": {PropertyName: "message", InternalPropertyName: "message", Type: TypeText}},
				Aliases: map[FieldName]FieldName{"message_alias": "message"},
			},
			resolvedField: Field{PropertyName: "message", InternalPropertyName: "message", Type: TypeText},
			exists:        true,
		},
		{
			name:      "should not resolve aliased field",
			fieldName: "message_alias",
			schema: Schema{
				Fields:  map[FieldName]Field{"message": {PropertyName: "message", InternalPropertyName: "message", Type: TypeText}},
				Aliases: map[FieldName]FieldName{"message_alias": "foo"},
			},
			resolvedField: Field{},
			exists:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exists := tt.schema.ResolveField(tt.fieldName)
			if exists != tt.exists {
				t.Errorf("ResolveField() exists = %v, want %v", exists, tt.exists)
			}
			if !reflect.DeepEqual(got, tt.resolvedField) {
				t.Errorf("ResolveField() got = %v, want %v", got, tt.resolvedField)
			}
		})
	}
}
