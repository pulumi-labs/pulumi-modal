using Pulumi;
using Pulumi.Labs.Modal;
using System.Collections.Generic;

// Needs the Modal CLI on this machine (`pip install modal`): `modal deploy` does
// the packaging. Credentials: set modal:tokenId / modal:tokenSecret, or the
// MODAL_TOKEN_ID / MODAL_TOKEN_SECRET environment variables.
return await Deployment.RunAsync(() =>
{
    // Runs `modal deploy` for app.py and manages the deployed App. Editing app.py
    // (or anything beside it) redeploys in place on the next `pulumi up`.
    var api = new AppDeployment("api", new AppDeploymentArgs
    {
        Name = "deployment-example",
        AppFile = "app.py",
    });

    // A non-human identity for CI/CD. Modal returns its token secret only once;
    // Pulumi keeps it as a secret output.
    var ci = new ServiceUser("ci", new ServiceUserArgs
    {
        Name = "deployment-example-ci",
        Scoped = true,
    });

    return new Dictionary<string, object?>
    {
        ["appId"] = api.AppId,
        ["functions"] = api.Functions,
        ["webUrls"] = api.WebUrls,
        ["ciTokenId"] = ci.ServiceTokenId,
        ["ciTokenSecret"] = ci.ServiceTokenSecret,
    };
});
