import modal

# The Modal App Pulumi deploys. `modal deploy` turns this file into a deployed
# App with one web endpoint.
app = modal.App("deployment-example")

image = modal.Image.debian_slim().pip_install("fastapi[standard]")


@app.function(image=image)
@modal.fastapi_endpoint()
def hello() -> dict:
    return {"message": "hello from a Pulumi-managed Modal deployment"}
