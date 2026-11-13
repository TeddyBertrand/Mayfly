# infra

The Azure host for benchmarks and CI-heavy sprints (decisions 014 and 016). It exists only while in use: `terraform apply` builds it in minutes, `terraform destroy` removes it.

## What gets created

| Resource | Notes |
|---|---|
| `mayfly-tfstate-rg` + storage account | Created once by `bootstrap/bootstrap.sh`, never by Terraform. Entra ID access only (no shared keys), versioning and 30-day soft delete. Survives `destroy`. |
| `mayfly-rg` | Everything below; `destroy` removes it all. |
| VNet `10.42.0.0/16`, subnet `host` | Workers are blocked from the whole VNet. |
| NSG on the subnet | Inbound 22 from `ssh_allowed_cidr` only, 80 and 443 from the internet. Everything else inbound is denied. |
| Static public IP | Stable name `mayfly-<hash>.denmarkeast.cloudapp.azure.com` across rebuilds. |
| Ubuntu 24.04 VM | `Standard_B2ats_v2` (free tier, 1 GB) by default. Cloud-init installs Docker and the worker egress rules. |

## First time

Needs the Azure CLI, Terraform 1.15 or later, and `az login` on the Azure for Students subscription.

```bash
infra/bootstrap/bootstrap.sh
```

It writes `infra/terraform/backend.hcl` (gitignored). The storage role can take a few minutes to apply. Then:

```bash
cd infra/terraform
cp terraform.tfvars.example terraform.tfvars   # set ssh_allowed_cidr to your IP, e.g. "$(curl -s ifconfig.me)/32"
terraform init -backend-config=backend.hcl
```

## Each session

```bash
terraform apply                                  # about 3 minutes, then cloud-init needs 2-3 more
ssh "$(terraform output -raw ssh | cut -d' ' -f2)" cloud-init status --wait
terraform destroy                                # when done; only the state storage remains
```

For benchmarks, `terraform apply -var vm_size=Standard_B2als_v2` (4 GB). Quotas in `denmarkeast`: 10 vCPU for `Basv2`, 4 for `BS`, 6 in total.

## Worker egress

An NSG filters by VM, so it cannot tell a worker's packets from the host's, and Azure never applies it to 169.254.169.254 or 168.63.129.16. The rules live on the host instead, in `/usr/local/sbin/mayfly-egress`, run by the `mayfly-egress` systemd unit after Docker starts:

| From the `mayfly-workers` bridge to | Result |
|---|---|
| The internet | Allowed (NAT) |
| The host itself (`INPUT`) | New connections dropped |
| VNet, `10/8`, `172.16/12`, `192.168/16`, `100.64/10` | Dropped |
| `169.254.0.0/16` (instance metadata, managed identity tokens) | Dropped |
| `168.63.129.16` (Azure wire server) | Dropped |

DNS still works because Docker's embedded resolver forwards queries from the host namespace. mayfly creates the bridge on first use (`MAYFLY_WORKER_NETWORK`). The rules match it by name, so they can be in place before it exists.

Check on the host:

```bash
sudo iptables -L MAYFLY-EGRESS -n -v
docker network create -o com.docker.network.bridge.name=mayfly-workers mayfly-workers
docker run --rm --network mayfly-workers curlimages/curl -sS -m 5 https://api.github.com/zen        # works
docker run --rm --network mayfly-workers curlimages/curl -sS -m 5 -H Metadata:true \
  "http://169.254.169.254/metadata/instance?api-version=2021-02-01"                                 # times out
```

## Costs

The VM is free tier for 750 hours a month. While it exists, the public IP (about $4 a month), the disk and the boot diagnostics cost a few cents a day. The state storage account costs cents a month.
