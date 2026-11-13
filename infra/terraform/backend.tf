# Partial config: the account and container come from backend.hcl, written by infra/bootstrap/bootstrap.sh.
terraform {
  backend "azurerm" {
    use_cli          = true
    use_azuread_auth = true
  }
}
