# LeptoStack Cluster Template

A template FluxCD repository for configuring and deploying [LeptoStack](https://github.com/leptostack-fluxcd/leptostack-base) on Kubernetes clusters. Fork or clone this repository to create a dedicated deployment repository per cluster, then bootstrap FluxCD from it.

## Repository Structure

The repository uses a **Kustomize base/overlay layout**. The `base` defines the default LeptoStack components and must not be modified. Per-cluster customizations live in `overlays/`, and each cluster is bootstrapped from its own directory under `clusters/`.

```
.
├── infrastructure/                      # Shared infrastructure operators (source + Kustomizations)
│   ├── kustomization.yaml               # Groups leptostack-source and leptostack/
│   ├── leptostack-source.yaml           # GitRepository source pointing to leptostack-base
│   └── leptostack/                      # Infra Kustomizations (cert-manager, operators, etc.)
│       ├── kustomization.yaml
│       ├── infra-config.yaml            # Internal CA bundle, image catalogs, Helm repositories
│       ├── infra-*.yaml                 # Dedicated infra-segment services (see below)
│       └── keycloak-crd.yaml            # Cluster-wide Keycloak operator RBAC + CRDs
├── apps/
│   └── leptostack/
│       ├── base/                        # Default LeptoStack definitions — DO NOT MODIFY
│       │   ├── kustomization.yaml
│       │   ├── leptostack-config.yaml   # Default ConfigMap with placeholder values
│       │   └── *.yaml                   # Core service Kustomizations and dependency chain
│       └── overlays/
│           └── example/                 # Example overlay; copy for your own cluster
│               ├── kustomization.yaml   # namePrefix, naming config, references base
│               └── naming-conf.yaml     # Rewrites substituteFrom/dependsOn names under prefix
└── clusters/
    └── example/                         # One directory per cluster, bootstrapped by Flux
        ├── kustomization.yaml           # References infrastructure + your overlay
        └── naming-conf.yaml             # Rewrites infra substituteFrom names to the cluster prefix
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

configurations:
  - naming-conf.yaml

resources:
  - ../../infrastructure
  - ../../apps/leptostack/overlays/<cluster-name>
```

The cluster-level `naming-conf.yaml` rewrites the infra-segment Kustomizations'
`substituteFrom` references to your overlay's prefixed `leptostack-config` ConfigMap.

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
    ├──► keycloak-crd
    │
    ├──► infra-apisix → infra-apisix-post
    ├──► infra-apisix-internal → infra-apisix-internal-post
    ├──► infra-openbao → infra-openbao-init → infra-openbao-post
    ├──► infra-pgcluster-pre (after infra-openbao-init) → infra-pgcluster (after infra-openbao-post)
    ├──► infra-keycloak-pre (after infra-pgcluster, keycloak-crd)
    │         → infra-keycloak (after infra-apisix-post) → infra-keycloak-post
    ├──► infra-openbao-oidc-client (after infra-openbao-post, infra-keycloak-post)
    │         → infra-openbao-oidc
    └──► infra-rustfs-pre (after infra-keycloak-post, infra-openbao-post)
              → infra-rustfs (after infra-apisix-post, infra-apisix-internal-post)
    │
    ▼
  base (overlay)
    │
    ├──► valkey
    ├──► apisix → apisix-post
    ├──► centrifugo        (after apisix)
    ├──► camel-k
    ├──► rustfs            (after rustfs-pre → keycloak-post, openbao-post)
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

## Infrastructure Segment

The template also deploys a second, infrastructure-owned copy of PostgreSQL, OpenBao, Keycloak and RustFS. It lives in `infrastructure/leptostack/infra-*.yaml`, reuses the `leptostack-base` manifests with a static `postBuild.substitute` (`cluster_name: infra`), and is intended for platform-level services that must not share the application segment's credentials, database or object storage. The cluster-level `naming-conf.yaml` points their `substituteFrom` at the overlay's prefixed `leptostack-config` ConfigMap so they still receive `leptostack_domain`.

| Instance | Namespace | Flux Kustomizations | Hostnames |
|---|---|---|---|
| Keycloak | `infra-keycloak` | `infra-keycloak-pre`, `infra-keycloak`, `infra-keycloak-post` | `infra-sso.<domain>`, `infra-sso-admin.<domain>` |
| PostgreSQL | `infra-pgcluster` | `infra-pgcluster-pre`, `infra-pgcluster` | `infra-pgpooler.infra-pgcluster.svc.cluster.local` |
| OpenBao | `infra-openbao` | `infra-openbao`, `infra-openbao-init`, `infra-openbao-post`, `infra-openbao-oidc-client`, `infra-openbao-oidc` | `infra-openbao-openbao.infra-openbao.svc.cluster.local:8200` |
| RustFS | `infra-rustfs` | `infra-rustfs-pre`, `infra-rustfs` | `infra-objectstore.<domain>`, `infra-rustfs.api.infra.internal` |
| APISIX (public) | `ingress-infra` | `infra-apisix`, `infra-apisix-post` | `infra-sso.<domain>`, `infra-sso-admin.<domain>`, `infra-objectstore.<domain>` |
| APISIX (internal) | `ingress-infra-internal` | `infra-apisix-internal`, `infra-apisix-internal-post` | `*.api.infra.internal` |

Notes:

- The infra segment has its own APISIX gateways, ingress classes (`infra-apisix`, `infra-internal-apisix`) and TLS certificates. The public infra gateway is a separate `LoadBalancer` and therefore receives its own IP; the wildcard DNS record that points `<domain>` at the application APISIX does **not** reach it. Add explicit DNS records for `infra-sso.<domain>`, `infra-sso-admin.<domain>` and `infra-objectstore.<domain>` pointing at the infra gateway's external IP.
- `*.api.infra.internal` is resolved to the infra internal gateway. On minikube the CLI adds the matching CoreDNS rewrite to `infrastructure/cluster-config.yaml`; clusters managed elsewhere need an equivalent rewrite (or a wildcard DNS entry) for in-cluster S3 access.
- The CLI `port-forward` catalog covers the application segment only. Reach the infra instances with `kubectl port-forward`, for example `kubectl -n infra-openbao port-forward svc/infra-openbao-openbao 8200:8200`, `kubectl -n infra-pgcluster port-forward svc/infra-pgcluster-rw 5432:5432`, and `kubectl -n infra-rustfs port-forward svc/rustfs-svc 9000:9000`.
- `infra-rustfs` intentionally does **not** link `./leptostack/rustfs/post`: that path creates the cluster-wide `flux-system/fluxcd-s3` secret and `Bucket/fluxcd` source, which the application RustFS instance already owns.
- The infra Keycloak realm is named `infra` and its realm administrator is `infra-admin`; the bootstrap admin credentials come from the `keycloak-admin` secret in `infra-keycloak`. Retrieve the `infra-admin` password from its secret in `infra-keycloak` or from OpenBao.
- The infra realm reuses the application greenmail SMTP host (`smtp.greenmail.svc.cluster.local`). Greenmail's network policy only admits `leptostack-<cluster>` namespaces, so add `leptostack-infra` to that policy if infra Keycloak email delivery is required.
- The static `infra` prefix must not collide with the cluster name itself. If a cluster is literally named `infra`, change the substitution prefix in `infrastructure/leptostack/infra-*.yaml` consistently.
- Existing cluster repositories refreshed with `sync flux` receive the new `infrastructure/leptostack/infra-*.yaml` files automatically. The matching `clusters/<cluster>/kustomization.yaml` changes (`configurations: naming-conf.yaml` plus the `infra-keycloak` operator subject and the `leptostack-infra` CA-bundle selector) and the `clusters/<cluster>/naming-conf.yaml` file are not refreshed by `sync flux` and must be added manually. Without the name reference, the infra Kustomizations cannot resolve the cluster-prefixed `leptostack-config` ConfigMap.

## Key Files

- **`infrastructure/leptostack-source.yaml`** — Defines the `leptostack-base` GitRepository source. Update the URL if you use a fork.
- **`infrastructure/leptostack/`** — Deploys shared infrastructure operators (cert-manager, trust-manager, external-secrets, crossplane, operators, keycloak CRDs) and the dedicated `infra-*.yaml` infrastructure segment from the `leptostack-base` repository. Generally does not need modification.
- **`apps/leptostack/base/leptostack-config.yaml`** — Default cluster configuration as a ConfigMap. Contains placeholder values only; do not modify. Override via overlay patches.
- **`apps/leptostack/base/*.yaml`** — Define all core LeptoStack service Kustomizations and their dependency chain. Do not modify.
- **`apps/leptostack/overlays/<cluster>/`** — The primary place to customize. Add your cluster-specific config patches and `namePrefix` here.
- **`clusters/<cluster>/naming-conf.yaml`** — Cluster-level name references that point the infra Kustomizations at the overlay's prefixed `leptostack-config` ConfigMap.
- **`clusters/<cluster>/kustomization.yaml`** — Ties infrastructure and your overlay together; this directory is the Flux bootstrap path. It also adds the `infra-keycloak` operator ServiceAccount to the `keycloak-crd` ClusterRoleBinding, includes `leptostack-infra` in the internal CA bundle namespace selector, and loads `naming-conf.yaml`.
