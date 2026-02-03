<h1 align="center"> <a href="https://argo-cd.readthedocs.io/en/stable/">ArgoCD</a> Complementary Operator </h1>
<h6 align="center">Manage your ArgoCD users and projects with ease using labels!</h6>

<p align="center">
    <img alt="GitHub Workflow Status" src="https://img.shields.io/github/actions/workflow/status/snapp-incubator/argocd-complementary-operator/ci.yml?logo=github&style=for-the-badge">
    <img alt="GitHub repo size" src="https://img.shields.io/github/repo-size/snapp-incubator/argocd-complementary-operator?logo=github&style=for-the-badge">
    <img alt="GitHub tag (with filter)" src="https://img.shields.io/github/v/tag/snapp-incubator/argocd-complementary-operator?style=for-the-badge&logo=git">
    <img alt="GitHub go.mod Go version (subdirectory of monorepo)" src="https://img.shields.io/github/go-mod/go-version/snapp-incubator/argocd-complementary-operator?style=for-the-badge&logo=go">
</p>

Add `ArgocdUser` CRD to be able to create a static ArgoCD user for each `ArgocdUser`.
Also, it creates [ArgoCD projects](https://argo-cd.readthedocs.io/en/stable/user-guide/projects/) based on labels you have on the namespaces beside the users defined as `ArgocdUser`.

## Overview

This operator extends ArgoCD functionality by providing:

- **ArgocdUser CRD** - Create static ArgoCD users with admin/view roles and CI credentials
- **Automatic AppProject Management** - Projects are automatically created and configured based on ArgocdUser resources
- **Namespace-based Configuration** - Use labels on namespaces to define project destinations and application sources
- **RBAC Integration** - Automatic ClusterRole and ClusterRoleBinding creation for user access control
- **OpenShift Group Support** - Automatic Group management on OpenShift clusters (skipped on vanilla Kubernetes)
- **Cleanup on Deletion** - Finalizer-based cleanup ensures all related resources are removed when an ArgocdUser is deleted

## Architecture

The operator consists of two controllers:

### ArgocdUser Controller

Watches `ArgocdUser` resources and manages:

| Resource | Description |
|----------|-------------|
| **AppProject** | ArgoCD project with roles, policies, destinations, and source repos |
| **ConfigMap** (`argocd-cm`) | Static user account entries |
| **Secret** (`argocd-secret`) | Hashed passwords for CI users |
| **ClusterRole** | RBAC rules allowing users to view/edit their ArgocdUser |
| **ClusterRoleBinding** | Binds admin users to the ClusterRole |
| **Group** (OpenShift only) | OpenShift groups for admin and view users |

### Namespace Controller

Watches Namespaces and updates AppProject configurations:

| Label | Description |
|-------|-------------|
| `argocd.snappcloud.io/appproj` | Adds namespace as a deployment destination for the specified project(s) |
| `argocd.snappcloud.io/source` | Allows ArgoCD Applications to be created in this namespace for the project |

## How it Works

### ArgoCD Projects

Each ArgoCD project maps to a team. Label namespaces to configure project destinations:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-app-namespace
  labels:
    argocd.snappcloud.io/appproj: team-a.team-b  # Projects team-a and team-b can deploy here
```

Which means ArgoCD projects `team-a` and `team-b` can deploy resources into namespace `my-app-namespace`. Also, there are some cases in which you want to create ArgoCD applications by submitting them from the CLI instead of using the UI. For these cases, you can use [`ApplicationSet`](https://argo-cd.readthedocs.io/en/stable/user-guide/application-set/), or you can use your team namespace.

To allow creating ArgoCD Applications in a namespace (instead of `user-argocd`):

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-a-apps
  labels:
    argocd.snappcloud.io/appproj: team-a
    argocd.snappcloud.io/source: team-a  # Applications can be created here
```

### ArgoCD Users

Create an `ArgocdUser` to set up a team with admin and view roles:

```yaml
apiVersion: argocd.snappcloud.io/v1alpha1
kind: ArgocdUser
metadata:
  name: team-a
spec:
  admin:
    ciPass: "secure-ci-password-for-admin"
    users:
      - admin@example.com
      - lead@example.com
  view:
    ciPass: "secure-ci-password-for-view"
    users:
      - dev1@example.com
      - dev2@example.com
```

This creates:

1. **AppProject** `team-a` with:
   - Admin role: full access to applications, repositories, and exec
   - View role: read-only access to applications, repositories, and logs
   - Destinations from labeled namespaces
   - Source namespaces from labeled namespaces

2. **Static users** in ArgoCD:
   - `team-a-admin-ci` - for CI/CD pipelines with admin access
   - `team-a-view-ci` - for CI/CD pipelines with view access

3. **RBAC resources**:
   - ClusterRole allowing access to the ArgocdUser resource
   - ClusterRoleBinding for admin users

4. **OpenShift Groups** (if running on OpenShift):
   - `team-a-admin` - contains admin users
   - `team-a-view` - contains view users

### Cleanup

When an `ArgocdUser` is deleted, the operator automatically cleans up:

- The associated AppProject
- ConfigMap entries for static users
- Secret entries for passwords
- ClusterRole and ClusterRoleBinding (via OwnerReferences)
- ~~OpenShift Groups~~ (Currently Disabled)

## Configuration

### Environment Variables

| Variable | Description |
|----------|-------------|
| `PUBLIC_REPOS` | Comma-separated list of public repositories available to all projects |
| `CLUSTER_ADMIN_TEAMS` | Comma-separated list of teams with cluster-admin privileges |

## Instructions

### Development

```bash
make generate    # Update generated code
make manifests   # Generate CRD manifests
make test        # Run tests
make lint        # Run linter
```

### Build

```bash
export IMG=ghcr.io/your-repo/argocd-complementary-operator:latest

make build         # Build binary locally
make docker-build  # Build Docker image
make docker-push   # Push to registry
```

### Deploy

```bash
make run      # Run locally (for development)
make install  # Install CRDs
make deploy   # Deploy to Kubernetes
```

### Clean up

```bash
make undeploy # delete resouces in k8s.
```

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

### Reporting Security Vulnerabilities

If you find a security vulnerability or any security related issues, please DO NOT file a public issue. Instead, send your report privately to <cloud@snapp.cab>. Security reports are greatly appreciated and we will publicly thank you for it.

## License

Apache-2.0 License, see [LICENSE](LICENSE).
