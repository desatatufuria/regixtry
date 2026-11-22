package tui

// newSigningReposScreen constructs the "Signing policy" repositories screen,
// backed by the shared featureOverridesScreen (screen_gitleaks_repos.go) fixed
// to signingFeatureName, so neither Secret scanning nor Signing policy can be
// confused with the other at a call site.
func newSigningReposScreen() featureOverridesScreen {
	return newFeatureOverridesScreen(screenSecuritySigningRepos, signingFeatureName)
}
