<h1 align="center"> <a href="https://argo-cd.readthedocs.io/en/stable/">ArgoCD</a> Complementary Operator </h1>
<h6 align="center">Manage your ArgoCD users and project with ease and some labels!</h6>

<p align="center">
    <img alt="GitHub Workflow Status" src="https://img.shields.io/github/actions/workflow/status/snapp-incubator/argocd-complementary-operator/ci.yml?logo=github&style=for-the-badge">
    <img alt="GitHub repo size" src="https://img.shields.io/github/repo-size/snapp-incubator/argocd-complementary-operator?logo=github&style=for-the-badge">
    <img alt="GitHub tag (with filter)" src="https://img.shields.io/github/v/tag/snapp-incubator/argocd-complementary-operator?style=for-the-badge&logo=git">
    <img alt="GitHub go.mod Go version (subdirectory of monorepo)" src="https://img.shields.io/github/go-mod/go-version/snapp-incubator/argocd-complementary-operator?style=for-the-badge&logo=go">
</p>

Add `ArgocdUser` CRD to be able to create static ArgoCD user for each `ArgocdUser`.
Also, it creates [ArgoCD projects](https://argo-cd.readthedocs.io/en/stable/user-guide/projects/) based on
labels you have on the namespaces beside the users defined as `ArgocdUser`.

## How it works?

### ArgoCD Project

Each ArgoCD project in Snapp is mapped into a team. For each project you need to have destinations which are namespaces
that project can deploy resources on them, using this operator you can label these namespaces as follows:

```yaml
apiVersion: "v1"
kind: "Namespace"
metadata:
  labels:
    argocd.snappcloud.io/appproj: A.B
```

Which means ArgoCD projects A and B can deploy resources into namespace NS. Also, there are some cases in which you want
to create ArgoCD applications by submitting them from CLI instead of using UI. For these cases you can use
[`ApplicationSet`](https://argo-cd.readthedocs.io/en/stable/user-guide/application-set/) or you can use your team namespace.
By default, ArgoCD applications are created in the `user-argocd`
which is the namespace that (in Snapp) we deployed our ArgoCD. For having team namespace instead of `user-argocd` you
need to label that namespace as follows:

```yaml
apiVersion: "v1"
kind: "Namespace"
metadata:
  labels:
    argocd.snappcloud.io/appproj: A
    argocd.snappcloud.io/source: A
```

Please note that, because of the current architecture of this operator you need both of these label on the namespace.

### ArgoCD User

Here is the ArgoCD user created using operator:

```yaml
apiVersion: argocd.snappcloud.io/v1alpha1
kind: ArgocdUser
metadata:
  name: {{argocduserName}}
spec:
  admin:
    ciPass: ******
    users:
    - user1
    - user2
  view:
    ciPass: ******
    users:
    - user1
    - user2
```

### Deletion and Lifecycle Management

The operator implements deletion prevention to ensure safe removal of ArgoCD resources:

**Finalizers**: When an `ArgocdUser` is created, the operator automatically adds a finalizer (`argocd.snappcloud.io/finalizer`) to prevent accidental deletion.

**Deletion Protection**: An `ArgocdUser` cannot be deleted if any namespace still references it via the `argocd.snappcloud.io/appproj` label. This includes:

- Single-team namespaces (e.g., `argocd.snappcloud.io/appproj: team-a`)
- Multi-team namespaces (e.g., `argocd.snappcloud.io/appproj: team-a.team-b`)

**Deletion Process**:

1. When you delete an `ArgocdUser`, it enters a pending deletion state
2. The operator checks if any namespaces reference this team
3. If namespaces still reference it, deletion is blocked and the resource remains
4. Once all namespace labels are removed, the operator:
   - Cleans up RBAC policies from `argocd-rbac-cm`
   - Removes static accounts from `argocd-cm`
   - Deletes account passwords from `argocd-secret`
   - Removes the finalizer
   - Allows Kubernetes to complete the deletion

**Garbage Collection**: The operator uses OwnerReferences to enable automatic cleanup:

- When an `ArgocdUser` is deleted, its associated `AppProject` is automatically removed
- OpenShift Groups (if used) are also automatically cleaned up
- This ensures no orphaned resources remain in the cluster

### Architecture

The operator uses two separate controllers with distinct responsibilities:

**ArgocdUserReconciler**:

- Creates and manages `AppProject` resources
- Configures RBAC policies and roles in `argocd-rbac-cm`
- Creates static accounts in `argocd-cm`
- Manages account passwords in `argocd-secret`
- Creates OpenShift Groups for RBAC integration
- Sets OwnerReferences for garbage collection
- Manages finalizers for safe deletion

**NamespaceReconciler**:

- Watches namespace labels (`argocd.snappcloud.io/appproj` and `argocd.snappcloud.io/source`)
- Updates `AppProject` destinations based on namespace labels
- Updates `AppProject` source namespaces
- Supports multi-team namespaces (e.g., `team-a.team-b`)
- **Does not create** `AppProject` resources (only updates existing ones)

**Separation of Concerns**: The NamespaceReconciler only updates `AppProject` destinations and sources. It validates that the `AppProject` exists (created by ArgocdUserReconciler) before attempting updates. If an `AppProject` doesn't exist, the reconciliation fails with an error, ensuring users create the `ArgocdUser` resource first.

**Multi-Team Support**: When a namespace has a multi-team label (e.g., `argocd.snappcloud.io/appproj: team-a.team-b`), both teams' `AppProjects` will include that namespace in their destinations, enabling shared access.

## Instructions

### Development

- `make generate` update the generated code for that resource type.
- `make manifests` Generating CRD manifests.
- `make test` Run tests.

### Build

Export your image name:

```
export IMG=ghcr.io/your-repo-path/image-name:latest
```

- `make build` builds Golang app locally.
- `make docker-build` build docker image locally.
- `make docker-push` push container image to registry.

### Run, Deploy

- `make run` run app locally
- `make deploy` deploy to k8s.

### Clean up

- `make undeploy` delete resouces in k8s.

## Metrics

|                     Metric                     | Notes                                                                                                                                                                                                                                  |
| :--------------------------------------------: | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
|      `controller_runtime_active_workers`       | Number of currently used workers per controller                                                                                                                                                                                        |
| `controller_runtime_max_concurrent_reconciles` | Maximum number of concurrent reconciles per controller                                                                                                                                                                                 |
|  `controller_runtime_reconcile_errors_total`   | Total number of reconciliation errors per controller                                                                                                                                                                                   |
|  `controller_runtime_reconcile_time_seconds`   | Length of time per reconciliation per controller                                                                                                                                                                                       |
|      `controller_runtime_reconcile_total`      | Total number of reconciliations per controller                                                                                                                                                                                         |
|     `rest_client_request_latency_seconds`      | Request latency in seconds. Broken down by verb and URL.                                                                                                                                                                               |
|          `rest_client_requests_total`          | Number of HTTP requests, partitioned by status code, method, and host.                                                                                                                                                                 |
|             `workqueue_adds_total`             | Total number of adds handled by workqueue                                                                                                                                                                                              |
|               `workqueue_depth`                | Current depth of workqueue                                                                                                                                                                                                             |
| `workqueue_longest_running_processor_seconds`  | How many seconds has the longest running processor for workqueue been running.                                                                                                                                                         |
|       `workqueue_queue_duration_seconds`       | How long in seconds an item stays in workqueue before being requested                                                                                                                                                                  |
|           `workqueue_retries_total`            | Total number of retries handled by workqueue                                                                                                                                                                                           |
|      `workqueue_unfinished_work_seconds`       | How many seconds of work has been done that is in progress and hasn't been observed by `work_duration`. Large values indicate stuck threads. One can deduce the number of stuck threads by observing the rate at which this increases. |
|       `workqueue_work_duration_seconds`        | How long in seconds processing an item from workqueue takes.                                                                                                                                                                           |

## Security

### Reporting security vulnerabilities

If you find a security vulnerability or any security related issues, please DO NOT file a public issue, instead send your report privately to cloud@snapp.cab. Security reports are greatly appreciated and we will publicly thank you for it.

## License

Apache-2.0 License, see [LICENSE](LICENSE).
