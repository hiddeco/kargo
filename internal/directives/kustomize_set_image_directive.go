package directives

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/xeipuuv/gojsonschema"
	"sigs.k8s.io/kustomize/api/konfig"
	kustypes "sigs.k8s.io/kustomize/api/types"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
	yaml "sigs.k8s.io/yaml/goyaml.v3"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	"github.com/akuity/kargo/internal/controller/freight"
	intyaml "github.com/akuity/kargo/internal/yaml"
)

// preserveSeparator is the separator used to preserve values in the
// Kustomization image field.
const preserveSeparator = "*"

func init() {
	// Register the kustomize-set-image directive with the builtins registry.
	builtins.RegisterDirective(
		newKustomizeSetImageDirective(),
		&DirectivePermissions{
			AllowKargoClient: true,
		},
	)
}

// kustomizeSetImageDirective is a directive that sets images in a Kustomization
// file.
type kustomizeSetImageDirective struct {
	schemaLoader gojsonschema.JSONLoader
}

// newKustomizeSetImageDirective creates a new kustomize-set-image directive.
func newKustomizeSetImageDirective() Directive {
	return &kustomizeSetImageDirective{
		schemaLoader: getConfigSchemaLoader("kustomize-set-image"),
	}
}

func (d *kustomizeSetImageDirective) Name() string {
	return "kustomize-set-image"
}

func (d *kustomizeSetImageDirective) Run(ctx context.Context, stepCtx *StepContext) (Result, error) {
	// Validate the configuration against the JSON Schema.
	if err := validate(d.schemaLoader, gojsonschema.NewGoLoader(stepCtx.Config), d.Name()); err != nil {
		return Result{Status: StatusFailure}, err
	}

	// Convert the configuration into a typed object.
	cfg, err := configToStruct[KustomizeSetImageConfig](stepCtx.Config)
	if err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("could not convert config into kustomize-set-image config: %w", err)
	}

	return d.run(ctx, stepCtx, cfg)
}

func (d *kustomizeSetImageDirective) run(
	ctx context.Context,
	stepCtx *StepContext,
	cfg KustomizeSetImageConfig,
) (Result, error) {
	// Find the Kustomization file.
	kusPath, err := findKustomization(stepCtx.WorkDir, cfg.Path)
	if err != nil {
		return Result{Status: StatusFailure}, fmt.Errorf("could not discover kustomization file: %w", err)
	}

	// Discover image origins and collect target images.
	targetImages, err := d.buildTargetImages(ctx, stepCtx, cfg.Images)
	if err != nil {
		return Result{Status: StatusFailure}, err
	}

	// Update the Kustomization file with the new images.
	if err := updateKustomizationFile(kusPath, targetImages); err != nil {
		return Result{Status: StatusFailure}, err
	}

	return Result{Status: StatusSuccess}, nil
}

func (d *kustomizeSetImageDirective) buildTargetImages(
	ctx context.Context,
	stepCtx *StepContext,
	images []KustomizeSetImageConfigImage,
) (map[string]kustypes.Image, error) {
	targetImages := make(map[string]kustypes.Image, len(images))

	for _, img := range images {
		var desiredOrigin *kargoapi.FreightOrigin
		if img.FromOrigin != nil {
			desiredOrigin = &kargoapi.FreightOrigin{
				Kind: kargoapi.FreightOriginKind(img.FromOrigin.Kind),
				Name: img.FromOrigin.Name,
			}
		}

		discoveredImage, err := freight.FindImage(
			ctx,
			stepCtx.KargoClient,
			stepCtx.Project,
			stepCtx.FreightRequests,
			desiredOrigin,
			stepCtx.Freight.References(),
			img.Image,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to discover image for %q: %w", img.Image, err)
		}
		if discoveredImage == nil {
			return nil, fmt.Errorf("no image found for %q", img.Image)
		}

		targetImage := kustypes.Image{
			Name:    img.Image,
			NewName: img.NewName,
			NewTag:  discoveredImage.Tag,
		}
		if img.Name != "" {
			targetImage.Name = img.Name
		}
		if img.UseDigest {
			targetImage.Digest = discoveredImage.Digest
		}

		targetImages[targetImage.Name] = targetImage
	}

	return targetImages, nil
}

func updateKustomizationFile(kusPath string, targetImages map[string]kustypes.Image) error {
	// Read the Kustomization file, and unmarshal it.
	node, err := readKustomizationFile(kusPath)
	if err != nil {
		return err
	}

	// Decode the Kustomization file into a typed object to work with.
	currentImages, err := getCurrentImages(node)
	if err != nil {
		return err
	}

	// Merge existing images with new images.
	newImages := mergeImages(currentImages, targetImages)

	// Update the images field in the Kustomization file.
	if err = intyaml.UpdateField(node, "images", newImages); err != nil {
		return fmt.Errorf("could not update images field in Kustomization file: %w", err)
	}

	// Write the updated Kustomization file.
	return writeKustomizationFile(kusPath, node)
}

func readKustomizationFile(kusPath string) (*yaml.Node, error) {
	b, err := os.ReadFile(kusPath)
	if err != nil {
		return nil, fmt.Errorf("could not read Kustomization file: %w", err)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(b, &node); err != nil {
		return nil, fmt.Errorf("could not unmarshal Kustomization file: %w", err)
	}
	return &node, nil
}

func getCurrentImages(node *yaml.Node) ([]kustypes.Image, error) {
	var currentImages []kustypes.Image
	if err := intyaml.DecodeField(node, "images", &currentImages); err != nil {
		var fieldErr intyaml.FieldNotFoundErr
		if !errors.As(err, &fieldErr) {
			return nil, fmt.Errorf("could not decode images field in Kustomization file: %w", err)
		}
	}
	return currentImages, nil
}

func mergeImages(currentImages []kustypes.Image, targetImages map[string]kustypes.Image) []kustypes.Image {
	for _, img := range currentImages {
		if targetImg, ok := targetImages[img.Name]; ok {
			// Reuse the existing new name when asterisk new name is passed
			if targetImg.NewName == preserveSeparator {
				targetImg = replaceNewName(targetImg, img.NewName)
			}

			// Reuse the existing new tag when asterisk new tag is passed
			if targetImg.NewTag == preserveSeparator {
				targetImg = replaceNewTag(targetImg, img.NewTag)
			}

			// Reuse the existing digest when asterisk digest is passed
			if targetImg.Digest == preserveSeparator {
				targetImg = replaceDigest(targetImg, img.Digest)
			}

			targetImages[img.Name] = targetImg

			continue
		}

		targetImages[img.Name] = img
	}

	var newImages []kustypes.Image
	for _, v := range targetImages {
		if v.NewName == preserveSeparator {
			v = replaceNewName(v, "")
		}

		if v.NewTag == preserveSeparator {
			v = replaceNewTag(v, "")
		}

		if v.Digest == preserveSeparator {
			v = replaceDigest(v, "")
		}

		newImages = append(newImages, v)
	}

	// Sort the images by name, in descending order.
	slices.SortFunc(newImages, func(a, b kustypes.Image) int {
		return strings.Compare(a.Name, b.Name)
	})

	return newImages
}

func writeKustomizationFile(kusPath string, node *yaml.Node) error {
	b, err := kyaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("could not marshal updated Kustomization file: %w", err)
	}
	if err = os.WriteFile(kusPath, b, fs.ModePerm); err != nil {
		return fmt.Errorf("could not write updated Kustomization file: %w", err)
	}
	return nil
}

func findKustomization(workDir, path string) (string, error) {
	secureDir, err := securejoin.SecureJoin(workDir, path)
	if err != nil {
		return "", fmt.Errorf("could not secure join path %q: %w", path, err)
	}

	var candidates []string
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		p := filepath.Join(secureDir, name)
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, p)
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("could not find any Kustomization files in %q", path)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("ambiguous result: found multiple Kustomization files in %q: %v", path, candidates)
	}
}

func replaceNewName(image kustypes.Image, newName string) kustypes.Image {
	return kustypes.Image{
		Name:    image.Name,
		NewName: newName,
		NewTag:  image.NewTag,
		Digest:  image.Digest,
	}
}

func replaceNewTag(image kustypes.Image, newTag string) kustypes.Image {
	return kustypes.Image{
		Name:    image.Name,
		NewName: image.NewName,
		NewTag:  newTag,
		Digest:  image.Digest,
	}
}

func replaceDigest(image kustypes.Image, digest string) kustypes.Image {
	return kustypes.Image{
		Name:    image.Name,
		NewName: image.NewName,
		NewTag:  image.NewTag,
		Digest:  digest,
	}
}
