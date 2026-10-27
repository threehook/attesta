data "azurerm_client_config" "current" {}

# In tst made by hand and kept (README), found by the data source below; uncomment where Terraform creates the vault.
# resource "azurerm_key_vault" "this" {
#   name                = var.key_vault_name
#   location            = azurerm_resource_group.this.location
#   resource_group_name = azurerm_resource_group.this.name
#   tenant_id           = data.azurerm_client_config.current.tenant_id
#   sku_name            = "standard"
#
#   # Access is given by role assignments, not by access policies.
#   rbac_authorization_enabled = true
#   purge_protection_enabled   = var.key_vault_purge_protection
#   soft_delete_retention_days = var.key_vault_soft_delete_days
#
#   # Only the cluster's subnet and the listed addresses reach the vault. The vault takes a single address, not a /32 range.
#   network_acls {
#     default_action             = "Deny"
#     bypass                     = "AzureServices"
#     virtual_network_subnet_ids = [azurerm_subnet.aks.id]
#     ip_rules                   = [for cidr in var.key_vault_allowed_cidrs : replace(cidr, "/32", "")]
#   }
# }

data "azurerm_key_vault" "this" {
  name                = var.key_vault_name
  resource_group_name = data.azurerm_resource_group.this.name
}

# The wallet's identity: the pod's service account is exchanged for a token of this identity.
resource "azurerm_user_assigned_identity" "wallet" {
  name                = "id-${local.name}-wallet"
  location            = data.azurerm_resource_group.this.location
  resource_group_name = data.azurerm_resource_group.this.name
}

resource "azurerm_federated_identity_credential" "wallet" {
  name                = "wallet"
  resource_group_name = data.azurerm_resource_group.this.name
  parent_id           = azurerm_user_assigned_identity.wallet.id
  issuer              = azurerm_kubernetes_cluster.this.oidc_issuer_url
  subject             = "system:serviceaccount:${var.wallet_namespace}:${var.wallet_service_account}"
  audience            = ["api://AzureADTokenExchange"]
}

# Mode provisioned: the key is made through the management plane, so Terraform needs no access to the vault's data.
resource "azapi_resource" "wallet_key" {
  count     = var.wallet_key_mode == "provisioned" ? 1 : 0
  type      = "Microsoft.KeyVault/vaults/keys@2023-07-01"
  name      = var.wallet_key_name
  parent_id = data.azurerm_key_vault.this.id

  body = {
    properties = {
      kty       = "EC"
      curveName = "P-256"
      keyOps    = ["sign", "verify"]
    }
  }
}

# Mode create: the wallet makes and deletes its own keys. Mode provisioned: it only signs with the one key.
resource "azurerm_role_assignment" "wallet" {
  principal_id         = azurerm_user_assigned_identity.wallet.principal_id
  principal_type       = "ServicePrincipal"
  role_definition_name = var.wallet_key_mode == "create" ? "Key Vault Crypto Officer" : "Key Vault Crypto User"
  scope                = var.wallet_key_mode == "create" ? data.azurerm_key_vault.this.id : azapi_resource.wallet_key[0].id
}

# The issuer's seed (its signing key and so its DID) is a vault secret that a cloud engineer makes by hand; Terraform never writes it. The issuer's pod reads it
# through the Secrets Store CSI driver as its own identity, and may read nothing else of the vault's secrets than this role gives.
resource "azurerm_user_assigned_identity" "issuer" {
  name                = "id-${local.name}-issuer"
  location            = data.azurerm_resource_group.this.location
  resource_group_name = data.azurerm_resource_group.this.name
}

resource "azurerm_federated_identity_credential" "issuer" {
  name                = "issuer"
  resource_group_name = data.azurerm_resource_group.this.name
  parent_id           = azurerm_user_assigned_identity.issuer.id
  issuer              = azurerm_kubernetes_cluster.this.oidc_issuer_url
  subject             = "system:serviceaccount:${var.issuer_namespace}:${var.issuer_service_account}"
  audience            = ["api://AzureADTokenExchange"]
}

resource "azurerm_role_assignment" "issuer_seed" {
  principal_id         = azurerm_user_assigned_identity.issuer.principal_id
  principal_type       = "ServicePrincipal"
  role_definition_name = "Key Vault Secrets User"
  scope                = data.azurerm_key_vault.this.id
}

# An earlier version of this stack made the seed here. The secret stays in the vault; only the state forgets it, without reading it.
removed {
  from = azurerm_key_vault_secret.issuer_seed

  lifecycle {
    destroy = false
  }
}

# An earlier version of this stack gave the people and apps that ran Terraform the role Key Vault Secrets Officer. Those assignments stay until a person removes them:
# the workflow's app may not remove them, and the person who makes the seed by hand needs theirs.
removed {
  from = azurerm_role_assignment.secrets_officer

  lifecycle {
    destroy = false
  }
}
