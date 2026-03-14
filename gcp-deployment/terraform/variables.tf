# ─── GCP Project ───
variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for all resources"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone for zonal resources"
  type        = string
  default     = "us-central1-a"
}

# ─── GKE ───
variable "cluster_name" {
  description = "Name of the GKE cluster"
  type        = string
  default     = "nexusgate"
}

variable "node_count" {
  description = "Number of nodes per zone"
  type        = number
  default     = 2
}

variable "node_machine_type" {
  description = "Machine type for GKE nodes"
  type        = string
  default     = "e2-standard-4"
}

variable "node_disk_size_gb" {
  description = "Disk size for GKE nodes in GB"
  type        = number
  default     = 50
}

# ─── Cloud SQL ───
variable "db_tier" {
  description = "Cloud SQL instance tier"
  type        = string
  default     = "db-custom-2-4096"
}

variable "db_name" {
  description = "PostgreSQL database name"
  type        = string
  default     = "nexusgate"
}

variable "db_user" {
  description = "PostgreSQL admin username"
  type        = string
  default     = "nexusgate"
}

variable "db_password" {
  description = "PostgreSQL admin password"
  type        = string
  sensitive   = true
}

# ─── Memorystore ───
variable "redis_memory_size_gb" {
  description = "Redis instance memory in GB"
  type        = number
  default     = 1
}

variable "redis_version" {
  description = "Redis version"
  type        = string
  default     = "REDIS_7_0"
}
