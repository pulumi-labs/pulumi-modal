---
title: Modal Installation & Configuration
meta_desc: How to install the Pulumi Modal provider and its language SDKs, and how to configure credentials.
layout: package
---

## Installation

The Modal provider is available as a package in all Pulumi-supported languages:

* JavaScript/TypeScript: [`@pulumi-labs/modal`](https://www.npmjs.com/package/@pulumi-labs/modal)
* Python: [`pulumi-labs-modal`](https://pypi.org/project/pulumi-labs-modal/)
* Go: [`github.com/pulumi-labs/pulumi-modal/sdk/go/modal`](https://github.com/pulumi-labs/pulumi-modal)
* .NET: [`Pulumi.Labs.Modal`](https://www.nuget.org/packages/Pulumi.Labs.Modal)
* Java: [`com.pulumi.labs:modal`](https://central.sonatype.com/artifact/com.pulumi.labs/modal)

In normal usage, installing one of the language SDKs above is enough. The first `pulumi up` automatically downloads the matching provider binary from the URL embedded in the SDK, so no separate step is required.

### Provider Binary (optional)

If you need to install the provider binary explicitly (for example, in an air-gapped environment or to prewarm a CI cache), use the `pulumi plugin` command:

```bash
pulumi plugin install resource modal <version> --server github://api.github.com/pulumi-labs/pulumi-modal
```

Replace `<version>` with your desired release (see the [releases page](https://github.com/pulumi-labs/pulumi-modal/releases)).

## Configuration

Create a Modal API token pair (token ID and token secret) from the [Modal dashboard](https://modal.com/settings/tokens), or by running `modal token new` with the Modal CLI. The provider needs no explicit `Provider` resource and no stack config when the token pair is available as environment variables:

```bash
export MODAL_TOKEN_ID=ak-xxxxxxxxxxxxxxxx
export MODAL_TOKEN_SECRET=as-xxxxxxxxxxxxxxxx
export MODAL_ENVIRONMENT=production   # optional
```

You can also set them as provider configuration:

```bash
pulumi config set --secret modal:tokenId ak-xxxxxxxxxxxxxxxx
pulumi config set --secret modal:tokenSecret as-xxxxxxxxxxxxxxxx
pulumi config set modal:environment production
```

If you have already authenticated the Modal CLI with `modal token new` or `modal setup`, neither is needed: when no token is set anywhere else, the provider uses the profile in `~/.modal.toml` (or the file named by `MODAL_CONFIG_PATH`), choosing the profile named by `MODAL_PROFILE`, otherwise the one marked `active = true`. The lookup order is the resource's `tokenId` / `tokenSecret`, then `modal:tokenId` / `modal:tokenSecret`, then `MODAL_TOKEN_ID` / `MODAL_TOKEN_SECRET`, then the profile file. Unlike the Modal Python SDK, which merges them field by field, the provider takes the token pair from one source only, and it applies the profile's `environment` and `server_url` only when the profile supplies the credentials.

Each resource also accepts `tokenId`, `tokenSecret` and `environment` inputs, so a single stack can provision resources under different Modal accounts or workspaces. When no environment is set, Modal uses the account's default environment.
