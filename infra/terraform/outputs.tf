output "public_ip" {
  description = "Public IPv4 address of the host."
  value       = azurerm_public_ip.host.ip_address
}

output "fqdn" {
  description = "Stable DNS name of the host; the App webhook URL is https://<fqdn>/webhook."
  value       = azurerm_public_ip.host.fqdn
}

output "ssh" {
  description = "Command to log in to the host."
  value       = "ssh ${var.admin_username}@${azurerm_public_ip.host.fqdn}"
}
