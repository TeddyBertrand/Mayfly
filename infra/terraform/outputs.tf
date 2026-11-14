output "public_ip" {
  description = "Public IPv4 address of the host."
  value       = azurerm_public_ip.host.ip_address
}

output "fqdn" {
  description = "Stable DNS name of the host; the App webhook URL is https://<fqdn>/webhook."
  value       = azurerm_public_ip.host.fqdn
}

output "webhook_url" {
  description = "Value for the GitHub App webhook URL; stable across rebuilds."
  value       = "https://${azurerm_public_ip.host.fqdn}/webhook"
}

output "ssh" {
  description = "Command to log in to the host."
  value       = "ssh ${var.admin_username}@${azurerm_public_ip.host.fqdn}"
}
