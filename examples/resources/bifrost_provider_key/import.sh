# Bifrost provider keys are imported by composite ID "<provider_name>:<key_name>".
# Terraform resolves the server-assigned UUID by listing the provider's keys and
# matching on `name`, so use the same key name you'd put in HCL.
terraform import bifrost_provider_key.bedrock_primary bedrock:primary
