package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterAppConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []ApplicationCondition
		types      []ApplicationConditionType
		assertions func(*testing.T, []ApplicationCondition)
	}{
		{
			name: "no conditions",
			assertions: func(t *testing.T, conditions []ApplicationCondition) {
				require.Len(t, conditions, 0)
			},
		},
		{
			name: "single matching condition",
			conditions: []ApplicationCondition{
				{
					Type: ApplicationConditionComparisonError,
				},
			},
			types: []ApplicationConditionType{
				ApplicationConditionComparisonError,
			},
			assertions: func(t *testing.T, conditions []ApplicationCondition) {
				require.Len(t, conditions, 1)
				require.Equal(t, ApplicationConditionComparisonError, conditions[0].Type)
			},
		},
		{
			name: "multiple matching conditions",
			conditions: []ApplicationCondition{
				{
					Type: ApplicationConditionComparisonError,
				},
				{
					Type: ApplicationConditionInvalidSpecError,
				},
				{
					Type: ApplicationConditionComparisonError,
				},
				{
					Type: "SomeOtherType",
				},
			},
			types: []ApplicationConditionType{
				ApplicationConditionComparisonError,
				"SomeOtherType",
			},
			assertions: func(t *testing.T, conditions []ApplicationCondition) {
				require.Len(t, conditions, 3)
				require.Equal(t, ApplicationConditionComparisonError, conditions[0].Type)
				require.Equal(t, ApplicationConditionComparisonError, conditions[1].Type)
				require.Equal(t, ApplicationConditionType("SomeOtherType"), conditions[2].Type)
			},
		}, {
			name: "no matching conditions",
			conditions: []ApplicationCondition{
				{
					Type: ApplicationConditionComparisonError,
				},
				{
					Type: ApplicationConditionInvalidSpecError,
				},
			},
			types: []ApplicationConditionType{
				"NonMatchingType",
			},
			assertions: func(t *testing.T, conditions []ApplicationCondition) {
				require.Len(t, conditions, 0)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &Application{
				Status: ApplicationStatus{
					Conditions: tt.conditions,
				},
			}
			got := FilterAppConditions(app, tt.types...)
			tt.assertions(t, got)
		})
	}
}
