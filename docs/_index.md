---
title: Modal Provider
meta_desc: Manage Modal serverless compute resources (Secrets, Volumes, and Queues) with Pulumi.
layout: package
---

An experimental Pulumi provider for [Modal](https://modal.com), the serverless GPU and CPU compute platform. It manages Modal `Secret`, `Volume` and `Queue` resources as infrastructure.

## Installation

The Modal provider is available as a package in all Pulumi-supported languages:

* JavaScript/TypeScript: [`@pulumi-labs/modal`](https://www.npmjs.com/package/@pulumi-labs/modal)
* Python: [`pulumi-labs-modal`](https://pypi.org/project/pulumi-labs-modal/)
* Go: [`github.com/pulumi-labs/pulumi-modal/sdk/go/modal`](https://github.com/pulumi-labs/pulumi-modal)
* .NET: [`Pulumi.Labs.Modal`](https://www.nuget.org/packages/Pulumi.Labs.Modal)
* Java: [`com.pulumi.labs:modal`](https://central.sonatype.com/artifact/com.pulumi.labs/modal)

## Example Usage

{{< chooser language "typescript,python,yaml" >}}
{{% choosable language typescript %}}
```typescript
import * as modal from "@pulumi-labs/modal";

const queue = new modal.Queue("job-queue", {
    name: "job-queue",
});

export const queueId = queue.id;
```
{{% /choosable %}}
{{% choosable language python %}}
```python
import pulumi
import pulumi_labs_modal as modal

queue = modal.Queue("job-queue", name="job-queue")

pulumi.export("queueId", queue.id)
```
{{% /choosable %}}
{{% choosable language yaml %}}
```yaml
resources:
  job-queue:
    type: modal:index:Queue
    properties:
      name: job-queue
outputs:
  queueId: ${job-queue.id}
```
{{% /choosable %}}
{{< /chooser >}}

For runnable programs in every language, see the [`examples/`](https://github.com/pulumi-labs/pulumi-modal/tree/main/examples) directory in the provider repository.

## Configuration

The provider works with no explicit `Provider` resource. Set the Modal API token pair as environment variables and every resource picks them up. See Installation & Configuration for the other ways to configure credentials and the Modal environment.
