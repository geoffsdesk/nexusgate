pub mod rest;
pub mod graphql;
pub mod grpc;
pub mod database;
pub mod manifest;

// async_trait is available but we use native async traits (Rust 1.75+)
use serde::{Deserialize, Serialize};

use manifest::CapabilityManifest;

/// Trait that all service connectors must implement.
/// Each connector introspects a backend service and produces a capability manifest.
pub trait ServiceConnector: Send + Sync {
    /// Introspect the backend and return its capabilities.
    fn discover(&self, endpoint: &str) -> impl std::future::Future<Output = Result<CapabilityManifest, ConnectorError>> + Send;

    /// Validate that the backend is reachable and responsive.
    fn health_check(&self, endpoint: &str) -> impl std::future::Future<Output = Result<bool, ConnectorError>> + Send;

    /// Get the connector type identifier.
    fn connector_type(&self) -> ConnectorType;
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ConnectorType {
    Rest,
    GraphQL,
    Grpc,
    Database,
    MessageQueue,
}

#[derive(Debug, thiserror::Error)]
pub enum ConnectorError {
    #[error("Connection failed: {0}")]
    ConnectionFailed(String),

    #[error("Introspection failed: {0}")]
    IntrospectionFailed(String),

    #[error("Schema parsing error: {0}")]
    SchemaParsing(String),

    #[error("Authentication required: {0}")]
    AuthRequired(String),

    #[error("Timeout: {0}")]
    Timeout(String),
}
