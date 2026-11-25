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
  default     = "0.3.0"

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
