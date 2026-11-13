#!/usr/bin/env bash
# Create the Terraform state backend once (decision 014): resource group, storage account, container and
# the data role for the signed-in user. Safe to rerun: every step checks before creating.
set -euo pipefail

cd "$(dirname "$0")/.."

LOCATION="denmarkeast"
RESOURCE_GROUP="mayfly-tfstate-rg"
CONTAINER="tfstate"
SOFT_DELETE_DAYS=30
BACKEND_FILE="terraform/backend.hcl"

# log prints a progress line.
log() { printf '==> %s\n' "$*"; }

# die prints an error to stderr and exits.
die() {
	printf 'bootstrap.sh: %s\n' "$*" >&2
	exit 1
}

az account show --output none 2>/dev/null || die "not logged in, run 'az login' first"
SUBSCRIPTION_ID="$(az account show --query id -o tsv)"
log "subscription $(az account show --query name -o tsv)"

# Storage account names are global, 3-24 lowercase alphanumerics: derive a stable one from the subscription.
STORAGE_ACCOUNT="mayflytf$(tr -d '-' <<<"$SUBSCRIPTION_ID" | cut -c1-12)"

# A new subscription has most providers unregistered, and storage calls then fail with "SubscriptionNotFound".
if [[ "$(az provider show --namespace Microsoft.Storage --query registrationState -o tsv)" != "Registered" ]]; then
	log "registering provider Microsoft.Storage"
	az provider register --namespace Microsoft.Storage --wait
fi

if [[ "$(az group exists --name "$RESOURCE_GROUP")" == "true" ]]; then
	log "resource group $RESOURCE_GROUP exists"
else
	log "creating resource group $RESOURCE_GROUP"
	az group create --name "$RESOURCE_GROUP" --location "$LOCATION" --tags project=mayfly purpose=tfstate --output none
fi

if az storage account show --name "$STORAGE_ACCOUNT" --resource-group "$RESOURCE_GROUP" --output none 2>/dev/null; then
	log "storage account $STORAGE_ACCOUNT exists"
else
	log "creating storage account $STORAGE_ACCOUNT"
	# No shared keys: access goes through Entra ID roles only, so there is no account key to leak.
	az storage account create \
		--name "$STORAGE_ACCOUNT" \
		--resource-group "$RESOURCE_GROUP" \
		--location "$LOCATION" \
		--sku Standard_LRS \
		--kind StorageV2 \
		--min-tls-version TLS1_2 \
		--allow-blob-public-access false \
		--allow-shared-key-access false \
		--output none
fi

log "enabling versioning and soft delete"
az storage account blob-service-properties update \
	--account-name "$STORAGE_ACCOUNT" \
	--resource-group "$RESOURCE_GROUP" \
	--enable-versioning true \
	--enable-delete-retention true --delete-retention-days "$SOFT_DELETE_DAYS" \
	--enable-container-delete-retention true --container-delete-retention-days "$SOFT_DELETE_DAYS" \
	--output none

# container-rm goes through the management plane, so it works before the data role has propagated.
exists="$(az storage container-rm exists --storage-account "$STORAGE_ACCOUNT" \
	--resource-group "$RESOURCE_GROUP" --name "$CONTAINER" -o tsv)"
# This command prints "True", unlike `az group exists`.
if [[ "${exists,,}" == "true" ]]; then
	log "container $CONTAINER exists"
else
	log "creating container $CONTAINER"
	az storage container-rm create --storage-account "$STORAGE_ACCOUNT" \
		--resource-group "$RESOURCE_GROUP" --name "$CONTAINER" --public-access off --output none
fi

STORAGE_SCOPE="$(az storage account show --name "$STORAGE_ACCOUNT" --resource-group "$RESOURCE_GROUP" --query id -o tsv)"
CALLER_ID="$(az ad signed-in-user show --query id -o tsv)"
if [[ "$(az role assignment list --assignee "$CALLER_ID" --role "Storage Blob Data Contributor" \
	--scope "$STORAGE_SCOPE" --query 'length(@)' -o tsv)" -gt 0 ]]; then
	log "Storage Blob Data Contributor already granted"
else
	log "granting Storage Blob Data Contributor to the signed-in user"
	az role assignment create --assignee-object-id "$CALLER_ID" --assignee-principal-type User \
		--role "Storage Blob Data Contributor" --scope "$STORAGE_SCOPE" --output none
fi

cat >"$BACKEND_FILE" <<EOF
resource_group_name  = "$RESOURCE_GROUP"
storage_account_name = "$STORAGE_ACCOUNT"
container_name       = "$CONTAINER"
key                  = "mayfly.tfstate"
EOF
log "wrote $BACKEND_FILE"

cat <<EOF

Backend ready. The role assignment can take a few minutes to apply. Then:

  cd infra/terraform
  terraform init -backend-config=backend.hcl
EOF
