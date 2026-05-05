# Bifrost virtual keys are imported by their UUID (the `id` returned at
# creation time). The `value` (sk-bf-...) cannot be recovered from the API
# after creation, so imported keys will have a null `value` until rotated.
terraform import bifrost_virtual_key.team_key vk-12345abc-...
