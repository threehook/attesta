data "azurerm_client_config" "current" {}

resource "azurerm_key_vault" "this" {
  name                = var.key_vault_name
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tenant_id           = data.azurerm_client_config.current.tenant_id
  sku_name            = "standard"

  # Access is given by role assignments, not by access policies.
  rbac_authorization_enabled = true
  purge_protection_enabled   = var.key_vault_purge_protection
  soft_delete_retention_days = var.key_vault_soft_delete_days

  # Only the cluster's subnet and the listed addresses reach the vault. The vault takes a single address, not a /32 range.
  network_acls {
    default_action             = "Deny"
    bypass                     = "AzureServices"
    virtual_network_subnet_ids = [azurerm_subnet.aks.id]
    ip_rules                   = [for cidr in var.key_vault_allowed_cidrs : replace(cidr, "/32", "")]
  }
}

# The wallet's identity: the pod's service account is exchanged for a token of this identity.
resource "azurerm_user_assigned_identity" "wallet" {
  name                = "id-${local.name}-wallet"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
}

resource "azurerm_federated_identity_credential" "wallet" {
  name                = "wallet"
  resource_group_name = azurerm_resource_group.this.name
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
  parent_id = azurerm_key_vault.this.id

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
  scope                = var.wallet_key_mode == "create" ? azurerm_key_vault.this.id : azapi_resource.wallet_key[0].id
}

# The issuer's seed. In mode create it is random and made once; it is a write-only value, so it is sent to the vault and never stored in the state or the plan.
# A new seed needs a new value_wo_version. In mode provisioned the secret is there already and Terraform leaves it alone.
# Writing a secret is a data-plane call, so whoever applies needs the role below and an address that the vault's firewall admits.
ephemeral "random_password" "issuer_seed" {
  count   = var.issuer_seed_mode == "create" ? 1 : 0
  length  = 64
  special = false
}

resource "azurerm_role_assignment" "applier_secrets" {
  count                = var.issuer_seed_mode == "create" ? 1 : 0
  principal_id         = data.azurerm_client_config.current.object_id
  role_definition_name = "Key Vault Secrets Officer"
  scope                = azurerm_key_vault.this.id
}

resource "azurerm_key_vault_secret" "issuer_seed" {
  count            = var.issuer_seed_mode == "create" ? 1 : 0
  name             = var.issuer_seed_name
  value_wo         = ephemeral.random_password.issuer_seed[0].result
  value_wo_version = 1
  key_vault_id     = azurerm_key_vault.this.id

  depends_on = [azurerm_role_assignment.applier_secrets]
}
