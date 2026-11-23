package compose

import (
	"os"
	"strings"
	"testing"
)

// TestEmbeddedComposeAssetMatchesRepoRootFile is the drift lock design.md
// requires (tasks.md 2.3): the repo-root docker-compose.yml MUST stay
// byte-identical to the embedded asset, the same "cannot drift" rule the
// Dockerfile build stages already follow. The relative path climbs from
// internal/infra/install/compose to the repository root.
func TestEmbeddedComposeAssetMatchesRepoRootFile(t *testing.T) {
	t.Parallel()

	repoRootBytes, err := os.ReadFile("../../../../docker-compose.yml")
	if err != nil {
		t.Fatalf("ReadFile(repo-root docker-compose.yml) error = %v", err)
	}
	if string(repoRootBytes) != string(ComposeAsset) {
		t.Fatalf("repo-root docker-compose.yml has drifted from the embedded asset (internal/infra/install/compose/assets/docker-compose.yml); keep both files byte-identical")
	}
}

// The project-enforcement opt-out must reach the container: only variables
// listed under the service environment are forwarded, and an empty value must
// stay empty (serve treats it as unset, so the default applies).
func TestComposeAssetForwardsRequireProjectWithEmptyDefault(t *testing.T) {
	t.Parallel()

	const want = `REGISTRY_REQUIRE_PROJECT: "${REGIXTRY_REQUIRE_PROJECT:-}"`
	if !strings.Contains(string(ComposeAsset), want) {
		t.Fatalf("compose asset does not forward %s", want)
	}
}
