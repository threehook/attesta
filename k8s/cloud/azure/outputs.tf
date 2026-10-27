output "resource_group" {
  value = data.azurerm_resource_group.this.name
}

output "cluster" {
  value = azurerm_kubernetes_cluster.this.name
}

output "key_vault_uri" {
  value = data.azurerm_key_vault.this.vault_uri
}

output "wallet_client_id" {
  value = azurerm_user_assigned_identity.wallet.client_id
}

output "wallet_key_mode" {
  value = var.wallet_key_mode
}
