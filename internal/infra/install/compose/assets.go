package compose

import _ "embed"

// ComposeAsset is the single source of truth for the shippable
// docker-compose.yml (design.md "Compose file source of truth" decision):
// generated YAML would be unreviewable and untestable as text, so this is
// one reviewed file, embedded verbatim. The repo-root docker-compose.yml is
// a byte-identical copy, held by TestEmbeddedComposeAssetMatchesRepoRootFile
// in drift_test.go.
//
//go:embed assets/docker-compose.yml
var ComposeAsset []byte
