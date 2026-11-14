#!/usr/bin/env bash
# Create the long-lived resources once (decisions 014 and 017): the Terraform state backend and the Key Vault
# holding the deploy secrets, plus data roles for the signed-in user. Safe to rerun: every step checks first.
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
# Must match local.key_vault_name in infra/terraform/main.tf.
KEY_VAULT="mayfly-kv-$(tr -d '-' <<<"$SUBSCRIPTION_ID" | cut -c1-12)"

# A new subscription has most providers unregistered, and storage calls then fail with "SubscriptionNotFound".
for ns in Microsoft.Storage Microsoft.KeyVault; do
	if [[ "$(az provider show --namespace "$ns" --query registrationState -o tsv)" != "Registered" ]]; then
		log "registering provider $ns"
		az provider register --namespace "$ns" --wait
	fi
done

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

if az keyvault show --name "$KEY_VAULT" --resource-group "$RESOURCE_GROUP" --output none 2>/dev/null; then
	log "key vault $KEY_VAULT exists"
else
	log "creating key vault $KEY_VAULT"
	# RBAC instead of access policies, so reading a secret is a role assignment Terraform can grant the VM.
	az keyvault create \
		--name "$KEY_VAULT" \
		--resource-group "$RESOURCE_GROUP" \
		--location "$LOCATION" \
		--enable-rbac-authorization true \
		--retention-days 7 \
		--tags project=mayfly purpose=secrets \
		--output none
fi

VAULT_SCOPE="$(az keyvault show --name "$KEY_VAULT" --resource-group "$RESOURCE_GROUP" --query id -o tsv)"
if [[ "$(az role assignment list --assignee "$CALLER_ID" --role "Key Vault Secrets Officer" \
	--scope "$VAULT_SCOPE" --query 'length(@)' -o tsv)" -gt 0 ]]; then
	log "Key Vault Secrets Officer already granted"
else
	log "granting Key Vault Secrets Officer to the signed-in user"
	az role assignment create --assignee-object-id "$CALLER_ID" --assignee-principal-type User \
		--role "Key Vault Secrets Officer" --scope "$VAULT_SCOPE" --output none
fi

cat >"$BACKEND_FILE" <<EOF
resource_group_name  = "$RESOURCE_GROUP"
storage_account_name = "$STORAGE_ACCOUNT"
container_name       = "$CONTAINER"
key                  = "mayfly.tfstate"
EOF
log "wrote $BACKEND_FILE"

cat <<EOF

Backend and key vault ready. The role assignments can take a few minutes to apply. Then upload the secrets:

  az keyvault secret set --vault-name $KEY_VAULT --name webhook-secret --file <webhook secret file> --output none
  az keyvault secret set --vault-name $KEY_VAULT --name app-key --file <App private key .pem> --output none

and initialise Terraform:

  cd infra/terraform
  terraform init -backend-config=backend.hcl
EOF
