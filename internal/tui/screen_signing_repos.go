package tui

// newSigningReposScreen constructs Signing's own dedicated per-repository
// override list screen (design.md D3, D8), backed by the shared
// featureOverridesScreen type (screen_gitleaks_repos.go) fixed to
// signingFeatureName — Gitleaks and Signing are two DEDICATED screens, not
// one parameterized picker (user-confirmed D3), so this constructor exists
// even though the underlying type is shared: neither screen's identity can
// be confused with the other's at any call site.
func newSigningReposScreen() featureOverridesScreen {
	return newFeatureOverridesScreen(screenSecuritySigningRepos, signingFeatureName)
}
