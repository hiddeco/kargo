package promotion

import (
	"slices"
	"time"

	"k8s.io/utils/ptr"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/pkg/health"
	"github.com/akuity/kargo/pkg/promotion"
)

// Context is the context of a user-defined promotion process that is executed
// by the Engine.
type Context struct {
	// UIBaseURL may be used to construct deeper URLs for interacting with the
	// Kargo UI.
	UIBaseURL string
	// WorkDir is the working directory to use for the Promotion.
	WorkDir string
	// Project is the Project that the Promotion is associated with.
	Project string
	// Stage is the Stage that the Promotion is targeting.
	Stage string
	// Promotion is the name of the Promotion.
	Promotion string
	// FreightRequests is the list of Freight from various origins that is
	// requested by the Stage targeted by the Promotion.
	//
	// This information is sometimes useful to Steps that reference a particular
	// artifact. When explicit information about the artifact's origin is absent,
	// Steps may need to examine FreightRequests.
	// This examination helps determine whether any ambiguity exists regarding
	// the artifact's origin. If ambiguity is found, a user may need to resolve
	// it.
	FreightRequests []kargoapi.FreightRequest
	// Freight is the collection of all Freight referenced by the Promotion. This
	// collection contains both the Freight that is actively being promoted and
	// any Freight that has been inherited from the target Stage's current
	// state.
	Freight kargoapi.FreightCollection
	// TargetFreightRef is the actual Freight that triggered this Promotion.
	TargetFreightRef kargoapi.FreightReference
	// StartFromStep is the index of the step from which the promotion should
	// begin execution.
	StartFromStep int64
	// StepExecutionMetadata tracks metadata pertaining to the execution
	// of individual promotion steps.
	StepExecutionMetadata kargoapi.StepExecutionMetadataList
	// State is the current state of the promotion process.
	State promotion.State
	// Vars is a list of variable definitions that can be used by the
	// Steps.
	Vars []kargoapi.ExpressionVariable
	// Actor is the name of the actor triggering the Promotion.
	Actor string
}

// GetStepExecutionMetadata retrieves the StepExecutionMetadata for a given
// Step. If metadata for the Step does not already exist, it creates a new
// StepExecutionMetadata entry with the Step's alias and ContinueOnError
// property, and returns it.
func (c *Context) GetStepExecutionMetadata(step Step) *kargoapi.StepExecutionMetadata {
	for i := range c.StepExecutionMetadata {
		if c.StepExecutionMetadata[i].Alias == step.Alias {
			// Found existing metadata for this step, return it.
			return &c.StepExecutionMetadata[i]
		}
	}

	// If not found, append new metadata
	c.StepExecutionMetadata = append(
		c.StepExecutionMetadata,
		kargoapi.StepExecutionMetadata{
			Alias:           step.Alias,
			ContinueOnError: step.ContinueOnError,
		},
	)
	return &c.StepExecutionMetadata[len(c.StepExecutionMetadata)-1]
}

// DeepCopy creates a deep copy of the Context. It can be used to ensure that
// modifications to the Context do not affect the original Context.
func (c *Context) DeepCopy() Context {
	newC := Context{
		UIBaseURL:             c.UIBaseURL,
		WorkDir:               c.WorkDir,
		Project:               c.Project,
		Stage:                 c.Stage,
		Promotion:             c.Promotion,
		Freight:               *c.Freight.DeepCopy(),
		TargetFreightRef:      *c.TargetFreightRef.DeepCopy(),
		StartFromStep:         c.StartFromStep,
		StepExecutionMetadata: c.StepExecutionMetadata.DeepCopy(),
		State:                 c.State.DeepCopy(),
		Vars:                  slices.Clone(c.Vars),
		Actor:                 c.Actor,
	}

	if c.FreightRequests != nil {
		newC.FreightRequests = make([]kargoapi.FreightRequest, len(c.FreightRequests))
		for i, fr := range c.FreightRequests {
			newC.FreightRequests[i] = *fr.DeepCopy()
		}
	}

	return newC
}

// Step describes a single step in a user-defined promotion process. Steps are
// executed in sequence by the Engine, which delegates of each to a StepRunner.
type Step struct {
	// Kind identifies a registered StepRunner that implements the logic for this
	// step of the user-defined promotion process.
	Kind string
	// Alias is an optional identifier for this step of the use-defined promotion
	// process, which must be unique to the process. Output from execution of the
	// step will be keyed to this alias by the Engine and made accessible to
	// subsequent steps.
	Alias string
	// If is an optional expression that, if present, must evaluate to a boolean
	// value. If the expression evaluates to false, the step will be skipped.
	// If the expression does not evaluate to a boolean value, the step will
	// fail.
	If string
	// ContinueOnError is a boolean value that, if set to true, will cause the
	// Promotion to continue executing the next step even if this step fails. It
	// also will not permit this failure to impact the overall status of the
	// Promotion.
	ContinueOnError bool
	// Retry is the retry configuration for the Step.
	Retry *kargoapi.PromotionStepRetry
	// Vars is a list of variables definitions that can be used by the
	// Step.
	Vars []kargoapi.ExpressionVariable
	// Config is an opaque JSON to be passed to the StepRunner executing this
	// step.
	Config []byte
}

// GetTimeout returns the maximum interval the provided StepRunner may spend
// attempting to execute the Step before retries are abandoned and the entire
// Promotion is marked as failed. If the StepRunner is a RetryableStepRunner,
// its timeout is used as the default. Otherwise, the default is 0 (no limit).
func (s *Step) GetTimeout(runner promotion.StepRunner) *time.Duration {
	fallback := ptr.To(time.Duration(0))
	if retryCfg, isRetryable := runner.(promotion.RetryableStepRunner); isRetryable {
		fallback = retryCfg.DefaultTimeout()
	}
	return s.Retry.GetTimeout(fallback)
}

// GetErrorThreshold returns the number of consecutive times the provided
// StepRunner must fail to execute the Step (for any reason) before retries are
// abandoned and the entire Promotion is marked as failed. If the StepRunner is
// a RetryableStepRunner, its threshold is used as the default. Otherwise, the
// default is 1.
func (s *Step) GetErrorThreshold(runner promotion.StepRunner) uint32 {
	fallback := uint32(1)
	if retryCfg, isRetryable := runner.(promotion.RetryableStepRunner); isRetryable {
		fallback = retryCfg.DefaultErrorThreshold()
	}
	return s.Retry.GetErrorThreshold(fallback)
}

// Result is the result of a user-defined promotion process executed by the
// Engine. It aggregates the status and output of the individual StepResults
// returned by the StepRunner executing each Step.
type Result struct {
	// Status is the high-level outcome of the user-defined promotion executed by
	// the Engine.
	Status kargoapi.PromotionPhase
	// Message is an optional message that provides additional context about the
	// outcome of the user-defined promotion executed by the Engine.
	Message string
	// HealthChecks collects health.Criteria returned from the execution of
	// individual Steps by their corresponding StepRunners. These criteria can
	// later be used as input to health.Checkers.
	HealthChecks []health.Criteria
	// If the promotion process remains in-progress, perhaps waiting for a change
	// in some external state, the value of this field will indicate where to
	// resume the process in the next reconciliation.
	CurrentStep int64
	// StepExecutionMetadata tracks metadata pertaining to the execution
	// of individual promotion steps.
	StepExecutionMetadata kargoapi.StepExecutionMetadataList
	// State is the current state of the promotion process.
	State promotion.State
}
