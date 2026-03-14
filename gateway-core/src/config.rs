use std::net::SocketAddr;
use std::path::PathBuf;

use anyhow::Result;
use serde::Deserialize;

#[derive(Debug, Deserialize, Clone)]
pub struct GatewayConfig {
    pub server: ServerConfig,
    pub proxy: ProxyConfig,
    pub security: SecurityConfig,
    pub connectors: ConnectorsConfig,
}

#[derive(Debug, Deserialize, Clone)]
pub struct ServerConfig {
    pub listen_addr: SocketAddr,
    pub management_addr: SocketAddr,
    pub metrics_addr: SocketAddr,
    pub worker_threads: Option<usize>,
    pub max_connections: usize,
    pub request_timeout_ms: u64,
    pub graceful_shutdown_timeout_ms: u64,
}

#[derive(Debug, Deserialize, Clone)]
pub struct ProxyConfig {
    pub connect_timeout_ms: u64,
    pub request_timeout_ms: u64,
    pub max_retries: u32,
    pub retry_backoff_ms: u64,
    pub buffer_size: usize,
}

#[derive(Debug, Deserialize, Clone)]
pub struct SecurityConfig {
    pub tls_enabled: bool,
    pub tls_cert_path: Option<PathBuf>,
    pub tls_key_path: Option<PathBuf>,
    pub mtls_enabled: bool,
    pub mtls_ca_path: Option<PathBuf>,
    pub cors_allowed_origins: Vec<String>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct ConnectorsConfig {
    pub rest: RestConnectorConfig,
    pub graphql: GraphQLConnectorConfig,
    pub grpc: GrpcConnectorConfig,
    pub database: DatabaseConnectorConfig,
}

#[derive(Debug, Deserialize, Clone)]
pub struct RestConnectorConfig {
    pub enabled: bool,
    pub openapi_discovery: bool,
}

#[derive(Debug, Deserialize, Clone)]
pub struct GraphQLConnectorConfig {
    pub enabled: bool,
    pub introspection: bool,
}

#[derive(Debug, Deserialize, Clone)]
pub struct GrpcConnectorConfig {
    pub enabled: bool,
    pub reflection: bool,
}

#[derive(Debug, Deserialize, Clone)]
pub struct DatabaseConnectorConfig {
    pub enabled: bool,
    pub connection_pool_size: u32,
}

impl GatewayConfig {
    pub fn load() -> Result<Self> {
        // Try loading from file, then env, then defaults
        let config_path = std::env::var("NEXUSGATE_CONFIG")
            .unwrap_or_else(|_| "nexusgate.toml".to_string());

        if let Ok(contents) = std::fs::read_to_string(&config_path) {
            let config: GatewayConfig = toml::from_str(&contents)?;
            return Ok(config);
        }

        // Return sensible defaults
        Ok(Self::default())
    }
}

impl Default for GatewayConfig {
    fn default() -> Self {
        Self {
            server: ServerConfig {
                listen_addr: "0.0.0.0:8080".parse().unwrap(),
                management_addr: "127.0.0.1:8090".parse().unwrap(),
                metrics_addr: "0.0.0.0:9090".parse().unwrap(),
                worker_threads: None,
                max_connections: 10000,
                request_timeout_ms: 30000,
                graceful_shutdown_timeout_ms: 15000,
            },
            proxy: ProxyConfig {
                connect_timeout_ms: 5000,
                request_timeout_ms: 30000,
                max_retries: 3,
                retry_backoff_ms: 100,
                buffer_size: 65536,
            },
            security: SecurityConfig {
                tls_enabled: false,
                tls_cert_path: None,
                tls_key_path: None,
                mtls_enabled: false,
                mtls_ca_path: None,
                cors_allowed_origins: vec!["*".to_string()],
            },
            connectors: ConnectorsConfig {
                rest: RestConnectorConfig {
                    enabled: true,
                    openapi_discovery: true,
                },
                graphql: GraphQLConnectorConfig {
                    enabled: true,
                    introspection: true,
                },
                grpc: GrpcConnectorConfig {
                    enabled: true,
                    reflection: true,
                },
                database: DatabaseConnectorConfig {
                    enabled: true,
                    connection_pool_size: 20,
                },
            },
        }
    }
}
