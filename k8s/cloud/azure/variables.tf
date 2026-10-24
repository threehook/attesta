variable "subscription_id" {
  type    = string
  default = "a6ce566c-53c3-4548-ac59-63c982e8ab78"
}

variable "location" {
  type    = string
  default = "westeurope"
}

variable "name" {
  type        = string
  default     = "attesta-test"
  description = "Prefix of every resource name"
}

variable "node_count" {
  type    = number
  default = 1
}

variable "node_size" {
  type    = string
  default = "Standard_B2s"
}
