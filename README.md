# Updatekate
A utility the updates a Kubernetes deployment based on a Quay.io webhook. 

[![Go Report Card](https://goreportcard.com/badge/github.com/tingold/updatekate)](https://goreportcard.com/report/github.com/tingold/updatekate)

## Why
Updatekate allows a Kubernetes deployment update to be included as part of a CI/CD workflow without  
 `kubectl` needing to be installed on the build box.  This is especially useful for builds directly on Quay or 
GKE environments where auth is more problematic (`gcloud`cli etc). By providing a webhook on success it also allows
post deployment steps such as interface or load testing to be performed.  

## Workflow

Basic workflow is this:

1) Updatekate starts and pulls the following info from the ENV
    * `UK_NAMESPACE`: The k8s namespace when the target deployment lives - defaults to `default` 
    * `UK_DEPLOYMENT`: The k8s deployment to update - empty by default 
    * `UK_REPO`: The repository to allow updates from -- empty by default
    * `UK_WEBHOOK`: A webhook to invoke upon success -- empty by default
    * `UK_INFO`: Setting to false disables the `/info` endpoint which could possibly leak sensitive data - defaults to true (i.e. the endpoint is enabled by default)

2) It then listens on port container port 8888 for a Quay.io webhook post to `/webhook`.  
See docs [here](https://docs.quay.io/guides/notifications.html) for the webhooks expected format.  It also exposes
`/info` which will the deployments json to 

3) When a webhook is received it will check the version of the deployment's container image against the updated tags in 
the webhook. The code uses the [semantic versioning rules](http://semver.org/) to evaluate which versions are newer. 
For example:
    * `1.0.0` > `0.5.0`
    * `0.0.2-SNAPSHOT` < `0.0.2-SNAPSHOT.2`
    * etc... full docs on the library used found [here](https://github.com/blang/semver)
    
4) If a newer tag is found the deployment will be updated to use that image. Updatekate will poll the deployments 
status 10 time (incremental backoff) or until there is at least 1 container available.

5) After a successful deployment update, updatekate will POST a simple chunk of JSON to the webhook -- if provided.
  _todo: add json sample_

## Security 
By restricting the image updates to a single repository, updatekate essentially restricts updates to those allowed
to push to your repo. Of course, by opening listening port it does expose these system to the typical vulnerabilities (DDOS etc).
Unless needed for debugging the `/info` endpoint should also be disabled   

## Building

```
# get source
git clone https://github.com/tingold/updatekate.git
cd update kate 
# install glide for dependencies 
curl https://glide.sh/get | sh
glide install
# build it
go build 
```