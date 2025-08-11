package promotion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
)

func TestContext_GetStepExecutionMetadata(t *testing.T) {
	tests := []struct {
		name       string
		context    *Context
		step       Step
		assertions func(t *testing.T, ctx *Context, result *kargoapi.StepExecutionMetadata)
	}{
		{
			name: "returns existing metadata when found",
			context: &Context{
				StepExecutionMetadata: kargoapi.StepExecutionMetadataList{
					{
						Alias:           "existing-step",
						ContinueOnError: true,
					},
				},
			},
			step: Step{
				Alias:           "existing-step",
				ContinueOnError: false,
			},
			assertions: func(t *testing.T, ctx *Context, result *kargoapi.StepExecutionMetadata) {
				assert.Equal(t, "existing-step", result.Alias)
				assert.True(t, result.ContinueOnError) // Should preserve existing value
				assert.Len(t, ctx.StepExecutionMetadata, 1)
			},
		},
		{
			name: "creates new metadata when not found",
			context: &Context{
				StepExecutionMetadata: kargoapi.StepExecutionMetadataList{
					{
						Alias:           "other-step",
						ContinueOnError: false,
					},
				},
			},
			step: Step{
				Alias:           "new-step",
				ContinueOnError: true,
			},
			assertions: func(t *testing.T, ctx *Context, result *kargoapi.StepExecutionMetadata) {
				assert.Equal(t, "new-step", result.Alias)
				assert.True(t, result.ContinueOnError)
				assert.Len(t, ctx.StepExecutionMetadata, 2)
				assert.Equal(t, "new-step", ctx.StepExecutionMetadata[1].Alias)
			},
		},
		{
			name: "creates new metadata when list is empty",
			context: &Context{
				StepExecutionMetadata: kargoapi.StepExecutionMetadataList{},
			},
			step: Step{
				Alias:           "first-step",
				ContinueOnError: false,
			},
			assertions: func(t *testing.T, ctx *Context, result *kargoapi.StepExecutionMetadata) {
				assert.Equal(t, "first-step", result.Alias)
				assert.False(t, result.ContinueOnError)
				assert.Len(t, ctx.StepExecutionMetadata, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.context.GetStepExecutionMetadata(tt.step)
			tt.assertions(t, tt.context, result)
		})
	}
}

func TestContext_DeepCopy(t *testing.T) {
	tests := []struct {
		name       string
		context    *Context
		assertions func(t *testing.T, original *Context, copy Context)
	}{
		{
			name: "creates deep copy of all fields",
			context: &Context{
				UIBaseURL:     "https://example.com",
				WorkDir:       "/tmp/work",
				Project:       "test-project",
				Stage:         "test-stage",
				Promotion:     "test-promotion",
				StartFromStep: 2,
				Actor:         "test-actor",
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{
							Kind: "Warehouse",
							Name: "test-warehouse",
						},
					},
				},
				Freight: kargoapi.FreightCollection{
					ID: "test-collection-id",
					Freight: map[string]kargoapi.FreightReference{
						"warehouse-1": {
							Name: "freight-1",
							Origin: kargoapi.FreightOrigin{
								Kind: "Warehouse",
								Name: "warehouse1",
							},
						},
						"warehouse-2": {
							Name: "freight-2",
							Origin: kargoapi.FreightOrigin{
								Kind: "Warehouse",
								Name: "warehouse-2",
							},
						},
					},
					VerificationHistory: []kargoapi.VerificationInfo{
						{
							ID:     "verification-1",
							Phase:  kargoapi.VerificationPhaseSuccessful,
							Message: "Test verification",
						},
					},
				},
				TargetFreightRef: kargoapi.FreightReference{
					Name: "target-freight",
					Origin: kargoapi.FreightOrigin{
						Kind: "Warehouse",
						Name: "target-warehouse",
					},
				},
				StepExecutionMetadata: kargoapi.StepExecutionMetadataList{
					{
						Alias:           "test-step",
						ContinueOnError: true,
					},
				},
				State: promotion.State{
					"key": "value",
				},
				Vars: []kargoapi.ExpressionVariable{
					{
						Name:  "test-var",
						Value: "test-value",
					},
				},
			},
			assertions: func(t *testing.T, original *Context, copy Context) {
				// Verify all fields are copied
				assert.Equal(t, original.UIBaseURL, copy.UIBaseURL)
				assert.Equal(t, original.WorkDir, copy.WorkDir)
				assert.Equal(t, original.Project, copy.Project)
				assert.Equal(t, original.Stage, copy.Stage)
				assert.Equal(t, original.Promotion, copy.Promotion)
				assert.Equal(t, original.StartFromStep, copy.StartFromStep)
				assert.Equal(t, original.Actor, copy.Actor)

				// Verify FreightRequests is deep copied
				assert.Equal(t, len(original.FreightRequests), len(copy.FreightRequests))
				if len(original.FreightRequests) > 0 {
					assert.Equal(t, original.FreightRequests[0].Origin.Name, copy.FreightRequests[0].Origin.Name)
					// Verify it's a different slice
					assert.NotSame(t, &original.FreightRequests[0], &copy.FreightRequests[0])
				}

				// Verify FreightCollection is deep copied
				assert.Equal(t, original.Freight.ID, copy.Freight.ID)
				assert.Equal(t, len(original.Freight.Freight), len(copy.Freight.Freight))
				assert.Equal(t, len(original.Freight.VerificationHistory), len(copy.Freight.VerificationHistory))

				// Verify Freight map contents
				for key, originalFreight := range original.Freight.Freight {
					copyFreight, exists := copy.Freight.Freight[key]
					assert.True(t, exists, "Freight key %s should exist in copy", key)
					assert.Equal(t, originalFreight.Name, copyFreight.Name)
					assert.Equal(t, originalFreight.Origin, copyFreight.Origin)
				}

				// Verify VerificationHistory contents
				if len(original.Freight.VerificationHistory) > 0 {
					assert.Equal(t, original.Freight.VerificationHistory[0].ID, copy.Freight.VerificationHistory[0].ID)
					assert.Equal(t, original.Freight.VerificationHistory[0].Phase, copy.Freight.VerificationHistory[0].Phase)
					assert.Equal(t, original.Freight.VerificationHistory[0].Message, copy.Freight.VerificationHistory[0].Message)
				}

				// Verify TargetFreightRef is deep copied
				assert.Equal(t, original.TargetFreightRef.Name, copy.TargetFreightRef.Name)
				assert.Equal(t, original.TargetFreightRef.Origin, copy.TargetFreightRef.Origin)

				// Verify StepExecutionMetadata is deep copied
				assert.Equal(t, len(original.StepExecutionMetadata), len(copy.StepExecutionMetadata))
				if len(original.StepExecutionMetadata) > 0 {
					assert.Equal(t, original.StepExecutionMetadata[0].Alias, copy.StepExecutionMetadata[0].Alias)
				}

				// Verify State is deep copied
				assert.Equal(t, original.State["key"], copy.State["key"])

				// Verify Vars is cloned
				assert.Equal(t, len(original.Vars), len(copy.Vars))
				if len(original.Vars) > 0 {
					assert.Equal(t, original.Vars[0].Name, copy.Vars[0].Name)
				}

				// Verify modifications to copy don't affect original
				copy.UIBaseURL = "modified"
				copy.StartFromStep = 999
				copy.Freight.ID = "modified-id"
				if len(copy.FreightRequests) > 0 {
					copy.FreightRequests[0].Origin.Name = "modified"
				}
				if len(copy.StepExecutionMetadata) > 0 {
					copy.StepExecutionMetadata[0].Alias = "modified"
				}
				copy.State["key"] = "modified"
				if len(copy.Freight.Freight) > 0 {
					for key := range copy.Freight.Freight {
						freight := copy.Freight.Freight[key]
						freight.Name = "modified-freight"
						copy.Freight.Freight[key] = freight
						break
					}
				}

				assert.Equal(t, "https://example.com", original.UIBaseURL)
				assert.Equal(t, int64(2), original.StartFromStep)
				assert.Equal(t, "test-collection-id", original.Freight.ID)
				if len(original.FreightRequests) > 0 {
					assert.Equal(t, "test-warehouse", original.FreightRequests[0].Origin.Name)
				}
				if len(original.StepExecutionMetadata) > 0 {
					assert.Equal(t, "test-step", original.StepExecutionMetadata[0].Alias)
				}
				assert.Equal(t, "value", original.State["key"])
				if len(original.Freight.Freight) > 0 {
					for _, freight := range original.Freight.Freight {
						assert.NotEqual(t, "modified-freight", freight.Name)
						break
					}
				}
			},
		},
		{
			name: "handles nil FreightRequests",
			context: &Context{
				UIBaseURL:       "https://example.com",
				FreightRequests: nil,
				Freight: kargoapi.FreightCollection{
					ID:      "empty-collection",
					Freight: nil,
				},
			},
			assertions: func(t *testing.T, original *Context, copy Context) {
				assert.Nil(t, copy.FreightRequests)
				assert.Equal(t, original.UIBaseURL, copy.UIBaseURL)
				assert.Equal(t, original.Freight.ID, copy.Freight.ID)
				assert.Nil(t, copy.Freight.Freight)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assertions(t, tt.context, tt.context.DeepCopy())
		})
	}
}

func TestStep_GetTimeout(t *testing.T) {
	tests := []struct {
		name       string
		step       *Step
		runner     promotion.StepRunner
		assertions func(t *testing.T, result *time.Duration)
	}{
		{
			name: "returns 0 with no retry config",
			step: &Step{
				Retry: nil,
			},
			assertions: func(t *testing.T, result *time.Duration) {
				assert.Equal(t, ptr.To(time.Duration(0)), result)
			},
		},
		{
			name: "returns configured timeout for non-retryable runner",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{
					Timeout: &metav1.Duration{
						Duration: time.Duration(5),
					},
				},
			},
			runner: nil,
			assertions: func(t *testing.T, result *time.Duration) {
				assert.Equal(t, ptr.To(time.Duration(5)), result)
			},
		},
		{
			name: "returns configured timeout for retryable runner",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{
					Timeout: &metav1.Duration{
						Duration: time.Duration(5),
					},
				},
			},
			runner: promotion.NewRetryableStepRunner(nil, ptr.To(time.Duration(3)), 0),
			assertions: func(t *testing.T, result *time.Duration) {
				assert.Equal(t, ptr.To(time.Duration(5)), result)
			},
		},
		{
			name: "returns default timeout when retry config returns nil",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{},
			},
			runner: promotion.NewRetryableStepRunner(nil, ptr.To(time.Duration(3)), 0),
			assertions: func(t *testing.T, result *time.Duration) {
				assert.Equal(t, ptr.To(time.Duration(3)), result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.step.GetTimeout(tt.runner)
			tt.assertions(t, result)
		})
	}
}

func TestStep_GetErrorThreshold(t *testing.T) {
	tests := []struct {
		name       string
		step       *Step
		runner     promotion.StepRunner
		assertions func(t *testing.T, result uint32)
	}{
		{
			name: "returns 1 with no retry config",
			step: &Step{
				Retry: nil,
			},
			runner: nil,
			assertions: func(t *testing.T, result uint32) {
				assert.Equal(t, uint32(1), result)
			},
		},
		{
			name: "returns configured error threshold for non-retryable runner",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{
					ErrorThreshold: uint32(3),
				},
			},
			runner: nil,
			assertions: func(t *testing.T, result uint32) {
				assert.Equal(t, uint32(3), result)
			},
		},
		{
			name: "returns configured error threshold for retryable runner",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{
					ErrorThreshold: uint32(5),
				},
			},
			runner: promotion.NewRetryableStepRunner(nil, nil, 2),
			assertions: func(t *testing.T, result uint32) {
				assert.Equal(t, uint32(5), result)
			},
		},
		{
			name: "returns default error threshold when retry config returns default",
			step: &Step{
				Retry: &kargoapi.PromotionStepRetry{},
			},
			runner: promotion.NewRetryableStepRunner(nil, nil, 4),
			assertions: func(t *testing.T, result uint32) {
				assert.Equal(t, uint32(4), result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.step.GetErrorThreshold(tt.runner)
			tt.assertions(t, result)
		})
	}
}
