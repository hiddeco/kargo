package promotion

import (
	"context"
	"fmt"

	gocache "github.com/patrickmn/go-cache"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/health"
	"github.com/akuity/kargo/pkg/promotion"
)

// Orchestrator is an interface that defines the methods required to execute
// a series of steps in a Promotion. It is responsible for orchestrating the
// execution of steps, handling their results, and managing the state of the
// Promotion as it progresses through the steps.
type Orchestrator interface {
	// ExecuteSteps executes the provided steps in the context of the given
	// Promotion context. It returns a Result that contains the status of the
	// Promotion after executing the steps, including any health checks that
	// were performed during the execution.
	ExecuteSteps(ctx context.Context, promoCtx Context, steps []Step) (Result, error)
}

// LocalOrchestrator is an implementation of the Orchestrator interface that
// executes steps locally using the provided StepExecutor and StepRunner
// registry.
type LocalOrchestrator struct {
	executor  StepExecutor
	registry  stepRunnerRegistry
	client    client.Client
	cacheFunc ExprDataCacheFn
}

// NewLocalOrchestrator creates a new LocalOrchestrator instance with the
// provided client, step runner registry, and cache function.
func NewLocalOrchestrator(
	cl client.Client,
	registry stepRunnerRegistry,
	cacheFunc ExprDataCacheFn,
) *LocalOrchestrator {
	return &LocalOrchestrator{
		executor:  NewLocalStepExecutor(registry),
		registry:  registry,
		client:    cl,
		cacheFunc: cacheFunc,
	}
}

// ExecuteSteps executes the provided steps in the context of the given
// Promotion context. It iterates through the steps, evaluates their "if"
// conditions, and executes them if they are not skipped. It also handles
// the execution metadata for each step, including start and finish times,
// statuses, and error handling. The method returns a Result that contains
// the final status of the Promotion after executing all steps, including
// any health checks that were performed during the execution.
func (o *LocalOrchestrator) ExecuteSteps(
	ctx context.Context,
	promoCtx Context,
	steps []Step,
) (Result, error) {
	if promoCtx.State == nil {
		// Initialize the state if it is not provided.
		promoCtx.State = make(promotion.State)
	}

	var healthChecks []health.Criteria

	// Execute each step in sequence, starting from the step specified in
	// the Context if provided.
	for i := promoCtx.StartFromStep; i < int64(len(steps)); i++ {
		step := steps[i]

		stepExecMeta := promoCtx.GetStepExecutionMetadata(step)

		if o.isContextCanceled(ctx, stepExecMeta) {
			break
		}

		processor := NewStepEvaluator(o.client, o.newCache())

		// Evaluate the "if" condition for the step to determine if it should
		// be executed.
		skip, err := processor.ShouldSkip(ctx, promoCtx, step)
		switch {
		case err != nil:
			stepExecMeta.Status = kargoapi.PromotionStepStatusErrored
			stepExecMeta.Message = fmt.Sprintf("error checking if step %q should be skipped: %s", step.Alias, err)
			// Skip the step, because despite this failure, some steps' "if"
			// conditions may still allow them to run.
			continue
		case skip:
			stepExecMeta.Status = kargoapi.PromotionStepStatusSkipped
			continue
		}

		// Get the runner for the step (for validation purposes).
		//
		// NOTE(hidde): We primarily do this to ensure we do not mark the step
		// as started if we cannot find a runner for it. In the future, we
		// should consider validating the steps existence during the creation
		// of the Promotion, or e.g. work with a typed within the executor to
		// identify the lack of a registered runner.
		runner := o.registry.getStepRunner(step.Kind)
		if runner == nil {
			stepExecMeta.Status = kargoapi.PromotionStepStatusErrored
			stepExecMeta.Message = fmt.Sprintf("no promotion step runner found for kind %q", step.Kind)
			// Continue, because despite this failure, some steps' "if" conditions may
			// still allow them to run.
			//
			// TODO(hidde): Arguably, we should return a TerminalError here. As
			// it is an obvious misconfiguration that could have been caught
			// if our validation webhook was aware of registered steps.
			continue
		}

		// Mark the step as started.
		if stepExecMeta.StartedAt == nil {
			stepExecMeta.StartedAt = ptr.To(metav1.Now())
		}

		// Build step context for the step execution.
		stepCtx, err := processor.BuildStepContext(ctx, promoCtx, step)
		if err != nil {
			stepExecMeta.Status = kargoapi.PromotionStepStatusErrored
			stepExecMeta.Message = fmt.Sprintf("failed to build step context: %s", err)
			continue
		}

		// Execute the step.
		result, err := o.executor.ExecuteStep(ctx, StepExecutionRequest{
			Context: *stepCtx,
			Step:    step,
		})

		// Propagate the step output to the state.
		o.propagateStepOutput(promoCtx, step, runner, result)

		// Confirm the step has a valid status.
		if !result.Status.Valid() {
			stepExecMeta.FinishedAt = ptr.To(metav1.Now())
			stepExecMeta.Status = kargoapi.PromotionStepStatusErrored
			stepExecMeta.Message = fmt.Sprintf("step %q returned an invalid status: %s", step.Alias, result.Status)
			continue
		}

		// Update the step execution metadata with the result.
		err = o.reconcileResultWithMetadata(stepExecMeta, step, result, err)

		// Determine the completion of the step based on the metadata.
		if !o.determineStepCompletion(step, runner, stepExecMeta, err) {
			// The step is still running, so we need to wait
			return Result{
				Status:                kargoapi.PromotionPhaseRunning,
				CurrentStep:           i,
				StepExecutionMetadata: promoCtx.StepExecutionMetadata,
				State:                 promoCtx.State,
				HealthChecks:          healthChecks,
			}, nil
		}

		// If the step succeeded, we can add any health checks to the list.
		if stepExecMeta.Status == kargoapi.PromotionStepStatusSucceeded {
			if result.HealthCheck != nil {
				healthChecks = append(healthChecks, *result.HealthCheck)
			}
		}
	}

	status, msg := determinePromoPhase(steps, promoCtx.StepExecutionMetadata)

	// All steps have succeeded, return the final state.
	return Result{
		Status:                status,
		Message:               msg,
		CurrentStep:           int64(len(steps)) - 1,
		StepExecutionMetadata: promoCtx.StepExecutionMetadata,
		State:                 promoCtx.State,
		HealthChecks:          healthChecks,
	}, nil
}

func (o *LocalOrchestrator) isContextCanceled(ctx context.Context, meta *kargoapi.StepExecutionMetadata) bool {
	select {
	case <-ctx.Done():
		meta.Status = kargoapi.PromotionStepStatusErrored
		meta.Message = ctx.Err().Error()
		if meta.StartedAt != nil {
			meta.FinishedAt = ptr.To(metav1.Now())
		}
		return true
	default:
		return false
	}
}

func (o *LocalOrchestrator) propagateStepOutput(
	promoCtx Context,
	step Step,
	runner promotion.StepRunner,
	result promotion.StepResult,
) {
	// Update the state with the output of the step.
	promoCtx.State[step.Alias] = result.Output

	// If the step instructs that the output should be propagated to the
	// task namespace, do so.
	if p, ok := runner.(promotion.TaskLevelOutputStepRunner); ok && p.TaskLevelOutput() {
		if aliasNamespace := getAliasNamespace(step.Alias); aliasNamespace != "" {
			if promoCtx.State[aliasNamespace] == nil {
				promoCtx.State[aliasNamespace] = make(map[string]any)
			}
			for k, v := range result.Output {
				promoCtx.State[aliasNamespace].(map[string]any)[k] = v // nolint: forcetypeassert
			}
		}
	}
}

func (o *LocalOrchestrator) reconcileResultWithMetadata(
	meta *kargoapi.StepExecutionMetadata,
	step Step,
	result promotion.StepResult,
	err error,
) error {
	meta.Status = result.Status
	meta.Message = result.Message

	if err != nil {
		if meta.Status != kargoapi.PromotionStepStatusFailed {
			// All states other than Errored and Failed should be mutually
			// exclusive with a hard error. If we got to here, a step has
			// violated this assumption. We will prioritize the error over the
			// status and change the status to Errored.
			meta.Status = kargoapi.PromotionStepStatusErrored
		}
		meta.Message = err.Error()
		return err
	}

	if result.Status == kargoapi.PromotionStepStatusErrored {
		message := meta.Message
		if message == "" {
			message = "no details provided"
		}
		// A nil err should be mutually exclusive with an Errored status. If we
		// got to here, a step has violated this assumption. We will prioritize
		// the Errored status over the nil error and create an error.
		err = fmt.Errorf("step %q errored: %s", step.Alias, message)
		return err
	}

	return nil
}

func (o *LocalOrchestrator) determineStepCompletion(
	step Step,
	runner promotion.StepRunner,
	meta *kargoapi.StepExecutionMetadata,
	err error,
) bool {
	switch {
	case meta.Status == kargoapi.PromotionStepStatusSucceeded ||
		meta.Status == kargoapi.PromotionStepStatusSkipped:
		// Note: A step that ran briefly and self-determined it should be
		// "skipped" is treated similarly to success.
		meta.FinishedAt = ptr.To(metav1.Now())
		return true
	case promotion.IsTerminal(err):
		// This is an unrecoverable error.
		meta.FinishedAt = ptr.To(metav1.Now())
		meta.Status = kargoapi.PromotionStepStatusErrored
		meta.Message = fmt.Sprintf("an unrecoverable error occurred: %s", err)
		// Continue, because despite this failure, some steps' "if" conditions may
		// still allow them to run.
		return true
	case err != nil:
		// If we get to here, the error is POTENTIALLY recoverable.
		meta.ErrorCount++
		// Check if the error threshold has been met.
		errorThreshold := step.GetErrorThreshold(runner)
		if meta.ErrorCount >= errorThreshold {
			// The error threshold has been met.
			meta.FinishedAt = ptr.To(metav1.Now())
			meta.Status = kargoapi.PromotionStepStatusErrored
			meta.Message = fmt.Sprintf(
				"step %q met error threshold of %d: %s", step.Alias,
				errorThreshold, meta.Message,
			)
			// Continue, because despite this failure, some steps' "if" conditions
			// may still allow them to run.
			return true
		}
	}

	// If we get to here, the step is either Running (waiting for some external
	// condition to be met) or it Errored/Failed but did not meet the error
	// threshold. Now we need to check if the timeout has elapsed. A nil timeout
	// or any non-positive timeout interval are treated as NO timeout, although
	// a nil timeout really shouldn't happen.
	timeout := step.GetTimeout(runner)
	if timeout != nil && *timeout > 0 && metav1.Now().Sub(meta.StartedAt.Time) > *timeout {
		// Timeout has elapsed.
		meta.FinishedAt = ptr.To(metav1.Now())
		meta.Status = kargoapi.PromotionStepStatusErrored
		meta.Message = fmt.Sprintf("step %q timed out after %s", step.Alias, timeout.String())
		// Continue, because despite this failure, some steps' "if" conditions may
		// still allow them to run.
		return true
	}

	if err != nil {
		// Treat Errored/Failed as if the step is still running so that the
		// Promotion will be requeued. The step will be retried on the next
		// reconciliation.
		meta.Message += "; step will be retried"
		return false
	}

	// If we get to here, the step is still Running (waiting for some external
	// condition to be met).
	meta.ErrorCount = 0 // Reset the error count
	return false
}

func (o *LocalOrchestrator) newCache() *gocache.Cache {
	if o.cacheFunc == nil {
		return nil
	}
	return o.cacheFunc()
}

// determinePromoPhase determines the final PromotionPhase as a function of the
// step configuration and step execution metadata.
func determinePromoPhase(
	steps []Step,
	stepExecMetas kargoapi.StepExecutionMetadataList,
) (kargoapi.PromotionPhase, string) {
	worstStepStatus := kargoapi.PromotionStepStatusSucceeded
	var worstMsg string
	for i, stepExecMeta := range stepExecMetas {
		if steps[i].ContinueOnError {
			// If continueOnError is set, we don't permit this step's outcome
			// to affect the overall PromotionPhase.
			continue
		}
		if stepExecMeta.Status.Compare(worstStepStatus) > 0 {
			worstStepStatus = stepExecMeta.Status
			worstMsg = stepExecMeta.Message
		}
	}

	switch worstStepStatus {
	case kargoapi.PromotionStepStatusSucceeded, kargoapi.PromotionStepStatusSkipped:
		return kargoapi.PromotionPhaseSucceeded, worstMsg
	case kargoapi.PromotionStepStatusAborted:
		return kargoapi.PromotionPhaseAborted, worstMsg
	case kargoapi.PromotionStepStatusFailed:
		return kargoapi.PromotionPhaseFailed, worstMsg
	case kargoapi.PromotionStepStatusErrored:
		return kargoapi.PromotionPhaseErrored, worstMsg
	default:
		// This really shouldn't ever happen. We'll treat it as an error.
		return kargoapi.PromotionPhaseErrored, worstMsg
	}
}
