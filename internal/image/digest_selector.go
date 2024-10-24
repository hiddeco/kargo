package image

import (
	"context"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/akuity/kargo/internal/logging"
)

// digestSelector implements the Selector interface for SelectionStrategyDigest.
type digestSelector struct {
	repoClient *repositoryClient
	constraint string
	platform   *platformConstraint
}

// newDigestSelector returns an implementation of the Selector interface for
// SelectionStrategyDigest.
func newDigestSelector(
	repoClient *repositoryClient,
	constraint string,
	platform *platformConstraint,
) (Selector, error) {
	if constraint == "" {
		return nil, errors.New("digest selection strategy requires a constraint")
	}
	return &digestSelector{
		repoClient: repoClient,
		constraint: constraint,
		platform:   platform,
	}, nil
}

// Select implements the Selector interface.
func (d *digestSelector) Select(ctx context.Context) (*Image, error) {
	logger := logging.LoggerFromContext(ctx).WithFields(log.Fields{
		"registry":            d.repoClient.registry.name,
		"image":               d.repoClient.image,
		"selectionStrategy":   SelectionStrategyDigest,
		"platformConstrained": d.platform != nil,
	})
	logger.Trace("selecting image")

	ctx = logging.ContextWithLogger(ctx, logger)

	tags, err := d.repoClient.getTags(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error listing tags")
	}
	if len(tags) == 0 {
		logger.Trace("found no tags")
		return nil, nil
	}
	logger.Trace("got all tags")

	for _, tag := range tags {
		if tag != d.constraint {
			continue
		}
		image, err := d.repoClient.getImageByTag(ctx, tag, d.platform)
		if err != nil {
			return nil, errors.Wrapf(err, "error retrieving image with tag %q", tag)
		}
		if image == nil {
			logger.Tracef(
				"image with tag %q was found, but did not match platform constraint",
				tag,
			)
			return nil, nil
		}
		logger.WithFields(log.Fields{
			"tag":    image.Tag,
			"digest": image.Digest.String(),
		}).Trace("found image")
		return image, nil
	}

	logger.Trace("no images matched criteria")
	return nil, nil
}
