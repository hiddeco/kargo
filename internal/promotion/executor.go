package promotion

import (
	"context"
	"fmt"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
)

// StepExecutionRequest represents a request to execute a promotion step.
// It contains all the necessary context and step information required for
// executing the step.
type StepExecutionRequest struct {
	Context promotion.StepContext
	Step    Step
}

// StepExecutor defines the interface for executing a single promotion step.
type StepExecutor interface {
	ExecuteStep(ctx context.Context, req StepExecutionRequest) (promotion.StepResult, error)
}

// LocalStepExecutor is a concrete implementation of StepExecutor that
// executes steps locally using step runners registered in a registry.
type LocalStepExecutor struct {
	registry stepRunnerRegistry
}

// NewLocalStepExecutor creates a new LocalStepExecutor with the provided
// step runner registry. This executor will use the registered runners to
// execute steps in the promotion process.
func NewLocalStepExecutor(registry stepRunnerRegistry) *LocalStepExecutor {
	return &LocalStepExecutor{
		registry: registry,
	}
}

// ExecuteStep executes a single promotion step using the registered step
// runner for the step's kind. It handles any errors that occur during execution
// and returns a StepResult indicating the outcome of the step execution.
func (e *LocalStepExecutor) ExecuteStep(
	ctx context.Context,
	req StepExecutionRequest,
) (result promotion.StepResult, err error) {
	runner := e.registry.getStepRunner(req.Step.Kind)
	if runner == nil {
		return promotion.StepResult{
			Status: kargoapi.PromotionStepStatusErrored,
		}, fmt.Errorf("no runner registered for step %q", req.Step.Kind)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				result = promotion.StepResult{
					Status: kargoapi.PromotionStepStatusErrored,
				}
				err = &promotion.TerminalError{
					Err: fmt.Errorf("step panicked: %v", r),
				}
			}
		}()

		result, err = runner.Run(ctx, &req.Context)
	}()

	if err != nil {
		err = fmt.Errorf("error running step %q: %w", req.Step.Alias, err)
	}

	return result, err
}
