import * as modal from "@pulumi-labs/modal";

// An environment is an isolated namespace inside your Modal workspace.
// Everything below lives in it, and it caps concurrent tasks at 20.
const env = new modal.Environment("env", {
    name: "platform",
    maxConcurrentTasks: 20,
});

// An App groups Functions and other Modal objects. Referencing env.name
// makes the dependency on the environment implicit.
const app = new modal.App("app", {
    name: "platform-app",
    environment: env.name,
    tags: {
        team: "ml",
        managedBy: "pulumi",
    },
});

// A Dict is a durable key/value store shared between Functions.
const state = new modal.Dict("state", {
    name: "platform-state",
    environment: env.name,
});

// A published Image built from a base image plus extra Dockerfile commands,
// so every Function can start from the same pinned runtime.
const base = new modal.Image("base", {
    baseImage: "python:3.12-slim",
    dockerfileCommands: ["RUN pip install numpy==2.1.3"],
    name: "platform-base",
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
