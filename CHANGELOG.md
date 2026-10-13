# Changelog

All notable changes to the Modal Pulumi provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

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
