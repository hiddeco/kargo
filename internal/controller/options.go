package controller

import (
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/akuity/kargo/internal/os"
)

func CommonOptions() controller.Options {
	return controller.Options{
		MaxConcurrentReconciles: os.GetEnvInt("MAX_CONCURRENT_RECONCILES", 1),
		RecoverPanic:            ptr.To(true),
	}
}
