resource "azurerm_resource_group" "this" {
  name     = "rg-${var.name}"
  location = var.location
}

resource "azurerm_virtual_network" "this" {
  name                = "vnet-${var.name}"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  address_space       = ["10.20.0.0/16"]
}

resource "azurerm_subnet" "aks" {
  name                 = "snet-aks"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.this.name
  address_prefixes     = ["10.20.0.0/22"]
}

resource "random_string" "acr" {
  length  = 6
  upper   = false
  special = false
}

# Basic has no private endpoint; the registry keeps its public endpoint, behind Entra authentication.
resource "azurerm_container_registry" "this" {
  name                = "acr${replace(var.name, "-", "")}${random_string.acr.result}"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  sku                 = "Basic"
  admin_enabled       = false
}

resource "azurerm_kubernetes_cluster" "this" {
  name                    = "aks-${var.name}"
  location                = azurerm_resource_group.this.location
  resource_group_name     = azurerm_resource_group.this.name
  dns_prefix              = var.name
  private_cluster_enabled = false
  sku_tier                = "Free"

  api_server_access_profile {
    authorized_ip_ranges = var.api_allowed_cidrs
  }

  default_node_pool {
    name            = "system"
    node_count      = var.node_count
    vm_size         = var.node_size
    os_disk_size_gb = 32
    vnet_subnet_id  = azurerm_subnet.aks.id
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

resource "azurerm_role_assignment" "acr_pull" {
  scope                            = azurerm_container_registry.this.id
  role_definition_name             = "AcrPull"
  principal_id                     = azurerm_kubernetes_cluster.this.kubelet_identity[0].object_id
  skip_service_principal_aad_check = true
}
