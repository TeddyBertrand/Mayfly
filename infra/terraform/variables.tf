variable "location" {
  description = "Azure region; denmarkeast is the only allowed region with B-series quota (decision 016)."
  type        = string
  default     = "denmarkeast"
}

variable "name" {
  description = "Prefix for every resource name."
  type        = string
  default     = "mayfly"
}

variable "vm_size" {
  description = "VM size. Standard_B2ats_v2 is free tier (1 GB); use Standard_B2als_v2 (4 GB) for benchmarks."
  type        = string
  default     = "Standard_B2ats_v2"
}

variable "os_disk_size_gb" {
  description = "OS disk size; worker images and job workspaces live here."
  type        = number
  default     = 32
}

variable "admin_username" {
  description = "Login user created on the VM."
  type        = string
  default     = "mayfly"
}

variable "ssh_public_key_path" {
  description = "Path to the public SSH key allowed to log in."
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "ssh_allowed_cidr" {
  description = "Only source allowed on port 22, e.g. \"203.0.113.7/32\"."
  type        = string

  validation {
    condition     = can(cidrhost(var.ssh_allowed_cidr, 0)) && var.ssh_allowed_cidr != "0.0.0.0/0"
    error_message = "ssh_allowed_cidr must be a CIDR block narrower than 0.0.0.0/0."
  }
}

variable "dns_label" {
  description = "DNS label of the public IP (<label>.<region>.cloudapp.azure.com); empty derives a stable one."
  type        = string
  default     = ""
}

variable "vnet_cidr" {
  description = "Address space of the VNet; workers are blocked from it."
  type        = string
  default     = "10.42.0.0/16"
}

variable "app_id" {
  description = "GitHub App ID (not a secret)."
  type        = string
}

variable "mayfly_version" {
  description = "Release of ghcr.io/teddybertrand/mayfly to run; pinned so every rebuild runs the same code."
  type        = string
  default     = "0.3.9"

  validation {
    condition     = can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+$", var.mayfly_version))
    error_message = "mayfly_version must be a release version such as 0.2.0, not dev or latest."
  }
}

variable "runner_labels" {
  description = "Comma-separated runner labels mayfly serves."
  type        = string
  default     = "mayfly"
}

variable "workers" {
  description = "Maximum concurrent workers (MAYFLY_WORKERS); keep it low on the 1 GB size."
  type        = number
  default     = 1
}

variable "worker_image" {
  description = "Worker image (MAYFLY_WORKER_IMAGE); empty uses the one matching mayfly_version (decision 024)."
  type        = string
  default     = ""

  validation {
    # The value lands in the host's .env, where spaces, "$" and "#" would change its meaning.
    condition     = can(regex("^[A-Za-z0-9._/:@-]*$", var.worker_image))
    error_message = "worker_image must be an image reference such as ghcr.io/actions/actions-runner:2.338.2."
  }
}

variable "pool_repos" {
  description = "Comma-separated owner/name repositories that keep warm runners (MAYFLY_POOL_REPOS); empty disables the pool."
  type        = string
  default     = ""

  validation {
    condition     = can(regex("^([A-Za-z0-9._-]+/[A-Za-z0-9._-]+(,[A-Za-z0-9._-]+/[A-Za-z0-9._-]+)*)?$", var.pool_repos))
    error_message = "pool_repos must be empty or owner/name entries separated by commas, without spaces."
  }
}

variable "pool_size" {
  description = "Idle warm runners per pooled repository (MAYFLY_POOL_SIZE)."
  type        = number
  default     = 1

  validation {
    condition     = var.pool_size >= 1 && floor(var.pool_size) == var.pool_size
    error_message = "pool_size must be a whole number of at least 1."
  }
}

variable "pool_max" {
  description = "Cap on warm runners alive per pooled repository (MAYFLY_POOL_MAX); null uses mayfly's default, twice pool_size."
  type        = number
  default     = null

  validation {
    condition     = var.pool_max == null ? true : var.pool_max >= var.pool_size && floor(var.pool_max) == var.pool_max
    error_message = "pool_max must be null or a whole number of at least pool_size."
  }
}

variable "pool_idle_timeout" {
  description = "How long a warm runner may idle before it is replaced (MAYFLY_POOL_IDLE_TIMEOUT), such as 30m."
  type        = string
  default     = "30m"

  validation {
    condition     = can(regex("^([0-9]+(h|m|s))+$", var.pool_idle_timeout))
    error_message = "pool_idle_timeout must be a duration such as 30m or 1h30m."
  }
}

variable "key_vault_name" {
  description = "Key Vault holding webhook-secret and app-key; empty uses the name bootstrap.sh derives."
  type        = string
  default     = ""
}

variable "key_vault_resource_group" {
  description = "Resource group of the Key Vault, created by bootstrap.sh."
  type        = string
  default     = "mayfly-tfstate-rg"
}

variable "tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default = {
    project    = "mayfly"
    managed_by = "terraform"
  }
}
