data "azurerm_client_config" "current" {}

locals {
  dns_label = var.dns_label != "" ? var.dns_label : "${var.name}-${substr(sha1(data.azurerm_client_config.current.subscription_id), 0, 8)}"
  # Same derivation as KEY_VAULT in infra/bootstrap/bootstrap.sh.
  key_vault_name = var.key_vault_name != "" ? var.key_vault_name : "mayfly-kv-${substr(replace(data.azurerm_client_config.current.subscription_id, "-", ""), 0, 12)}"
}

# Created by bootstrap.sh, outside this state, so destroy never deletes the secrets (decision 017).
data "azurerm_key_vault" "secrets" {
  name                = local.key_vault_name
  resource_group_name = var.key_vault_resource_group
}

resource "azurerm_resource_group" "main" {
  name     = "${var.name}-rg"
  location = var.location
  tags     = var.tags
}

resource "azurerm_virtual_network" "main" {
  name                = "${var.name}-vnet"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  address_space       = [var.vnet_cidr]
  tags                = var.tags
}

resource "azurerm_subnet" "host" {
  name                 = "host"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = [cidrsubnet(var.vnet_cidr, 8, 1)]
}

resource "azurerm_network_security_group" "host" {
  name                = "${var.name}-nsg"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags

  security_rule {
    name                       = "ssh-from-owner"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_address_prefix      = var.ssh_allowed_cidr
    source_port_range          = "*"
    destination_address_prefix = "*"
    destination_port_range     = "22"
  }

  # 80 serves only the ACME HTTP challenge and the redirect; webhooks arrive on 443 (decision 016).
  security_rule {
    name                       = "https-and-acme"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_address_prefix      = "Internet"
    source_port_range          = "*"
    destination_address_prefix = "*"
    destination_port_ranges    = ["80", "443"]
  }
}

resource "azurerm_subnet_network_security_group_association" "host" {
  subnet_id                 = azurerm_subnet.host.id
  network_security_group_id = azurerm_network_security_group.host.id
}

resource "azurerm_public_ip" "host" {
  name                = "${var.name}-ip"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  allocation_method   = "Static"
  sku                 = "Standard"
  domain_name_label   = local.dns_label
  tags                = var.tags
}

resource "azurerm_network_interface" "host" {
  name                = "${var.name}-nic"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags

  ip_configuration {
    name                          = "primary"
    subnet_id                     = azurerm_subnet.host.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.host.id
  }
}

resource "azurerm_linux_virtual_machine" "host" {
  name                            = "${var.name}-host"
  location                        = azurerm_resource_group.main.location
  resource_group_name             = azurerm_resource_group.main.name
  size                            = var.vm_size
  admin_username                  = var.admin_username
  disable_password_authentication = true
  network_interface_ids           = [azurerm_network_interface.host.id]
  custom_data = base64encode(templatefile("${path.module}/cloud-init.yaml", {
    vnet_cidr      = var.vnet_cidr
    admin_username = var.admin_username
    compose_yaml   = file("${path.module}/../../compose.yaml")
    # Gzipped so a large dashboard stays under Azure's 64 KB custom_data limit (decision 021).
    observability_files = {
      for f in fileset("${path.module}/../observability", "**") :
      f => base64gzip(file("${path.module}/../observability/${f}"))
    }
    key_vault_uri = data.azurerm_key_vault.secrets.vault_uri
    domain        = azurerm_public_ip.host.fqdn
    env = {
      COMPOSE_PROFILES         = var.cache_enabled ? "cloud,cache" : "cloud"
      MAYFLY_CACHE_URL         = var.cache_enabled ? "http://cache:3000/" : ""
      MAYFLY_VERSION           = var.mayfly_version
      MAYFLY_APP_ID            = var.app_id
      MAYFLY_RUNNER_LABELS     = var.runner_labels
      MAYFLY_WORKERS           = var.workers
      MAYFLY_WORKER_IMAGE      = var.worker_image
      MAYFLY_POOL_REPOS        = var.pool_repos
      MAYFLY_POOL_SIZE         = var.pool_size
      MAYFLY_POOL_MAX          = var.pool_max == null ? "" : tostring(var.pool_max)
      MAYFLY_POOL_IDLE_TIMEOUT = var.pool_idle_timeout
    }
  }))
  tags = var.tags

  # Lets cloud-init read the secrets from Key Vault with no stored credential.
  identity {
    type = "SystemAssigned"
  }

  admin_ssh_key {
    username   = var.admin_username
    public_key = file(pathexpand(var.ssh_public_key_path))
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "StandardSSD_LRS"
    disk_size_gb         = var.os_disk_size_gb
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }

  # Managed boot diagnostics give a serial console to debug cloud-init without SSH.
  boot_diagnostics {}
}

resource "azurerm_role_assignment" "host_secrets" {
  scope                = data.azurerm_key_vault.secrets.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_linux_virtual_machine.host.identity[0].principal_id
  principal_type       = "ServicePrincipal"
}
