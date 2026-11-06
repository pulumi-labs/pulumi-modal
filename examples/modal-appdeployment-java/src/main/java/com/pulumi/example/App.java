package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.labs.modal.AppDeployment;
import com.pulumi.labs.modal.AppDeploymentArgs;
import com.pulumi.labs.modal.ServiceUser;
import com.pulumi.labs.modal.ServiceUserArgs;

// Needs the Modal CLI on this machine (`pip install modal`): `modal deploy` does
// the packaging. Credentials: set modal:tokenId / modal:tokenSecret, or the
// MODAL_TOKEN_ID / MODAL_TOKEN_SECRET environment variables.
public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            // Runs `modal deploy` for app.py and manages the deployed App. Editing app.py
            // (or anything beside it) redeploys in place on the next `pulumi up`.
            var api = new AppDeployment("api",
                AppDeploymentArgs.builder()
                    .name("deployment-example")
                    .appFile("app.py")
                    .build());

            // A non-human identity for CI/CD. Modal returns its token secret only once;
            // Pulumi keeps it as a secret output.
            var ci = new ServiceUser("ci",
                ServiceUserArgs.builder()
                    .name("deployment-example-ci")
                    .scoped(true)
                    .build());

            ctx.export("appId", api.appId());
            ctx.export("functions", api.functions());
            ctx.export("webUrls", api.webUrls());
            ctx.export("ciTokenId", ci.serviceTokenId());
            ctx.export("ciTokenSecret", ci.serviceTokenSecret());
        });
    }
}
