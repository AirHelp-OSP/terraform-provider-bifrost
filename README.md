# terraform-provider-bifrost

> [!NOTE]
> **Bifrost** is a product of [MaximAI](https://github.com/maximhq).

Terraform provider for [Bifrost](https://github.com/maximhq/bifrost) — the AI/LLM gateway by MaximAI.

## Resource Coverage

Coverage is measured against the CRUD-able entities exposed by the Bifrost management API. Read-only telemetry endpoints (logs, metrics) and static `config.json` settings are out of scope.

Teams and customers are excluded — they are Bifrost enterprise features and out of scope for this provider.

**Overall coverage: 2 / 5 resources (40%)**

| Bifrost entity | API endpoint | Terraform resource | Supported |
|----------------|--------------|--------------------|:---------:|
| Provider config | `/api/providers` | `bifrost_provider` | ✅ |
| Virtual key | `/api/governance/virtual-keys` | `bifrost_virtual_key` | ✅ |
| Budget (standalone) | `/api/governance/budgets` | `bifrost_budget` | ❌ |
| Rate limit (standalone) | `/api/governance/rate-limits` | `bifrost_rate_limit` | ❌ |
| MCP client | `/api/mcp/client` | `bifrost_mcp_client` | ❌ |

### Notes

- Inline `budget` and `rate_limit` blocks **are** supported on `bifrost_virtual_key`. The ❌ for the standalone variants refers to the top-level governance resources that can be created independently and referenced by ID across virtual keys and provider configs.
- Data sources: **0 / N/A** — none implemented yet.
- Provider-defined functions: **0 / N/A** — none implemented yet.

## Documentation

Resource and provider documentation lives in [`docs/`](./docs) and is rendered by the Terraform Registry. Docs are generated from schema descriptions and `examples/` by [`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs). Regenerate with:

```sh
go generate ./...
```

Commit the resulting `docs/` diff. CI verifies that `go generate ./...` produces no diff.

## Development

```sh
task build          # compile the provider binary
task install        # install into the local plugin cache
task testacc        # run e2e tests (requires Docker + OpenTofu)
task fmt            # gofmt -s -w
task vet            # go vet
go test ./internal/...   # plugin-testing acceptance suite (no network)
```

## Releasing

Releases are cut by pushing an annotated `vX.Y.Z` tag. The `.github/workflows/release.yml` workflow runs [GoReleaser](https://goreleaser.com/) which builds for `darwin/linux/windows/freebsd × amd64/386/arm/arm64`, GPG-signs the checksum file, attaches the registry manifest, and creates a GitHub Release. Required repo secrets:

- `GPG_PRIVATE_KEY` — armored ASCII; the public half must be registered on the `airhelp-osp` Terraform Registry namespace.
- `PASSPHRASE` — passphrase for that key.

Process:

1. Update `CHANGELOG.md` (move `[Unreleased]` items under a new version heading).
2. Commit and push to `master`.
3. Tag: `git tag -a vX.Y.Z -m "vX.Y.Z"` then `git push --tags`.
4. Watch the `release` workflow finish, then publish the version on the [Terraform Registry](https://registry.terraform.io/providers/maximhq/bifrost).
