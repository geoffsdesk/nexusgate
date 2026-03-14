use std::collections::HashMap;

use reqwest::Client;
use tracing::info;

use super::manifest::*;
use super::{ConnectorError, ConnectorType};

/// GraphQL service connector.
/// Uses the introspection query to discover schema and capabilities.
pub struct GraphQLConnector {
    client: Client,
}

const INTROSPECTION_QUERY: &str = r#"
{
  __schema {
    queryType { name }
    mutationType { name }
    types {
      name
      kind
      description
      fields {
        name
        description
        args {
          name
          type { name kind ofType { name kind ofType { name kind } } }
          defaultValue
        }
        type { name kind ofType { name kind ofType { name kind } } }
      }
    }
  }
}
"#;

impl GraphQLConnector {
    pub fn new() -> Self {
        Self {
            client: Client::new(),
        }
    }

    pub fn connector_type(&self) -> ConnectorType {
        ConnectorType::GraphQL
    }

    pub async fn discover(&self, endpoint: &str) -> Result<CapabilityManifest, ConnectorError> {
        info!(endpoint = endpoint, "Discovering GraphQL service capabilities");

        let response = self
            .client
            .post(endpoint)
            .json(&serde_json::json!({ "query": INTROSPECTION_QUERY }))
            .send()
            .await
            .map_err(|e| ConnectorError::ConnectionFailed(e.to_string()))?;

        let result: serde_json::Value = response
            .json()
            .await
            .map_err(|e| ConnectorError::IntrospectionFailed(e.to_string()))?;

        let schema = result
            .pointer("/data/__schema")
            .ok_or_else(|| ConnectorError::SchemaParsing("Invalid introspection response".into()))?;

        let mut manifest = CapabilityManifest::new(ServiceInfo {
            name: "graphql-service".to_string(),
            version: "1.0".to_string(),
            protocol: "graphql".to_string(),
            base_url: endpoint.to_string(),
            description: Some("GraphQL service discovered via introspection".to_string()),
            tags: vec!["graphql".to_string()],
        });

        // Parse query type fields as capabilities
        let query_type_name = schema
            .pointer("/queryType/name")
            .and_then(|n| n.as_str())
            .unwrap_or("Query");

        let types = schema
            .get("types")
            .and_then(|t| t.as_array())
            .ok_or_else(|| ConnectorError::SchemaParsing("No types found".into()))?;

        for type_def in types {
            let type_name = type_def["name"].as_str().unwrap_or("");

            // Skip introspection types
            if type_name.starts_with("__") {
                continue;
            }

            let kind = type_def["kind"].as_str().unwrap_or("");

            if type_name == query_type_name {
                // Parse queries as read capabilities
                if let Some(fields) = type_def.get("fields").and_then(|f| f.as_array()) {
                    for field in fields {
                        if let Some(cap) = self.parse_field_as_capability(field, "QUERY") {
                            manifest.add_capability(cap);
                        }
                    }
                }
            } else if kind == "OBJECT" && !["String", "Int", "Float", "Boolean", "ID"].contains(&type_name) {
                // Parse object types as schemas
                if let Some(schema_def) = self.parse_type_as_schema(type_def) {
                    manifest.add_schema(type_name, schema_def);
                }
            }
        }

        // Parse mutation type
        if let Some(mutation_name) = schema.pointer("/mutationType/name").and_then(|n| n.as_str()) {
            for type_def in types {
                if type_def["name"].as_str() == Some(mutation_name) {
                    if let Some(fields) = type_def.get("fields").and_then(|f| f.as_array()) {
                        for field in fields {
                            if let Some(cap) = self.parse_field_as_capability(field, "MUTATION") {
                                manifest.add_capability(cap);
                            }
                        }
                    }
                }
            }
        }

        info!(
            capabilities = manifest.capabilities.len(),
            schemas = manifest.schemas.len(),
            "GraphQL service discovered"
        );

        Ok(manifest)
    }

    fn parse_field_as_capability(
        &self,
        field: &serde_json::Value,
        operation_type: &str,
    ) -> Option<Capability> {
        let name = field["name"].as_str()?;
        let description = field["description"].as_str().map(String::from);

        let mut input = HashMap::new();
        if let Some(args) = field.get("args").and_then(|a| a.as_array()) {
            for arg in args {
                let arg_name = arg["name"].as_str().unwrap_or("unknown");
                let type_info = &arg["type"];
                let field_type = self.resolve_type_name(type_info);
                let required = type_info["kind"].as_str() == Some("NON_NULL");

                input.insert(
                    arg_name.to_string(),
                    FieldDefinition {
                        field_type,
                        format: None,
                        required,
                        description: None,
                        default: arg["defaultValue"]
                            .as_str()
                            .map(|v| serde_json::Value::String(v.to_string())),
                        constraints: None,
                    },
                );
            }
        }

        let return_type = self.resolve_type_name(&field["type"]);

        Some(Capability {
            operation: name.to_string(),
            description,
            method: Some(operation_type.to_string()),
            path: None,
            input,
            output: OutputDefinition::Reference {
                reference: format!("#/schemas/{}", return_type),
            },
            errors: vec![],
            idempotent: operation_type == "QUERY",
            cacheable: operation_type == "QUERY",
        })
    }

    fn parse_type_as_schema(&self, type_def: &serde_json::Value) -> Option<SchemaDefinition> {
        let fields = type_def.get("fields")?.as_array()?;

        let mut properties = HashMap::new();
        for field in fields {
            let name = field["name"].as_str().unwrap_or("unknown");
            let field_type = self.resolve_type_name(&field["type"]);

            properties.insert(
                name.to_string(),
                FieldDefinition {
                    field_type,
                    format: None,
                    required: false,
                    description: field["description"].as_str().map(String::from),
                    default: None,
                    constraints: None,
                },
            );
        }

        Some(SchemaDefinition {
            schema_type: "object".to_string(),
            properties: Some(properties),
            items: None,
            required: None,
            description: type_def["description"].as_str().map(String::from),
        })
    }

    fn resolve_type_name(&self, type_info: &serde_json::Value) -> String {
        if let Some(name) = type_info["name"].as_str() {
            return name.to_string();
        }
        if let Some(of_type) = type_info.get("ofType") {
            return self.resolve_type_name(of_type);
        }
        "Unknown".to_string()
    }

    pub async fn health_check(&self, endpoint: &str) -> Result<bool, ConnectorError> {
        let resp = self
            .client
            .post(endpoint)
            .json(&serde_json::json!({ "query": "{ __typename }" }))
            .send()
            .await
            .map_err(|e| ConnectorError::ConnectionFailed(e.to_string()))?;

        Ok(resp.status().is_success())
    }
}
