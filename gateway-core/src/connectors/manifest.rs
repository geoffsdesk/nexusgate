use std::collections::HashMap;

use serde::{Deserialize, Serialize};

/// JSON-LD based capability manifest describing what a backend service can do.
/// This is the universal schema that all connectors produce and the AI layer consumes.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CapabilityManifest {
    #[serde(rename = "@context")]
    pub context: String,

    #[serde(rename = "@type")]
    pub manifest_type: String,

    pub service: ServiceInfo,
    pub capabilities: Vec<Capability>,
    pub schemas: HashMap<String, SchemaDefinition>,
    pub authentication: Option<AuthenticationInfo>,
    pub rate_limits: Option<RateLimitInfo>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServiceInfo {
    pub name: String,
    pub version: String,
    pub protocol: String,
    pub base_url: String,
    pub description: Option<String>,
    pub tags: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Capability {
    pub operation: String,
    pub description: Option<String>,
    pub method: Option<String>,
    pub path: Option<String>,
    pub input: HashMap<String, FieldDefinition>,
    pub output: OutputDefinition,
    pub errors: Vec<ErrorDefinition>,
    pub idempotent: bool,
    pub cacheable: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FieldDefinition {
    #[serde(rename = "type")]
    pub field_type: String,
    pub format: Option<String>,
    pub required: bool,
    pub description: Option<String>,
    pub default: Option<serde_json::Value>,
    pub constraints: Option<FieldConstraints>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FieldConstraints {
    pub min: Option<f64>,
    pub max: Option<f64>,
    pub pattern: Option<String>,
    pub enum_values: Option<Vec<String>>,
    pub min_length: Option<usize>,
    pub max_length: Option<usize>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum OutputDefinition {
    Reference { #[serde(rename = "$ref")] reference: String },
    Inline(SchemaDefinition),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SchemaDefinition {
    #[serde(rename = "type")]
    pub schema_type: String,
    pub properties: Option<HashMap<String, FieldDefinition>>,
    pub items: Option<Box<SchemaDefinition>>,
    pub required: Option<Vec<String>>,
    pub description: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ErrorDefinition {
    pub code: u16,
    pub description: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthenticationInfo {
    pub schemes: Vec<AuthScheme>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthScheme {
    pub scheme_type: String, // "bearer", "api_key", "oauth2", "basic"
    pub location: Option<String>, // "header", "query"
    pub name: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RateLimitInfo {
    pub requests_per_second: Option<u64>,
    pub burst: Option<u64>,
    pub quota_per_day: Option<u64>,
}

impl CapabilityManifest {
    pub fn new(service: ServiceInfo) -> Self {
        Self {
            context: "https://nexusgate.io/schema/v1".to_string(),
            manifest_type: "ServiceCapability".to_string(),
            service,
            capabilities: Vec::new(),
            schemas: HashMap::new(),
            authentication: None,
            rate_limits: None,
        }
    }

    /// Add a capability to the manifest.
    pub fn add_capability(&mut self, capability: Capability) {
        self.capabilities.push(capability);
    }

    /// Add a schema definition.
    pub fn add_schema(&mut self, name: &str, schema: SchemaDefinition) {
        self.schemas.insert(name.to_string(), schema);
    }

    /// Serialize to JSON-LD string.
    pub fn to_json(&self) -> Result<String, serde_json::Error> {
        serde_json::to_string_pretty(self)
    }
}
