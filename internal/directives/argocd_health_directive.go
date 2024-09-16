package directives

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xeipuuv/gojsonschema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argocd "github.com/akuity/kargo/internal/controller/argocd/api/v1alpha1"
)

func init() {
	// Register the git-clone directive with the builtins registry.
	builtins.RegisterDirective(
		newArgoCDHealthDirective(),
		&DirectivePermissions{
			AllowArgoCDClient: true,
		},
	)
}

// gitCloneDirective is a directive that clones one or more refs from a remote
// Git repository to one or more working directories.
type argoCDHealthDirective struct {
	schemaLoader gojsonschema.JSONLoader
}

// newArgoCDHealthDirective creates a new argocd-health directive.
func newArgoCDHealthDirective() Directive {
	d := &argoCDHealthDirective{}
	d.schemaLoader = getConfigSchemaLoader(d.Name())
	return d
}

// Name implements the Directive interface.
func (d *argoCDHealthDirective) Name() string {
	return "argocd-health"
}

// Run implements the Directive interface.
func (d *argoCDHealthDirective) Run(
	_ context.Context,
	stepCtx *StepContext,
) (Result, error) {
	failure := Result{Status: StatusFailure}

	// Validate the configuration against the JSON Schema.
	if err := validate(d.schemaLoader, gojsonschema.NewGoLoader(stepCtx.Config), d.Name()); err != nil {
		return failure, err
	}

	// Convert the configuration into a typed object.
	cfg, err := configToStruct[ArgoCDHealthConfig](stepCtx.Config)
	if err != nil {
		return failure, fmt.Errorf("could not convert config into %s config: %w", d.Name(), err)
	}

	return d.run(context.Background(), stepCtx, cfg)
}

func (d *argoCDHealthDirective) run(
	ctx context.Context,
	stepCtx *StepContext,
	cfg ArgoCDHealthConfig,
) (Result, error) {
	if !cfg.Wait.Enabled {
		return d.runHealthCheck(ctx, stepCtx, cfg.Applications, make(map[string]struct{}, len(cfg.Applications)))
	}

	duration, err := time.ParseDuration(cfg.Wait.Timeout)
	if err != nil {
		return Result{Status: StatusFailure}, fmt.Errorf("could not parse timeout duration: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var healthyApplications = make(map[string]struct{}, len(cfg.Applications))
	var lastResult Result
	var lastErr error

	if err = wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.5,
		Jitter:   0.1,
		Steps:    0,
		Cap:      1 * time.Minute,
	}, func(ctx context.Context) (done bool, err error) {
		lastResult, lastErr = d.runHealthCheck(ctx, stepCtx, cfg.Applications, healthyApplications)
		return lastResult.Status == StatusSuccess, nil
	}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Result{Status: StatusFailure}, fmt.Errorf(
				"health check timed out after %s: %w",
				cfg.Wait.Timeout, lastErr,
			)
		}
		return Result{Status: StatusFailure}, err
	}
	return lastResult, nil
}

// runHealthCheck runs a health check on the provided Argo CD Applications.
func (d *argoCDHealthDirective) runHealthCheck(
	ctx context.Context,
	stepCtx *StepContext,
	applications []Application,
	healthyApplications map[string]struct{},
) (Result, error) {
	var errs []error

	for _, ref := range applications {
		fqRef := fmt.Sprintf("%s/%s", ref.Namespace, ref.Name)

		if _, ok := healthyApplications[fqRef]; ok {
			continue
		}

		if err := d.checkApplicationHealth(ctx, stepCtx.ArgoCDClient, ref); err != nil {
			errs = append(errs, err)
			continue
		}

		healthyApplications[fqRef] = struct{}{}
	}

	if len(errs) > 0 {
		return Result{Status: StatusFailure}, errors.Join(errs...)
	}

	return Result{Status: StatusSuccess}, nil
}


// checkApplicationHealth checks the health of an Argo CD Application by
// querying the Kubernetes API server for the Application resource and
// inspecting its health conditions and health state. If the Application
// is not healthy, an error is returned.
func (d *argoCDHealthDirective) checkApplicationHealth(ctx context.Context, c client.Client, app Application) error {
	argoApp := &argocd.Application{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.Name}, argoApp); err != nil {
		err = fmt.Errorf(
			"error finding Argo CD Application %q in namespace %q: %w",
			app.Name, app.Namespace, err,
		)
		if client.IgnoreNotFound(err) == nil {
			err = fmt.Errorf(
				"unable to find Argo CD Application %q in namespace %q",
				app.Name, app.Namespace,
			)
		}
		return err
	}

	if err := d.checkHealthConditions(argoApp); err != nil {
		return fmt.Errorf("Application %q in namespace %q has health issues: %w", app.Name, app.Namespace, err)
	}

	if err := d.checkApplicationHealthState(argoApp); err != nil {
		return err
	}

	return nil
}

// checkHealthConditions checks the health conditions of an Argo CD Application.
// If any conditions are found that indicate the application is unhealthy, an
// error is returned.
func (d *argoCDHealthDirective) checkHealthConditions(app *argocd.Application) error {
	if conditions := argocd.FilterAppConditions(app, argocd.ApplicationUnhealthyConditions...); len(conditions) > 0 {
		issues := make([]error, 0, len(conditions))
		for _, c := range conditions {
			issues = append(issues, fmt.Errorf("%s: %s", c.Type, c.Message))
		}
		return errors.Join(issues...)
	}
	return nil
}

// checkApplicationHealthState checks the health state of an Argo CD Application.
// If the application is not healthy (i.e. not in a "Healthy" state), an error
// is returned.
func (d *argoCDHealthDirective) checkApplicationHealthState(app *argocd.Application) error {
	switch app.Status.Health.Status {
	case argocd.HealthStatusProgressing, "":
		return fmt.Errorf("Argo CD Application %q in namespace %q is progressing", app.Name, app.Namespace)
	case argocd.HealthStatusSuspended:
		return fmt.Errorf("Argo CD Application %q in namespace %q is suspended", app.Name, app.Namespace)
	case argocd.HealthStatusHealthy:
		return nil
	default:
		return fmt.Errorf(
			"ArgoCD Application %q in namespace %q has health state %q",
			app.Name, app.Namespace, app.Status.Health.Status,
		)
	}
}
