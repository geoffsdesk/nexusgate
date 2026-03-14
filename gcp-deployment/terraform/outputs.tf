output "cluster_name" {
  description = "GKE cluster name"
  value       = google_container_cluster.nexusgate.name
}

output "cluster_endpoint" {
  description = "GKE cluster endpoint"
  value       = google_container_cluster.nexusgate.endpoint
  sensitive   = true
}

output "artifact_registry_url" {
  description = "Artifact Registry repository URL"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.nexusgate.repository_id}"
}

output "cloud_sql_connection_name" {
  description = "Cloud SQL instance connection name (for Cloud SQL Proxy)"
  value       = google_sql_database_instance.nexusgate.connection_name
}

output "cloud_sql_private_ip" {
  description = "Cloud SQL private IP address"
  value       = google_sql_database_instance.nexusgate.private_ip_address
}

output "redis_host" {
  description = "Memorystore Redis host"
  value       = google_redis_instance.nexusgate.host
}

output "redis_port" {
  description = "Memorystore Redis port"
  value       = google_redis_instance.nexusgate.port
}

output "kubeconfig_command" {
  description = "Command to configure kubectl"
  value       = "gcloud container clusters get-credentials ${google_container_cluster.nexusgate.name} --region ${var.region} --project ${var.project_id}"
}
