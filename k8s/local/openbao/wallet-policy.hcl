# What a system's wallet may do in OpenBao: make its keys and sign with them, nothing else.
path "transit/keys/*" {
  capabilities = ["create", "read", "update", "delete"]
}
path "transit/keys" {
  capabilities = ["list"]
}
path "transit/sign/*" {
  capabilities = ["update"]
}
