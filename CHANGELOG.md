# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `id` computed attribute on `bifrost_provider` (mirrors `provider_name`) for
  tooling compatibility.
- Schema-level defaults for `network_config` (`default_request_timeout_in_seconds`,
  `max_retries`, `retry_backoff_initial_ms`, `retry_backoff_max_ms`,
  `insecure_skip_verify`), `concurrency_and_buffer_size`, and key `weight`/`enabled`.
- `MarkdownDescription` on every schema attribute and resource for proper
  Registry rendering.
- `tflog.Debug` instrumentation at every CRUD boundary.
- GoReleaser configuration and tag-triggered release workflow
  (`.github/workflows/release.yml`).
- CI workflow (`.github/workflows/test.yml`) running build, golangci-lint,
  generate-diff check, plugin-testing, and Terratest e2e on every PR.
- `tfplugindocs` integration via `tools/tools.go` and `//go:generate` in
  `main.go`. Docs land in `docs/` and are checked in.
- Plugin-testing acceptance suite under `internal/resources/*_test.go`
  validating config validators and string-OneOf validators with no network.
- `terraform-registry-manifest.json` declaring protocol v6.0.

### Changed
- Examples restructured into the Registry-expected layout
  (`examples/{provider,resources/<resource_name>}/`).
- `responseToModel` no longer infers nested-block presence from a hand-curated
  list of fields — it follows the user's original configuration via prior
  state, with schema-level defaults filling unset fields.

## [0.1.0] - YYYY-MM-DD

Initial release.

[Unreleased]: https://github.com/maximhq/terraform-provider-bifrost/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/maximhq/terraform-provider-bifrost/releases/tag/v0.1.0
