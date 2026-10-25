variable "subscription_id" {
  type    = string
  default = "a6ce566c-53c3-4548-ac59-63c982e8ab78"
}

variable "location" {
  type    = string
  default = "westeurope"
}

variable "name" {
  type        = string
  default     = "attesta-test"
  description = "Prefix of every resource name"
}

variable "node_count" {
  type    = number
  default = 1
}

variable "node_size" {
  type    = string
  default = "Standard_B2s"
}

variable "api_allowed_cidrs" {
  type        = list(string)
  description = "Who may reach the cluster's API server, e.g. [\"203.0.113.7/32\"]; set in the repository variable API_ALLOWED_CIDRS"

  validation {
    condition     = length(var.api_allowed_cidrs) > 0
    error_message = "An empty list would open the API server to everyone."
  }
}

variable "key_vault_name" {
  type        = string
  description = "Name of the Key Vault, unique in all of Azure (3-24 letters, digits and hyphens)"
}

variable "key_vault_purge_protection" {
  type        = bool
  description = "Keep a deleted vault and its keys until the retention ends, so nothing can purge them"
}

variable "key_vault_soft_delete_days" {
  type        = number
  description = "Days a deleted vault and its keys are kept (7-90)"
}

variable "key_vault_purge_on_destroy" {
  type        = bool
  description = "Purge the vault when Terraform destroys it, so its name is free again; incompatible with purge protection"
}

variable "wallet_key_mode" {
  type        = string
  description = "How the wallet gets its holder key: create (it makes a key at every start; role Key Vault Crypto Officer on the vault) or provisioned (Terraform makes one key; role Key Vault Crypto User on that key only)"

  validation {
    condition     = contains(["create", "provisioned"], var.wallet_key_mode)
    error_message = "wallet_key_mode is create or provisioned."
  }
}

variable "wallet_key_name" {
  type        = string
  default     = "cicd-agent"
  description = "Name of the key that Terraform makes in mode provisioned"
}

variable "wallet_namespace" {
  type    = string
  default = "attesta"
}

variable "wallet_service_account" {
  type    = string
  default = "cicd-agent"
}
