# Modal Pulumi Provider

Manage [Modal](https://modal.com) — serverless GPU/CPU compute — as infrastructure with
Pulumi.

Modal does not publish a Terraform provider, so this provider talks directly to Modal's
public gRPC control-plane API using the same generated protobuf client
([`github.com/modal-labs/modal-client/go/proto/modal_proto`](https://pkg.go.dev/github.com/modal-labs/modal-client/go/proto/modal_proto))
that Modal's own official Go SDK (`libmodal`) is built on. Authentication mirrors the
Modal Python SDK exactly: every RPC carries the static `x-modal-token-id` /
`x-modal-token-secret` headers that the Python SDK, the CLI, and the server itself accept
as sufficient on their own.

This is the only IaC provider for Modal, built natively on `pulumi-go-provider/infer`.

## Resources

| Resource | Description |
|---|---|
| `modal:index:Secret` | A durable, named collection of key/value pairs mounted as environment variables into Modal Functions. Values are updatable in place. |
| `modal:index:Volume` | A durable, named distributed filesystem volume Modal Functions can mount for persistent storage. Name and environment changes force-replace. |
| `modal:index:Queue` | A durable, named FIFO/LIFO queue for passing data between Modal Functions and external callers. Name and environment changes force-replace. |

## Installation

### TypeScript / JavaScript

```bash
npm install @pulumi/modal
```

### Python

```bash
pip install pulumi_modal
```

### Go

```bash
go get github.com/pulumi/pulumi-modal/sdk/go/modal
```

### .NET

```bash
dotnet add package Pulumi.Modal
```

### Java (Maven)

```xml
<dependency>
  <groupId>com.pulumi</groupId>
  <artifactId>modal</artifactId>
  <version>[0.1,1.0)</version>
</dependency>
```

## Authentication

Create a Modal API token pair (token ID + token secret) from the
[Modal dashboard](https://modal.com/settings/tokens), or by running `modal token new`
with the Modal CLI.

Set them via provider config:

```bash
pulumi config set --secret modal:tokenId ak-xxxxxxxxxxxxxxxx
pulumi config set --secret modal:tokenSecret as-xxxxxxxxxxxxxxxx
```

Or via environment variables — the same ones the Modal Python SDK and CLI read:

```bash
export MODAL_TOKEN_ID=ak-xxxxxxxxxxxxxxxx
export MODAL_TOKEN_SECRET=as-xxxxxxxxxxxxxxxx
```

Per-resource override is also supported via the `tokenId` / `tokenSecret` inputs on each
resource, so a single stack can provision resources under different Modal accounts or
workspaces.

### Environments

Modal groups resources into named "environments" (a workspace sub-namespace, distinct
from a Pulumi stack). Set a stack-wide default:

```bash
pulumi config set modal:environment production
```

or via `MODAL_ENVIRONMENT`, or override per-resource with the `environment` input.
When omitted entirely, Modal uses the account's default environment.

## Example

### TypeScript

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi/modal";

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
import pulumi_modal as modal

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
| `modal:tokenId` | `MODAL_TOKEN_ID` | Modal API token ID. Required (provider- or resource-level). |
| `modal:tokenSecret` (secret) | `MODAL_TOKEN_SECRET` | Modal API token secret. Required (provider- or resource-level). |
| `modal:environment` | `MODAL_ENVIRONMENT` | Default Modal environment for resources that don't set their own. |
| `modal:serverAddr` | `MODAL_SERVER_ADDR` | Override for Modal's gRPC endpoint. Testing only. |

## Notes on the Modal API Surface

Modal's gRPC control-plane API is not formally published as a stability-guaranteed
public API in the way a typical Terraform-bridged REST API is — Modal's own tooling
(the Python SDK, the CLI, and this provider) all consume the same
`modal_proto` definitions Modal publishes as a versioned Go module
(`github.com/modal-labs/modal-client/go/proto/modal_proto`), and the wire-level
authentication contract (static token headers) has been stable across every official
SDK release. This provider pins a specific version of that generated client and will be
updated as Modal evolves the schema.
