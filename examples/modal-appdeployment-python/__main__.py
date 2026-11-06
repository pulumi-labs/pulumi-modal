import pulumi
import pulumi_labs_modal as modal

# Needs the Modal CLI on this machine (`pip install modal`): `modal deploy` does
# the packaging. Credentials: set modal:tokenId / modal:tokenSecret, or the
# MODAL_TOKEN_ID / MODAL_TOKEN_SECRET environment variables.

# Runs `modal deploy` for app.py and manages the deployed App. Editing app.py
# (or anything beside it) redeploys in place on the next `pulumi up`.
api = modal.AppDeployment(
    "api",
    name="deployment-example",
    app_file="app.py",
)

# A non-human identity for CI/CD. Modal returns its token secret only once;
# Pulumi keeps it as a secret output.
ci = modal.ServiceUser(
    "ci",
    name="deployment-example-ci",
    scoped=True,
)

pulumi.export("appId", api.app_id)
pulumi.export("functions", api.functions)
pulumi.export("webUrls", api.web_urls)
pulumi.export("ciTokenId", ci.service_token_id)
pulumi.export("ciTokenSecret", ci.service_token_secret)
