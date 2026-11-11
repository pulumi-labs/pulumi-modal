# Changelog

All notable changes to the Modal Pulumi provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.7.0] - 2026-11-11

### Added

- `Domain` resource and `getDomain` lookup (#1538): register a custom domain with the workspace. `dnsRecords` lists the DNS records to create; the optional `verify` input asks Modal to check them and issue the certificate (in place, on create, update and refresh until issued); `domainName` replaces. Modal has no call that deletes a domain, so destroy only forgets it (the domain stays registered, remove it in the Modal dashboard) and creating an already registered name adopts it. Import ID is the domain ID. Additive, not breaking; fake control plane tests only, not verified against a live workspace.

### Fixed

- `AppDeployment`: after an interrupted deploy (the read-back failed, so the resource ID is the App's name), refresh and destroy now find the App by name even when the name starts with `ap-`. They used to treat such an ID as a real App ID, report the App gone and leave it running. Modal is now asked whether the ID is an App ID, and only an unknown ID falls back to the name lookup (#1540, #1534).

## [0.6.0] - 2026-11-10

### Added

- `getWorkspaceSettings` lookup and `WorkspaceSettings` resource (#1537): the settings singleton of the workspace the API token belongs to. `defaultEnvironment` (`WorkspaceSetDefaultEnvironment`) and `imageBuilderVersion` (`WorkspaceSetImageBuilderVersion`) update in place, each only when it changed; at least one must be set. The ID and import ID are the workspace ID (a token of another workspace reports it as missing). A refresh reads back the settings the program manages (all of them on import); an unset setting is never a diff. Modal has no call that unsets either setting, so destroy and removing a setting leave the workspace as it is. Declare it once per workspace. `WorkspaceSetDefaultEnvironmentSettings` (block unauthenticated resources) is not modelled because the settings read does not return it. Additive, not breaking; fake control plane tests only, not verified against a live workspace.

### Fixed

- `Image`: destroying an Image no longer deletes the image another Image still uses. Modal image IDs are content-addressed, so two Images with the same definition share one ID, and destroying one of two same-name Images in different environments deleted the shared image and left the other Image pointing at nothing. Delete now removes this Image's name, then checks every environment and keeps the image while any still publishes a name for it (the provider logs which). If the environments cannot be listed the destroy fails and deletes nothing, so retry it. Remaining limitation: two unpublished Images with the same definition still share an ID and cannot be told apart, so destroying either deletes the image the other uses; publish a `name` to avoid it. Mirror issue https://github.com/pulumi-labs/pulumi-modal/issues/26 (#1783).
- `Image`: importing an Image that is published in several environments now honours the configured environment in every case. `MODAL_ENVIRONMENT` was read only through the config default pass, so a provider configured without it ignored the variable and the import failed asking the user to set it; it is now read directly for every resource, matching the documented precedence (resource, provider config, `MODAL_ENVIRONMENT`, profile). The configured environment is chosen whenever the image is published there, and when it is not, the error names the configured environment and where the image is published instead of telling the user to set what they already set. Mirror issue https://github.com/pulumi-labs/pulumi-modal/issues/22 (#1673). The `modal:environment` stack-config path passes in unit tests; if it still fails on a live import the new error shows the environment the provider received.

## [0.5.2] - 2026-11-09

### Fixed

- `pulumi import` of an object that does not exist (an App, AppDeployment, Dict, Environment, Image, Proxy, Queue, Secret, ServiceUser, Volume or WebhookToken; an app that was stopped counts as missing) now fails with an error instead of exiting 0 with an empty resource in state. Read returned the empty response that only a refresh treats as deleted. A refresh of a deleted object still drops it from state (#1715, same defect as #1712).
- `Image`: an imported Image now notices later changes to its build inputs. Modal does not store what an image was built from, so an import recorded no `baseImage`, `dockerfileCommands`, `env`, `gpu` or `registrySecret`, the diff skipped them for the life of the resource and a changed Dockerfile command never rebuilt the Image. When the program declares a `baseImage`, the first update after an import now adopts the declared build inputs in place (planned as one update, never a replace, and it builds nothing); after that a change to any build input plans a replace as for an Image the program created. Previously the first preview after an import showed no changes, now it shows that one adoption update. Mirror issue https://github.com/pulumi-labs/pulumi-modal/issues/23 (#1674).

## [0.5.1] - 2026-11-08

### Fixed

- Security: updated the indirect dependency `github.com/klauspost/compress` from v1.18.7 to v1.20.2, which fixes GO-2026-6865 (GHSA-fpmh-frq8-7rj8, a panic on two-block frames). No behaviour changes.

## [0.5.0] - 2026-11-06

### Added

- `Environment`: new optional `defaultMemberRole` input (`viewer`, `contributor` or `no_access`), the role workspace members have in the environment (`EnvironmentSetDefaultMemberRole`/`EnvironmentGetRoles`). It updates in place, is read back on refresh (drift shows as an update) and adopted on import; unset leaves it as is; other values are rejected at preview. Per-principal role bindings and the managed flag are not modelled yet (#1535). Additive, not breaking.

### Fixed

- `Image`: importing an Image no longer records the account's default environment when the Image lives in another one. Modal looks an image up by ID without saying which environment it is in, so import now records the environment the Image is published in (a configured `modal:environment` / `MODAL_ENVIRONMENT` wins when the Image is published there), and the first preview of the matching program plans no replacement. An Image published in several environments with none configured fails with a message naming them; an unpublished Image records the configured or default environment as before. Mirror issue https://github.com/pulumi-labs/pulumi-modal/issues/19 (#1612).
- `Image`: replacing an Image no longer deletes the replacement. Modal image IDs are content-addressed, so rebuilding an identical definition while the old Image still exists returned the same ID and the engine's delete-replaced step then deleted the new Image. Every replace now deletes the old Image first (`deleteBeforeReplace`), which guarantees a fresh ID; the published name is taken down for the rebuild and republished by the replacement. Deleting or renaming a published name in a non-default environment also targeted the default environment's name, because the name delete was sent without the environment; it is now environment-qualified. Mirror issue https://github.com/pulumi-labs/pulumi-modal/issues/20 (#1613).
- If the provider schema cannot be loaded during a `Read`, the failure is no longer cached for the life of the process: the next `Read` retries (#1599). The lookup functions were audited: none returns a credential value (`getSecret` returns key names only), so no function output changes.

## [0.4.0] - 2026-11-05

### Fixed

- Security: `pulumi import` and `pulumi refresh` now store the properties this provider marks secret (`Secret.values`, `AppDeployment.env`, `ServiceUser.serviceTokenSecret`, `WebhookToken.webhookTokenSecret` and every per-resource `tokenId` / `tokenSecret`) as secrets. A `Read` response only kept the secretness of the state it was given, and an import has no state, so a value the provider recovered from the API was written to the state as plaintext. The secret properties are read from the provider's own schema, so every resource is covered. State imported before this fix stays plaintext until its next `pulumi refresh`, which re-encrypts it (#1577).
- A preview no longer invents resource IDs. `Create` during `pulumi preview` returned the resource's logical name (or the placeholder `preview`) as its ID, so `id` was a known, wrong value and a program passing it to a lookup or to another resource ran against a name that does not exist and failed the preview (for example `getWebhook(webhook.id)`). A preview has no real ID now, so `id` stays unknown until the apply. Mirror issue https://github.com/pulumi-labs/pulumi-huggingface/issues/7 (#1590).
- Changing the provider configuration (a rotated token, API key or secret, a new endpoint or region) is an in-place update of the default provider. By default pulumi-go-provider replaced the provider on any config change, and replacing a provider replaces every resource that uses it, so rotating a credential would have deleted and recreated every resource. Each resource's own diff still decides whether it changes. Mirror issue https://github.com/pulumi-labs/pulumi-huggingface/issues/8 (#1591).

### Added

- `Environment`: new optional `cycleBudgetDollars` input, the environment's spend limit per billing cycle (`EnvironmentSetBudget`/`EnvironmentGetBudget`). It updates in place, is read back on refresh (drift shows as an update) and adopted on import. Unset leaves the budget as is, so removing the property does not clear a budget that was set. Negative values are rejected at preview. Additive, not breaking.
- Add the `WebhookToken` resource and `getWebhookToken` lookup: a Modal proxy auth token (`Modal-Key`/`Modal-Secret`). The secret is a write-once secret output kept across refreshes (an import has none); `name` updates in place, `scoped` replaces. Attaching a scoped token to environments is not modelled yet (#1536).
- Add the `Proxy` resource: a Modal Proxy with static outbound IP addresses. `ipCount` adds or releases addresses in place; name, environment and region replace. Imports by ID (#1445).
- Add the `ServiceUser` resource and `getServiceUser`: a non-human workspace identity with its own API token. The token secret is a write-once secret output (`serviceTokenSecret`); an imported service user has none. Modal has no update call, so every input change replaces it (#1445).
- Add the `AppDeployment` resource and `getAppDeployment`: deploys a Modal App from a Python app file by running `modal deploy`, redeploys in place when the contents of the source directory (or a trigger) change, reports the App's `functions` and `webUrls`, and stops the App on delete. It needs the Modal CLI on the machine that runs Pulumi (#1445).

## [0.3.1] - 2026-11-05

### Added

- Add `qa.yaml`, the runtime test manifest QA reads: credentials, every resource and function with its test plan and cost, and the regressions to re-check. It is copied to the mirror root and changes no provider behavior.

### Fixed

- Fix an imported `Secret` showing an update to `values` on every preview straight after `pulumi import` (and refusing the `import:` resource option): Diff now compares key names while the values are unknown and picks up value changes from the program's last recorded inputs (pulumi-labs/pulumi-modal#15; upstream pulumi/pulumi-go-provider#710).
- Fix `getQueue`, `getVolume` and `getDict` returning `environment: ""` when no environment is configured: they now report the environment searched, else the account default, like `getSecret` and `getApp` (pulumi-labs/pulumi-modal#16).

## [0.3.0] - 2026-11-04

### Added

- The provider reads `~/.modal.toml` (or the file named by `MODAL_CONFIG_PATH`) as its last credential source, so a user who ran `modal token new` or `modal setup` needs no environment variables (pulumi-labs/pulumi-modal#6). Lookup order: resource `tokenId`/`tokenSecret`, `modal:tokenId`/`modal:tokenSecret`, `MODAL_TOKEN_ID`/`MODAL_TOKEN_SECRET`, then the profile named by `MODAL_PROFILE`, otherwise the one marked `active = true` (or called `default`). When the profile supplies the credentials, its `environment` applies below `modal:environment`/`MODAL_ENVIRONMENT` and its `server_url` below `modal:serverAddr`.

### Changed

- The missing-credentials error lists every credential source and no longer repeats its prefix (`modal: modal: both tokenId and tokenSecret are required` is now `modal: no Modal credentials found: ...`).
- The README no longer says authentication "mirrors the Modal Python SDK exactly", and documents the lookup order and where it differs: the token pair comes from one source only, and the profile's `environment` and `server_url` apply only when the profile supplies the credentials.
- `github.com/pelletier/go-toml/v2` is now a direct dependency (it was indirect through the Modal SDK).

### Fixed

- Fix `getApp` for an App that does not exist: the live API answers the lookup with success and an empty ID, which surfaced as `'' is not a valid App ID`. It now fails with `modal: app "<name>" was not found in ...` like the other lookups (pulumi-labs/pulumi-modal#13).
- Fix every `App` and `Image` create failing against the live Modal API with "Could not determine the SDK version for client type CLIENT_TYPE_LIBMODAL_GO": the provider sent `x-modal-libmodal-version: pulumi-modal/1.0.0`, which the control plane does not recognise. It now sends `modal-go/<vendored SDK version>` like the official Go SDK, read from the build info (a test keeps the fallback equal to go.mod) (pulumi-labs/pulumi-modal#12).

- Fix `Environment` update on the live Modal API, whose update response is sparse: the resource wrote an empty `environmentId` and `name` into state, so the next preview showed a spurious rename and later updates failed. Update now resolves the current name by ID and reads the environment back by ID; refresh repairs already-broken state (pulumi-labs/pulumi-modal#11).
- Fix `App` create failing against the live Modal API with "Invalid object creation type": Apps have no fail-if-exists creation type, so create now looks the name up first (a duplicate still fails) and then creates with create-if-missing (pulumi-labs/pulumi-modal#10).

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
