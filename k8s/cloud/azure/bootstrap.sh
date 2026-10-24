#!/usr/bin/env bash
# One time, as an Owner of the subscription: the Entra app that the GitHub workflow signs in as (OIDC, no secret).
# Needs `az login`. Prints the three values to store as repository variables.
set -euo pipefail

REPO=threehook/attesta
ENVIRONMENT=azure-test
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
federate environment "repo:$REPO:environment:$ENVIRONMENT"

# Contributor creates the resources; User Access Administrator lets Terraform create the AcrPull role assignment.
for role in Contributor "User Access Administrator"; do
	az role assignment create --assignee "$CLIENT_ID" --role "$role" --scope "/subscriptions/$SUBSCRIPTION_ID" >/dev/null || true
done

echo "AZURE_CLIENT_ID=$CLIENT_ID"
echo "AZURE_TENANT_ID=$TENANT_ID"
echo "AZURE_SUBSCRIPTION_ID=$SUBSCRIPTION_ID"
