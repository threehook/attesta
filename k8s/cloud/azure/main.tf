locals {
  name = "${var.environment}-${var.application}"
}

# The group, VNet, subnet and vault are made by hand and kept (README), so the daily destroy leaves them; data sources find them.
# resource "azurerm_resource_group" "this" {
#   name     = "rg-${local.name}"
#   location = var.location
# }

# resource "azurerm_virtual_network" "this" {
#   name                = "vnet-${local.name}"
#   location            = azurerm_resource_group.this.location
#   resource_group_name = azurerm_resource_group.this.name
#   address_space       = ["10.20.0.0/16"]
# }

# resource "azurerm_subnet" "aks" {
#   name                 = "snet-aks"
#   resource_group_name  = azurerm_resource_group.this.name
#   virtual_network_name = azurerm_virtual_network.this.name
#   address_prefixes     = ["10.20.0.0/22"]
#   service_endpoints    = ["Microsoft.KeyVault"]
# }

data "azurerm_resource_group" "this" {
  name = "rg-${local.name}"
}

data "azurerm_subnet" "aks" {
  name                 = "snet-aks"
  resource_group_name  = data.azurerm_resource_group.this.name
  virtual_network_name = "vnet-${local.name}"
}

resource "azurerm_kubernetes_cluster" "this" {
  name                    = "aks-${local.name}"
  location                = data.azurerm_resource_group.this.location
  resource_group_name     = data.azurerm_resource_group.this.name
  dns_prefix              = local.name
  private_cluster_enabled = false
  sku_tier                = "Free"

  # Pods sign in to Azure with a token of their service account (Workload Identity): no secret in the cluster.
  oidc_issuer_enabled       = true
  workload_identity_enabled = true

  # Mounts vault secrets into pods through the Secrets Store CSI driver; the pod's own identity reads them (see keyvault.tf).
  key_vault_secrets_provider {
    secret_rotation_enabled = false
  }

  api_server_access_profile {
    authorized_ip_ranges = var.api_allowed_cidrs
  }

  default_node_pool {
    name            = "system"
    node_count      = var.node_count
    vm_size         = var.node_size
    os_disk_size_gb = 32
    vnet_subnet_id  = data.azurerm_subnet.aks.id
  }

  identity {
    type = "SystemAssigned"
  }

  network_profile {
    network_plugin = "azure"
    service_cidr   = "10.21.0.0/16"
    dns_service_ip = "10.21.0.10"
    # Nodes get no public address; pods leave through the load balancer's outbound IP.
    outbound_type = "loadBalancer"
  }
}
