# AWS Custom Route Controller

[![REUSE status](https://api.reuse.software/badge/github.com/gardener/aws-custom-route-controller)](https://api.reuse.software/info/github.com/gardener/aws-custom-route-controller)

The AWS Custom Route Controller manages the routes to the pods via their node instances.
It watches for node creation and deletions and updates the route tables accordingly.

## Configuration

The AWS cloud controller manager needs to be running with `--configure-cloud-routes=false` to disable the standard
routes controller.

The `aws-custom-route-controller` needs to be started with several flags. All flags without default value need to be set.

```
Usage of ./aws-custom-route-controller:
      --cluster-name string             cluster name used for AWS tags
      --control-kubeconfig string       path of control plane kubeconfig or 'inClusterConfig' for in-cluster config (default "inClusterConfig")
      --health-probe-port int           port for health probes (default 8081)
      --max-delay-on-failure duration   maximum delay if communication with AWS fails (default 5m0s)
      --metrics-port int                port for metrics (default 8080)
      --namespace string                namespace of secret containing the AWS credentials on control plane
      --pod-network-cidr string         CIDR for pod network
      --region string                   AWS region
      --secret-name string              name of secret containing the AWS credentials on control plane (default "cloudprovider")
      --sync-period duration            period for syncing routes (default 1h0m0s)
      --target-kubeconfig string        path of target kubeconfig
      --tick-period duration            tick period for checking for updates (default 5s)
```

The AWS credentials are loaded from a secret using the control plane kubeconfig.
The secret needs to provide one of the following combinations:
 - the data keys `accessKeyID` and `secretAccessKey`
 - the data keys `roleARN` and `workloadIdentityTokenFile`

The AWS credentials must have permissions to describe route tables of the cluster and to create and delete routes.

## What is it good for?

The standard [routes controller of the AWS cloud provider](https://github.com/kubernetes/cloud-provider-aws/blob/master/pkg/providers/v1/aws_routes.go)
cannot deal with multi-zone clusters which have multiple routing tables. Currently, the standard controller only
supports a single route table.
