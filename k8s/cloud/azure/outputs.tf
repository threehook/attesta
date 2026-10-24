output "resource_group" {
  value = azurerm_resource_group.this.name
}

output "cluster" {
  value = azurerm_kubernetes_cluster.this.name
}
