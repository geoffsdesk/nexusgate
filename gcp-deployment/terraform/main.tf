# ============================================================================
# NexusGate GKE Deployment - Terraform Configuration
# Consumer/operator deployment: provisions all GCP infrastructure needed
# to run NexusGate in Google Kubernetes Engine.
# ============================================================================

terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.9"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# ---------------------------------------------------------------------------
# Enable required APIs
# ---------------------------------------------------------------------------

resource "google_project_service" "apis" {
  for_each = toset([
    "container.googleapis.com",
    "artifactregistry.googleapis.com",
    "sqladmin.googleapis.com",
    "redis.googleapis.com",
    "servicenetworking.googleapis.com",
    "compute.googleapis.com",
    "iam.googleapis.com",
  ])

  project = var.project_id
  service = each.value

  disable_dependent_services = false
  disable_on_destroy         = false
}

# Wait for APIs to propagate before creating resources
resource "time_sleep" "api_propagation" {
  depends_on      = [google_project_service.apis]
  create_duration = "60s"
}

# ---------------------------------------------------------------------------
# Networking
# ---------------------------------------------------------------------------

resource "google_compute_network" "nexusgate" {
  name                    = "nexusgate-vpc"
  auto_create_subnetworks = false
  depends_on              = [time_sleep.api_propagation]
}

resource "google_compute_subnetwork" "nexusgate" {
  name          = "nexusgate-subnet"
  ip_cidr_range = "10.0.0.0/20"
  region        = var.region
  network       = google_compute_network.nexusgate.id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.4.0.0/14"
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.8.0.0/20"
  }

  private_ip_google_access = true
}

resource "google_compute_global_address" "private_ip" {
  name          = "nexusgate-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.nexusgate.id
}

resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = google_compute_network.nexusgate.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip.name]
}

# ---------------------------------------------------------------------------
# Artifact Registry (container images)
# ---------------------------------------------------------------------------

resource "google_artifact_registry_repository" "nexusgate" {
  location      = var.region
  repository_id = "nexusgate"
  description   = "NexusGate container images"
  format        = "DOCKER"
  depends_on    = [time_sleep.api_propagation]
}

# ---------------------------------------------------------------------------
# GKE Cluster
# ---------------------------------------------------------------------------

resource "google_container_cluster" "nexusgate" {
  name     = var.cluster_name
  location = var.region

  remove_default_node_pool = true
  initial_node_count       = 1

  network    = google_compute_network.nexusgate.id
  subnetwork = google_compute_subnetwork.nexusgate.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  network_policy {
    enabled = true
  }

  release_channel {
    channel = "REGULAR"
  }

  deletion_protection = false

  depends_on = [time_sleep.api_propagation]
}

resource "google_container_node_pool" "primary" {
  name       = "nexusgate-pool"
  location   = var.region
  cluster    = google_container_cluster.nexusgate.name
  node_count = var.node_count

  node_config {
    machine_type = var.node_machine_type
    disk_size_gb = var.node_disk_size_gb
    disk_type    = "pd-ssd"

    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    labels = {
      app = "nexusgate"
    }
  }

  autoscaling {
    min_node_count = 1
    max_node_count = 5
  }

  management {
    auto_repair  = true
    auto_upgrade = true
  }
}

# ---------------------------------------------------------------------------
# Cloud SQL (PostgreSQL 16)
# ---------------------------------------------------------------------------

resource "google_sql_database_instance" "nexusgate" {
  name             = "nexusgate-postgres"
  database_version = "POSTGRES_16"
  region           = var.region

  depends_on = [
    google_service_networking_connection.private_vpc_connection,
    time_sleep.api_propagation,
  ]

  settings {
    tier              = var.db_tier
    availability_type = "REGIONAL"
    disk_autoresize   = true
    disk_size         = 20
    disk_type         = "PD_SSD"

    ip_configuration {
      ipv4_enabled                                  = false
      private_network                               = google_compute_network.nexusgate.id
      enable_private_path_for_google_cloud_services = true
    }

    backup_configuration {
      enabled                        = true
      start_time                     = "03:00"
      point_in_time_recovery_enabled = true
      transaction_log_retention_days = 7

      backup_retention_settings {
        retained_backups = 14
      }
    }

    database_flags {
      name  = "max_connections"
      value = "200"
    }

    insights_config {
      query_insights_enabled  = true
      record_application_tags = true
    }
  }

  deletion_protection = false
}

resource "google_sql_database" "nexusgate" {
  name     = var.db_name
  instance = google_sql_database_instance.nexusgate.name
}

resource "google_sql_user" "nexusgate" {
  name     = var.db_user
  instance = google_sql_database_instance.nexusgate.name
  password = var.db_password
}

# ---------------------------------------------------------------------------
# Memorystore (Redis 7)
# ---------------------------------------------------------------------------

resource "google_redis_instance" "nexusgate" {
  name           = "nexusgate-redis"
  tier           = "BASIC"
  memory_size_gb = var.redis_memory_size_gb
  region         = var.region
  redis_version  = var.redis_version

  authorized_network = google_compute_network.nexusgate.id
  connect_mode       = "PRIVATE_SERVICE_ACCESS"

  depends_on = [
    google_service_networking_connection.private_vpc_connection,
    time_sleep.api_propagation,
  ]

  labels = {
    app = "nexusgate"
  }
}

# ---------------------------------------------------------------------------
# IAM - Service account for GKE workloads
# ---------------------------------------------------------------------------

resource "google_service_account" "nexusgate_workload" {
  account_id   = "nexusgate-workload"
  display_name = "NexusGate GKE Workload Identity"
  depends_on   = [time_sleep.api_propagation]
}

resource "google_project_iam_member" "cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.nexusgate_workload.email}"
}

resource "google_project_iam_member" "artifact_reader" {
  project = var.project_id
  role    = "roles/artifactregistry.reader"
  member  = "serviceAccount:${google_service_account.nexusgate_workload.email}"
}

# Workload Identity binding - must wait for GKE cluster to create the pool
resource "google_service_account_iam_member" "workload_identity_binding" {
  service_account_id = google_service_account.nexusgate_workload.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[nexusgate/nexusgate]"
  depends_on         = [google_container_cluster.nexusgate]
}
