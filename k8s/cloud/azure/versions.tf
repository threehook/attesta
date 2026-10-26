terraform {
  required_version = ">= 1.11"

  # Execution mode of the workspace is Local: the GitHub workflow runs Terraform, HCP keeps the state.
  cloud {
    organization = "corbencreatives"

    workspaces {
      name = "attesta-azure-tst"
    }
  }

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
    azapi = {
      source  = "Azure/azapi"
      version = "~> 2.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "azurerm" {
  features {
    key_vault {
      purge_soft_delete_on_destroy    = var.key_vault_purge_on_destroy
      recover_soft_deleted_key_vaults = var.key_vault_purge_on_destroy
    }
  }
  subscription_id = var.subscription_id
}

provider "azapi" {
  subscription_id = var.subscription_id
}
