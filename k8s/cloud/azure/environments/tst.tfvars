# The test environment: small, and the vault can be thrown away.
environment                = "tst"
key_vault_name             = "kv-tst-attesta"
key_vault_purge_protection = false
key_vault_soft_delete_days = 7
key_vault_purge_on_destroy = true
wallet_key_mode            = "create"
issuer_seed_mode           = "create"
# Whoever runs Terraform: the person applying and the Entra app attesta-github-terraform that the workflow signs in as.
secrets_officer_object_ids = ["50aec691-086f-4f04-a45f-42d191ee90f1", "590434fc-6f9f-45ad-9c4d-d04905cf04e1"]
