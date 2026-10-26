# The test environment: small, and the vault can be thrown away.
environment                = "tst"
key_vault_name             = "kv-tst-attesta"
key_vault_purge_protection = false
key_vault_soft_delete_days = 7
key_vault_purge_on_destroy = true
wallet_key_mode            = "create"
# One node cannot hold SPIRE, the issuer with Keycloak and Postgres, the backend and the agent, and leave room for a rolling update.
node_count = 2
