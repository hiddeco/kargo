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
