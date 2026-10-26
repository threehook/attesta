# Azure infrastructure

Terraform for the AKS cluster, its network and the Key Vault, applied by `.github/workflows/terraform.yml`. Terraform manages identities and their roles; it never
writes into the vault.

## What a cloud engineer makes by hand

Secrets and keys in the vault are made by a person, once per vault (the vault's firewall admits only the cluster and the addresses in `KEY_VAULT_ALLOWED_CIDRS`,
and the person needs the role Key Vault Secrets Officer or Crypto Officer):

```sh
# The issuer's seed: its signing key, and so its DID, derive from it. The policies that trust the issuer name the DID, so a new seed needs a new policy.
az keyvault secret set --vault-name kv-tst-attesta --name issuer-seed --value "$(openssl rand -hex 32)"
```

The issuer's pod reads the secret as its own identity (`id-tst-attesta-issuer`, role Key Vault Secrets User) through the Secrets Store CSI driver. The wallet makes and
deletes its short-lived holder keys itself (`wallet_key_mode`).
