use std::collections::HashMap;

use reqwest::Client;
use tracing::{debug, info};

use super::manifest::*;
use super::{ConnectorError, ConnectorType};

/// REST/OpenAPI service connector.
/// Discovers capabilities by parsing OpenAPI/Swagger specifications.
pub struct RestConnector {
    client: Client,
}

impl RestConnector {
    pub fn new() -> Self {
        Self {
            client: Client::new(),
        }
    }

    pub fn connector_type(&self) -> ConnectorType {
        ConnectorType::Rest
    }

    /// Discover capabilities from an OpenAPI specification URL.
    pub async fn discover(&self, spec_url: &str) -> Result<CapabilityManifest, ConnectorError> {
        info!(url = spec_url, "Discovering REST service capabilities");

        let spec_text = self
            .client
            .get(spec_url)
            .send()
            .await
            .map_err(|e| ConnectorError::ConnectionFailed(e.to_string()))?
            .text()
            .await
            .map_err(|e| ConnectorError::IntrospectionFailed(e.to_string()))?;

        // Parse as JSON (OpenAPI 3.x)
        let spec: serde_json::Value = serde_json::from_str(&spec_text)
            .map_err(|e| ConnectorError::SchemaParsing(e.to_string()))?;

        self.parse_openapi_spec(&spec)
    }

    fn parse_openapi_spec(
        &self,
        spec: &serde_json::Value,
    ) -> Result<CapabilityManifest, ConnectorError> {
        let info = spec.get("info").ok_or_else(|| {
            ConnectorError::SchemaParsing("Missing 'info' in OpenAPI spec".into())
        })?;

        let title = info["title"].as_str().unwrap_or("unknown");
        let version = info["version"].as_str().unwrap_or("0.0.0");
        let description = info["description"].as_str().map(String::from);

        // Extract base URL from servers
        let base_url = spec
            .get("servers")
            .and_then(|s| s.as_array())
            .and_then(|arr| arr.first())
            .and_then(|s| s["url"].as_str())
            .unwrap_or("http://localhost")
            .to_string();

        let mut manifest = CapabilityManifest::new(ServiceInfo {
            name: title.to_string(),
            version: version.to_string(),
            protocol: "rest".to_string(),
            base_url,
            description,
            tags: vec![],
        });

        // Parse paths into capabilities
        if let Some(paths) = spec.get("paths").and_then(|p| p.as_object()) {
            for (path, methods) in paths {
                if let Some(methods_obj) = methods.as_object() {
                    for (method, operation) in methods_obj {
                        if let Some(cap) = self.parse_operation(path, method, operation) {
                            manifest.add_capability(cap);
                        }
                    }
                }
            }
        }

        // Parse component schemas
        if let Some(schemas) = spec
            .pointer("/components/schemas")
            .and_then(|s| s.as_object())
        {
            for (name, schema) in schemas {
                if let Some(def) = self.parse_schema_definition(schema) {
                    manifest.add_schema(name, def);
                }
            }
        }

        info!(
            service = title,
            capabilities = manifest.capabilities.len(),
            schemas = manifest.schemas.len(),
            "REST service discovered"
        );

        Ok(manifest)
    }

    fn parse_operation(
        &self,
        path: &str,
        method: &str,
        operation: &serde_json::Value,
    ) -> Option<Capability> {
        let operation_id = operation["operationId"]
            .as_str()
            .unwrap_or(&format!("{}_{}", method, path.replace('/', "_")));

        let description = operation["summary"]
            .as_str()
            .or_else(|| operation["description"].as_str())
            .map(String::from);

        // Parse parameters into input fields
        let mut input = HashMap::new();
        if let Some(params) = operation.get("parameters").and_then(|p| p.as_array()) {
            for param in params {
                let name = param["name"].as_str().unwrap_or("unknown");
                let required = param["required"].as_bool().unwrap_or(false);
                let schema = param.get("schema").unwrap_or(param);
                let field_type = schema["type"].as_str().unwrap_or("string").to_string();

                input.insert(
                    name.to_string(),
                    FieldDefinition {
                        field_type,
                        format: schema["format"].as_str().map(String::from),
                        required,
                        description: param["description"].as_str().map(String::from),
                        default: None,
                        constraints: None,
                    },
                );
            }
        }

        // Parse request body
        if let Some(body) = operation.pointer("/requestBody/content/application~1json/schema") {
            if let Some(ref_path) = body["$ref"].as_str() {
                input.insert(
                    "body".to_string(),
                    FieldDefinition {
                        field_type: "object".to_string(),
                        format: Some(ref_path.to_string()),
                        required: true,
                        description: Some("Request body".to_string()),
                        default: None,
                        constraints: None,
                    },
                );
            }
        }

        // Parse response
        let output = if let Some(resp_schema) =
            operation.pointer("/responses/200/content/application~1json/schema")
        {
            if let Some(ref_path) = resp_schema["$ref"].as_str() {
                OutputDefinition::Reference {
                    reference: ref_path.to_string(),
                }
            } else {
                OutputDefinition::Reference {
                    reference: "#/schemas/Unknown".to_string(),
                }
            }
        } else {
            OutputDefinition::Reference {
                reference: "#/schemas/Empty".to_string(),
            }
        };

        let is_idempotent = matches!(method, "get" | "put" | "delete" | "head" | "options");
        let is_cacheable = matches!(method, "get" | "head");

        Some(Capability {
            operation: operation_id.to_string(),
            description,
            method: Some(method.to_uppercase()),
            path: Some(path.to_string()),
            input,
            output,
            errors: vec![],
            idempotent: is_idempotent,
            cacheable: is_cacheable,
        })
    }

    fn parse_schema_definition(
        &self,
        schema: &serde_json::Value,
    ) -> Option<SchemaDefinition> {
        let schema_type = schema["type"].as_str().unwrap_or("object").to_string();

        let properties = schema
            .get("properties")
            .and_then(|p| p.as_object())
            .map(|props| {
                props
                    .iter()
                    .map(|(name, prop)| {
                        (
                            name.clone(),
                            FieldDefinition {
                                field_type: prop["type"]
                                    .as_str()
                                    .unwrap_or("string")
                                    .to_string(),
                                format: prop["format"].as_str().map(String::from),
                                required: false,
                                description: prop["description"].as_str().map(String::from),
                                default: None,
                                constraints: None,
                            },
                        )
                    })
                    .collect()
            });

        let required = schema
            .get("required")
            .and_then(|r| r.as_array())
            .map(|arr| {
                arr.iter()
                    .filter_map(|v| v.as_str().map(String::from))
                    .collect()
            });

        Some(SchemaDefinition {
            schema_type,
            properties,
            items: None,
            required,
            description: schema["description"].as_str().map(String::from),
        })
    }

    pub async fn health_check(&self, endpoint: &str) -> Result<bool, ConnectorError> {
        match self.client.get(endpoint).send().await {
            Ok(resp) => Ok(resp.status().is_success()),
            Err(e) => Err(ConnectorError::ConnectionFailed(e.to_string())),
        }
    }
}
