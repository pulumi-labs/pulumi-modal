# Modal Pulumi Provider

> [!NOTE]
> **Experimental.** This provider is experimental, versions are `0.x`, and its API may change without notice.

Manage [Modal](https://modal.com) — serverless GPU/CPU compute — as infrastructure with
Pulumi.

Modal does not publish a Terraform provider, so this provider talks directly to Modal's
public gRPC control-plane API using the same generated protobuf client
([`github.com/modal-labs/modal-client/go/proto/modal_proto`](https://pkg.go.dev/github.com/modal-labs/modal-client/go/proto/modal_proto))
that Modal's own official Go SDK (`libmodal`) is built on. Every RPC carries the static
`x-modal-token-id` / `x-modal-token-secret` headers that the Python SDK, the CLI, and the
server itself accept as sufficient on their own. Credentials are found the way the Modal
clients find them (environment variables, then the `~/.modal.toml` profile written by
`modal token new`); see [Authentication](#authentication) for the exact order, including
the one place this provider differs from the Python SDK.

This is the only IaC provider for Modal, built natively on `pulumi-go-provider/infer`.

## Resources

| Resource | Description |
|---|---|
| `modal:index:Secret` | A durable, named collection of key/value pairs mounted as environment variables into Modal Functions. Values are updatable in place. |
| `modal:index:Volume` | A durable, named distributed filesystem volume Modal Functions can mount for persistent storage. Name changes rename in place; environment and `fsVersion` changes force-replace (and delete the volume's data). |
| `modal:index:Queue` | A durable, named FIFO/LIFO queue for passing data between Modal Functions and external callers. Name and environment changes force-replace. |
| `modal:index:Dict` | A durable, named distributed key/value store shared between Modal Functions. Name and environment changes force-replace. |
| `modal:index:Environment` | A workspace-level Modal environment (sub-namespace). Name, web suffix and concurrency limits update in place. |
| `modal:index:App` | A named, empty Modal App (deploying code is out of scope). Tags update in place; name and environment changes force-replace. |
| `modal:index:Image` | A Modal Image built from a registry base image plus Dockerfile commands, optionally published under a stable name. Build input changes replace it; the published name updates in place. |

### Lookup functions

Existing objects, including ones created outside Pulumi, can be referenced without managing them. Lookups never create anything and fail with an error naming the object and environment when it does not exist.

| Function | Looks up |
|---|---|
| `modal:index:getSecret` | A Secret's ID and key names (never values). |
| `modal:index:getVolume` | A Volume's ID and filesystem version. |
| `modal:index:getQueue` | A Queue's ID. |
| `modal:index:getDict` | A Dict's ID. |
| `modal:index:getEnvironment` | An environment's ID, web suffix, default flag and limits. |
| `modal:index:getApp` | An App's ID and tags, including Apps created by `modal deploy`. |
| `modal:index:getImage` | A published Image's ID by `name[:tag]`. |
| `modal:index:getFunction` | A deployed Function's ID and web URL. |
| `modal:index:getCls` | A deployed Cls's ID and method names. |
| `modal:index:getProxy` | A Proxy's ID, region and static IPs. |
| `modal:index:getWorkspace` | The workspace the token belongs to, its default environment and image builder version. |

## Installation

### TypeScript / JavaScript

```bash
npm install @pulumi-labs/modal
```

### Python

```bash
pip install pulumi_labs_modal
```

### Go

```bash
go get github.com/pulumi-labs/pulumi-modal/sdk/go/modal
```

### .NET

```bash
dotnet add package Pulumi.Labs.Modal
```

### Java (Maven)

```xml
<dependency>
  <groupId>com.pulumi.labs</groupId>
  <artifactId>modal</artifactId>
  <version>[0.1,1.0)</version>
</dependency>
```

## Authentication

Create a Modal API token pair (token ID + token secret) from the
[Modal dashboard](https://modal.com/settings/tokens), or by running `modal token new`
with the Modal CLI.

Export them as environment variables, the same ones the Modal Python SDK and CLI read.
The default provider picks them up, so no explicit `Provider` resource and no stack config
is needed. Every program under [`examples/`](./examples) and [`templates/`](./templates)
runs this way:

```bash
export MODAL_TOKEN_ID=ak-xxxxxxxxxxxxxxxx
export MODAL_TOKEN_SECRET=as-xxxxxxxxxxxxxxxx
export MODAL_ENVIRONMENT=production   # optional
```

Or set them via provider config:

```bash
pulumi config set --secret modal:tokenId ak-xxxxxxxxxxxxxxxx
pulumi config set --secret modal:tokenSecret as-xxxxxxxxxxxxxxxx
```

Per-resource override is also supported via the `tokenId` / `tokenSecret` inputs on each
resource, so a single stack can provision resources under different Modal accounts or
workspaces.

If you have already run `modal token new` or `modal setup`, you need none of the above:
the provider falls back to the profile in `~/.modal.toml`.

### Credential lookup order

The first source that supplies a token wins:

1. The resource's `tokenId` / `tokenSecret` inputs.
2. The provider config `modal:tokenId` / `modal:tokenSecret`.
3. `MODAL_TOKEN_ID` / `MODAL_TOKEN_SECRET`.
4. The profile in `~/.modal.toml` (or the file named by `MODAL_CONFIG_PATH`): the profile
   named by `MODAL_PROFILE`, otherwise the one marked `active = true`, otherwise a profile
   called `default`.

Differences from the Modal Python SDK, which merges environment variables and the profile
field by field: the provider takes the token pair from one source only (a token ID from
the environment never combines with a token secret from the file; it is an error), and it
applies the profile's `environment` and `server_url` only when the profile supplies the
credentials. Use `modal:environment` / `MODAL_ENVIRONMENT` and `modal:serverAddr` to
override either.

### Environments

Modal groups resources into named "environments" (a workspace sub-namespace, distinct
from a Pulumi stack). Set a stack-wide default:

```bash
pulumi config set modal:environment production
```

or via `MODAL_ENVIRONMENT`, or override per-resource with the `environment` input.
When omitted entirely, Modal uses the account's default environment.

The environment an object was created in is recorded in state (including when you
relied on the account default). Pointing the stack at a different environment later
therefore replaces the resource instead of silently looking in the wrong place.

### Volume filesystem version

`Volume.fsVersion` selects the filesystem backend, `"v1"` or `"v2"`. When omitted the
provider creates a `v2` volume (Modal's recommended backend). Note that Modal's own SDKs
default to `v1`, so set `fsVersion: "v1"` explicitly if you need that. The effective
version is always reported as an output. Changing it replaces the volume and deletes its
data. In v0.1.0 this property was called `version`; it was renamed because
pulumi-go-provider silently drops any input named `version`
([pulumi-go-provider#704](https://github.com/pulumi/pulumi-go-provider/issues/704)),
which made every update replace the volume. State written by v0.1.0 migrates
automatically; update your programs to use `fsVersion` (`fs_version` in Python).

### Importing existing resources

Secrets, Volumes, Queues, Dicts, Environments, Apps and Images import by their Modal ID alone (`st-...`, `vo-...`, `qu-...`, `di-...`, `en-...`, `ap-...`, `im-...`):

```bash
pulumi import modal:index:Volume model-cache vo-AbCdEf123
```

Modal does not report a Volume's or Queue's environment, so the import looks in the
`modal:environment` / `MODAL_ENVIRONMENT` environment (or the account default) and fails
with a clear message if the ID does not live there (Dicts behave like Volumes and Queues; Apps and Secrets report their environment). An imported Image has no build inputs, because Modal does not store them: it adopts whatever the program declares. Modal never returns Secret values:
an imported Secret records its name, environment and key names, and the next
`pulumi up` writes the values from your program.

### Coverage

The provider covers the Modal objects that are infrastructure: Secrets, Volumes, Queues, Dicts,
Environments, Apps and Images, plus lookups for those and for deployed Functions, Classes, Proxies
and the workspace. It does not deploy code (`modal deploy` packages Python code, which Pulumi
cannot do) and has no Sandbox resource yet (Sandboxes expire after at most 24 hours, so they would
drift). [`research/coverage/modal.md`](../../../research/coverage/modal.md) classifies every Modal SDK service and
RPC group, with the reason for each decision; service users, proxy auth tokens and workspace settings are planned.

## Example

### TypeScript

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi-labs/modal";

// A Secret exposed to Modal Functions as environment variables.
const apiKeys = new modal.Secret("api-keys", {
    name: "api-keys",
    values: {
        STRIPE_API_KEY: "sk_live_xxx",
        SENDGRID_API_KEY: "SG.xxx",
    },
});

// A durable Volume Functions can mount for persistent storage.
const modelCache = new modal.Volume("model-cache", {
    name: "model-cache",
});

// A Queue for passing work between Functions.
const jobQueue = new modal.Queue("job-queue", {
    name: "job-queue",
});

export const secretId = apiKeys.id;
export const volumeId = modelCache.id;
export const queueId = jobQueue.id;
```

### Python

```python
import pulumi
import pulumi_labs_modal as modal

api_keys = modal.Secret(
    "api-keys",
    name="api-keys",
    values={
        "STRIPE_API_KEY": "sk_live_xxx",
        "SENDGRID_API_KEY": "SG.xxx",
    },
)

model_cache = modal.Volume("model-cache", name="model-cache")
job_queue = modal.Queue("job-queue", name="job-queue")

pulumi.export("secretId", api_keys.id)
pulumi.export("volumeId", model_cache.id)
pulumi.export("queueId", job_queue.id)
```

See the [`examples/`](./examples) and [`templates/`](./templates) directories for
complete, runnable programs in all 6 supported languages (TypeScript, Python, Go, C#,
Java, YAML).

## Configuration Reference

| Key | Environment Variable | Description |
|---|---|---|
| `modal:tokenId` | `MODAL_TOKEN_ID` | Modal API token ID. Required unless set per resource or available from `~/.modal.toml`. Falls back to the environment variable, then the profile file. |
| `modal:tokenSecret` (secret) | `MODAL_TOKEN_SECRET` | Modal API token secret. Required unless set per resource or available from `~/.modal.toml`. Falls back to the environment variable, then the profile file. |
| `modal:environment` | `MODAL_ENVIRONMENT` | Default Modal environment for resources that don't set their own. Falls back to the `environment` of the `~/.modal.toml` profile that supplies the credentials. |
| `modal:serverAddr` | `MODAL_SERVER_ADDR` | Override for Modal's gRPC endpoint. Falls back to the profile's `server_url`. Testing only. |
| (none) | `MODAL_CONFIG_PATH`, `MODAL_PROFILE` | Location of the profile file (default `~/.modal.toml`) and the profile to use (default: the one marked `active = true`). Read directly, not provider config. |

## Notes on the Modal API Surface

Modal's gRPC control-plane API is not formally published as a stability-guaranteed
public API in the way a typical Terraform-bridged REST API is — Modal's own tooling
(the Python SDK, the CLI, and this provider) all consume the same
`modal_proto` definitions Modal publishes as a versioned Go module
(`github.com/modal-labs/modal-client/go/proto/modal_proto`), and the wire-level
authentication contract (static token headers) has been stable across every official
SDK release. This provider pins a specific version of that generated client and will be
updated as Modal evolves the schema.
