package v1alpha1

import (
	"context"
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetApplication returns a pointer to the Argo CD Application resource
// specified by the namespace and name arguments. If no such resource is found,
// nil is returned instead.
func GetApplication(
	ctx context.Context,
	ctrlRuntimeClient client.Client,
	namespace string,
	name string,
) (*Application, error) {
	app := Application{}
	if err := ctrlRuntimeClient.Get(
		ctx,
		client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
		&app,
	); err != nil {
		if err = client.IgnoreNotFound(err); err == nil {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"error getting Argo CD Application %q in namespace %q: %w",
			name,
			namespace,
			err,
		)
	}
	return &app, nil
}

// FilterAppConditions returns a slice of v1alpha1.ApplicationCondition that
// match the provided types.
func FilterAppConditions(
	app *Application,
	t ...ApplicationConditionType,
) []ApplicationCondition {
	c := make([]ApplicationCondition, 0, len(app.Status.Conditions))
	for _, condition := range app.Status.Conditions {
		if slices.Contains(t, condition.Type) {
			c = append(c, condition)
		}
	}
	return c
}
