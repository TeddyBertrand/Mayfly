provider "azurerm" {
  features {}

  use_cli = true

  # azurerm v5 no longer registers providers by default, and a new subscription has none registered.
  resource_providers_to_register = [
    "Microsoft.Compute",
    "Microsoft.Network",
  ]
}
