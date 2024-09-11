package directives

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/sosedoff/gitkit"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/akuity/kargo/internal/controller/git"
	"github.com/akuity/kargo/internal/credentials"
	"github.com/akuity/kargo/internal/gitprovider"
)

func TestGitOpenPRDirective_Validate(t *testing.T) {
	testCases := []struct {
		name             string
		config           Config
		expectedProblems []string
	}{
		{
			name:   "repoURL not specified",
			config: Config{},
			expectedProblems: []string{
				"(root): repoURL is required",
			},
		},
		{
			name: "repoURL is empty string",
			config: Config{
				"repoURL": "",
			},
			expectedProblems: []string{
				"repoURL: String length must be greater than or equal to 1",
			},
		},
		{
			name:   "targetBranch not specified",
			config: Config{},
			expectedProblems: []string{
				"(root): targetBranch is required",
			},
		},
		{
			name: "targetBranch is empty string",
			config: Config{
				"targetBranch": "",
			},
			expectedProblems: []string{
				"targetBranch: String length must be greater than or equal to 1",
			},
		},
		{
			name:   "neither sourceBranch nor sourceBranchFromPush specified",
			config: Config{},
			expectedProblems: []string{
				"(root): Must validate one and only one schema",
			},
		},
		{
			name: "both sourceBranch and sourceBranchFromPush specified",
			config: Config{
				"sourceBranch":         "main",
				"sourceBranchFromPush": "push",
			},
			expectedProblems: []string{
				"(root): Must validate one and only one schema",
			},
		},
		{
			name: "provider is an invalid value",
			config: Config{
				"provider": "bogus",
			},
			expectedProblems: []string{
				"provider: provider must be one of the following:",
			},
		},
		{
			name: "valid without explicit provider",
			config: Config{
				"repoURL":      "https://github.com/example/repo.git",
				"sourceBranch": "fake-branch",
				"targetBranch": "another-fake-branch",
			},
		},
		{
			name: "valid with explicit provider",
			config: Config{
				"provider":     "github",
				"repoURL":      "https://github.com/example/repo.git",
				"sourceBranch": "fake-branch",
				"targetBranch": "another-fake-branch",
			},
		},
		{
			name: "valid with source branch from push",
			config: Config{
				"provider":             "github",
				"repoURL":              "https://github.com/example/repo.git",
				"sourceBranchFromPush": "fake-step",
				"targetBranch":         "fake-branch",
			},
		},
	}

	d := newGitOpenPRDirective()
	dir, ok := d.(*gitOpenPRDirective)
	require.True(t, ok)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := dir.validate(testCase.config)
			if len(testCase.expectedProblems) == 0 {
				require.NoError(t, err)
			} else {
				for _, problem := range testCase.expectedProblems {
					require.ErrorContains(t, err, problem)
				}
			}
		})
	}
}

func TestGitOpenPRDirective_Run(t *testing.T) {
	const testSourceBranch = "source"
	const testTargetBranch = "target"

	// Set up a test Git server in-process
	service := gitkit.New(
		gitkit.Config{
			Dir:        t.TempDir(),
			AutoCreate: true,
		},
	)
	require.NoError(t, service.Setup())
	server := httptest.NewServer(service)
	defer server.Close()

	// This is the URL of the "remote" repository
	testRepoURL := fmt.Sprintf("%s/test.git", server.URL)

	workDir := t.TempDir()

	repo, err := git.Clone(testRepoURL, nil, nil)
	require.NoError(t, err)
	defer repo.Close()
	err = repo.CreateOrphanedBranch(testSourceBranch)
	require.NoError(t, err)
	err = repo.Commit("Initial commit", &git.CommitOptions{AllowEmpty: true})
	require.NoError(t, err)
	err = repo.Push(nil)
	require.NoError(t, err)

	// Set up a fake git provider
	const fakeGitProviderName = "fake"
	const testPRNumber int64 = 42
	gitprovider.RegisterProvider(
		fakeGitProviderName,
		gitprovider.ProviderRegistration{
			NewService: func(
				string,
				*gitprovider.GitProviderOptions,
			) (gitprovider.GitProviderService, error) {
				return &gitprovider.FakeGitProviderService{
					CreatePullRequestFn: func(
						context.Context,
						gitprovider.CreatePullRequestOpts,
					) (*gitprovider.PullRequest, error) {
						return &gitprovider.PullRequest{Number: testPRNumber}, nil
					},
				}, nil
			},
		},
	)

	// Now we can proceed to test the git-open-pr directive...

	d := newGitOpenPRDirective()
	dir, ok := d.(*gitOpenPRDirective)
	require.True(t, ok)

	res, err := dir.run(
		context.Background(),
		&StepContext{
			Project:       "fake-project",
			Stage:         "fake-stage",
			WorkDir:       workDir,
			CredentialsDB: &credentials.FakeDB{},
		},
		GitOpenPRConfig{
			RepoURL:            testRepoURL,
			SourceBranch:       testSourceBranch,
			TargetBranch:       testTargetBranch,
			CreateTargetBranch: true,
			Provider:           ptr.To(Provider(fakeGitProviderName)),
		},
	)
	require.NoError(t, err)
	prNumber, ok := res.Output.Get(prNumberKey)
	require.True(t, ok)
	require.Equal(t, testPRNumber, prNumber)

	// Assert that the target branch, which didn't already exist, was created
	exists, err := repo.RemoteBranchExists(testTargetBranch)
	require.NoError(t, err)
	require.True(t, exists)
}
