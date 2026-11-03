# Changelog

All notable changes to the Modal Pulumi provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-11-03

### Changed

- BREAKING (approved by Engin on #1440): `Volume.version` is renamed `fsVersion` (`fs_version` in Python, `FsVersion` in Go and C#, `.fsVersion` in Java). pulumi-go-provider drops every input named `version` before Diff runs ([pulumi-go-provider#704](https://github.com/pulumi/pulumi-go-provider/issues/704)), so the old property could not be diffed and every `pulumi up` replaced the volume, deleting its data. State written by v0.1.0 migrates automatically; programs must rename the property. `fsVersion` is optional (default `v2`, unchanged) and is always reported as an output.
- `Volume.name` now renames the volume in place instead of replacing it.
- A Volume replacement caused by a changed `fsVersion` deletes the old volume first, since the new one reuses its name.
- The provider records the effective Modal environment of each resource in state, so changing `modal:environment` (or the account default) later replaces the resource instead of looking in the wrong place.
- Examples and templates use `fsVersion`.
- `google.golang.org/protobuf` v1.36.12 (now a direct dependency) and `google.golang.org/grpc` v1.84.0; only go.mod and go.sum change.

### Security

- `golang.org/x/net` v0.61.0 to v0.62.0 (indirect dependency; fixes GO-2026-6860 and GO-2026-6866; only go.mod and go.sum change).

### Added

- New resources `Dict`, `Environment`, `App` and `Image`. `Dict` mirrors `Queue`. `Environment` manages a workspace environment (name, web suffix, task and GPU limits, all in place). `App` creates the empty named App with in-place tags (no code deployment). `Image` builds from a registry base image plus Dockerfile commands through the Modal SDK, optionally publishes it under a name and pulls from a private registry with a Secret. All four read by ID, import by ID alone and are covered by the fake-server tests.
- Lookup functions `getSecret`, `getVolume`, `getQueue`, `getDict`, `getEnvironment`, `getApp`, `getImage`, `getFunction`, `getCls`, `getProxy` and `getWorkspace`. They never create anything; a missing object is an error naming the object and environment.
- Coverage inventory `research/coverage/modal.md` classifying every Modal SDK service and RPC group. Sandbox is recorded as deferred pending a decision; service users, proxy auth tokens and workspace settings are planned.
- The provider now links the Modal SDK root package (new indirect dependencies `fxamacker/cbor`, `golang-jwt/jwt`, `og-rek`, `go-toml`) and sends the auth headers on streaming calls too.
- Examples `modal-platform-*` and templates `platform-modal-cloud-*` in all six languages.
- Import by ID alone for Secret, Volume and Queue. Volume and Queue import look in `modal:environment` (or the account default) and fail with a clear message when the ID lives elsewhere; Secret import reads the environment from Modal.
- Secret output `keys`: the sorted key names currently stored, so drift in keys shows up on refresh. Values stay write-only.
- Secret key names are validated at preview, and Volume `fsVersion` must be `v1` or `v2` at preview.

### Fixed

- Volume was replaced on every update, losing its data, because the `version` property was dropped by the framework (see the breaking rename above).
- `pulumi refresh` now reads by ID and refreshes every field (Volume name and filesystem version, Secret name, environment and keys, Queue name); previously it looked objects up by name and never updated them, and a changed name or environment reported the object as deleted.

## [0.1.0] - 2026-10-27

### Changed

- No user-facing change: the monorepo source module moved to the provider root (`go.mod`/`go.sum` out of `provider/`); the published module path `github.com/pulumi-labs/pulumi-modal` is unchanged, and so are the provider binary, schema and SDKs. `CLAUDE.md` (from `skills/SKILLS.md`) now describes building from the repository root.
- The `job-queue-modal-cloud-java` and `ml-training-modal-cloud-java` templates pass the secret `values` as an `Output<Map<String,String>>`, so they compile and the secret stays secret.
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
