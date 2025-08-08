package promotion

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
)

func TestNewLocalStepExecutor(t *testing.T) {
	registry := stepRunnerRegistry{
		"fake-step": &promotion.MockStepRunner{},
	}
	executor := NewLocalStepExecutor(registry)

	require.NotNil(t, executor)
	require.IsType(t, &LocalStepExecutor{}, executor)
	require.Equal(t, registry, executor.registry)
}

func TestLocalStepExecutor_ExecuteStep(t *testing.T) {
	tests := []struct {
		name       string
		registry   stepRunnerRegistry
		request    StepExecutionRequest
		assertions func(t *testing.T, result promotion.StepResult, err error)
	}{
		{
			name:     "no runner registered for step kind",
			registry: stepRunnerRegistry{},
			request: StepExecutionRequest{
				Context: promotion.StepContext{},
				Step: Step{
					Kind: "unknown-step",
				},
			},
			assertions: func(t *testing.T, result promotion.StepResult, err error) {
				require.Equal(t, promotion.StepResult{
					Status: kargoapi.PromotionStepStatusErrored,
				}, result)
				require.ErrorContains(t, err, `no runner registered for step "unknown-step"`)
			},
		},
		{
			name: "successful step execution",
			registry: stepRunnerRegistry{
				"test-step": &promotion.MockStepRunner{
					RunResult: promotion.StepResult{
						Status: kargoapi.PromotionStepStatusSucceeded,
					},
				},
			},
			request: StepExecutionRequest{
				Context: promotion.StepContext{},
				Step: Step{
					Kind: "test-step",
				},
			},
			assertions: func(t *testing.T, result promotion.StepResult, err error) {
				require.Equal(t, promotion.StepResult{
					Status: kargoapi.PromotionStepStatusSucceeded,
				}, result)
				require.NoError(t, err)
			},
		},
		{
			name: "step execution returns error",
			registry: stepRunnerRegistry{
				"test-step": &promotion.MockStepRunner{
					RunResult: promotion.StepResult{
						Status: kargoapi.PromotionStepStatusErrored,
					},
					RunErr: errors.New("step execution failed"),
				},
			},
			request: StepExecutionRequest{
				Context: promotion.StepContext{},
				Step: Step{
					Kind: "test-step",
				},
			},
			assertions: func(t *testing.T, result promotion.StepResult, err error) {
				require.Equal(t, promotion.StepResult{
					Status: kargoapi.PromotionStepStatusErrored,
				}, result)
				require.ErrorContains(t, err, "step execution failed")
			},
		},
		{
			name: "step execution panics",
			registry: stepRunnerRegistry{
				"test-step": &promotion.MockStepRunner{
					RunFunc: func(ctx context.Context, stepCtx *promotion.StepContext) (promotion.StepResult, error) {
						panic("step runner panicked")
					},
				},
			},
			request: StepExecutionRequest{
				Context: promotion.StepContext{},
				Step: Step{
					Kind: "test-step",
				},
			},
			assertions: func(t *testing.T, result promotion.StepResult, err error) {
				require.Equal(t, promotion.StepResult{
					Status: kargoapi.PromotionStepStatusErrored,
				}, result)
				require.ErrorContains(t, err, "step panicked: step runner panicked")

				var terminalErr *promotion.TerminalError
				require.ErrorAs(t, err, &terminalErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewLocalStepExecutor(tt.registry)
			result, err := executor.ExecuteStep(context.Background(), tt.request)
			tt.assertions(t, result, err)
		})
	}
}
