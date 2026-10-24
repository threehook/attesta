terraform {
  required_version = ">= 1.6"

  # Execution mode of the workspace is Local: the GitHub workflow runs Terraform, HCP keeps the state.
  cloud {
    organization = "corbencreatives"

    workspaces {
      name = "attesta-azure-test"
    }
  }

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "azurerm" {
  features {}
  subscription_id = var.subscription_id
}
