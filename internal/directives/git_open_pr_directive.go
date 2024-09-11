package directives

import (
	"context"
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/akuity/kargo/internal/controller/git"
	"github.com/akuity/kargo/internal/credentials"
	"github.com/akuity/kargo/internal/gitprovider"
)

const prNumberKey = "prNumber"

func init() {
	// Register the git-open-pr directive with the builtins registry.
	builtins.RegisterDirective(
		newGitOpenPRDirective(),
		&DirectivePermissions{AllowCredentialsDB: true},
	)
}

// gitOpenPRDirective is a directive that opens a pull request.
type gitOpenPRDirective struct {
	schemaLoader gojsonschema.JSONLoader
}

// newGitOpenPRDirective creates a new git-open-pr directive.
func newGitOpenPRDirective() Directive {
	d := &gitOpenPRDirective{}
	d.schemaLoader = getConfigSchemaLoader(d.Name())
	return d
}

// Name implements the Directive interface.
func (g *gitOpenPRDirective) Name() string {
	return "git-open-pr"
}

// Run implements the Directive interface.
func (g *gitOpenPRDirective) Run(
	ctx context.Context,
	stepCtx *StepContext,
) (Result, error) {
	if err := g.validate(stepCtx.Config); err != nil {
		return Result{Status: StatusFailure}, err
	}
	cfg, err := configToStruct[GitOpenPRConfig](stepCtx.Config)
	if err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("could not convert config into git-open-pr config: %w", err)
	}
	return g.run(ctx, stepCtx, cfg)
}

// validate validates the git-open-pr directive configuration against the JSON
// schema.
func (g *gitOpenPRDirective) validate(cfg Config) error {
	return validate(g.schemaLoader, gojsonschema.NewGoLoader(cfg), g.Name())
}

func (g *gitOpenPRDirective) run(
	ctx context.Context,
	stepCtx *StepContext,
	cfg GitOpenPRConfig,
) (Result, error) {
	var repoCreds *git.RepoCredentials
	if creds, found, err := stepCtx.CredentialsDB.Get(
		ctx,
		stepCtx.Project,
		credentials.TypeGit,
		cfg.RepoURL,
	); err != nil {
		return Result{Status: StatusFailure}, fmt.Errorf("error getting credentials for %s: %w", cfg.RepoURL, err)
	} else if found {
		repoCreds = &git.RepoCredentials{
			Username:      creds.Username,
			Password:      creds.Password,
			SSHPrivateKey: creds.SSHPrivateKey,
		}
	}

	// Note: Strictly speaking, you don't need to clone a repo to check if a
	// remote branch exists, but our options for authenticating to a remote
	// repository are really only applied when cloning.
	//
	// We could have the user provide the path to a working tree, load it, and use
	// that to check for the existence of the remote branch, but that feels
	// limiting, as it would only enable workflows wherein the user HAS a working
	// tree at their disposal. It is theoretically possible that the PR the user
	// is opening isn't related to anything that occurred in a previous step. i.e.
	// We cannot assume that they have already cloned the repository and have a
	// working tree. Beyond this, if we detect the need to CREATE and push the
	// remote branch, it is preferable that we do not mess with the state of the
	// user's working tree in the process of doing so.
	//
	// For all the reasons above, we ask the user to provide a URL instead of a
	// path and we perform a shallow clone to check for the existence of the
	// remote branch.
	repo, err := git.Clone(
		cfg.RepoURL,
		&git.ClientOptions{
			Credentials:           repoCreds,
			InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify,
		},
		&git.CloneOptions{
			Depth:  1,
			Branch: cfg.SourceBranch,
		},
	)
	if err != nil {
		return Result{Status: StatusFailure}, fmt.Errorf("error cloning %s: %w", cfg.RepoURL, err)
	}
	defer repo.Close()

	// Get the title from the commit message of the head of the source branch
	// BEFORE we move on to ensuring the existence of the target branch because
	// that may involve creating a new branch and committing to it.
	title, err := repo.CommitMessage(cfg.SourceBranch)
	if err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("error getting commit message from head of branch %s: %w", cfg.SourceBranch, err)
	}

	if err = ensureRemoteTargetBranch(repo, cfg.TargetBranch, cfg.CreateTargetBranch); err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("error ensuring existence of remote branch %s: %w", cfg.TargetBranch, err)
	}

	gpOpts := &gitprovider.GitProviderOptions{
		InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify,
	}
	if repoCreds != nil {
		gpOpts.Token = repoCreds.Password
	}
	if cfg.Provider != nil {
		gpOpts.Name = string(*cfg.Provider)
	}
	gitProviderSvc, err := gitprovider.NewGitProviderService(cfg.RepoURL, gpOpts)
	if err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("error creating git provider service: %w", err)
	}

	pr, err := gitProviderSvc.CreatePullRequest(
		ctx,
		gitprovider.CreatePullRequestOpts{
			Head:  cfg.SourceBranch,
			Base:  cfg.TargetBranch,
			Title: title,
		},
	)
	if err != nil {
		return Result{Status: StatusFailure},
			fmt.Errorf("error creating pull request: %w", err)
	}
	return Result{
		Status: StatusSuccess,
		Output: State{
			prNumberKey: pr.Number,
		},
	}, nil
}

// ensureRemoteTargetBranch ensures the existence of a remote branch. If the
// branch does not exist, an empty orphaned branch is created and pushed to the
// remote.
func ensureRemoteTargetBranch(repo git.Repo, branch string, create bool) error {
	exists, err := repo.RemoteBranchExists(branch)
	if err != nil {
		return fmt.Errorf(
			"error checking if remote branch %q of repo %s exists: %w",
			branch, repo.URL(), err,
		)
	}
	if exists {
		return nil
	}
	if !create {
		return fmt.Errorf(
			"remote branch %q does not exist in repo %s", branch, repo.URL(),
		)
	}
	if err = repo.CreateOrphanedBranch(branch); err != nil {
		return fmt.Errorf(
			"error creating orphaned branch %q in repo %s: %w",
			branch, repo.URL(), err,
		)
	}
	if err = repo.Commit(
		"Initial commit",
		&git.CommitOptions{AllowEmpty: true},
	); err != nil {
		return fmt.Errorf(
			"error making initial commit to new branch %q of repo %s: %w",
			branch, repo.URL(), err,
		)
	}
	if err = repo.Push(&git.PushOptions{TargetBranch: branch}); err != nil {
		return fmt.Errorf(
			"error pushing initial commit to new branch %q to repo %s: %w",
			branch, repo.URL(), err,
		)
	}
	return nil
}
