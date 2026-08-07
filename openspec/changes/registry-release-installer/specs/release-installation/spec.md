# Release Installation Specification

## Purpose

Define the planned release-backed installer behavior for `registry`.

## Current Repository Facts

| Topic | Current fact |
|-------|--------------|
| Installer | `install.sh` currently clones the repository and builds `./cmd/registry` locally. |
| README | `README.md` currently documents a source-build `curl | bash` flow that requires `git` and `go`. |
| Main spec | No existing `openspec/specs/release-installation/spec.md` exists in the repository today. |

## Requirements

### Requirement: Release-backed Linux installation

The system MUST install `registry` for supported Linux targets from GitHub Release assets, and MUST NOT require a local source build in the default installer flow.

#### Scenario: Install from a published Linux release

- GIVEN a supported Linux host and a tagged release with the required Linux asset
- WHEN the user runs the installer
- THEN the installer downloads the release asset for that Linux target
- AND installs the binary without cloning or building the repository locally

#### Scenario: Unsupported non-Linux target

- GIVEN a host outside the Linux support boundary for this change
- WHEN the user runs the installer
- THEN the installer stops and reports that automated release installation is not available for that target

### Requirement: Mandatory checksum verification

The system MUST verify the downloaded release asset against a published checksum before installation, and SHALL treat checksum retrieval, lookup, or validation failures as fatal.

#### Scenario: Checksum verification succeeds

- GIVEN a Linux release asset and matching published checksum
- WHEN the installer downloads and validates the asset
- THEN installation continues only after the checksum verification passes

#### Scenario: Checksum asset missing or mismatched

- GIVEN the selected release asset is missing from checksums, or its checksum does not match
- WHEN the installer validates the download
- THEN the installer exits without installing the binary

### Requirement: Hard failure with manual guidance

The system MUST fail hard on release asset or checksum problems, MUST provide clear manual guidance, and MUST NOT fall back to a source-build path automatically.

#### Scenario: Release asset lookup fails

- GIVEN the required Linux release asset cannot be resolved or downloaded
- WHEN the installer attempts installation
- THEN the installer exits with an explicit error
- AND includes manual guidance instead of triggering a source build

### Requirement: Stable installed binary name

The system SHALL install the executable under the stable command name `registry` for this release-backed flow.

#### Scenario: Installed command name remains stable

- GIVEN a successful release-backed installation
- WHEN the binary is placed in the target install directory
- THEN the installed executable name is `registry`
