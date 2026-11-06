import * as modal from "@pulumi-labs/modal";

// Needs the Modal CLI on this machine (`pip install modal`): `modal deploy` does
// the packaging. Credentials: set modal:tokenId / modal:tokenSecret, or the
// MODAL_TOKEN_ID / MODAL_TOKEN_SECRET environment variables.

// Runs `modal deploy` for app.py and manages the deployed App. Editing app.py
// (or anything beside it) redeploys in place on the next `pulumi up`.
const api = new modal.AppDeployment("api", {
    name: "deployment-example",
    appFile: "app.py",
});

// A non-human identity for CI/CD. Modal returns its token secret only once;
// Pulumi keeps it as a secret output.
const ci = new modal.ServiceUser("ci", {
    name: "deployment-example-ci",
    scoped: true,
});

export const appId = api.appId;
export const functions = api.functions;
export const webUrls = api.webUrls;
export const ciTokenId = ci.serviceTokenId;
export const ciTokenSecret = ci.serviceTokenSecret;
