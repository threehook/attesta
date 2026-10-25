#!/usr/bin/env bash
# One time, as an Owner of the subscription: the Entra app that the GitHub workflow signs in as (OIDC, no secret).
# Needs `az login`. Prints the three values to store as repository variables.
set -euo pipefail

# This repo's tokens carry the numeric owner and repo ids in the subject.
REPO=threehook@442604/attesta@1404472147
ENVIRONMENT=azure-tst
APP_NAME=attesta-github-terraform

SUBSCRIPTION_ID=$(az account show --query id -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)

CLIENT_ID=$(az ad app list --display-name "$APP_NAME" --query '[0].appId' -o tsv)
if [ -z "$CLIENT_ID" ]; then
	CLIENT_ID=$(az ad app create --display-name "$APP_NAME" --query appId -o tsv)
	az ad sp create --id "$CLIENT_ID" >/dev/null
fi

federate() {
	local name=$1 subject=$2
	if [ -z "$(az ad app federated-credential list --id "$CLIENT_ID" --query "[?name=='$name'].name" -o tsv)" ]; then
		az ad app federated-credential create --id "$CLIENT_ID" --parameters "{
			\"name\": \"$name\",
			\"issuer\": \"https://token.actions.githubusercontent.com\",
			\"subject\": \"$subject\",
			\"audiences\": [\"api://AzureADTokenExchange\"]}" >/dev/null
	fi
}
federate main "repo:$REPO:ref:refs/heads/main"
federate pull-request "repo:$REPO:pull_request"
federate "environment-$ENVIRONMENT" "repo:$REPO:environment:$ENVIRONMENT"
# The credential of the environment that was called azure-test before.
az ad app federated-credential delete --id "$CLIENT_ID" --federated-credential-id environment >/dev/null 2>&1 || true

# Contributor creates the resources.
az role assignment create --assignee "$CLIENT_ID" --role Contributor --scope "/subscriptions/$SUBSCRIPTION_ID" >/dev/null || true

# Terraform gives the wallet's identity its role on the Key Vault. The app may assign only these two roles, Key Vault Crypto Officer and Key Vault Crypto User,
# and no other. RBAC_SCOPE is where it may do that: the subscription by default; an environment whose resource group exists beforehand sets that group.
RBAC_SCOPE=${RBAC_SCOPE:-/subscriptions/$SUBSCRIPTION_ID}
ROLES="14b46e9e-c2b7-41b4-b07b-48a6ebf60603, 12338af0-0e69-4776-bea7-57ae8d297424"
CONDITION="((!(ActionMatches{'Microsoft.Authorization/roleAssignments/write'})) OR (@Request[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {$ROLES})) AND ((!(ActionMatches{'Microsoft.Authorization/roleAssignments/delete'})) OR (@Resource[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {$ROLES}))"
az role assignment create --assignee "$CLIENT_ID" --role "Role Based Access Control Administrator" --scope "$RBAC_SCOPE" \
	--condition "$CONDITION" --condition-version 2.0 >/dev/null || true

echo "AZURE_CLIENT_ID=$CLIENT_ID"
echo "AZURE_TENANT_ID=$TENANT_ID"
echo "AZURE_SUBSCRIPTION_ID=$SUBSCRIPTION_ID"
