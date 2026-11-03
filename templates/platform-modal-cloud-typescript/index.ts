import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi-labs/modal";

const config = new pulumi.Config();

const environmentName = config.get("environmentName") ?? "platform";
const appName = config.get("appName") ?? "platform-app";
const dictName = config.get("dictName") ?? "platform-state";
const imageName = config.get("imageName") ?? "platform-base";

// An environment is an isolated namespace inside your Modal workspace.
// Everything below lives in it, and it caps concurrent tasks at 20.
const env = new modal.Environment("env", {
    name: environmentName,
    maxConcurrentTasks: 20,
});

// An App groups Functions and other Modal objects. Referencing env.name
// makes the dependency on the environment implicit.
const app = new modal.App("app", {
    name: appName,
    environment: env.name,
    tags: {
        team: "ml",
        managedBy: "pulumi",
    },
});

// A Dict is a durable key/value store shared between Functions.
const state = new modal.Dict("state", {
    name: dictName,
    environment: env.name,
});

// A published Image built from a base image plus extra Dockerfile commands,
// so every Function can start from the same pinned runtime.
const base = new modal.Image("base", {
    baseImage: "python:3.12-slim",
    dockerfileCommands: ["RUN pip install numpy==2.1.3"],
    name: imageName,
    environment: env.name,
});

// Look up the workspace the provider token belongs to, then read back its
// default environment.
const workspace = modal.getWorkspaceOutput();
const defaultEnv = modal.getEnvironmentOutput({
    name: workspace.defaultEnvironment,
});

export const environmentId = env.environmentId;
export const appId = app.appId;
export const dictId = state.dictId;
export const imageId = base.imageId;
export const workspaceName = workspace.workspaceName;
export const defaultEnvironmentWebSuffix = defaultEnv.webSuffix;
