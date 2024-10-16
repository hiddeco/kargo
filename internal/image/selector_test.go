package image

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/distribution/distribution/v3/registry/client/auth/challenge"
	"github.com/stretchr/testify/require"
)

func TestNewSelector(t *testing.T) {
	getChallengeManagerBackup := getChallengeManager
	getChallengeManager = func(
		string,
		http.RoundTripper,
	) (challenge.Manager, error) {
		return challenge.NewSimpleManager(), nil
	}
	defer func() {
		getChallengeManager = getChallengeManagerBackup
	}()

	testCases := []struct {
		name       string
		repoURL    string
		strategy   SelectionStrategy
		opts       *SelectorOptions
		assertions func(s Selector, err error)
	}{
		{
			name:    "invalid allow regex",
			repoURL: "debian",
			opts: &SelectorOptions{
				AllowRegex: "(invalid", // Invalid regex due to unclosed parenthesis
			},
			assertions: func(_ Selector, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "error compiling regular expression")
			},
		},
		{
			name:    "invalid platform constraint",
			repoURL: "debian",
			opts: &SelectorOptions{
				Platform: "invalid",
			},
			assertions: func(_ Selector, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "error parsing platform constraint")
			},
		},
		{
			name:     "invalid selection strategy",
			strategy: SelectionStrategy("invalid"),
			repoURL:  "debian",
			opts: &SelectorOptions{
				Constraint: "invalid", // Not a semver
			},
			assertions: func(_ Selector, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid image selection strategy")
			},
		},
		{
			name:     "success with digest image selector",
			strategy: SelectionStrategyDigest,
			opts: &SelectorOptions{
				Constraint: "fake-constraint",
			},
			repoURL: "debian",
			assertions: func(selector Selector, err error) {
				require.NoError(t, err)
				require.IsType(t, &digestSelector{}, selector)
			},
		},
		{
			name:     "success with lexical image selector",
			strategy: SelectionStrategyLexical,
			repoURL:  "debian",
			assertions: func(selector Selector, err error) {
				require.NoError(t, err)
				require.IsType(t, &lexicalSelector{}, selector)
			},
		},
		{
			name:     "success with newest build image selector",
			strategy: SelectionStrategyNewestBuild,
			repoURL:  "debian",
			assertions: func(selector Selector, err error) {
				require.NoError(t, err)
				require.IsType(t, &newestBuildSelector{}, selector)
			},
		},
		{
			name:     "success with semver image selector",
			strategy: SelectionStrategySemVer,
			repoURL:  "debian",
			assertions: func(selector Selector, err error) {
				require.NoError(t, err)
				require.IsType(t, &semVerSelector{}, selector)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			s, err := NewSelector(
				testCase.repoURL,
				testCase.strategy,
				testCase.opts,
			)
			testCase.assertions(s, err)
		})
	}
}

func TestAllowsTag(t *testing.T) {
	testRegex := regexp.MustCompile("^[a-z]*$")
	testCases := []struct {
		name    string
		tag     string
		allowed bool
	}{
		{
			name:    "tag isn't allowed",
			tag:     "NO",
			allowed: false,
		},
		{
			name:    "tag is allowed",
			tag:     "yes",
			allowed: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(
				t,
				testCase.allowed,
				allowsTag(testCase.tag, testRegex),
			)
		})
	}
}

func TestIgnoresTag(t *testing.T) {
	testIgnore := []string{"ignore-me"}
	testCases := []struct {
		name    string
		tag     string
		ignored bool
	}{
		{
			name:    "tag isn't ignored",
			tag:     "allow-me",
			ignored: false,
		},
		{
			name:    "tag is ignored",
			tag:     "ignore-me",
			ignored: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(
				t,
				testCase.ignored,
				ignoresTag(testCase.tag, testIgnore),
			)
		})
	}
}
