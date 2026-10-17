# LeptoStack Cluster Template

A template FluxCD repository for configuring and deploying [LeptoStack](https://github.com/leptostack-fluxcd/leptostack-base) on Kubernetes clusters. Fork or clone this repository to create a dedicated deployment repository per cluster, then bootstrap FluxCD from it.

## Repository Structure

The repository uses a **Kustomize base/overlay layout**. The `base` defines the default LeptoStack components and must not be modified. Per-cluster customizations live in `overlays/`, and each cluster is bootstrapped from its own directory under `clusters/`.

```
.
├── infrastructure/                      # Shared infrastructure operators (source + Kustomizations)
│   ├── kustomization.yaml               # Groups leptostack-source and leptostack-infra
│   ├── leptostack-source.yaml           # GitRepository source pointing to leptostack-base
│   └── leptostack-infra.yaml            # Infra Kustomizations (cert-manager, operators, etc.)
├── apps/
│   └── leptostack/
│       ├── base/                        # Default LeptoStack definitions — DO NOT MODIFY
│       │   ├── kustomization.yaml
│       │   ├── leptostack-config.yaml   # Default ConfigMap with placeholder values
│       │   └── leptostack-base.yaml     # All core service Kustomizations and dependency chain
│       └── overlays/
│           └── example/                 # Example overlay; copy for your own cluster
│               ├── kustomization.yaml   # namePrefix, naming config, references base
│               └── naming-conf.yaml     # Rewrites substituteFrom/dependsOn names under prefix
└── clusters/
    └── example/                         # One directory per cluster, bootstrapped by Flux
        └── kustomization.yaml           # References infrastructure + your overlay
```

## Getting Started

### 1. Create a Cluster Repository

Fork or clone this template into a new repository dedicated to your cluster:

```bash
git clone <this-template-url> my-cluster-fluxcd
cd my-cluster-fluxcd
git remote set-url origin <your-new-repo-url>
```

### 2. Create an Overlay for Your Cluster

Copy the `example` overlay to a name matching your cluster:

```bash
cp -r apps/leptostack/overlays/example apps/leptostack/overlays/<cluster-name>
```

Edit the `namePrefix` in the new overlay's `kustomization.yaml` to a value unique to your cluster:

```yaml
namePrefix: <cluster-name>-
```

> **Note:** The default values in `apps/leptostack/base/leptostack-config.yaml` are placeholders. **Do not edit the base.** Add your cluster-specific settings as patches in your overlay.

### 3. Configure Your Cluster

Add a patch to your overlay's `kustomization.yaml` that sets the cluster-specific values on the `leptostack-config` ConfigMap:

```yaml
patches:
  - target:
      kind: ConfigMap
      name: leptostack-config
    patch: |-
      - op: replace
        path: /data/cluster_name
        value: <cluster-name>
      - op: replace
        path: /data/leptostack_domain
        value: leptostack.example.com
```

| Variable | Description | Example |
|---|---|---|
| `cluster_name` | Unique name for this cluster | `production` |
| `leptostack_domain` | Base domain for all services | `leptostack.example.com` |
| `leptostack_name` | LeptoStack instance name | `portal` |
| `leptostack_acme_email` | Email for Let's Encrypt ACME account | `support@micronisus.com` |

The ConfigMap is consumed by all LeptoStack Kustomizations via `postBuild.substituteFrom`. Apply any other cluster-specific patches (e.g. MetalLB, Ingress, node selectors) in your overlay as well.

### 4. Create the Cluster Bootstrap Directory

Create a directory under `clusters/` for your cluster and a `kustomization.yaml` that references the infrastructure and your overlay:

```bash
mkdir -p clusters/<cluster-name>
```

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../infrastructure
  - ../../apps/leptostack/overlays/<cluster-name>
```

### 5. Bootstrap FluxCD

Bootstrap FluxCD pointing to your cluster repository, using your cluster's directory as the path:

```bash
flux bootstrap git \
  --url=<your-cluster-repo-url> \
  --branch=main \
  --path=clusters/<cluster-name>
```

Refer to the [FluxCD documentation](https://fluxcd.io/flux/installation/bootstrap/) for detailed bootstrap options.

### 6. Create the LeptoStack Base Secret

After bootstrapping, create a secret in the `flux-system` namespace that grants FluxCD read access to the `leptostack-base` repository. This is required by the `leptostack-base` GitRepository source defined in `infrastructure/leptostack-source.yaml`.

**Using a personal access token (HTTPS):**

```bash
flux create secret git leptostack-base \
  --namespace=flux-system \
  --url=https://github.com/leptostack-fluxcd/leptostack-base.git \
  --username=<username> \
  --password=<personal-access-token>
```

**Using an SSH deploy key:**

```bash
flux create secret git leptostack-base \
  --namespace=flux-system \
  --url=ssh://git@github.com/leptostack-fluxcd/leptostack-base.git \
  --private-key-file=<path-to-private-key>
```

> **Note:** The token or key only needs read permissions on the `leptostack-base` repository.

### 7. Apply LeptoStack Resources

Once the secret is in place, trigger a reconciliation to deploy the LeptoStack resources:

```bash
flux reconcile kustomization flux-system --with-source
```

FluxCD will reconcile all Kustomization resources and begin deploying the infrastructure and LeptoStack services in the correct dependency order. You can monitor the progress with:

```bash
flux get kustomizations --watch
```

## Deployment Order

FluxCD manages dependencies between components. The deployment follows this order:

```
Infrastructure (cert-manager → trust-manager, crossplane → crossplane-post, operators, external-secrets)
    │
    ├──► infra-config      (waits on all infra)
    └──► keycloak-crd
    │
    ▼
  base (overlay)
    │
    ├──► valkey
    ├──► apisix → apisix-post
    ├──► centrifugo        (after apisix)
    ├──► camel-k
    ├──► rustfs
    ├──► openbao → openbao-init → openbao-post
    │
    ├──► pgcluster         (after openbao-post)
    │
    ├──► keycloak-pre → keycloak → keycloak-post
    │         │
    │         ├──► supabase-pre → supabase
    │         ├──► rabbitmq → flowable-pre → flowable-rabbitmq / flowable
    │         └──► superset → superset-post
    │
    └──► (modules, if enabled)
```

## Key Files

- **`infrastructure/leptostack-source.yaml`** — Defines the `leptostack-base` GitRepository source. Update the URL if you use a fork.
- **`infrastructure/leptostack-infra.yaml`** — Deploys shared infrastructure operators (cert-manager, trust-manager, external-secrets, crossplane, operators, keycloak CRDs) from the `leptostack-base` repository. Generally does not need modification.
- **`apps/leptostack/base/leptostack-config.yaml`** — Default cluster configuration as a ConfigMap. Contains placeholder values only; do not modify. Override via overlay patches.
- **`apps/leptostack/base/leptostack-base.yaml`** — Defines all core LeptoStack service Kustomizations and their dependency chain. Do not modify.
- **`apps/leptostack/overlays/<cluster>/`** — The primary place to customize. Add your cluster-specific config patches and `namePrefix` here.
- **`clusters/<cluster>/kustomization.yaml`** — Ties infrastructure and your overlay together; this directory is the Flux bootstrap path.
