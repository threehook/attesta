# Azure infrastructure

Terraform for the AKS cluster and the identities of the wallet and the issuer, applied by `.github/workflows/terraform.yml`. The resource group, VNet, subnet and
Key Vault are made by hand once and kept; Terraform looks them up (their `resource` blocks are commented out in `main.tf` and `keyvault.tf`), so destroying the
environment removes only the cluster, the identities and their role assignments. Terraform manages identities and their roles; it never writes into the vault.

## What a cloud engineer makes by hand, once

```sh
az group create -n rg-tst-attesta -l westeurope
az network vnet create -g rg-tst-attesta -n vnet-tst-attesta -l westeurope --address-prefixes 10.20.0.0/16 --subnet-name snet-aks --subnet-prefixes 10.20.0.0/22
az network vnet subnet update -g rg-tst-attesta --vnet-name vnet-tst-attesta -n snet-aks --service-endpoints Microsoft.KeyVault
az keyvault create -g rg-tst-attesta -n kv-tst-attesta -l westeurope --sku standard --enable-rbac-authorization true --retention-days 7 \
  --default-action Deny --bypass AzureServices
az keyvault network-rule add -n kv-tst-attesta --vnet-name vnet-tst-attesta --subnet snet-aks
az keyvault network-rule add -n kv-tst-attesta --ip-address <your address>
```

## The vault's contents

Secrets and keys in the vault are made by a person, once per vault (the vault's firewall admits only the cluster and the addresses in `KEY_VAULT_ALLOWED_CIDRS`,
and the person needs the role Key Vault Secrets Officer or Crypto Officer):

```sh
# The issuer's seed: its signing key, and so its DID, derive from it. The policies that trust the issuer name the DID, so a new seed needs a new policy.
az keyvault secret set --vault-name kv-tst-attesta --name issuer-seed --value "$(openssl rand -hex 32)"
```

The issuer's pod reads the secret as its own identity (`id-tst-attesta-issuer`, role Key Vault Secrets User) through the Secrets Store CSI driver. The wallet makes and
deletes its short-lived holder keys itself (`wallet_key_mode`).
