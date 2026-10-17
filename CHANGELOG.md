# Changelog

All notable changes to the Modal Pulumi provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Added the Apache-2.0 "Copyright 2026, Pulumi Corporation." header to every Go source file under `provider/`
  (`make check_copyright` in the archetype requires it). Comment-only change, no behavior or schema change.
- The `modal-python` and `modal-ts` examples use the placeholder `sk_test_xxx_placeholder` for the `STRIPE_API_KEY`
  app secret instead of `sk_test_placeholder`, which gitleaks reports as a Stripe access token.
- Added `.gitleaks.toml`, a reviewed allowlist for the single literal `sk_test_placeholder` that remains in git
  history (documentation placeholder, not a credential).
- Updated the Modal Go SDK (`github.com/modal-labs/modal-client/go`) from v0.9.0 to v0.11.1 and gRPC
  from v1.83.0 to v1.83.2 (clears GO-2026-6443). No resource or schema changes.

### Fixed

- Examples and templates no longer read `tokenId` / `tokenSecret` from stack config or pass them to
  each resource. All 12 examples and 18 templates now run through the default provider using only
  `MODAL_TOKEN_ID`, `MODAL_TOKEN_SECRET` and the optional `MODAL_ENVIRONMENT`. The `tokenId` /
  `tokenSecret` prompts were removed from the template `Pulumi.yaml` files. README documents the
  environment-variable flow.
- Provider config now falls back to `MODAL_TOKEN_ID`, `MODAL_TOKEN_SECRET`,
  `MODAL_ENVIRONMENT` and `MODAL_SERVER_ADDR`. The fallbacks were declared as `env`
  struct tags, which `infer` ignores, so the default provider failed with
  "both tokenId and tokenSecret are required" unless `modal:*` config was set.

### Added

- Initial release of the Modal provider. Manages Modal (modal.com) serverless
  compute resources directly against Modal's public gRPC control-plane API
  (there is no Terraform provider for Modal to bridge from):
  - `modal:index:Secret` — a durable, named collection of key/value pairs
    injected as environment variables into Modal Functions. Values are
    updatable in place via `SecretUpdate`.
  - `modal:index:Volume` — a durable, named distributed filesystem volume
    (V1 or V2 filesystem backend).
  - `modal:index:Queue` — a durable, named FIFO/LIFO queue for passing data
    between Modal Functions and external callers.
- Provider-level configuration (`tokenId` / `tokenSecret` / `environment` /
  `serverAddr`), each overridable per-resource, with environment-variable
  fallback (`MODAL_TOKEN_ID`, `MODAL_TOKEN_SECRET`, `MODAL_ENVIRONMENT`,
  `MODAL_SERVER_ADDR`) matching the Modal Python SDK and CLI conventions.
- Examples and `pulumi new` templates for all 6 languages (TypeScript,
  Python, Go, C#, Java, YAML).
